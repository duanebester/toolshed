// Content-addressed hashing for tool definitions (v2).
//
// The hash covers: provider domain, schema (input + output), invocation
// (protocol + endpoint + tool_name), and capabilities. Everything else
// (name, pricing, description, timestamps) is mutable metadata on the
// ToolListing and is deliberately excluded.
//
// v2 change: Provider DID is removed. Only the domain participates in
// the content hash, matching the v2 identity model (SSH keys + domains).
package core

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// hashableProvider contains only the provider fields that participate in
// the content hash. In v2 this is just the domain — no DID.
type hashableProvider struct {
	Domain string `json:"domain"`
}

// hashableInvocation mirrors Invocation for canonical hashing.
type hashableInvocation struct {
	Protocol string `json:"protocol"`
	Endpoint string `json:"endpoint"`
	ToolName string `json:"tool_name"`
}

// hashableSchema mirrors Schema for canonical hashing.
type hashableSchema struct {
	Input  map[string]FieldDef `json:"input"`
	Output map[string]FieldDef `json:"output"`
}

// hashableDefinition contains exactly the fields that form the immutable
// contract. Everything else (name, pricing, description, timestamps) is
// mutable metadata on the ToolListing and is deliberately excluded.
type hashableDefinition struct {
	Provider     hashableProvider   `json:"provider"`
	Schema       hashableSchema     `json:"schema"`
	Invocation   hashableInvocation `json:"invocation"`
	Capabilities []string           `json:"capabilities"`
}

// ContentHash computes the content-addressed hash of a ToolDefinition.
//
// The hash covers: provider domain, schema (input + output), invocation
// (protocol + endpoint + tool_name), and capabilities.
//
// Determinism guarantees:
//   - Struct fields are marshaled in declaration order by encoding/json.
//   - Map keys (schema field names) are sorted alphabetically by encoding/json.
//   - Capabilities are sorted before hashing so order of insertion doesn't matter.
//
// Returns a string in the form "sha256:<hex>".
func ContentHash(def ToolDefinition) (string, error) {
	// Sort capabilities so that order doesn't affect the hash.
	caps := make([]string, len(def.Capabilities))
	copy(caps, def.Capabilities)
	sort.Strings(caps)

	h := hashableDefinition{
		Provider: hashableProvider{
			Domain: def.Provider.Domain,
		},
		Schema: hashableSchema{
			Input:  def.Schema.Input,
			Output: def.Schema.Output,
		},
		Invocation: hashableInvocation{
			Protocol: def.Invocation.Protocol,
			Endpoint: def.Invocation.Endpoint,
			ToolName: def.Invocation.ToolName,
		},
		Capabilities: caps,
	}

	data, err := json.Marshal(h)
	if err != nil {
		return "", fmt.Errorf("content hash: marshal failed: %w", err)
	}

	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum), nil
}

// ContentHashFromEntry computes the content hash for a ToolEntry combined
// with a provider domain. This is the convenience form used when processing
// a ProviderFile — the entry carries schema/invocation/capabilities and the
// provider domain comes from the file-level Provider field.
func ContentHashFromEntry(entry ToolEntry, providerDomain string) (string, error) {
	def := ToolDefinition{
		Provider: Provider{
			Domain: providerDomain,
		},
		Schema:       entry.Schema,
		Invocation:   entry.Invoke,
		Capabilities: entry.Capabilities,
	}
	return ContentHash(def)
}
