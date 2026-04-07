// Package core defines the domain types for the ToolShed registry (v2).
//
// Key changes from v1:
//   - Identity is SSH public key fingerprints (no DID, no Stripe account IDs)
//   - No SLA type (providers declare what they want; ToolShed doesn't enforce)
//   - Upvotes are linked to invocation reports via SSH key, not Stripe meter events
//   - Payment is decoupled — declared on the tool record, settled between parties
//   - All types carry YAML tags for SSH command output and toolshed.yaml parsing
package core

import "time"

// ---------------------------------------------------------------------------
// Account (SSH key identity)
// ---------------------------------------------------------------------------

// Account represents a ToolShed identity. The ID is the SSH public key
// fingerprint (e.g. "SHA256:nThbg6kXUpJW..."). Accounts are created
// automatically on first SSH connection — zero signup.
type Account struct {
	ID             string    `json:"id" yaml:"id"`                                         // SSH key fingerprint
	Domain         string    `json:"domain,omitempty" yaml:"domain,omitempty"`             // acme.com (verified via DNS TXT)
	DomainVerified bool      `json:"domain_verified" yaml:"domain_verified"`               // true after DNS TXT or .well-known check
	DisplayName    string    `json:"display_name,omitempty" yaml:"display_name,omitempty"` // optional human-readable name
	IsProvider     bool      `json:"is_provider" yaml:"is_provider"`                       // has registered at least one tool
	KeyType        string    `json:"key_type,omitempty" yaml:"key_type,omitempty"`         // ssh-ed25519, ssh-rsa, etc.
	PublicKey      string    `json:"public_key,omitempty" yaml:"public_key,omitempty"`     // full public key for verification
	FirstSeen      time.Time `json:"first_seen" yaml:"first_seen"`
	LastSeen       time.Time `json:"last_seen" yaml:"last_seen"`
	CreatedAt      time.Time `json:"created_at" yaml:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" yaml:"updated_at"`
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// Provider identifies who provides a tool. Domain is the primary identity;
// contact is optional and informational.
type Provider struct {
	Domain  string `json:"domain" yaml:"domain"`
	Contact string `json:"contact,omitempty" yaml:"contact,omitempty"`
}

// ---------------------------------------------------------------------------
// Schema
// ---------------------------------------------------------------------------

// FieldDef describes a single field in a tool's input or output schema.
type FieldDef struct {
	Type  string    `json:"type" yaml:"type"`
	Min   *float64  `json:"min,omitempty" yaml:"min,omitempty"`
	Max   *float64  `json:"max,omitempty" yaml:"max,omitempty"`
	Items *FieldDef `json:"items,omitempty" yaml:"items,omitempty"`
}

// Schema declares a tool's input/output contract. This is part of the
// immutable definition — changing it produces a new content hash.
type Schema struct {
	Input  map[string]FieldDef `json:"input" yaml:"input"`
	Output map[string]FieldDef `json:"output" yaml:"output"`
}

// ---------------------------------------------------------------------------
// Invocation
// ---------------------------------------------------------------------------

// Invocation describes how to call a tool. ToolShed is protocol-agnostic —
// it stores this information but never uses it to route calls.
type Invocation struct {
	Protocol string `json:"protocol" yaml:"protocol"` // rest, mcp, grpc, graphql
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	ToolName string `json:"tool_name" yaml:"tool_name"`
}

// ---------------------------------------------------------------------------
// Tool Definition (immutable, content-addressed)
// ---------------------------------------------------------------------------

// ToolDefinition is the immutable contract for a tool. The ContentHash is
// computed from (provider.domain + schema + invocation + capabilities).
// Once written, a definition is never updated or deleted. A new schema
// means a new hash means a new definition. Inspired by Unison.
type ToolDefinition struct {
	ContentHash  string     `json:"content_hash,omitempty" yaml:"content_hash,omitempty"` // sha256:... (computed server-side)
	Provider     Provider   `json:"provider" yaml:"provider"`
	Schema       Schema     `json:"schema" yaml:"schema"`
	Invocation   Invocation `json:"invocation" yaml:"invocation"`
	Capabilities []string   `json:"capabilities" yaml:"capabilities"`
	CreatedAt    time.Time  `json:"created_at" yaml:"created_at"`
}

// ---------------------------------------------------------------------------
// Pricing
// ---------------------------------------------------------------------------

// Pricing declares the cost model for a tool. ToolShed surfaces this
// information but never processes payments.
type Pricing struct {
	Model    string  `json:"model" yaml:"model"`                           // free, per_call, subscription, contact
	Price    float64 `json:"price,omitempty" yaml:"price,omitempty"`       // e.g. 0.005
	Currency string  `json:"currency,omitempty" yaml:"currency,omitempty"` // e.g. usd
}

// ---------------------------------------------------------------------------
// Payment
// ---------------------------------------------------------------------------

// PaymentMethod declares one way to pay for a tool. Settlement happens
// directly between the agent/operator and the provider — ToolShed is
// never in the payment path.
type PaymentMethod struct {
	Type             string   `json:"type" yaml:"type"`                                               // free, stripe_connect, api_key, l402, cashu, mpp
	AccountID        string   `json:"account_id,omitempty" yaml:"account_id,omitempty"`               // stripe_connect
	SignupURL        string   `json:"signup_url,omitempty" yaml:"signup_url,omitempty"`               // api_key
	Endpoint         string   `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`                   // l402
	PriceSats        int      `json:"price_sats,omitempty" yaml:"price_sats,omitempty"`               // l402, cashu
	Mint             string   `json:"mint,omitempty" yaml:"mint,omitempty"`                           // cashu
	Limit            string   `json:"limit,omitempty" yaml:"limit,omitempty"`                         // free_tier (e.g. "100/month")
	SupportedMethods []string `json:"supported_methods,omitempty" yaml:"supported_methods,omitempty"` // mpp: payment methods the 402 challenge offers (e.g. [stablecoin, card])
	Currency         string   `json:"currency,omitempty" yaml:"currency,omitempty"`                   // mpp: primary currency (e.g. usd)
}

// Payment holds the list of accepted payment methods for a tool.
type Payment struct {
	Methods []PaymentMethod `json:"methods,omitempty" yaml:"methods,omitempty"`
}

// ---------------------------------------------------------------------------
// Tool Listing (mutable metadata)
// ---------------------------------------------------------------------------

// ToolListing is the mutable metadata layer over an immutable ToolDefinition.
// Name, description, pricing, and payment can change freely. The listing
// points to a definition via DefinitionHash.
type ToolListing struct {
	ID              string    `json:"id" yaml:"id"`                                                 // acme.com/fraud-detection
	DefinitionHash  string    `json:"definition_hash" yaml:"definition_hash"`                       // → ToolDefinition.ContentHash
	ProviderAccount string    `json:"provider_account,omitempty" yaml:"provider_account,omitempty"` // SSH key fingerprint
	ProviderDomain  string    `json:"provider_domain" yaml:"provider_domain"`
	Name            string    `json:"name" yaml:"name"`
	VersionLabel    string    `json:"version_label,omitempty" yaml:"version_label,omitempty"`
	Description     string    `json:"description,omitempty" yaml:"description,omitempty"`
	Pricing         Pricing   `json:"pricing" yaml:"pricing"`
	Payment         Payment   `json:"payment,omitempty" yaml:"payment,omitempty"`
	Source          string    `json:"source,omitempty" yaml:"source,omitempty"` // push | crawl
	CreatedAt       time.Time `json:"created_at" yaml:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" yaml:"updated_at"`
}

// ---------------------------------------------------------------------------
// Upvote (proof-of-use quality signal)
// ---------------------------------------------------------------------------

// Upvote is a quality signal linked to an invocation report. You can't
// upvote a tool you haven't called — the system checks that the SSH key
// has a matching invocation record in the ledger.
type Upvote struct {
	ID             string    `json:"id" yaml:"id"`
	ToolID         string    `json:"tool_id" yaml:"tool_id"`
	KeyFingerprint string    `json:"key_fingerprint" yaml:"key_fingerprint"` // SSH key that submitted
	InvocationID   string    `json:"invocation_id" yaml:"invocation_id"`     // linked report
	InvocationHash string    `json:"invocation_hash" yaml:"invocation_hash"` // hash of the invocation record
	LedgerCommit   string    `json:"ledger_commit,omitempty" yaml:"ledger_commit,omitempty"`
	QualityScore   int       `json:"quality" yaml:"quality"` // 1–5
	Useful         bool      `json:"useful" yaml:"useful"`
	Comment        string    `json:"comment,omitempty" yaml:"comment,omitempty"`
	CreatedAt      time.Time `json:"created_at" yaml:"created_at"`
}

// ---------------------------------------------------------------------------
// Invocation Record (local ledger — never shared)
// ---------------------------------------------------------------------------

// InvocationRecord is a self-reported record of a tool call, stored in
// the local ledger. ToolShed never sees actual inputs or outputs — only
// their hashes. The SSH key fingerprint ties the report to an identity.
type InvocationRecord struct {
	ID             string    `json:"id" yaml:"id"`
	ToolID         string    `json:"tool_id" yaml:"tool_id"`
	DefinitionHash string    `json:"definition_hash" yaml:"definition_hash"`
	KeyFingerprint string    `json:"key_fingerprint" yaml:"key_fingerprint"` // SSH key that reported
	InputHash      string    `json:"input_hash,omitempty" yaml:"input_hash,omitempty"`
	OutputHash     string    `json:"output_hash,omitempty" yaml:"output_hash,omitempty"`
	LatencyMs      int       `json:"latency_ms" yaml:"latency_ms"`
	Success        bool      `json:"success" yaml:"success"`
	CreatedAt      time.Time `json:"created_at" yaml:"created_at"`
}

// ---------------------------------------------------------------------------
// Reputation (computed, cached — materialized view)
// ---------------------------------------------------------------------------

// Reputation is a derived view aggregated from upvotes and invocation
// reports. Nobody owns this score — anyone can compute it from the Dolt
// data by cloning the registry.
type Reputation struct {
	ToolID          string    `json:"tool_id" yaml:"tool_id"`
	TotalUpvotes    int       `json:"total_upvotes" yaml:"total_upvotes"`
	VerifiedUpvotes int       `json:"verified_upvotes" yaml:"verified_upvotes"`
	AvgQuality      float64   `json:"avg_quality" yaml:"avg_quality"`
	UniqueCallers   int       `json:"unique_callers" yaml:"unique_callers"`
	TotalReports    int       `json:"total_reports" yaml:"total_reports"`
	Trend           string    `json:"trend,omitempty" yaml:"trend,omitempty"` // rising, stable, declining
	ComputedAt      time.Time `json:"computed_at" yaml:"computed_at"`
}

// ---------------------------------------------------------------------------
// YAML Provider File (/.well-known/toolshed.yaml)
// ---------------------------------------------------------------------------

// ProviderFile represents the top-level structure of a toolshed.yaml file
// that providers host at /.well-known/toolshed.yaml or push via SSH.
type ProviderFile struct {
	Version  string      `json:"version" yaml:"version"`
	Provider Provider    `json:"provider" yaml:"provider"`
	Tools    []ToolEntry `json:"tools" yaml:"tools"`
}

// ToolEntry is a single tool declaration within a ProviderFile. It contains
// everything needed to create both a ToolDefinition (immutable) and a
// ToolListing (mutable) in the registry.
type ToolEntry struct {
	Name         string     `json:"name" yaml:"name"`
	Description  string     `json:"description" yaml:"description"`
	Version      string     `json:"version,omitempty" yaml:"version,omitempty"`
	Capabilities []string   `json:"capabilities" yaml:"capabilities"`
	Invoke       Invocation `json:"invoke" yaml:"invoke"`
	Schema       Schema     `json:"schema" yaml:"schema"`
	Pricing      Pricing    `json:"pricing,omitempty" yaml:"pricing,omitempty"`
	Payment      Payment    `json:"payment,omitempty" yaml:"payment,omitempty"`
}

// ---------------------------------------------------------------------------
// Search Results (SSH command output)
// ---------------------------------------------------------------------------

// SearchResult is the YAML-friendly output returned by `ssh toolshed.sh search`.
// It enriches a tool listing with its definition details and reputation.
type SearchResult struct {
	Name           string       `json:"name" yaml:"name"`
	ID             string       `json:"id" yaml:"id"`
	DefinitionHash string       `json:"definition_hash" yaml:"definition_hash"`
	Description    string       `json:"description,omitempty" yaml:"description,omitempty"`
	Capabilities   []string     `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	Invoke         Invocation   `json:"invoke" yaml:"invoke"`
	Schema         Schema       `json:"schema" yaml:"schema"`
	Pricing        Pricing      `json:"pricing" yaml:"pricing"`
	Payment        Payment      `json:"payment,omitempty" yaml:"payment,omitempty"`
	Reputation     *Reputation  `json:"reputation,omitempty" yaml:"reputation,omitempty"`
	Provider       ProviderInfo `json:"provider" yaml:"provider"`
}

// ProviderInfo is the provider summary included in search results.
type ProviderInfo struct {
	Domain   string `json:"domain" yaml:"domain"`
	Verified bool   `json:"verified" yaml:"verified"`
}

// SearchResponse wraps a list of search results for YAML output.
type SearchResponse struct {
	Results []SearchResult `json:"results" yaml:"results"`
	Total   int            `json:"total" yaml:"total"`
}
