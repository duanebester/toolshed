package ssh

import (
	"testing"
	"time"
)

// testCommandConfig returns a CommandLimitConfig with tight limits for fast tests.
func testCommandConfig() CommandLimitConfig {
	return CommandLimitConfig{
		Limits: map[string]int{
			"report": 3,
			"upvote": 3,
		},
		Window: 200 * time.Millisecond,
	}
}

func TestCommandRateLimiter_AllowBasic(t *testing.T) {
	cl := NewCommandRateLimiter(testCommandConfig())
	defer cl.Close()

	if !cl.Allow("SHA256:abc123", "report") {
		t.Fatal("first call should be allowed")
	}
}

func TestCommandRateLimiter_RejectsOverLimit(t *testing.T) {
	cfg := testCommandConfig()
	cfg.Limits["report"] = 3
	cl := NewCommandRateLimiter(cfg)
	defer cl.Close()

	fp := "SHA256:overlimit"

	for i := 0; i < 3; i++ {
		if !cl.Allow(fp, "report") {
			t.Fatalf("call %d should be allowed", i+1)
		}
	}

	// 4th call should be rejected.
	if cl.Allow(fp, "report") {
		t.Fatal("call exceeding limit should be rejected")
	}

	// 5th call should also be rejected.
	if cl.Allow(fp, "report") {
		t.Fatal("subsequent call over limit should still be rejected")
	}
}

func TestCommandRateLimiter_WindowExpiry(t *testing.T) {
	cfg := testCommandConfig()
	cfg.Limits["report"] = 2
	cfg.Window = 100 * time.Millisecond
	cl := NewCommandRateLimiter(cfg)
	defer cl.Close()

	fp := "SHA256:expiry"

	// Exhaust the limit.
	for i := 0; i < 2; i++ {
		if !cl.Allow(fp, "report") {
			t.Fatalf("call %d should be allowed", i+1)
		}
	}

	if cl.Allow(fp, "report") {
		t.Fatal("should be rejected at limit")
	}

	// Wait for the window to expire.
	time.Sleep(150 * time.Millisecond)

	// Should be allowed again.
	if !cl.Allow(fp, "report") {
		t.Fatal("should be allowed after window expires")
	}
}

func TestCommandRateLimiter_UnlimitedCommands(t *testing.T) {
	cl := NewCommandRateLimiter(testCommandConfig())
	defer cl.Close()

	fp := "SHA256:unlimited"

	// "ls" is not in the config — should always be allowed.
	for i := 0; i < 100; i++ {
		if !cl.Allow(fp, "ls") {
			t.Fatalf("unlimited command should always be allowed, rejected at call %d", i+1)
		}
	}
}

func TestCommandRateLimiter_DifferentFingerprintsIndependent(t *testing.T) {
	cfg := testCommandConfig()
	cfg.Limits["report"] = 2
	cl := NewCommandRateLimiter(cfg)
	defer cl.Close()

	fpA := "SHA256:fingerprint-A"
	fpB := "SHA256:fingerprint-B"

	// Exhaust the limit for fingerprint A.
	for i := 0; i < 2; i++ {
		if !cl.Allow(fpA, "report") {
			t.Fatalf("fpA call %d should be allowed", i+1)
		}
	}

	if cl.Allow(fpA, "report") {
		t.Fatal("fpA should be rejected at limit")
	}

	// Fingerprint B should be completely independent.
	for i := 0; i < 2; i++ {
		if !cl.Allow(fpB, "report") {
			t.Fatalf("fpB call %d should be allowed (independent of fpA)", i+1)
		}
	}

	if cl.Allow(fpB, "report") {
		t.Fatal("fpB should be rejected at its own limit")
	}
}

func TestCommandRateLimiter_DifferentCommandsIndependent(t *testing.T) {
	cfg := testCommandConfig()
	cfg.Limits["report"] = 2
	cfg.Limits["upvote"] = 2
	cl := NewCommandRateLimiter(cfg)
	defer cl.Close()

	fp := "SHA256:multicmd"

	// Exhaust the limit for "report".
	for i := 0; i < 2; i++ {
		if !cl.Allow(fp, "report") {
			t.Fatalf("report call %d should be allowed", i+1)
		}
	}

	if cl.Allow(fp, "report") {
		t.Fatal("report should be rejected at limit")
	}

	// "upvote" should be independent and still allowed.
	for i := 0; i < 2; i++ {
		if !cl.Allow(fp, "upvote") {
			t.Fatalf("upvote call %d should be allowed (independent of report)", i+1)
		}
	}

	if cl.Allow(fp, "upvote") {
		t.Fatal("upvote should be rejected at its own limit")
	}
}

func TestCommandRateLimiter_Cleanup(t *testing.T) {
	cfg := testCommandConfig()
	cfg.Window = 50 * time.Millisecond
	cfg.Limits["report"] = 5
	cl := NewCommandRateLimiter(cfg)
	defer cl.Close()

	// Create history entries for several fingerprints.
	cl.Allow("SHA256:cleanup-1", "report")
	cl.Allow("SHA256:cleanup-2", "report")
	cl.Allow("SHA256:cleanup-3", "upvote")

	// Verify there are tracked keys.
	cl.mu.Lock()
	tracked := len(cl.history)
	cl.mu.Unlock()

	if tracked != 3 {
		t.Fatalf("expected 3 tracked keys before cleanup, got %d", tracked)
	}

	// Wait for all entries to go stale.
	time.Sleep(100 * time.Millisecond)

	// Run cleanup manually (the background goroutine runs every 5 min).
	cl.cleanup()

	cl.mu.Lock()
	tracked = len(cl.history)
	cl.mu.Unlock()

	if tracked != 0 {
		t.Fatalf("expected 0 tracked keys after cleanup, got %d", tracked)
	}
}

func TestCommandRateLimiter_CleanupPreservesActiveEntries(t *testing.T) {
	cfg := testCommandConfig()
	cfg.Window = 200 * time.Millisecond
	cfg.Limits["report"] = 5
	cl := NewCommandRateLimiter(cfg)
	defer cl.Close()

	// Create a stale entry.
	cl.Allow("SHA256:stale", "report")

	// Wait for the stale entry to expire, then add a fresh entry.
	time.Sleep(250 * time.Millisecond)
	cl.Allow("SHA256:fresh", "report")

	// Run cleanup — should purge stale but keep fresh.
	cl.cleanup()

	cl.mu.Lock()
	tracked := len(cl.history)
	_, hasFresh := cl.history["SHA256:fresh:report"]
	_, hasStale := cl.history["SHA256:stale:report"]
	cl.mu.Unlock()

	if tracked != 1 {
		t.Fatalf("expected 1 tracked key after cleanup, got %d", tracked)
	}
	if !hasFresh {
		t.Fatal("fresh entry should be preserved after cleanup")
	}
	if hasStale {
		t.Fatal("stale entry should be purged after cleanup")
	}
}
