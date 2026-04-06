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
	"github.com/toolshed/toolshed/internal/dolt"
)

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
func CrawlDomain(ctx context.Context, domain string, registry *dolt.Registry, providerAccount string) (*CrawlResult, error) {
	url := "https://" + domain + wellKnownPath

	// ----- 1. Fetch the manifest -----
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

	// ----- 2. Read body (capped at 1 MB) -----
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("crawl %s: read response body: %w", domain, err)
	}

	// ----- 3. Parse the provider file -----
	pf, err := core.ParseProviderFileFromBytes(body)
	if err != nil {
		return nil, fmt.Errorf("crawl %s: parse toolshed.yaml: %w", domain, err)
	}

	// ----- 4. Security: domain must match -----
	if pf.Provider.Domain != domain {
		return nil, fmt.Errorf(
			"crawl %s: domain mismatch — manifest declares provider.domain=%q but was fetched from %q",
			domain, pf.Provider.Domain, domain,
		)
	}

	// ----- 5. Process each tool -----
	now := time.Now().UTC()
	crawled := make([]CrawledTool, 0, len(pf.Tools))

	for _, entry := range pf.Tools {
		// 5a. Build the immutable ToolDefinition.
		def := core.ToolDefinition{
			Provider: core.Provider{
				Domain:  pf.Provider.Domain,
				Contact: pf.Provider.Contact,
			},
			Schema:       entry.Schema,
			Invocation:   entry.Invoke,
			Capabilities: entry.Capabilities,
			CreatedAt:    now,
		}

		// 5b. Compute content hash.
		hash, err := core.ContentHash(def)
		if err != nil {
			return nil, fmt.Errorf("crawl %s: content hash for tool %q: %w", domain, entry.Name, err)
		}
		def.ContentHash = hash

		// 5c. Set the provider account fingerprint via Contact (existing
		// convention — RegisterToolDefinition reads Contact into the
		// provider_account column).
		def.Provider.Contact = providerAccount

		// 5d. Default pricing model to "free" if not specified.
		pricing := entry.Pricing
		if pricing.Model == "" {
			pricing.Model = "free"
		}

		// 5e. Build the mutable ToolListing.
		toolID := core.ToolID(domain, entry.Name)
		listing := core.ToolListing{
			ID:              toolID,
			DefinitionHash:  hash,
			ProviderAccount: providerAccount,
			ProviderDomain:  pf.Provider.Domain,
			Name:            entry.Name,
			VersionLabel:    entry.Version,
			Description:     entry.Description,
			Pricing:         pricing,
			Payment:         entry.Payment,
			Source:          "crawl",
			CreatedAt:       now,
			UpdatedAt:       now,
		}

		// 5f. Upsert definition (INSERT IGNORE — idempotent).
		if err := registry.RegisterToolDefinition(ctx, def); err != nil {
			return nil, fmt.Errorf("crawl %s: register definition for tool %q: %w", domain, entry.Name, err)
		}

		// 5g. Upsert listing (ON DUPLICATE KEY UPDATE).
		if err := registry.RegisterToolListing(ctx, listing); err != nil {
			return nil, fmt.Errorf("crawl %s: register listing for tool %q: %w", domain, entry.Name, err)
		}

		// 5h. Record result. We say "indexed" because distinguishing
		// new/updated/unchanged would require an extra SELECT before
		// the upsert — not worth the cost for a best-effort status.
		crawled = append(crawled, CrawledTool{
			ID:             toolID,
			Name:           entry.Name,
			DefinitionHash: hash,
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
