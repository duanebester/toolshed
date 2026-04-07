package core_test

import (
	"strings"
	"testing"
	"time"

	"github.com/toolshed/toolshed/internal/core"
)

// helper to build the fraud-detection ToolDefinition from the design doc.
func fraudDetectionDef() core.ToolDefinition {
	minVal := 0.0
	maxVal := 1.0
	return core.ToolDefinition{
		Provider: core.Provider{
			Domain: "acme.com",
		},
		Schema: core.Schema{
			Input: map[string]core.FieldDef{
				"transaction_id":    {Type: "string"},
				"amount":            {Type: "number"},
				"merchant_category": {Type: "string"},
			},
			Output: map[string]core.FieldDef{
				"risk_score": {Type: "number", Min: &minVal, Max: &maxVal},
				"flags":      {Type: "array", Items: &core.FieldDef{Type: "string"}},
			},
		},
		Invocation: core.Invocation{
			Protocol: "mcp",
			Endpoint: "https://tools.acme.com/mcp",
			ToolName: "fraud_check",
		},
		Capabilities: []string{"fraud", "ml", "financial", "real-time"},
		CreatedAt:    time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestContentHashFormat verifies the hash output has the "sha256:" prefix and
// a 64-character hex digest.
func TestContentHashFormat(t *testing.T) {
	def := fraudDetectionDef()
	hash, err := core.ContentHash(def)
	if err != nil {
		t.Fatalf("ContentHash returned error: %v", err)
	}

	if !strings.HasPrefix(hash, "sha256:") {
		t.Errorf("expected hash to start with 'sha256:', got %q", hash)
	}

	hexPart := strings.TrimPrefix(hash, "sha256:")
	if len(hexPart) != 64 {
		t.Errorf("expected 64-char hex digest, got %d chars: %q", len(hexPart), hexPart)
	}
}

// TestContentHashDeterminism hashes the same definition 1000 times and asserts
// every result is identical.
func TestContentHashDeterminism(t *testing.T) {
	def := fraudDetectionDef()

	first, err := core.ContentHash(def)
	if err != nil {
		t.Fatalf("ContentHash returned error: %v", err)
	}

	for i := 1; i < 1000; i++ {
		h, err := core.ContentHash(def)
		if err != nil {
			t.Fatalf("iteration %d: ContentHash returned error: %v", i, err)
		}
		if h != first {
			t.Fatalf("iteration %d: hash %q != first hash %q", i, h, first)
		}
	}
}

// TestContentHashIdenticalDefs verifies that two separately constructed but
// identical definitions produce the same hash.
func TestContentHashIdenticalDefs(t *testing.T) {
	a := fraudDetectionDef()
	b := fraudDetectionDef()

	hashA, err := core.ContentHash(a)
	if err != nil {
		t.Fatalf("ContentHash(a) error: %v", err)
	}
	hashB, err := core.ContentHash(b)
	if err != nil {
		t.Fatalf("ContentHash(b) error: %v", err)
	}

	if hashA != hashB {
		t.Errorf("identical definitions produced different hashes:\n  a: %s\n  b: %s", hashA, hashB)
	}
}

// TestContentHashDifferentSchema verifies that changing an input field produces
// a different hash.
func TestContentHashDifferentSchema(t *testing.T) {
	a := fraudDetectionDef()
	b := fraudDetectionDef()

	// Add an extra input field to b.
	b.Schema.Input["ip_address"] = core.FieldDef{Type: "string"}

	hashA, err := core.ContentHash(a)
	if err != nil {
		t.Fatalf("ContentHash(a) error: %v", err)
	}
	hashB, err := core.ContentHash(b)
	if err != nil {
		t.Fatalf("ContentHash(b) error: %v", err)
	}

	if hashA == hashB {
		t.Errorf("different schemas produced the same hash: %s", hashA)
	}
}

// TestContentHashDifferentProvider verifies that changing the provider domain
// produces a different hash.
func TestContentHashDifferentProvider(t *testing.T) {
	a := fraudDetectionDef()
	b := fraudDetectionDef()
	b.Provider.Domain = "evil.com"

	hashA, _ := core.ContentHash(a)
	hashB, _ := core.ContentHash(b)

	if hashA == hashB {
		t.Errorf("different providers produced the same hash: %s", hashA)
	}
}

// TestContentHashDifferentInvocation verifies that changing the invocation
// endpoint produces a different hash.
func TestContentHashDifferentInvocation(t *testing.T) {
	a := fraudDetectionDef()
	b := fraudDetectionDef()
	b.Invocation.Endpoint = "https://tools.evil.com/mcp"

	hashA, _ := core.ContentHash(a)
	hashB, _ := core.ContentHash(b)

	if hashA == hashB {
		t.Errorf("different invocations produced the same hash: %s", hashA)
	}
}

// TestContentHashDifferentCapabilities verifies that changing capabilities
// produces a different hash.
func TestContentHashDifferentCapabilities(t *testing.T) {
	a := fraudDetectionDef()
	b := fraudDetectionDef()
	b.Capabilities = []string{"fraud", "ml", "financial"} // removed "real-time"

	hashA, _ := core.ContentHash(a)
	hashB, _ := core.ContentHash(b)

	if hashA == hashB {
		t.Errorf("different capabilities produced the same hash: %s", hashA)
	}
}

// TestContentHashExcludesContentHash verifies that setting the ContentHash
// field on the struct does NOT change the computed hash.
func TestContentHashExcludesContentHash(t *testing.T) {
	a := fraudDetectionDef()
	b := fraudDetectionDef()
	b.ContentHash = "sha256:should_be_ignored"

	hashA, _ := core.ContentHash(a)
	hashB, _ := core.ContentHash(b)

	if hashA != hashB {
		t.Errorf("ContentHash field should be excluded from hashing:\n  a: %s\n  b: %s", hashA, hashB)
	}
}

// TestContentHashExcludesCreatedAt verifies that differing CreatedAt values
// do NOT change the computed hash.
func TestContentHashExcludesCreatedAt(t *testing.T) {
	a := fraudDetectionDef()
	b := fraudDetectionDef()
	b.CreatedAt = time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)

	hashA, _ := core.ContentHash(a)
	hashB, _ := core.ContentHash(b)

	if hashA != hashB {
		t.Errorf("CreatedAt should be excluded from hashing:\n  a: %s\n  b: %s", hashA, hashB)
	}
}

// TestContentHashMapKeyOrder verifies that map key iteration order doesn't
// matter — input fields declared in any order still produce the same hash.
func TestContentHashMapKeyOrder(t *testing.T) {
	a := fraudDetectionDef()

	// Build b with input fields inserted in reverse alphabetical order.
	b := fraudDetectionDef()
	b.Schema.Input = map[string]core.FieldDef{
		"transaction_id":    {Type: "string"},
		"merchant_category": {Type: "string"},
		"amount":            {Type: "number"},
	}

	hashA, _ := core.ContentHash(a)
	hashB, _ := core.ContentHash(b)

	if hashA != hashB {
		t.Errorf("map key order should not affect hash:\n  a: %s\n  b: %s", hashA, hashB)
	}
}

// TestContentHashDifferentProtocol verifies that switching the invocation
// protocol (e.g. mcp → rest) produces a different hash.
func TestContentHashDifferentProtocol(t *testing.T) {
	a := fraudDetectionDef()
	b := fraudDetectionDef()
	b.Invocation.Protocol = "rest"

	hashA, _ := core.ContentHash(a)
	hashB, _ := core.ContentHash(b)

	if hashA == hashB {
		t.Errorf("different protocols produced the same hash: %s", hashA)
	}
}

// TestContentHashDifferentOutputSchema verifies that changing an output field
// produces a different hash.
func TestContentHashDifferentOutputSchema(t *testing.T) {
	a := fraudDetectionDef()
	b := fraudDetectionDef()

	// Change risk_score max from 1.0 to 100.0
	maxVal := 100.0
	minVal := 0.0
	b.Schema.Output["risk_score"] = core.FieldDef{Type: "number", Min: &minVal, Max: &maxVal}

	hashA, _ := core.ContentHash(a)
	hashB, _ := core.ContentHash(b)

	if hashA == hashB {
		t.Errorf("different output schemas produced the same hash: %s", hashA)
	}
}

// TestContentHashMinimalDefinition verifies hashing works on a minimal
// definition with empty maps and no capabilities.
func TestContentHashMinimalDefinition(t *testing.T) {
	def := core.ToolDefinition{
		Provider: core.Provider{Domain: "example.com"},
		Schema: core.Schema{
			Input:  map[string]core.FieldDef{},
			Output: map[string]core.FieldDef{},
		},
		Invocation: core.Invocation{
			Protocol: "rest",
			Endpoint: "https://example.com/api",
			ToolName: "noop",
		},
		Capabilities: []string{},
	}

	hash, err := core.ContentHash(def)
	if err != nil {
		t.Fatalf("ContentHash returned error on minimal def: %v", err)
	}
	if !strings.HasPrefix(hash, "sha256:") {
		t.Errorf("expected sha256: prefix, got %q", hash)
	}
}
