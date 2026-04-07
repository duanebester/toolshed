package crawl

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/toolshed/toolshed/internal/core"
)

// validYAML returns a well-formed toolshed.yaml whose provider.domain
// matches the given domain string.
func validYAML(domain string) string {
	return `version: "0.1"
provider:
  domain: ` + domain + `
  contact: tools@` + domain + `
tools:
  - name: Echo
    description: Echoes back whatever you send
    version: "1.0.0"
    capabilities:
      - echo
      - testing
    invoke:
      protocol: rest
      endpoint: https://` + domain + `/echo
      tool_name: echo
    schema:
      input:
        message:
          type: string
      output:
        message:
          type: string
    pricing:
      model: free
`
}

// multiToolYAML returns a manifest with two tools.
func multiToolYAML(domain string) string {
	return `version: "0.1"
provider:
  domain: ` + domain + `
  contact: tools@` + domain + `
tools:
  - name: Echo
    description: Echoes back whatever you send
    version: "1.0.0"
    capabilities:
      - echo
    invoke:
      protocol: rest
      endpoint: https://` + domain + `/echo
      tool_name: echo
    schema:
      input:
        message:
          type: string
      output:
        message:
          type: string
    pricing:
      model: free
  - name: Reverse
    description: Reverses a string
    version: "2.0.0"
    capabilities:
      - text
      - utility
    invoke:
      protocol: rest
      endpoint: https://` + domain + `/reverse
      tool_name: reverse
    schema:
      input:
        text:
          type: string
      output:
        reversed:
          type: string
    pricing:
      model: per_call
      price: 0.001
      currency: usd
`
}

// --------------------------------------------------------------------------
// Parse + validate tests (no Dolt required)
// --------------------------------------------------------------------------

func TestParseValidYAML(t *testing.T) {
	domain := "example.com"
	body := []byte(validYAML(domain))

	pf, err := core.ParseProviderFileFromBytes(body)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if pf.Provider.Domain != domain {
		t.Errorf("domain: got %q, want %q", pf.Provider.Domain, domain)
	}
	if len(pf.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(pf.Tools))
	}
	if pf.Tools[0].Name != "Echo" {
		t.Errorf("tool name: got %q, want Echo", pf.Tools[0].Name)
	}
}

func TestParseMultipleTools(t *testing.T) {
	domain := "example.com"
	body := []byte(multiToolYAML(domain))

	pf, err := core.ParseProviderFileFromBytes(body)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(pf.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(pf.Tools))
	}
	if pf.Tools[0].Name != "Echo" {
		t.Errorf("tool[0] name: got %q, want Echo", pf.Tools[0].Name)
	}
	if pf.Tools[1].Name != "Reverse" {
		t.Errorf("tool[1] name: got %q, want Reverse", pf.Tools[1].Name)
	}
	if pf.Tools[1].Pricing.Model != "per_call" {
		t.Errorf("tool[1] pricing: got %q, want per_call", pf.Tools[1].Pricing.Model)
	}
}

func TestParseEmptyToolList(t *testing.T) {
	body := []byte(`version: "0.1"
provider:
  domain: example.com
  contact: test@example.com
tools: []
`)
	_, err := core.ParseProviderFileFromBytes(body)
	if err == nil {
		t.Fatal("expected validation error for empty tools list, got nil")
	}
	if !strings.Contains(err.Error(), "at least one tool") {
		t.Errorf("expected 'at least one tool' in error, got: %v", err)
	}
}

func TestParseInvalidYAML(t *testing.T) {
	body := []byte("this: is: not: valid: toolshed: yaml:")
	_, err := core.ParseProviderFileFromBytes(body)
	if err == nil {
		t.Fatal("expected parse error for invalid YAML, got nil")
	}
}

// --------------------------------------------------------------------------
// Domain mismatch (security check)
// --------------------------------------------------------------------------

func TestDomainMismatchDetected(t *testing.T) {
	// Simulate: fetched from "legit.com" but YAML declares "evil.com"
	body := []byte(validYAML("evil.com"))

	pf, err := core.ParseProviderFileFromBytes(body)
	if err != nil {
		t.Fatalf("parse unexpectedly failed: %v", err)
	}

	crawledDomain := "legit.com"
	if pf.Provider.Domain == crawledDomain {
		t.Fatal("expected domain mismatch, but they matched")
	}
	if pf.Provider.Domain != "evil.com" {
		t.Errorf("expected evil.com, got %q", pf.Provider.Domain)
	}
}

// --------------------------------------------------------------------------
// Content hash tests
// --------------------------------------------------------------------------

func TestContentHash_Deterministic(t *testing.T) {
	domain := "example.com"
	body := []byte(validYAML(domain))

	pf, err := core.ParseProviderFileFromBytes(body)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	hash1, err := core.ContentHashFromEntry(pf.Tools[0], domain)
	if err != nil {
		t.Fatalf("hash1 failed: %v", err)
	}
	hash2, err := core.ContentHashFromEntry(pf.Tools[0], domain)
	if err != nil {
		t.Fatalf("hash2 failed: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("content hash not deterministic: %s != %s", hash1, hash2)
	}
	if !strings.HasPrefix(hash1, "sha256:") {
		t.Errorf("expected sha256: prefix, got %q", hash1)
	}
}

func TestContentHash_ChangesWhenEndpointChanges(t *testing.T) {
	domain := "example.com"

	pf1, _ := core.ParseProviderFileFromBytes([]byte(validYAML(domain)))
	hash1, _ := core.ContentHashFromEntry(pf1.Tools[0], domain)

	// Change the endpoint — this is part of the immutable definition,
	// so the hash must change.
	modified := strings.Replace(validYAML(domain), "/echo", "/echo-v2", 1)
	pf2, _ := core.ParseProviderFileFromBytes([]byte(modified))
	hash2, _ := core.ContentHashFromEntry(pf2.Tools[0], domain)

	if hash1 == hash2 {
		t.Error("expected different hashes when endpoint changes, got identical")
	}
}

func TestContentHash_SameAcrossCrawlAndPush(t *testing.T) {
	domain := "example.com"
	body := []byte(validYAML(domain))

	pf, _ := core.ParseProviderFileFromBytes(body)

	// Hash from entry (what crawl does internally)
	hashFromEntry, _ := core.ContentHashFromEntry(pf.Tools[0], domain)

	// Hash from ConvertToRecords (what push/register does)
	defs, _, _ := core.ConvertToRecords(pf, "SHA256:some_fingerprint")
	hashFromConvert := defs[0].ContentHash

	if hashFromEntry != hashFromConvert {
		t.Errorf("hash mismatch between crawl and push paths:\n  entry:   %s\n  convert: %s", hashFromEntry, hashFromConvert)
	}
}

// --------------------------------------------------------------------------
// ConvertToRecordsWithSource
// --------------------------------------------------------------------------

func TestConvertToRecords_DefaultSourceIsPush(t *testing.T) {
	domain := "example.com"
	body := []byte(validYAML(domain))
	pf, _ := core.ParseProviderFileFromBytes(body)

	_, listings, err := core.ConvertToRecords(pf, "SHA256:test_key")
	if err != nil {
		t.Fatalf("ConvertToRecords failed: %v", err)
	}

	for i, l := range listings {
		if l.Source != "push" {
			t.Errorf("listing[%d].Source: got %q, want \"push\"", i, l.Source)
		}
	}
}

func TestConvertToRecordsWithSource_Crawl(t *testing.T) {
	domain := "example.com"
	body := []byte(validYAML(domain))
	pf, _ := core.ParseProviderFileFromBytes(body)

	_, listings, err := core.ConvertToRecordsWithSource(pf, "SHA256:test_key", "crawl")
	if err != nil {
		t.Fatalf("ConvertToRecordsWithSource failed: %v", err)
	}

	for i, l := range listings {
		if l.Source != "crawl" {
			t.Errorf("listing[%d].Source: got %q, want \"crawl\"", i, l.Source)
		}
	}
}

func TestConvertToRecords_ToolID(t *testing.T) {
	domain := "example.com"
	body := []byte(validYAML(domain))
	pf, _ := core.ParseProviderFileFromBytes(body)

	_, listings, _ := core.ConvertToRecords(pf, "SHA256:test_key")
	if len(listings) != 1 {
		t.Fatalf("expected 1 listing, got %d", len(listings))
	}

	expected := "example.com/echo"
	if listings[0].ID != expected {
		t.Errorf("tool ID: got %q, want %q", listings[0].ID, expected)
	}
}

func TestConvertToRecords_DefaultPricingIsFree(t *testing.T) {
	body := []byte(`version: "0.1"
provider:
  domain: example.com
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
	pf, err := core.ParseProviderFileFromBytes(body)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	_, listings, _ := core.ConvertToRecords(pf, "SHA256:test")
	if listings[0].Pricing.Model != "free" {
		t.Errorf("default pricing: got %q, want \"free\"", listings[0].Pricing.Model)
	}
}

// --------------------------------------------------------------------------
// HTTP fetch behaviour (httptest, no Dolt)
// --------------------------------------------------------------------------

func TestHTTPFetch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wellKnownPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/yaml")
		w.Write([]byte(validYAML("example.com")))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + wellKnownPath)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	pf, err := core.ParseProviderFileFromBytes(body)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if pf.Provider.Domain != "example.com" {
		t.Errorf("domain: got %q, want example.com", pf.Provider.Domain)
	}
}

func TestHTTPFetch_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + wellKnownPath)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 status")
	}
}

func TestHTTPFetch_BodyLimited(t *testing.T) {
	// Serve a body larger than maxResponseBytes.
	bigBody := strings.Repeat("x", int(maxResponseBytes)+4096)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(bigBody))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + wellKnownPath)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if len(body) > int(maxResponseBytes) {
		t.Errorf("body too large: %d bytes, limit is %d", len(body), maxResponseBytes)
	}
}

func TestHTTPFetch_UserAgentHeader(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Write([]byte(validYAML("example.com")))
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+wellKnownPath, nil)
	req.Header.Set("User-Agent", "ToolShed-Crawler/1.0")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if gotUA != "ToolShed-Crawler/1.0" {
		t.Errorf("User-Agent: got %q, want ToolShed-Crawler/1.0", gotUA)
	}
}

func TestHTTPFetch_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(validYAML("example.com")))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+wellKnownPath, nil)
	_, err := http.DefaultClient.Do(req)
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}

// --------------------------------------------------------------------------
// Constants
// --------------------------------------------------------------------------

func TestWellKnownPath(t *testing.T) {
	if wellKnownPath != "/.well-known/toolshed.yaml" {
		t.Errorf("unexpected wellKnownPath: %q", wellKnownPath)
	}
}

func TestURLConstruction(t *testing.T) {
	domain := "acme.com"
	url := "https://" + domain + wellKnownPath
	expected := "https://acme.com/.well-known/toolshed.yaml"
	if url != expected {
		t.Errorf("URL: got %q, want %q", url, expected)
	}
}
