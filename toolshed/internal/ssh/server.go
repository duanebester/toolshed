// Package ssh implements the ToolShed v2 SSH server using the Charm wish stack.
//
// The SSH server is the primary interface for ToolShed — agents connect via SSH,
// authenticate with their public key (which IS their identity), and issue commands
// that return YAML. Zero signup, zero tokens, zero OAuth.
//
//	ssh toolshed.sh search "fraud detection"
//	ssh toolshed.sh info acme.com/fraud-detection
package ssh

import (
	"context"
	"crypto/rsa"
	"fmt"
	"log"
	"net"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	proxyproto "github.com/pires/go-proxyproto"
	gossh "golang.org/x/crypto/ssh"

	"github.com/toolshed/toolshed/internal/dolt"
	"github.com/toolshed/toolshed/internal/embeddings"
)

// Server wraps a wish SSH server with ToolShed command handling and
// production-grade connection hardening (rate limiting, auto-ban, timeouts).
type Server struct {
	registry   *dolt.Registry
	embedder   embeddings.Embedder // nil = semantic search disabled
	srv        *ssh.Server
	port       int
	limiter    *ConnLimiter
	cmdLimiter *CommandRateLimiter // per-fingerprint command rate limiting
	cfg        HardenConfig
}

// NewServer creates a new ToolShed SSH server with connection hardening.
//
// The hostKeyPath is where the server's host key is stored (generated
// automatically if missing). The port is the TCP port to listen on
// (default 2222). The hardenCfg controls rate limiting, timeouts, and
// auth restrictions — use DefaultHardenConfig() for production defaults.
func NewServer(registry *dolt.Registry, embedder embeddings.Embedder, hostKeyPath string, port int, hardenCfg HardenConfig) (*Server, error) {
	if port <= 0 {
		port = 2222
	}

	limiter := NewConnLimiter(hardenCfg)
	cmdLimiter := NewCommandRateLimiter(DefaultCommandLimitConfig())

	s := &Server{
		registry:   registry,
		embedder:   embedder,
		port:       port,
		limiter:    limiter,
		cmdLimiter: cmdLimiter,
		cfg:        hardenCfg,
	}

	addr := fmt.Sprintf(":%d", port)

	srv, err := wish.NewServer(
		wish.WithAddress(addr),
		wish.WithHostKeyPath(hostKeyPath),

		// ── Timeouts ────────────────────────────────────────────────
		// MaxTimeout caps the total lifetime of any SSH session.
		// IdleTimeout closes sessions that stop sending data.
		// Both are critical on a public server to prevent resource
		// exhaustion from abandoned or malicious connections.
		wish.WithMaxTimeout(hardenCfg.MaxSessionTime),
		wish.WithIdleTimeout(hardenCfg.IdleTimeout),

		// ── Public key auth ─────────────────────────────────────────
		// Accept all public keys — the key IS the identity.
		// We create/update the account record so it's available
		// to command handlers via the session.
		wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			fingerprint := gossh.FingerprintSHA256(key)
			keyType := key.Type()

			// Reject weak key types — DSA is broken and short RSA keys
			// are trivially factorable. This costs nothing and blocks a
			// class of impersonation attacks.
			if keyType == "ssh-dss" {
				log.Printf("ssh/hardening: rejected DSA key from %s", ctx.RemoteAddr())
				return false
			}
			if keyType == "ssh-rsa" {
				// Parse the RSA public key to check its bit length.
				// Keys under 2048 bits are considered insecure.
				parsed, err := gossh.ParsePublicKey(key.Marshal())
				if err == nil {
					if rsaKey, ok := parsed.(gossh.CryptoPublicKey); ok {
						if pub, ok := rsaKey.CryptoPublicKey().(*rsa.PublicKey); ok {
							if pub.N.BitLen() < 2048 {
								log.Printf("ssh/hardening: rejected weak RSA key (%d bits) from %s",
									pub.N.BitLen(), ctx.RemoteAddr())
								return false
							}
						}
					}
				}
			}

			pubKeyStr := gossh.MarshalAuthorizedKey(key)

			_, err := registry.GetOrCreateAccount(
				ctx,
				fingerprint,
				keyType,
				string(pubKeyStr),
			)
			if err != nil {
				log.Printf("ssh: failed to upsert account for %s: %v", fingerprint, err)
				// Still accept — don't lock users out because of a
				// transient DB issue. Commands will fail gracefully.
			}

			return true
		}),

		// ── Password auth — DISABLED ────────────────────────────────
		// Explicitly reject all password auth. This is the #1 attack
		// vector on port 22 — bots try username/password combos
		// endlessly. By returning false here the SSH library sends
		// an auth-failure response and the bot moves on.
		wish.WithPasswordAuth(func(ctx ssh.Context, password string) bool {
			log.Printf("ssh/hardening: rejected password auth from %s (user %q)",
				ctx.RemoteAddr(), ctx.User())
			return false
		}),

		// ── Keyboard-interactive auth — DISABLED ────────────────────
		// Another auth method bots sometimes probe. Reject it.
		wish.WithKeyboardInteractiveAuth(func(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) bool {
			log.Printf("ssh/hardening: rejected keyboard-interactive auth from %s",
				ctx.RemoteAddr())
			return false
		}),

		// ── Session middleware ───────────────────────────────────────
		wish.WithMiddleware(
			func(next ssh.Handler) ssh.Handler {
				return func(sess ssh.Session) {
					s.handleSession(sess)
				}
			},
		),
	)
	if err != nil {
		limiter.Close()
		return nil, fmt.Errorf("ssh: create server: %w", err)
	}

	// ── MaxAuthTries ────────────────────────────────────────────────────
	// Limit the number of authentication attempts per connection.
	// Without this, a single TCP connection can try thousands of keys
	// or passwords. The default of 3 is standard for hardened sshd.
	srv.ServerConfigCallback = func(ctx ssh.Context) *gossh.ServerConfig {
		cfg := &gossh.ServerConfig{
			MaxAuthTries: hardenCfg.MaxAuthTries,
		}
		// Only allow strong, modern ciphers. This drops legacy options
		// like arcfour, 3des-cbc, and aes*-cbc that Go's crypto/ssh
		// would otherwise negotiate.
		cfg.Ciphers = []string{
			"chacha20-poly1305@openssh.com",
			"aes256-gcm@openssh.com",
			"aes128-gcm@openssh.com",
			"aes256-ctr",
			"aes192-ctr",
			"aes128-ctr",
		}
		// Only allow modern key exchanges. Drops diffie-hellman-group14-sha1
		// and diffie-hellman-group1-sha1 which use SHA-1.
		cfg.KeyExchanges = []string{
			"curve25519-sha256",
			"curve25519-sha256@libssh.org",
			"ecdh-sha2-nistp256",
			"ecdh-sha2-nistp384",
			"ecdh-sha2-nistp521",
			"diffie-hellman-group14-sha256",
		}
		// Only allow strong MACs (ETM variants preferred).
		// Drops hmac-sha1 and hmac-sha1-96.
		cfg.MACs = []string{
			"hmac-sha2-256-etm@openssh.com",
			"hmac-sha2-512-etm@openssh.com",
			"hmac-sha2-256",
			"hmac-sha2-512",
		}
		return cfg
	}

	// ── Server version string ───────────────────────────────────────────
	// Don't advertise the exact implementation. A generic version string
	// makes fingerprinting harder for targeted attacks.
	srv.Version = "ToolShed"

	s.srv = srv
	return s, nil
}

// Start begins listening for SSH connections. It sets up a layered listener
// stack (TCP → PROXY protocol → rate limiter → SSH) and blocks until the
// server is shut down or encounters a fatal error.
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.port)

	// ── Layer 0: Raw TCP listener ───────────────────────────────────────
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("ssh: listen on %s: %w", addr, err)
	}

	// ── Layer 1: PROXY protocol (optional, for Fly.io) ──────────────────
	// Fly.io's TCP proxy prepends a PROXY protocol header containing the
	// real client IP. Without parsing this, RemoteAddr() returns the Fly
	// proxy's internal IP and per-IP rate limiting is ineffective.
	//
	// Enable with TOOLSHED_PROXY_PROTOCOL=true and add to fly.toml:
	//   [services.proxy_proto_options]
	//     version = "v2"
	if s.cfg.ProxyProtocol {
		// ReadHeaderTimeout is deliberately short (10s) rather than
		// using the session IdleTimeout (5m). A legitimate Fly.io proxy
		// sends the PROXY header immediately. A long timeout here would
		// let attackers hold TCP slots without completing the handshake.
		ln = &proxyproto.Listener{
			Listener:          ln,
			ReadHeaderTimeout: 10 * time.Second,
		}
		log.Println("ssh: PROXY protocol enabled — real client IPs will be visible")
	} else {
		log.Println("ssh: PROXY protocol disabled — rate limiting uses proxy IPs")
	}

	// ── Layer 2: Rate limiter ───────────────────────────────────────────
	// Silently drops connections from IPs that exceed rate limits.
	// This happens BEFORE the SSH handshake, so abusive bots consume
	// almost zero server resources.
	ln = NewRateLimitedListener(ln, s.limiter)

	log.Printf("ssh: listening on %s", addr)
	log.Printf("ssh: hardening — max %d total, %d/IP concurrent, %d/IP per %v, ban after %d violations for %v",
		s.cfg.MaxTotal, s.cfg.MaxPerIP, s.cfg.PerIPRate, s.cfg.RateWindow,
		s.cfg.BanAfter, s.cfg.BanDuration)
	log.Printf("ssh: timeouts — idle %v, max session %v, max auth tries %d",
		s.cfg.IdleTimeout, s.cfg.MaxSessionTime, s.cfg.MaxAuthTries)

	// ── Layer 3: SSH server ─────────────────────────────────────────────
	// Serve() uses our wrapped listener instead of creating its own,
	// which is why we call it instead of ListenAndServe().
	return s.srv.Serve(ln)
}

// Shutdown gracefully shuts down the SSH server, waiting for active sessions
// to complete or the context to expire, then cleans up the rate limiter.
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("ssh: shutting down...")

	// Stop accepting new connections and drain active sessions.
	err := s.srv.Shutdown(ctx)

	// Stop the rate limiters' background cleanup goroutines.
	s.limiter.Close()
	s.cmdLimiter.Close()

	// Log final stats.
	stats := s.limiter.Stats()
	log.Printf("ssh: shutdown complete — %d active conns, %d banned IPs at exit",
		stats.TotalActive, stats.ActiveBans)

	return err
}

// handleSession dispatches an SSH session to the appropriate command handler.
// If the session has no command (interactive mode), it launches the TUI.
// Otherwise it parses and executes the command.
func (s *Server) handleSession(sess ssh.Session) {
	cmd := sess.Command()

	// No command → interactive mode (bubbletea TUI).
	if len(cmd) == 0 {
		s.handleInteractive(sess)
		return
	}

	// Ensure the user connected with a public key.
	if sess.PublicKey() == nil {
		fmt.Fprintf(sess.Stderr(), "error: public key authentication required\n")
		fmt.Fprintf(sess.Stderr(), "hint: connect with ssh -i <key> toolshed.sh <command>\n")
		return
	}

	fingerprint := gossh.FingerprintSHA256(sess.PublicKey())

	// Dispatch to command handlers.
	dispatcher := &CommandDispatcher{
		registry:    s.registry,
		embedder:    s.embedder,
		fingerprint: fingerprint,
		cmdLimiter:  s.cmdLimiter,
	}

	dispatcher.Dispatch(sess, cmd)
}

// handleInteractive handles sessions with no command — interactive/TUI mode.
// If the session has a PTY, it launches the bubbletea TUI. Otherwise it falls
// back to a static welcome banner (e.g. when piped or used non-interactively).
func (s *Server) handleInteractive(sess ssh.Session) {
	fingerprint := ""
	if sess.PublicKey() != nil {
		fingerprint = gossh.FingerprintSHA256(sess.PublicKey())
	}

	pty, winChanges, isPty := sess.Pty()
	if !isPty {
		// No PTY — print a static banner and exit. This handles cases
		// like `ssh toolshed.sh | cat` or automated probes.
		s.handleInteractiveFallback(sess, fingerprint)
		return
	}

	m := NewTUIModel(s.registry, fingerprint, pty.Window.Width, pty.Window.Height)

	p := tea.NewProgram(m,
		tea.WithInput(sess),
		tea.WithOutput(sess),
		tea.WithAltScreen(),
	)

	// Forward terminal resize events to bubbletea.
	go func() {
		for win := range winChanges {
			if p != nil {
				p.Send(tea.WindowSizeMsg{Width: win.Width, Height: win.Height})
			}
		}
	}()

	if _, err := p.Run(); err != nil {
		log.Printf("ssh: TUI error for %s: %v", fingerprint, err)
	}
}

// handleInteractiveFallback prints a static welcome banner for non-PTY sessions.
func (s *Server) handleInteractiveFallback(sess ssh.Session, fingerprint string) {
	welcome := `
 🔧 ToolShed — the SSH-native tool registry for AI agents

 Usage:
   ssh toolshed.sh help                       Show all commands (YAML)
   ssh toolshed.sh search "fraud detection"   Search for tools
   ssh toolshed.sh info acme.com/fraud-tool   Get tool details
   ssh toolshed.sh crawl acme.com             Index tools from a domain

 Interactive TUI:
   ssh -t toolshed.sh                         Launch the TUI browser

 Your identity is your SSH key. No signup required.

`
	if fingerprint != "" {
		welcome += fmt.Sprintf(" Connected as: %s\n\n", fingerprint)
	} else {
		welcome += " ⚠ No public key detected. Commands require key-based auth.\n\n"
	}

	fmt.Fprint(sess, welcome)
}
