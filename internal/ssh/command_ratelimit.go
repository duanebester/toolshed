// Per-fingerprint, per-command rate limiting for the ToolShed SSH server.
//
// This complements the TCP-level ConnLimiter (ratelimit.go) by operating at
// the application/command layer. Once an SSH session is established and a
// user's key fingerprint is known, this limiter constrains how often each
// fingerprint can invoke specific commands (e.g. "report", "upvote").
//
// Architecture (innermost layer):
//
//	TCP accept → PROXY protocol → ConnLimiter → SSH handshake → CommandRateLimiter
//
// Commands not listed in the config are never rate-limited.
package ssh

import (
	"log"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// CommandLimitConfig defines rate limits for specific SSH commands.
// Commands not listed here are not rate-limited.
type CommandLimitConfig struct {
	// Per-command limits: command name → max calls per window.
	Limits map[string]int
	// Window duration for all command limits.
	Window time.Duration
}

// DefaultCommandLimitConfig returns production defaults.
func DefaultCommandLimitConfig() CommandLimitConfig {
	return CommandLimitConfig{
		Limits: map[string]int{
			"report": 10, // max 10 reports per key per window
			"upvote": 10, // max 10 upvotes per key per window
		},
		Window: 1 * time.Minute,
	}
}

// ---------------------------------------------------------------------------
// CommandRateLimiter — per-fingerprint, per-command rate limiting engine
// ---------------------------------------------------------------------------

// CommandRateLimiter tracks per-fingerprint, per-command call rates using a
// sliding window. All methods are safe for concurrent use.
type CommandRateLimiter struct {
	mu sync.Mutex

	// history maps "fingerprint:command" → list of timestamps within the
	// current sliding window. Entries are lazily trimmed on access and
	// periodically purged by the background cleanup goroutine.
	history map[string][]time.Time

	cfg       CommandLimitConfig
	closeOnce sync.Once
	done      chan struct{}
}

// NewCommandRateLimiter creates a command rate limiter and starts a background
// goroutine that periodically cleans up stale state. Call Close() to stop it.
func NewCommandRateLimiter(cfg CommandLimitConfig) *CommandRateLimiter {
	cl := &CommandRateLimiter{
		history: make(map[string][]time.Time),
		cfg:     cfg,
		done:    make(chan struct{}),
	}
	go cl.cleanupLoop()
	return cl
}

// Allow checks whether fingerprint is allowed to invoke command right now.
// Returns true if the call is permitted (and records it), false if the
// fingerprint has exceeded the rate limit for that command.
//
// Commands that are not present in the config are always allowed.
func (cl *CommandRateLimiter) Allow(fingerprint, command string) bool {
	limit, ok := cl.cfg.Limits[command]
	if !ok {
		// Command is not rate-limited.
		return true
	}

	key := fingerprint + ":" + command

	cl.mu.Lock()
	defer cl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-cl.cfg.Window)

	// Trim timestamps outside the current window.
	hist := cl.history[key]
	trimmed := hist[:0]
	for _, t := range hist {
		if t.After(cutoff) {
			trimmed = append(trimmed, t)
		}
	}

	// Check if the fingerprint has hit the limit for this command.
	if len(trimmed) >= limit {
		cl.history[key] = trimmed
		log.Printf("ssh/command-ratelimit: %s rate-limited for command %q (%d/%d in %v)",
			fingerprint, command, len(trimmed), limit, cl.cfg.Window)
		return false
	}

	// Under the limit — record this call.
	cl.history[key] = append(trimmed, now)
	return true
}

// Close stops the background cleanup goroutine.
func (cl *CommandRateLimiter) Close() {
	cl.closeOnce.Do(func() { close(cl.done) })
}

// ---------------------------------------------------------------------------
// Background cleanup
// ---------------------------------------------------------------------------

// cleanupLoop periodically purges stale history entries so memory doesn't
// grow unbounded for fingerprints that connected once and never returned.
func (cl *CommandRateLimiter) cleanupLoop() {
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

func (cl *CommandRateLimiter) cleanup() {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-cl.cfg.Window)

	for key, hist := range cl.history {
		trimmed := hist[:0]
		for _, t := range hist {
			if t.After(cutoff) {
				trimmed = append(trimmed, t)
			}
		}
		if len(trimmed) == 0 {
			delete(cl.history, key)
		} else {
			cl.history[key] = trimmed
		}
	}

	log.Printf("ssh/command-ratelimit: cleanup — %d tracked keys", len(cl.history))
}
