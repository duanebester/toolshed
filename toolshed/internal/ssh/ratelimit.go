// Rate limiting and connection hardening for the ToolShed SSH server.
//
// This package implements TCP-level connection limiting that sits in front of
// the SSH server. It blocks abusive IPs before they even begin the SSH
// handshake, which is critical for a public-facing port 22 on Fly.io.
//
// Architecture (outermost to innermost):
//
//	TCP accept → PROXY protocol (Fly.io) → Rate limiter → SSH handshake
//
// The rate limiter tracks:
//   - Per-IP connection rate (sliding window)
//   - Per-IP concurrent connections
//   - Global concurrent connections
//   - Auto-ban for repeat offenders
package ssh

import (
	"log"
	"net"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// HardenConfig holds all SSH server hardening parameters.
// Use DefaultHardenConfig() for sensible defaults tuned for a public Fly.io
// deployment, then override individual fields as needed.
type HardenConfig struct {
	// Per-IP rate limiting — sliding window.
	PerIPRate  int           // Max new connections per IP per window (default: 20)
	RateWindow time.Duration // Window duration (default: 1 minute)

	// Connection limits.
	MaxPerIP int // Max concurrent connections per IP (default: 10)
	MaxTotal int // Max total concurrent connections (default: 200)

	// Auto-ban — temporarily block IPs that repeatedly hit limits.
	BanAfter    int           // Ban after N violations in a window (default: 5)
	BanDuration time.Duration // How long bans last (default: 15 minutes)

	// Session timeouts (applied to the SSH server).
	MaxSessionTime time.Duration // Max total session duration (default: 30 min)
	IdleTimeout    time.Duration // Idle timeout (default: 5 min)

	// Auth (applied to the SSH server).
	MaxAuthTries int // Max auth attempts per connection (default: 3)

	// PROXY protocol — required on Fly.io to see real client IPs.
	// When enabled, the listener parses PROXY protocol v1/v2 headers.
	// Without this, all connections appear to come from Fly's internal proxy
	// and per-IP rate limiting is ineffective.
	ProxyProtocol bool // Enable PROXY protocol parsing (default: false)
}

// DefaultHardenConfig returns a HardenConfig with production-ready defaults
// tuned for a public SSH server on Fly.io.
func DefaultHardenConfig() HardenConfig {
	return HardenConfig{
		PerIPRate:      20,
		RateWindow:     1 * time.Minute,
		MaxPerIP:       10,
		MaxTotal:       200,
		BanAfter:       5,
		BanDuration:    15 * time.Minute,
		MaxSessionTime: 30 * time.Minute,
		IdleTimeout:    5 * time.Minute,
		MaxAuthTries:   3,
		ProxyProtocol:  false,
	}
}

// ---------------------------------------------------------------------------
// ConnLimiter — the rate-limiting engine
// ---------------------------------------------------------------------------

// ConnLimiter tracks per-IP connection rates and enforces limits at the TCP
// level. It's designed to sit between the TCP listener and the SSH server so
// abusive connections are rejected before consuming SSH handshake resources.
//
// All methods are safe for concurrent use.
type ConnLimiter struct {
	mu sync.Mutex

	// Per-IP sliding window of connection timestamps.
	history map[string][]time.Time

	// Per-IP count of currently open connections.
	active map[string]int

	// Banned IPs with their expiry time.
	banned map[string]time.Time

	// Per-IP violation counter (reset when ban expires).
	violations map[string]int

	// Total active connections across all IPs.
	totalActive int

	cfg  HardenConfig
	done chan struct{}
}

// NewConnLimiter creates a connection limiter and starts a background
// goroutine that periodically cleans up stale state. Call Close() to stop it.
func NewConnLimiter(cfg HardenConfig) *ConnLimiter {
	cl := &ConnLimiter{
		history:    make(map[string][]time.Time),
		active:     make(map[string]int),
		banned:     make(map[string]time.Time),
		violations: make(map[string]int),
		cfg:        cfg,
		done:       make(chan struct{}),
	}
	go cl.cleanupLoop()
	return cl
}

// Allow checks whether a new connection from ip should be accepted.
// If accepted, it records the connection — the caller MUST call Release(ip)
// when the connection is closed.
func (cl *ConnLimiter) Allow(ip string) bool {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	now := time.Now()

	// ── Check ban list ──────────────────────────────────────────────────
	if expiry, ok := cl.banned[ip]; ok {
		if now.Before(expiry) {
			return false
		}
		// Ban expired — clear it.
		delete(cl.banned, ip)
		delete(cl.violations, ip)
	}

	// ── Check global concurrent limit ───────────────────────────────────
	if cl.totalActive >= cl.cfg.MaxTotal {
		log.Printf("ssh/hardening: global limit reached (%d/%d), rejecting %s",
			cl.totalActive, cl.cfg.MaxTotal, ip)
		return false
	}

	// ── Check per-IP concurrent limit ───────────────────────────────────
	if cl.active[ip] >= cl.cfg.MaxPerIP {
		cl.recordViolation(ip, now)
		log.Printf("ssh/hardening: per-IP concurrent limit for %s (%d/%d)",
			ip, cl.active[ip], cl.cfg.MaxPerIP)
		return false
	}

	// ── Check per-IP rate (sliding window) ──────────────────────────────
	cutoff := now.Add(-cl.cfg.RateWindow)
	hist := cl.history[ip]

	// Trim timestamps outside the window.
	trimmed := make([]time.Time, 0, len(hist))
	for _, t := range hist {
		if t.After(cutoff) {
			trimmed = append(trimmed, t)
		}
	}
	cl.history[ip] = trimmed

	if len(trimmed) >= cl.cfg.PerIPRate {
		cl.recordViolation(ip, now)
		log.Printf("ssh/hardening: rate limit for %s (%d connections in %v)",
			ip, len(trimmed), cl.cfg.RateWindow)
		return false
	}

	// ── Allowed — record the connection ─────────────────────────────────
	cl.history[ip] = append(cl.history[ip], now)
	cl.active[ip]++
	cl.totalActive++

	return true
}

// Release decrements the active connection count for ip.
// Must be called exactly once per successful Allow() call.
func (cl *ConnLimiter) Release(ip string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	if cl.active[ip] > 0 {
		cl.active[ip]--
	}
	if cl.totalActive > 0 {
		cl.totalActive--
	}
	if cl.active[ip] == 0 {
		delete(cl.active, ip)
	}
}

// Stats returns a snapshot of the limiter's current state.
func (cl *ConnLimiter) Stats() LimiterStats {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	now := time.Now()
	activeBans := 0
	for _, expiry := range cl.banned {
		if now.Before(expiry) {
			activeBans++
		}
	}

	return LimiterStats{
		TotalActive: cl.totalActive,
		TrackedIPs:  len(cl.history),
		ActiveBans:  activeBans,
	}
}

// LimiterStats is a point-in-time snapshot of the connection limiter.
type LimiterStats struct {
	TotalActive int // Currently open connections.
	TrackedIPs  int // IPs with recent connection history.
	ActiveBans  int // IPs currently banned.
}

// Close stops the background cleanup goroutine.
func (cl *ConnLimiter) Close() {
	close(cl.done)
}

// recordViolation increments the violation counter for ip and auto-bans
// if the threshold is exceeded. Caller must hold cl.mu.
func (cl *ConnLimiter) recordViolation(ip string, now time.Time) {
	cl.violations[ip]++
	if cl.violations[ip] >= cl.cfg.BanAfter {
		cl.banned[ip] = now.Add(cl.cfg.BanDuration)
		log.Printf("ssh/hardening: BANNED %s for %v (%d violations)",
			ip, cl.cfg.BanDuration, cl.violations[ip])
	}
}

// cleanupLoop periodically purges expired bans and stale connection history.
func (cl *ConnLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-cl.done:
			return
		case <-ticker.C:
			cl.cleanup()
		}
	}
}

func (cl *ConnLimiter) cleanup() {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	now := time.Now()

	// Purge expired bans.
	for ip, expiry := range cl.banned {
		if now.After(expiry) {
			delete(cl.banned, ip)
			delete(cl.violations, ip)
		}
	}

	// Purge stale rate-limit history.
	cutoff := now.Add(-cl.cfg.RateWindow)
	for ip, hist := range cl.history {
		trimmed := hist[:0]
		for _, t := range hist {
			if t.After(cutoff) {
				trimmed = append(trimmed, t)
			}
		}
		if len(trimmed) == 0 {
			delete(cl.history, ip)
		} else {
			cl.history[ip] = trimmed
		}
	}

	// Log periodic stats for observability.
	activeBans := 0
	for _, expiry := range cl.banned {
		if now.Before(expiry) {
			activeBans++
		}
	}
	log.Printf("ssh/hardening: cleanup — %d active conns, %d banned IPs, %d tracked IPs",
		cl.totalActive, activeBans, len(cl.history))
}

// ---------------------------------------------------------------------------
// RateLimitedListener — net.Listener wrapper
// ---------------------------------------------------------------------------

// RateLimitedListener wraps a net.Listener and silently drops connections
// from IPs that exceed the configured rate limits. Allowed connections are
// wrapped in trackedConn so the limiter is notified when they close.
type RateLimitedListener struct {
	net.Listener
	limiter *ConnLimiter
}

// NewRateLimitedListener wraps inner with rate limiting from limiter.
func NewRateLimitedListener(inner net.Listener, limiter *ConnLimiter) *RateLimitedListener {
	return &RateLimitedListener{
		Listener: inner,
		limiter:  limiter,
	}
}

// Accept waits for the next connection and checks it against the rate limiter.
// Rejected connections are closed immediately and Accept loops to the next one.
func (l *RateLimitedListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}

		// Set an initial deadline for the connection to complete the
		// SSH handshake. Without this, a slow-loris attacker can hold
		// a TCP slot open indefinitely. The SSH server will extend or
		// clear this deadline once the handshake succeeds.
		conn.SetDeadline(time.Now().Add(30 * time.Second))

		ip := extractIP(conn.RemoteAddr())
		if !l.limiter.Allow(ip) {
			// Close silently — don't waste resources on a response.
			conn.Close()
			continue
		}

		return &trackedConn{
			Conn:    conn,
			limiter: l.limiter,
			ip:      ip,
		}, nil
	}
}

// ---------------------------------------------------------------------------
// trackedConn — connection lifecycle tracking
// ---------------------------------------------------------------------------

// trackedConn wraps a net.Conn so the ConnLimiter is notified exactly once
// when the connection is closed, regardless of how many times Close() is
// called (e.g., by both the SSH library and our middleware).
type trackedConn struct {
	net.Conn
	limiter *ConnLimiter
	ip      string
	once    sync.Once
}

// Close releases the connection from the rate limiter and closes the
// underlying connection. Safe to call multiple times.
func (c *trackedConn) Close() error {
	c.once.Do(func() {
		c.limiter.Release(c.ip)
	})
	return c.Conn.Close()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// extractIP returns the IP address (without port) from a net.Addr.
func extractIP(addr net.Addr) string {
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		return tcpAddr.IP.String()
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}
