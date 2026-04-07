package ssh

import (
	"net"
	"sync"
	"testing"
	"time"
)

// testConfig returns a HardenConfig with tight limits for fast tests.
func testConfig() HardenConfig {
	return HardenConfig{
		PerIPRate:      5,
		RateWindow:     1 * time.Second,
		MaxPerIP:       3,
		MaxTotal:       10,
		BanAfter:       3,
		BanDuration:    500 * time.Millisecond,
		MaxSessionTime: 30 * time.Minute,
		IdleTimeout:    5 * time.Minute,
		MaxAuthTries:   3,
		ProxyProtocol:  false,
	}
}

func TestConnLimiter_AllowBasic(t *testing.T) {
	cl := NewConnLimiter(testConfig())
	defer cl.Close()

	if !cl.Allow("1.2.3.4") {
		t.Fatal("first connection should be allowed")
	}

	stats := cl.Stats()
	if stats.TotalActive != 1 {
		t.Fatalf("expected 1 active, got %d", stats.TotalActive)
	}
}

func TestConnLimiter_ReleaseDecrementsActive(t *testing.T) {
	cl := NewConnLimiter(testConfig())
	defer cl.Close()

	ip := "10.0.0.1"
	cl.Allow(ip)
	cl.Allow(ip)

	stats := cl.Stats()
	if stats.TotalActive != 2 {
		t.Fatalf("expected 2 active, got %d", stats.TotalActive)
	}

	cl.Release(ip)
	stats = cl.Stats()
	if stats.TotalActive != 1 {
		t.Fatalf("expected 1 active after release, got %d", stats.TotalActive)
	}

	cl.Release(ip)
	stats = cl.Stats()
	if stats.TotalActive != 0 {
		t.Fatalf("expected 0 active after second release, got %d", stats.TotalActive)
	}
}

func TestConnLimiter_PerIPConcurrentLimit(t *testing.T) {
	cfg := testConfig()
	cfg.MaxPerIP = 2
	cl := NewConnLimiter(cfg)
	defer cl.Close()

	ip := "192.168.1.1"

	if !cl.Allow(ip) {
		t.Fatal("1st connection should be allowed")
	}
	if !cl.Allow(ip) {
		t.Fatal("2nd connection should be allowed")
	}
	if cl.Allow(ip) {
		t.Fatal("3rd connection should be rejected (per-IP concurrent limit)")
	}

	// Release one and try again.
	cl.Release(ip)
	if !cl.Allow(ip) {
		t.Fatal("should be allowed after release")
	}
}

func TestConnLimiter_GlobalConcurrentLimit(t *testing.T) {
	cfg := testConfig()
	cfg.MaxTotal = 3
	cfg.MaxPerIP = 10 // high so global limit triggers first
	cfg.PerIPRate = 100
	cl := NewConnLimiter(cfg)
	defer cl.Close()

	for i := 0; i < 3; i++ {
		if !cl.Allow("10.0.0.1") {
			t.Fatalf("connection %d should be allowed", i+1)
		}
	}

	// Different IP, but global limit reached.
	if cl.Allow("10.0.0.2") {
		t.Fatal("should be rejected by global limit")
	}

	cl.Release("10.0.0.1")
	if !cl.Allow("10.0.0.2") {
		t.Fatal("should be allowed after global slot freed")
	}
}

func TestConnLimiter_RateLimitSlidingWindow(t *testing.T) {
	cfg := testConfig()
	cfg.PerIPRate = 3
	cfg.RateWindow = 200 * time.Millisecond
	cfg.MaxPerIP = 100  // high so concurrent limit doesn't interfere
	cfg.MaxTotal = 1000 // high so global limit doesn't interfere
	cfg.BanAfter = 100  // high so we don't auto-ban during the test
	cl := NewConnLimiter(cfg)
	defer cl.Close()

	ip := "172.16.0.1"

	// Use up the rate limit (release each so concurrent limit doesn't block).
	for i := 0; i < 3; i++ {
		if !cl.Allow(ip) {
			t.Fatalf("connection %d should be allowed", i+1)
		}
		cl.Release(ip)
	}

	// 4th should be rate-limited.
	if cl.Allow(ip) {
		t.Fatal("should be rate-limited after 3 connections in window")
	}

	// Wait for the window to expire.
	time.Sleep(250 * time.Millisecond)

	// Should be allowed again.
	if !cl.Allow(ip) {
		t.Fatal("should be allowed after window expires")
	}
}

func TestConnLimiter_AutoBan(t *testing.T) {
	cfg := testConfig()
	cfg.MaxPerIP = 1
	cfg.BanAfter = 2
	cfg.BanDuration = 300 * time.Millisecond
	cl := NewConnLimiter(cfg)
	defer cl.Close()

	ip := "evil.bot"

	// First connection fills the per-IP concurrent slot.
	if !cl.Allow(ip) {
		t.Fatal("first connection should be allowed")
	}

	// Violation 1: per-IP concurrent limit.
	cl.Allow(ip)
	// Violation 2: triggers ban.
	cl.Allow(ip)

	stats := cl.Stats()
	if stats.ActiveBans != 1 {
		t.Fatalf("expected 1 ban, got %d", stats.ActiveBans)
	}

	// Even after releasing the concurrent connection, the IP is banned.
	cl.Release(ip)
	if cl.Allow(ip) {
		t.Fatal("banned IP should be rejected")
	}

	// Wait for ban to expire.
	time.Sleep(350 * time.Millisecond)

	if !cl.Allow(ip) {
		t.Fatal("should be allowed after ban expires")
	}

	stats = cl.Stats()
	if stats.ActiveBans != 0 {
		t.Fatalf("expected 0 bans after expiry, got %d", stats.ActiveBans)
	}
}

func TestConnLimiter_DifferentIPsAreIndependent(t *testing.T) {
	cfg := testConfig()
	cfg.MaxPerIP = 2
	cfg.PerIPRate = 2
	cl := NewConnLimiter(cfg)
	defer cl.Close()

	if !cl.Allow("1.1.1.1") {
		t.Fatal("IP A conn 1 should be allowed")
	}
	if !cl.Allow("1.1.1.1") {
		t.Fatal("IP A conn 2 should be allowed")
	}
	if cl.Allow("1.1.1.1") {
		t.Fatal("IP A conn 3 should be rejected")
	}

	// Different IP should still be allowed.
	if !cl.Allow("2.2.2.2") {
		t.Fatal("IP B conn 1 should be allowed")
	}
	if !cl.Allow("2.2.2.2") {
		t.Fatal("IP B conn 2 should be allowed")
	}
}

func TestConnLimiter_ConcurrentAccess(t *testing.T) {
	cfg := testConfig()
	cfg.MaxTotal = 100
	cfg.MaxPerIP = 100
	cfg.PerIPRate = 200
	cl := NewConnLimiter(cfg)
	defer cl.Close()

	var wg sync.WaitGroup
	allowed := make(chan bool, 200)

	// Hammer the limiter from 20 goroutines.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ip := "concurrent-test"
			for j := 0; j < 10; j++ {
				ok := cl.Allow(ip)
				allowed <- ok
				if ok {
					// Simulate some work, then release.
					time.Sleep(time.Millisecond)
					cl.Release(ip)
				}
			}
		}(i)
	}

	wg.Wait()
	close(allowed)

	total := 0
	accepted := 0
	for ok := range allowed {
		total++
		if ok {
			accepted++
		}
	}

	if total != 200 {
		t.Fatalf("expected 200 attempts, got %d", total)
	}
	if accepted == 0 {
		t.Fatal("expected at least some connections to be accepted")
	}

	// After all goroutines finish, active should be 0.
	stats := cl.Stats()
	if stats.TotalActive != 0 {
		t.Fatalf("expected 0 active after all released, got %d", stats.TotalActive)
	}
}

func TestConnLimiter_ReleaseIsIdempotent(t *testing.T) {
	cl := NewConnLimiter(testConfig())
	defer cl.Close()

	ip := "release-test"
	cl.Allow(ip)

	// Release multiple times — should not go negative.
	cl.Release(ip)
	cl.Release(ip)
	cl.Release(ip)

	stats := cl.Stats()
	if stats.TotalActive != 0 {
		t.Fatalf("expected 0 active, got %d", stats.TotalActive)
	}

	// Should still be able to allow new connections.
	if !cl.Allow(ip) {
		t.Fatal("should be allowed after releases")
	}
}

func TestConnLimiter_StatsSnapshot(t *testing.T) {
	cfg := testConfig()
	cfg.MaxPerIP = 1
	cfg.BanAfter = 1
	cfg.BanDuration = 10 * time.Second
	cl := NewConnLimiter(cfg)
	defer cl.Close()

	cl.Allow("a.a.a.a")
	cl.Allow("b.b.b.b")

	// Trigger a ban on c.c.c.c: fill the slot, then attempt again.
	cl.Allow("c.c.c.c")
	cl.Allow("c.c.c.c") // violation → ban (banAfter=1)

	stats := cl.Stats()
	if stats.TotalActive != 3 {
		t.Fatalf("expected 3 active, got %d", stats.TotalActive)
	}
	if stats.TrackedIPs < 3 {
		t.Fatalf("expected at least 3 tracked IPs, got %d", stats.TrackedIPs)
	}
	if stats.ActiveBans != 1 {
		t.Fatalf("expected 1 active ban, got %d", stats.ActiveBans)
	}
}

// ---------------------------------------------------------------------------
// RateLimitedListener tests
// ---------------------------------------------------------------------------

func TestRateLimitedListener_AcceptsAndRejects(t *testing.T) {
	cfg := testConfig()
	cfg.MaxPerIP = 1
	cfg.BanAfter = 100 // don't auto-ban in this test
	cl := NewConnLimiter(cfg)
	defer cl.Close()

	// Create a real TCP listener on a random port.
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer inner.Close()

	rl := NewRateLimitedListener(inner, cl)

	// Connect once — should be accepted.
	conn1, err := net.Dial("tcp", inner.Addr().String())
	if err != nil {
		t.Fatalf("dial 1 failed: %v", err)
	}
	defer conn1.Close()

	accepted1, err := rl.Accept()
	if err != nil {
		t.Fatalf("accept 1 failed: %v", err)
	}
	defer accepted1.Close()

	// Connect again — the per-IP concurrent limit (1) is hit, so the
	// RateLimitedListener should silently close this connection and
	// block on Accept() waiting for the next one.
	conn2, err := net.Dial("tcp", inner.Addr().String())
	if err != nil {
		t.Fatalf("dial 2 failed: %v", err)
	}
	defer conn2.Close()

	// Start Accept in a goroutine — it will pick up conn2, reject it
	// (per-IP limit hit), then block waiting for another connection.
	done := make(chan struct{})
	go func() {
		accepted3, err := rl.Accept()
		if err != nil {
			t.Errorf("accept 3 failed: %v", err)
			close(done)
			return
		}
		accepted3.Close()
		close(done)
	}()

	// Give Accept time to consume and reject conn2, then release the
	// slot by closing accepted1 so the next connection can succeed.
	time.Sleep(50 * time.Millisecond)
	accepted1.Close()

	// Now dial conn3 — the per-IP slot is free so Accept will return it.
	conn3, err := net.Dial("tcp", inner.Addr().String())
	if err != nil {
		t.Fatalf("dial 3 failed: %v", err)
	}
	defer conn3.Close()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for accept after release")
	}
}

func TestTrackedConn_CloseReleasesOnce(t *testing.T) {
	cl := NewConnLimiter(testConfig())
	defer cl.Close()

	ip := "tracked-test"
	cl.Allow(ip)

	stats := cl.Stats()
	if stats.TotalActive != 1 {
		t.Fatalf("expected 1 active, got %d", stats.TotalActive)
	}

	server, client := net.Pipe()
	defer client.Close()

	tc := &trackedConn{
		Conn:    server,
		limiter: cl,
		ip:      ip,
	}

	// Close multiple times — Release should only fire once.
	tc.Close()
	tc.Close()
	tc.Close()

	stats = cl.Stats()
	if stats.TotalActive != 0 {
		t.Fatalf("expected 0 active after close, got %d", stats.TotalActive)
	}
}

// ---------------------------------------------------------------------------
// extractIP tests
// ---------------------------------------------------------------------------

func TestExtractIP_TCPAddr(t *testing.T) {
	addr := &net.TCPAddr{IP: net.ParseIP("203.0.113.42"), Port: 54321}
	got := extractIP(addr)
	if got != "203.0.113.42" {
		t.Fatalf("expected 203.0.113.42, got %s", got)
	}
}

func TestExtractIP_IPv6(t *testing.T) {
	addr := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 22}
	got := extractIP(addr)
	if got != "::1" {
		t.Fatalf("expected ::1, got %s", got)
	}
}

func TestExtractIP_StringAddr(t *testing.T) {
	// Simulate a non-TCP addr that still has host:port format.
	addr := mockAddr("198.51.100.7:9999")
	got := extractIP(addr)
	if got != "198.51.100.7" {
		t.Fatalf("expected 198.51.100.7, got %s", got)
	}
}

// mockAddr implements net.Addr for testing extractIP with non-TCP addresses.
type mockAddr string

func (a mockAddr) Network() string { return "mock" }
func (a mockAddr) String() string  { return string(a) }

// ---------------------------------------------------------------------------
// Cleanup integration test
// ---------------------------------------------------------------------------

func TestConnLimiter_CleanupPurgesExpiredBans(t *testing.T) {
	cfg := testConfig()
	cfg.MaxPerIP = 1
	cfg.BanAfter = 1
	cfg.BanDuration = 50 * time.Millisecond
	cl := NewConnLimiter(cfg)
	defer cl.Close()

	ip := "cleanup-test"

	cl.Allow(ip)
	cl.Allow(ip) // violation → immediate ban

	stats := cl.Stats()
	if stats.ActiveBans != 1 {
		t.Fatalf("expected 1 ban, got %d", stats.ActiveBans)
	}

	// Wait for the ban to expire.
	time.Sleep(100 * time.Millisecond)

	// Run cleanup manually (the background goroutine runs every 5 min).
	cl.cleanup()

	stats = cl.Stats()
	if stats.ActiveBans != 0 {
		t.Fatalf("expected 0 bans after cleanup, got %d", stats.ActiveBans)
	}
}

func TestConnLimiter_CleanupPurgesStaleHistory(t *testing.T) {
	cfg := testConfig()
	cfg.RateWindow = 50 * time.Millisecond
	cl := NewConnLimiter(cfg)
	defer cl.Close()

	cl.Allow("stale-1")
	cl.Release("stale-1")
	cl.Allow("stale-2")
	cl.Release("stale-2")

	stats := cl.Stats()
	if stats.TrackedIPs < 2 {
		t.Fatalf("expected at least 2 tracked IPs, got %d", stats.TrackedIPs)
	}

	// Wait for the history to go stale.
	time.Sleep(100 * time.Millisecond)
	cl.cleanup()

	stats = cl.Stats()
	if stats.TrackedIPs != 0 {
		t.Fatalf("expected 0 tracked IPs after cleanup, got %d", stats.TrackedIPs)
	}
}
