// YAML parsing for toolshed.yaml provider files (v2).
//
// Providers declare their tools in a succinct YAML format — human-writable,
// agent-parseable. This package handles parsing, validation, and conversion
// of ProviderFile records into ToolDefinition + ToolListing pairs.
package core

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ParseProviderFile reads and parses a toolshed.yaml file from the given path.
func ParseProviderFile(path string) (*ProviderFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open provider file: %w", err)
	}
	defer f.Close()
	return ParseProviderFileFromReader(f)
}

// ParseProviderFileFromReader parses a toolshed.yaml from an io.Reader.
// This is the primary entry point used by both file-based and stdin-based
// parsing flows (e.g. crawl fetching from /.well-known/toolshed.yaml).
func ParseProviderFileFromReader(r io.Reader) (*ProviderFile, error) {
	var pf ProviderFile
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&pf); err != nil {
		return nil, fmt.Errorf("parse provider YAML: %w", err)
	}
	if err := ValidateProviderFile(&pf); err != nil {
		return nil, err
	}
	return &pf, nil
}

// ParseProviderFileFromBytes parses a toolshed.yaml from raw bytes.
func ParseProviderFileFromBytes(data []byte) (*ProviderFile, error) {
	return ParseProviderFileFromReader(strings.NewReader(string(data)))
}

// ValidateProviderFile checks that a parsed ProviderFile has all required
// fields and that each tool entry is well-formed.
func ValidateProviderFile(pf *ProviderFile) error {
	if pf.Version == "" {
		return fmt.Errorf("provider file: missing required field: version")
	}
	if pf.Provider.Domain == "" {
		return fmt.Errorf("provider file: missing required field: provider.domain")
	}
	if len(pf.Tools) == 0 {
		return fmt.Errorf("provider file: must declare at least one tool")
	}
	for i, tool := range pf.Tools {
		if err := validateToolEntry(tool, i); err != nil {
			return err
		}
	}
	return nil
}

// validateToolEntry checks that a single tool entry has all required fields.
func validateToolEntry(entry ToolEntry, index int) error {
	prefix := fmt.Sprintf("tools[%d]", index)

	if entry.Name == "" {
		return fmt.Errorf("%s: missing required field: name", prefix)
	}
	if entry.Description == "" {
		return fmt.Errorf("%s (%s): missing required field: description", prefix, entry.Name)
	}
	if len(entry.Capabilities) == 0 {
		return fmt.Errorf("%s (%s): missing required field: capabilities", prefix, entry.Name)
	}

	// Invocation validation.
	if entry.Invoke.Protocol == "" {
		return fmt.Errorf("%s (%s): missing required field: invoke.protocol", prefix, entry.Name)
	}
	validProtocols := map[string]bool{
		"rest": true, "mcp": true, "grpc": true, "graphql": true,
	}
	if !validProtocols[entry.Invoke.Protocol] {
		return fmt.Errorf("%s (%s): unsupported invoke.protocol %q (must be rest, mcp, grpc, or graphql)",
			prefix, entry.Name, entry.Invoke.Protocol)
	}
	if entry.Invoke.Endpoint == "" {
		return fmt.Errorf("%s (%s): missing required field: invoke.endpoint", prefix, entry.Name)
	}

	// Schema validation — at minimum, input and output must be present
	// (they can be empty maps for tools with no parameters).
	if entry.Schema.Input == nil {
		return fmt.Errorf("%s (%s): missing required field: schema.input", prefix, entry.Name)
	}
	if entry.Schema.Output == nil {
		return fmt.Errorf("%s (%s): missing required field: schema.output", prefix, entry.Name)
	}

	// Validate field types in schema.
	for fieldName, fieldDef := range entry.Schema.Input {
		if err := validateFieldType(fieldDef, fmt.Sprintf("%s.schema.input.%s", prefix, fieldName)); err != nil {
			return err
		}
	}
	for fieldName, fieldDef := range entry.Schema.Output {
		if err := validateFieldType(fieldDef, fmt.Sprintf("%s.schema.output.%s", prefix, fieldName)); err != nil {
			return err
		}
	}

	// Pricing model validation (optional — defaults to "free").
	if entry.Pricing.Model != "" {
		validModels := map[string]bool{
			"free": true, "per_call": true, "subscription": true, "contact": true,
		}
		if !validModels[entry.Pricing.Model] {
			return fmt.Errorf("%s (%s): unsupported pricing.model %q", prefix, entry.Name, entry.Pricing.Model)
		}
	}

	// Payment method type validation (optional).
	for j, method := range entry.Payment.Methods {
		validTypes := map[string]bool{
			"free": true, "stripe_connect": true, "api_key": true, "l402": true, "cashu": true, "mpp": true,
		}
		if !validTypes[method.Type] {
			return fmt.Errorf("%s (%s): payment.methods[%d]: unsupported type %q",
				prefix, entry.Name, j, method.Type)
		}
	}

	return nil
}

// validateFieldType checks that a schema field definition has a valid type.
func validateFieldType(fd FieldDef, path string) error {
	validTypes := map[string]bool{
		"string": true, "number": true, "boolean": true, "array": true, "object": true,
	}
	if !validTypes[fd.Type] {
		return fmt.Errorf("%s: unsupported type %q", path, fd.Type)
	}
	if fd.Type == "array" && fd.Items != nil {
		if err := validateFieldType(*fd.Items, path+".items"); err != nil {
			return err
		}
	}
	return nil
}

// ToolID generates the registry ID for a tool from its provider domain and
// tool name. The format is "domain/slugified-name", e.g. "acme.com/fraud-detection".
func ToolID(domain, name string) string {
	slug := slugify(name)
	return domain + "/" + slug
}

// slugify converts a human-readable name to a URL-safe slug.
// "Fraud Detection" → "fraud-detection"
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '-' || r == '_' || r == ' ' || r == '.':
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	result := b.String()
	// Trim trailing dash.
	result = strings.TrimRight(result, "-")
	return result
}

// ConvertToRecords converts a ProviderFile into ToolDefinition and ToolListing
// pairs, computing content hashes along the way. The providerAccount is the
// SSH key fingerprint of the account that registered the tools.
// Source is set to "push" (registered via SSH/API).
func ConvertToRecords(pf *ProviderFile, providerAccount string) ([]ToolDefinition, []ToolListing, error) {
	return ConvertToRecordsWithSource(pf, providerAccount, "push")
}

// ConvertToRecordsWithSource is like ConvertToRecords but allows specifying the
// source field on each listing (e.g. "push" for SSH/API registration, "crawl"
// for .well-known indexing).
func ConvertToRecordsWithSource(pf *ProviderFile, providerAccount string, source string) ([]ToolDefinition, []ToolListing, error) {
	now := time.Now().UTC()
	defs := make([]ToolDefinition, 0, len(pf.Tools))
	listings := make([]ToolListing, 0, len(pf.Tools))

	for _, entry := range pf.Tools {
		def := ToolDefinition{
			Provider: Provider{
				Domain:  pf.Provider.Domain,
				Contact: pf.Provider.Contact,
			},
			Schema:       entry.Schema,
			Invocation:   entry.Invoke,
			Capabilities: entry.Capabilities,
			CreatedAt:    now,
		}

		hash, err := ContentHash(def)
		if err != nil {
			return nil, nil, fmt.Errorf("compute content hash for %q: %w", entry.Name, err)
		}
		def.ContentHash = hash

		// Default pricing model to "free" if not specified.
		pricing := entry.Pricing
		if pricing.Model == "" {
			pricing.Model = "free"
		}

		toolID := ToolID(pf.Provider.Domain, entry.Name)

		listing := ToolListing{
			ID:              toolID,
			DefinitionHash:  hash,
			ProviderAccount: providerAccount,
			ProviderDomain:  pf.Provider.Domain,
			Name:            entry.Name,
			VersionLabel:    entry.Version,
			Description:     entry.Description,
			Pricing:         pricing,
			Payment:         entry.Payment,
			Source:          source,
			CreatedAt:       now,
			UpdatedAt:       now,
		}

		defs = append(defs, def)
		listings = append(listings, listing)
	}

	return defs, listings, nil
}

// MarshalYAML marshals a value to YAML bytes. Convenience wrapper used
// by the SSH command handlers to produce structured output.
func MarshalYAML(v any) ([]byte, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal YAML: %w", err)
	}
	return data, nil
}
