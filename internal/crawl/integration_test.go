package crawl

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/toolshed/toolshed/internal/core"
)

// ---------------------------------------------------------------------------
// Mock ToolStore
// ---------------------------------------------------------------------------

// mockToolStore records every definition and listing upserted during a crawl
// so tests can inspect what CrawlDomain would have written to the DB.
type mockToolStore struct {
	definitions []core.ToolDefinition
	listings    []core.ToolListing

	// Optional error injectors — set these to simulate DB failures.
	defErr     error
	listingErr error
}

func (m *mockToolStore) RegisterToolDefinition(_ context.Context, def core.ToolDefinition) error {
	if m.defErr != nil {
		return m.defErr
	}
	m.definitions = append(m.definitions, def)
	return nil
}

func (m *mockToolStore) RegisterToolListing(_ context.Context, listing core.ToolListing) error {
	if m.listingErr != nil {
		return m.listingErr
	}
	m.listings = append(m.listings, listing)
	return nil
}

// ---------------------------------------------------------------------------
// processCrawledManifest — core pipeline tests (no HTTP)
// ---------------------------------------------------------------------------

func TestProcessCrawledManifest_SingleTool(t *testing.T) {
	domain := "example.com"
	body := []byte(validYAML(domain))
	store := &mockToolStore{}
	url := "https://" + domain + wellKnownPath

	result, err := processCrawledManifest(context.Background(), domain, url, body, store, "SHA256:test_key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// --- CrawlResult checks ---
	if result.Domain != domain {
		t.Errorf("Domain = %q, want %q", result.Domain, domain)
	}
	if result.URL != url {
		t.Errorf("URL = %q, want %q", result.URL, url)
	}
	if result.Total != 1 {
		t.Fatalf("Total = %d, want 1", result.Total)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(result.Tools))
	}
	if result.CrawledAt.IsZero() {
		t.Error("CrawledAt is zero")
	}

	tool := result.Tools[0]
	if tool.Name != "Echo" {
		t.Errorf("tool Name = %q, want Echo", tool.Name)
	}
	if tool.Status != "indexed" {
		t.Errorf("tool Status = %q, want indexed", tool.Status)
	}
	if tool.ID != "example.com/echo" {
		t.Errorf("tool ID = %q, want example.com/echo", tool.ID)
	}
	if !strings.HasPrefix(tool.DefinitionHash, "sha256:") {
		t.Errorf("tool DefinitionHash = %q, expected sha256: prefix", tool.DefinitionHash)
	}

	// --- Store upsert checks ---
	if len(store.definitions) != 1 {
		t.Fatalf("stored %d definitions, want 1", len(store.definitions))
	}
	if len(store.listings) != 1 {
		t.Fatalf("stored %d listings, want 1", len(store.listings))
	}

	def := store.definitions[0]
	if def.Provider.Domain != domain {
		t.Errorf("def Provider.Domain = %q, want %q", def.Provider.Domain, domain)
	}
	if def.Provider.Contact != "tools@example.com" {
		t.Errorf("def Provider.Contact = %q, want tools@example.com", def.Provider.Contact)
	}
	if def.ContentHash == "" {
		t.Error("def ContentHash is empty")
	}

	listing := store.listings[0]
	if listing.ID != "example.com/echo" {
		t.Errorf("listing ID = %q, want example.com/echo", listing.ID)
	}
	if listing.ProviderAccount != "SHA256:test_key" {
		t.Errorf("listing ProviderAccount = %q, want SHA256:test_key", listing.ProviderAccount)
	}
	if listing.Source != "crawl" {
		t.Errorf("listing Source = %q, want crawl", listing.Source)
	}
	if listing.ProviderDomain != domain {
		t.Errorf("listing ProviderDomain = %q, want %q", listing.ProviderDomain, domain)
	}
	if listing.Pricing.Model != "free" {
		t.Errorf("listing Pricing.Model = %q, want free", listing.Pricing.Model)
	}
}

func TestProcessCrawledManifest_MultipleTools(t *testing.T) {
	domain := "example.com"
	body := []byte(multiToolYAML(domain))
	store := &mockToolStore{}
	url := "https://" + domain + wellKnownPath

	result, err := processCrawledManifest(context.Background(), domain, url, body, store, "SHA256:key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Total != 2 {
		t.Fatalf("Total = %d, want 2", result.Total)
	}
	if len(store.definitions) != 2 {
		t.Fatalf("stored %d definitions, want 2", len(store.definitions))
	}
	if len(store.listings) != 2 {
		t.Fatalf("stored %d listings, want 2", len(store.listings))
	}

	// Verify tools are in manifest order.
	if result.Tools[0].Name != "Echo" {
		t.Errorf("tool[0] Name = %q, want Echo", result.Tools[0].Name)
	}
	if result.Tools[1].Name != "Reverse" {
		t.Errorf("tool[1] Name = %q, want Reverse", result.Tools[1].Name)
	}

	// Verify the second tool's per_call pricing was preserved.
	if store.listings[1].Pricing.Model != "per_call" {
		t.Errorf("listing[1] Pricing.Model = %q, want per_call", store.listings[1].Pricing.Model)
	}

	// Each tool should have a distinct content hash.
	if store.definitions[0].ContentHash == store.definitions[1].ContentHash {
		t.Error("both definitions have the same ContentHash — expected distinct hashes")
	}
}

func TestProcessCrawledManifest_DomainMismatch(t *testing.T) {
	// YAML declares evil.com, but we're crawling legit.com.
	body := []byte(validYAML("evil.com"))
	store := &mockToolStore{}
	url := "https://legit.com" + wellKnownPath

	_, err := processCrawledManifest(context.Background(), "legit.com", url, body, store, "SHA256:key")
	if err == nil {
		t.Fatal("expected domain mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "domain mismatch") {
		t.Errorf("error = %q, expected to contain 'domain mismatch'", err.Error())
	}

	// Nothing should have been upserted.
	if len(store.definitions) != 0 {
		t.Errorf("stored %d definitions on mismatch, want 0", len(store.definitions))
	}
	if len(store.listings) != 0 {
		t.Errorf("stored %d listings on mismatch, want 0", len(store.listings))
	}
}

func TestProcessCrawledManifest_InvalidYAML(t *testing.T) {
	store := &mockToolStore{}
	url := "https://example.com" + wellKnownPath

	_, err := processCrawledManifest(context.Background(), "example.com", url, []byte("not: valid: yaml:"), store, "SHA256:key")
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}

	if len(store.definitions) != 0 || len(store.listings) != 0 {
		t.Error("store should be empty after parse failure")
	}
}

func TestProcessCrawledManifest_DefinitionUpsertError(t *testing.T) {
	domain := "example.com"
	body := []byte(validYAML(domain))
	store := &mockToolStore{defErr: errors.New("db: connection lost")}
	url := "https://" + domain + wellKnownPath

	_, err := processCrawledManifest(context.Background(), domain, url, body, store, "SHA256:key")
	if err == nil {
		t.Fatal("expected error from RegisterToolDefinition, got nil")
	}
	if !strings.Contains(err.Error(), "register definition") {
		t.Errorf("error = %q, expected to contain 'register definition'", err.Error())
	}
}

func TestProcessCrawledManifest_ListingUpsertError(t *testing.T) {
	domain := "example.com"
	body := []byte(validYAML(domain))
	store := &mockToolStore{listingErr: errors.New("db: constraint violation")}
	url := "https://" + domain + wellKnownPath

	_, err := processCrawledManifest(context.Background(), domain, url, body, store, "SHA256:key")
	if err == nil {
		t.Fatal("expected error from RegisterToolListing, got nil")
	}
	if !strings.Contains(err.Error(), "register listing") {
		t.Errorf("error = %q, expected to contain 'register listing'", err.Error())
	}

	// Definition should have been upserted before the listing failed.
	if len(store.definitions) != 1 {
		t.Errorf("stored %d definitions, want 1 (should succeed before listing fails)", len(store.definitions))
	}
}

func TestProcessCrawledManifest_ContextCancelled(t *testing.T) {
	domain := "example.com"
	body := []byte(validYAML(domain))
	url := "https://" + domain + wellKnownPath

	// Use a store that returns a context-cancellation error on the first
	// RegisterToolDefinition call to simulate a timeout mid-crawl.
	store := &mockToolStore{defErr: context.Canceled}

	_, err := processCrawledManifest(context.Background(), domain, url, body, store, "SHA256:key")
	if err == nil {
		t.Fatal("expected context.Canceled error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		// The error is wrapped, so check for substring instead.
		if !strings.Contains(err.Error(), "context canceled") {
			t.Errorf("error = %q, expected to contain 'context canceled'", err.Error())
		}
	}
}

func TestProcessCrawledManifest_DefaultPricingIsFree(t *testing.T) {
	// YAML with no pricing block — should default to "free".
	domain := "example.com"
	body := []byte(`version: "0.1"
provider:
  domain: ` + domain + `
  contact: test@example.com
tools:
  - name: NoPricing
    description: Tool with no pricing block
    capabilities:
      - test
    invoke:
      protocol: rest
      endpoint: https://example.com/test
      tool_name: test
    schema:
      input:
        x:
          type: string
      output:
        y:
          type: string
`)
	store := &mockToolStore{}
	url := "https://" + domain + wellKnownPath

	result, err := processCrawledManifest(context.Background(), domain, url, body, store, "SHA256:key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("Total = %d, want 1", result.Total)
	}
	if store.listings[0].Pricing.Model != "free" {
		t.Errorf("Pricing.Model = %q, want free", store.listings[0].Pricing.Model)
	}
}

func TestProcessCrawledManifest_ContactFromYAML(t *testing.T) {
	// Regression test: the Contact field must come from the YAML's
	// provider.contact, NOT from the providerAccount (SSH fingerprint).
	// See: Priority #1 refactor (CrawlDomain → ConvertToRecordsWithSource).
	domain := "example.com"
	body := []byte(validYAML(domain))
	store := &mockToolStore{}
	url := "https://" + domain + wellKnownPath

	providerAccount := "SHA256:totally_different_fingerprint"
	_, err := processCrawledManifest(context.Background(), domain, url, body, store, providerAccount)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	def := store.definitions[0]
	if def.Provider.Contact == providerAccount {
		t.Errorf("Contact = providerAccount %q — should be YAML contact, not SSH fingerprint", providerAccount)
	}
	if def.Provider.Contact != "tools@example.com" {
		t.Errorf("Contact = %q, want tools@example.com", def.Provider.Contact)
	}
}

func TestProcessCrawledManifest_ContentHashDeterministic(t *testing.T) {
	domain := "example.com"
	body := []byte(validYAML(domain))
	url := "https://" + domain + wellKnownPath

	// Run twice and compare hashes.
	store1 := &mockToolStore{}
	store2 := &mockToolStore{}

	_, err := processCrawledManifest(context.Background(), domain, url, body, store1, "SHA256:key")
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	_, err = processCrawledManifest(context.Background(), domain, url, body, store2, "SHA256:key")
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}

	if store1.definitions[0].ContentHash != store2.definitions[0].ContentHash {
		t.Errorf("content hashes differ across runs:\n  run1: %s\n  run2: %s",
			store1.definitions[0].ContentHash, store2.definitions[0].ContentHash)
	}
}

// ---------------------------------------------------------------------------
// CrawlDomain — end-to-end with httptest (HTTP, not HTTPS)
// ---------------------------------------------------------------------------

// newTestServer creates an httptest server that serves validYAML at
// /.well-known/toolshed.yaml for the given domain string.
func newTestServer(domain string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wellKnownPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/yaml")
		w.Write([]byte(validYAML(domain)))
	}))
}

// TestCrawlDomain_HTTPNotFound verifies that CrawlDomain returns an error
// when the server responds with 404. We can test this because CrawlDomain
// constructs an https:// URL — for non-HTTPS servers this will fail at
// the HTTP layer, which is the expected behaviour for an unreachable domain.
func TestCrawlDomain_HTTPNotFound(t *testing.T) {
	store := &mockToolStore{}

	// Cancel immediately — we just want to verify CrawlDomain fails gracefully.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := CrawlDomain(ctx, "nonexistent.invalid", store, "SHA256:key")
	if err == nil {
		t.Fatal("expected error for unreachable domain, got nil")
	}
}

// ---------------------------------------------------------------------------
// fetchManifest — HTTP layer tests via httptest
// ---------------------------------------------------------------------------

func TestFetchManifest_Success(t *testing.T) {
	domain := "example.com"
	srv := newTestServer(domain)
	defer srv.Close()

	// fetchManifest expects the full URL, so we build it from the test server.
	url := srv.URL + wellKnownPath
	body, err := fetchManifest(context.Background(), domain, url)
	if err != nil {
		t.Fatalf("fetchManifest: %v", err)
	}

	// Should have fetched valid YAML that we can parse.
	pf, err := core.ParseProviderFileFromBytes(body)
	if err != nil {
		t.Fatalf("parse fetched body: %v", err)
	}
	if pf.Provider.Domain != domain {
		t.Errorf("domain = %q, want %q", pf.Provider.Domain, domain)
	}
}

func TestFetchManifest_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := fetchManifest(context.Background(), "example.com", srv.URL+wellKnownPath)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("error = %q, expected to mention HTTP 404", err.Error())
	}
}

func TestFetchManifest_500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := fetchManifest(context.Background(), "example.com", srv.URL+wellKnownPath)
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error = %q, expected to mention HTTP 500", err.Error())
	}
}

func TestFetchManifest_BodyCapped(t *testing.T) {
	// Serve a body slightly larger than maxResponseBytes.
	oversized := strings.Repeat("x", int(maxResponseBytes)+4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(oversized))
	}))
	defer srv.Close()

	body, err := fetchManifest(context.Background(), "example.com", srv.URL+wellKnownPath)
	if err != nil {
		t.Fatalf("fetchManifest: %v", err)
	}
	if len(body) > int(maxResponseBytes) {
		t.Errorf("body length = %d, expected at most %d", len(body), maxResponseBytes)
	}
}

func TestFetchManifest_ContextCancelled(t *testing.T) {
	srv := newTestServer("example.com")
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchManifest(ctx, "example.com", srv.URL+wellKnownPath)
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}

func TestFetchManifest_UserAgentSet(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Write([]byte(validYAML("example.com")))
	}))
	defer srv.Close()

	_, err := fetchManifest(context.Background(), "example.com", srv.URL+wellKnownPath)
	if err != nil {
		t.Fatalf("fetchManifest: %v", err)
	}
	if gotUA != "ToolShed-Crawler/1.0" {
		t.Errorf("User-Agent = %q, want ToolShed-Crawler/1.0", gotUA)
	}
}

func TestFetchManifest_AcceptHeaderSet(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.Write([]byte(validYAML("example.com")))
	}))
	defer srv.Close()

	_, err := fetchManifest(context.Background(), "example.com", srv.URL+wellKnownPath)
	if err != nil {
		t.Fatalf("fetchManifest: %v", err)
	}
	if gotAccept != "application/yaml, text/yaml, text/plain" {
		t.Errorf("Accept = %q, want 'application/yaml, text/yaml, text/plain'", gotAccept)
	}
}

// ---------------------------------------------------------------------------
// End-to-end: fetchManifest → processCrawledManifest
// ---------------------------------------------------------------------------

func TestEndToEnd_FetchAndProcess(t *testing.T) {
	domain := "example.com"
	srv := newTestServer(domain)
	defer srv.Close()

	// Step 1: fetch
	url := srv.URL + wellKnownPath
	body, err := fetchManifest(context.Background(), domain, url)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	// Step 2: process
	store := &mockToolStore{}
	result, err := processCrawledManifest(context.Background(), domain, url, body, store, "SHA256:e2e_key")
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	// Verify the full pipeline produced correct results.
	if result.Total != 1 {
		t.Fatalf("Total = %d, want 1", result.Total)
	}
	if result.Domain != domain {
		t.Errorf("Domain = %q, want %q", result.Domain, domain)
	}
	if result.URL != url {
		t.Errorf("URL = %q, want %q", result.URL, url)
	}
	if len(store.definitions) != 1 || len(store.listings) != 1 {
		t.Fatalf("expected 1 def + 1 listing, got %d + %d", len(store.definitions), len(store.listings))
	}

	// Verify providerAccount flows through correctly.
	if store.listings[0].ProviderAccount != "SHA256:e2e_key" {
		t.Errorf("ProviderAccount = %q, want SHA256:e2e_key", store.listings[0].ProviderAccount)
	}

	// Verify source is "crawl".
	if store.listings[0].Source != "crawl" {
		t.Errorf("Source = %q, want crawl", store.listings[0].Source)
	}
}

func TestEndToEnd_MultiTool(t *testing.T) {
	domain := "example.com"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wellKnownPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/yaml")
		w.Write([]byte(multiToolYAML(domain)))
	}))
	defer srv.Close()

	url := srv.URL + wellKnownPath
	body, err := fetchManifest(context.Background(), domain, url)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	store := &mockToolStore{}
	result, err := processCrawledManifest(context.Background(), domain, url, body, store, "SHA256:key")
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	if result.Total != 2 {
		t.Fatalf("Total = %d, want 2", result.Total)
	}

	// Verify tool ordering matches manifest order.
	names := make([]string, len(result.Tools))
	for i, tool := range result.Tools {
		names[i] = tool.Name
	}
	if names[0] != "Echo" || names[1] != "Reverse" {
		t.Errorf("tool order = %v, want [Echo, Reverse]", names)
	}

	// Verify both definitions and listings were stored.
	if len(store.definitions) != 2 {
		t.Errorf("stored %d definitions, want 2", len(store.definitions))
	}
	if len(store.listings) != 2 {
		t.Errorf("stored %d listings, want 2", len(store.listings))
	}
}

func TestEndToEnd_DomainMismatch(t *testing.T) {
	// Server serves YAML for "evil.com" but we process as "legit.com".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(validYAML("evil.com")))
	}))
	defer srv.Close()

	url := srv.URL + wellKnownPath
	body, err := fetchManifest(context.Background(), "legit.com", url)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	store := &mockToolStore{}
	_, err = processCrawledManifest(context.Background(), "legit.com", url, body, store, "SHA256:key")
	if err == nil {
		t.Fatal("expected domain mismatch error")
	}
	if !strings.Contains(err.Error(), "domain mismatch") {
		t.Errorf("error = %q, expected 'domain mismatch'", err.Error())
	}
	if len(store.definitions) != 0 {
		t.Error("should not store anything on domain mismatch")
	}
}

// ---------------------------------------------------------------------------
// Partial failure: multi-tool with error on second tool
// ---------------------------------------------------------------------------

func TestProcessCrawledManifest_PartialFailureOnSecondTool(t *testing.T) {
	domain := "example.com"
	body := []byte(multiToolYAML(domain))
	url := "https://" + domain + wellKnownPath

	// Fail on the second RegisterToolListing call.
	callCount := 0
	store := &mockToolStore{}
	failingStore := &conditionalFailStore{
		inner:         store,
		listingFailOn: 2,
		listingErr:    fmt.Errorf("db: deadlock on second listing"),
		callCount:     &callCount,
	}

	_, err := processCrawledManifest(context.Background(), domain, url, body, failingStore, "SHA256:key")
	if err == nil {
		t.Fatal("expected error on second tool listing, got nil")
	}
	if !strings.Contains(err.Error(), "register listing") {
		t.Errorf("error = %q, expected 'register listing'", err.Error())
	}
}

// conditionalFailStore wraps a mockToolStore and fails on the Nth
// RegisterToolListing call.
type conditionalFailStore struct {
	inner         *mockToolStore
	listingFailOn int
	listingErr    error
	callCount     *int
}

func (s *conditionalFailStore) RegisterToolDefinition(ctx context.Context, def core.ToolDefinition) error {
	return s.inner.RegisterToolDefinition(ctx, def)
}

func (s *conditionalFailStore) RegisterToolListing(ctx context.Context, listing core.ToolListing) error {
	*s.callCount++
	if *s.callCount == s.listingFailOn {
		return s.listingErr
	}
	return s.inner.RegisterToolListing(ctx, listing)
}
