// Package ssh implements the ToolShed v2 SSH server using the Charm wish stack.
//
// The SSH server is the primary interface for ToolShed — agents connect via SSH,
// authenticate with their public key (which IS their identity), and issue commands
// that return YAML. Zero signup, zero tokens, zero OAuth.
//
//	ssh toolshed.sh search "fraud detection"
//	ssh toolshed.sh info acme.com/fraud-detection
//	ssh toolshed.sh register < toolshed.yaml
package ssh

import (
	"context"
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	gossh "golang.org/x/crypto/ssh"

	"github.com/toolshed/toolshed/internal/dolt"
	"github.com/toolshed/toolshed/internal/embeddings"
)

// Server wraps a wish SSH server with ToolShed command handling.
type Server struct {
	registry *dolt.Registry
	embedder embeddings.Embedder // nil = semantic search disabled
	srv      *ssh.Server
	port     int
}

// NewServer creates a new ToolShed SSH server. The hostKeyPath is where the
// server's host key is stored (generated automatically if missing). The port
// is the TCP port to listen on (default 2222).
func NewServer(registry *dolt.Registry, embedder embeddings.Embedder, hostKeyPath string, port int) (*Server, error) {
	if port <= 0 {
		port = 2222
	}

	s := &Server{
		registry: registry,
		embedder: embedder,
		port:     port,
	}

	addr := fmt.Sprintf(":%d", port)

	srv, err := wish.NewServer(
		wish.WithAddress(addr),
		wish.WithHostKeyPath(hostKeyPath),
		wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			// Accept all public keys — the key IS the identity.
			// We create/update the account record here so it's available
			// to command handlers via the session.
			fingerprint := gossh.FingerprintSHA256(key)
			keyType := key.Type()
			pubKeyStr := gossh.MarshalAuthorizedKey(key)

			_, err := registry.GetOrCreateAccount(
				ctx,
				fingerprint,
				keyType,
				string(pubKeyStr),
			)
			if err != nil {
				log.Printf("ssh: failed to upsert account for %s: %v", fingerprint, err)
				// Still accept the connection — don't lock users out because
				// of a transient DB issue. Commands will fail gracefully.
			}

			return true
		}),
		wish.WithMiddleware(
			// Command handler middleware — this is where ToolShed commands are dispatched.
			func(next ssh.Handler) ssh.Handler {
				return func(sess ssh.Session) {
					s.handleSession(sess)
					// Don't call next — we handle the full session lifecycle.
				}
			},
		),
	)
	if err != nil {
		return nil, fmt.Errorf("ssh: create server: %w", err)
	}

	s.srv = srv
	return s, nil
}

// Start begins listening for SSH connections. This blocks until the server
// is shut down or encounters a fatal error.
func (s *Server) Start() error {
	log.Printf("ssh: listening on :%d", s.port)
	return s.srv.ListenAndServe()
}

// Shutdown gracefully shuts down the SSH server, waiting for active sessions
// to complete or the context to expire.
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("ssh: shutting down...")
	return s.srv.Shutdown(ctx)
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
   ssh toolshed.sh register < toolshed.yaml   Register tools
   ssh toolshed.sh crawl acme.com             Crawl a domain

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
