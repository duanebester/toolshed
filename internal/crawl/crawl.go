// Package crawl implements ToolShed's domain crawl convention.
//
// Every ToolShed provider MAY host a machine-readable manifest at:
//
//	https://<domain>/.well-known/toolshed.yaml
//
// The crawl package fetches that file, parses it through the core YAML
// pipeline, computes content hashes, and upserts the resulting tool
// definitions and listings into the Dolt registry with Source="crawl".
//
// Security: the YAML's declared provider.domain MUST match the domain
// being crawled. This prevents a manifest hosted on evil.com from
// claiming to be acme.com's tools.
//
// Safety: response bodies are capped at 1 MB via io.LimitReader to
// prevent resource exhaustion from oversized or malicious payloads.
package crawl

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/toolshed/toolshed/internal/core"
)

// NOTE: Tool definition/listing construction is handled by
// core.ConvertToRecordsWithSource — do NOT duplicate that logic here.

// ToolStore is the subset of the registry that CrawlDomain needs. The
// production implementation is *dolt.Registry; tests can supply a mock.
type ToolStore interface {
	RegisterToolDefinition(ctx context.Context, def core.ToolDefinition) error
	RegisterToolListing(ctx context.Context, listing core.ToolListing) error
}

// maxResponseBytes is the upper bound on how many bytes we'll read from
// a provider's toolshed.yaml. 1 MB is more than enough for any
// reasonable manifest and prevents resource exhaustion.
const maxResponseBytes = 1 << 20 // 1 MB

// httpTimeout is the default timeout for the HTTP GET request.
const httpTimeout = 30 * time.Second

// wellKnownPath is the conventional location for the manifest.
const wellKnownPath = "/.well-known/toolshed.yaml"

// ---------------------------------------------------------------------------
// Result types
// ---------------------------------------------------------------------------

// CrawlResult summarises a single domain crawl.
type CrawlResult struct {
	Domain    string        `json:"domain" yaml:"domain"`
	URL       string        `json:"url" yaml:"url"`
	Tools     []CrawledTool `json:"tools" yaml:"tools"`
	Total     int           `json:"total" yaml:"total"`
	CrawledAt time.Time     `json:"crawled_at" yaml:"crawled_at"`
}

// CrawledTool records the outcome for one tool discovered during a crawl.
type CrawledTool struct {
	ID             string `json:"id" yaml:"id"`
	Name           string `json:"name" yaml:"name"`
	DefinitionHash string `json:"definition_hash" yaml:"definition_hash"`
	Status         string `json:"status" yaml:"status"` // "new", "updated", "unchanged", "indexed"
}

// ---------------------------------------------------------------------------
// CrawlDomain
// ---------------------------------------------------------------------------

// CrawlDomain fetches https://<domain>/.well-known/toolshed.yaml, parses it,
// and upserts every declared tool into the registry.
//
// providerAccount is the SSH key fingerprint (or other identifier) that
// should own the resulting records. For crawler-initiated crawls this is
// typically a system account; for user-initiated crawls it is the user's
// key fingerprint.
func CrawlDomain(ctx context.Context, domain string, store ToolStore, providerAccount string) (*CrawlResult, error) {
	url := "https://" + domain + wellKnownPath

	// ----- 1. Fetch the manifest -----
	body, err := fetchManifest(ctx, domain, url)
	if err != nil {
		return nil, err
	}

	// ----- 2. Parse, validate, and upsert -----
	return processCrawledManifest(ctx, domain, url, body, store, providerAccount)
}

// ---------------------------------------------------------------------------
// fetchManifest
// ---------------------------------------------------------------------------

// fetchManifest performs the HTTP GET for the well-known manifest URL,
// enforcing a response-body cap of maxResponseBytes.
func fetchManifest(ctx context.Context, domain, url string) ([]byte, error) {
	client := &http.Client{Timeout: httpTimeout}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("crawl %s: build request: %w", domain, err)
	}
	req.Header.Set("User-Agent", "ToolShed-Crawler/1.0")
	req.Header.Set("Accept", "application/yaml, text/yaml, text/plain")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("crawl %s: HTTP GET %s: %w", domain, url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("crawl %s: HTTP %d from %s", domain, resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("crawl %s: read response body: %w", domain, err)
	}

	return body, nil
}

// ---------------------------------------------------------------------------
// processCrawledManifest
// ---------------------------------------------------------------------------

// processCrawledManifest is the core crawl pipeline: parse, validate domain,
// convert to records, and upsert into the store. It is separated from the
// HTTP layer so that it can be unit-tested with a mock ToolStore.
func processCrawledManifest(
	ctx context.Context,
	domain string,
	url string,
	body []byte,
	store ToolStore,
	providerAccount string,
) (*CrawlResult, error) {

	// ----- 1. Parse the provider file -----
	pf, err := core.ParseProviderFileFromBytes(body)
	if err != nil {
		return nil, fmt.Errorf("crawl %s: parse toolshed.yaml: %w", domain, err)
	}

	// ----- 2. Security: domain must match -----
	if pf.Provider.Domain != domain {
		return nil, fmt.Errorf(
			"crawl %s: domain mismatch — manifest declares provider.domain=%q but was fetched from %q",
			domain, pf.Provider.Domain, domain,
		)
	}

	// ----- 3. Convert to records using shared logic -----
	// Delegates to ConvertToRecordsWithSource so that ToolDefinition and
	// ToolListing construction is defined in exactly one place.
	defs, listings, err := core.ConvertToRecordsWithSource(pf, providerAccount, "crawl")
	if err != nil {
		return nil, fmt.Errorf("crawl %s: %w", domain, err)
	}

	// ----- 4. Upsert each tool into the store -----
	now := time.Now().UTC()
	crawled := make([]CrawledTool, 0, len(defs))

	for i, def := range defs {
		listing := listings[i]

		// Upsert definition (INSERT IGNORE — idempotent).
		if err := store.RegisterToolDefinition(ctx, def); err != nil {
			return nil, fmt.Errorf("crawl %s: register definition for tool %q: %w", domain, listing.Name, err)
		}

		// Upsert listing (ON DUPLICATE KEY UPDATE).
		if err := store.RegisterToolListing(ctx, listing); err != nil {
			return nil, fmt.Errorf("crawl %s: register listing for tool %q: %w", domain, listing.Name, err)
		}

		// Record result. We say "indexed" because distinguishing
		// new/updated/unchanged would require an extra SELECT before
		// the upsert — not worth the cost for a best-effort status.
		crawled = append(crawled, CrawledTool{
			ID:             listing.ID,
			Name:           listing.Name,
			DefinitionHash: def.ContentHash,
			Status:         "indexed",
		})
	}

	return &CrawlResult{
		Domain:    domain,
		URL:       url,
		Tools:     crawled,
		Total:     len(crawled),
		CrawledAt: now,
	}, nil
}
