package core_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/toolshed/toolshed/internal/core"
)

var fixedTime = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

func TestAccountJSON(t *testing.T) {
	original := core.Account{
		ID:             "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8",
		Domain:         "acme.com",
		DomainVerified: true,
		DisplayName:    "Acme Corporation",
		IsProvider:     true,
		KeyType:        "ssh-ed25519",
		PublicKey:      "AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl",
		FirstSeen:      fixedTime.Add(-24 * time.Hour),
		LastSeen:       fixedTime,
		CreatedAt:      fixedTime.Add(-24 * time.Hour),
		UpdatedAt:      fixedTime,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal Account: %v", err)
	}

	var decoded core.Account
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal Account: %v", err)
	}

	if !reflect.DeepEqual(original, decoded) {
		t.Errorf("Account round-trip mismatch\n  original: %+v\n  decoded:  %+v", original, decoded)
	}
}

func TestToolDefinitionJSON(t *testing.T) {
	minScore := 0.0
	maxScore := 1.0

	original := core.ToolDefinition{
		ContentHash: "sha256:abc123def456",
		Provider: core.Provider{
			Domain:  "acme.com",
			Contact: "tools@acme.com",
		},
		Schema: core.Schema{
			Input: map[string]core.FieldDef{
				"transaction_id":    {Type: "string"},
				"amount":            {Type: "number"},
				"merchant_category": {Type: "string"},
			},
			Output: map[string]core.FieldDef{
				"risk_score": {
					Type: "number",
					Min:  &minScore,
					Max:  &maxScore,
				},
				"flags": {
					Type: "array",
					Items: &core.FieldDef{
						Type: "string",
					},
				},
			},
		},
		Invocation: core.Invocation{
			Protocol: "mcp",
			Endpoint: "https://tools.acme.com/mcp",
			ToolName: "fraud-detection",
		},
		Capabilities: []string{"fraud", "ml", "financial", "real-time"},
		CreatedAt:    fixedTime,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal ToolDefinition: %v", err)
	}

	var decoded core.ToolDefinition
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal ToolDefinition: %v", err)
	}

	if !reflect.DeepEqual(original, decoded) {
		t.Errorf("ToolDefinition round-trip mismatch\n  original: %+v\n  decoded:  %+v", original, decoded)
	}

	// Verify specific fields survived the round-trip.
	if decoded.Invocation.Protocol != "mcp" {
		t.Errorf("expected protocol %q, got %q", "mcp", decoded.Invocation.Protocol)
	}
	if len(decoded.Capabilities) != 4 {
		t.Errorf("expected 4 capabilities, got %d", len(decoded.Capabilities))
	}
	if decoded.Schema.Output["risk_score"].Min == nil || *decoded.Schema.Output["risk_score"].Min != 0.0 {
		t.Errorf("expected risk_score min=0.0, got %v", decoded.Schema.Output["risk_score"].Min)
	}
	if decoded.Schema.Output["risk_score"].Max == nil || *decoded.Schema.Output["risk_score"].Max != 1.0 {
		t.Errorf("expected risk_score max=1.0, got %v", decoded.Schema.Output["risk_score"].Max)
	}
	if decoded.Schema.Output["flags"].Items == nil || decoded.Schema.Output["flags"].Items.Type != "string" {
		t.Errorf("expected flags items type=string, got %+v", decoded.Schema.Output["flags"].Items)
	}
}

func TestToolListingJSON(t *testing.T) {
	original := core.ToolListing{
		ID:              "acme.com/fraud-detection",
		DefinitionHash:  "sha256:abc123def456",
		ProviderAccount: "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8",
		ProviderDomain:  "acme.com",
		Name:            "fraud-detection",
		VersionLabel:    "v1.2.0",
		Description:     "Real-time fraud detection for financial transactions",
		Pricing: core.Pricing{
			Model:    "per_call",
			Price:    0.003,
			Currency: "usd",
		},
		Payment: core.Payment{
			Methods: []core.PaymentMethod{
				{
					Type:      "stripe_connect",
					AccountID: "acct_acme_stripe",
				},
				{
					Type:      "l402",
					Endpoint:  "https://tools.acme.com/l402",
					PriceSats: 50,
				},
				{
					Type:      "cashu",
					PriceSats: 50,
					Mint:      "https://mint.acme.com",
				},
				{
					Type:      "api_key",
					SignupURL: "https://acme.com/signup",
					Limit:     "100/month",
				},
			},
		},
		Source:    "push",
		CreatedAt: fixedTime,
		UpdatedAt: fixedTime,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal ToolListing: %v", err)
	}

	var decoded core.ToolListing
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal ToolListing: %v", err)
	}

	if !reflect.DeepEqual(original, decoded) {
		t.Errorf("ToolListing round-trip mismatch\n  original: %+v\n  decoded:  %+v", original, decoded)
	}

	// Verify pricing survived.
	if decoded.Pricing.Model != "per_call" {
		t.Errorf("expected pricing model %q, got %q", "per_call", decoded.Pricing.Model)
	}
	if decoded.Pricing.Price != 0.003 {
		t.Errorf("expected pricing price=0.003, got %v", decoded.Pricing.Price)
	}

	// Verify payment methods survived.
	if len(decoded.Payment.Methods) != 4 {
		t.Errorf("expected 4 payment methods, got %d", len(decoded.Payment.Methods))
	}
	if decoded.Payment.Methods[0].AccountID != "acct_acme_stripe" {
		t.Errorf("expected AccountID=%q, got %q", "acct_acme_stripe", decoded.Payment.Methods[0].AccountID)
	}
	if decoded.Payment.Methods[1].PriceSats != 50 {
		t.Errorf("expected PriceSats=50, got %d", decoded.Payment.Methods[1].PriceSats)
	}
	if decoded.Payment.Methods[3].Limit != "100/month" {
		t.Errorf("expected Limit=%q, got %q", "100/month", decoded.Payment.Methods[3].Limit)
	}

	// Verify new v2 fields survived.
	if decoded.ProviderDomain != "acme.com" {
		t.Errorf("expected ProviderDomain=%q, got %q", "acme.com", decoded.ProviderDomain)
	}
	if decoded.Source != "push" {
		t.Errorf("expected Source=%q, got %q", "push", decoded.Source)
	}
}

func TestUpvoteJSON(t *testing.T) {
	original := core.Upvote{
		ID:             "upvote_001",
		ToolID:         "acme.com/fraud-detection",
		KeyFingerprint: "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8",
		InvocationID:   "inv_rec_001",
		InvocationHash: "sha256:invocation789",
		LedgerCommit:   "sha256:ledger456",
		QualityScore:   5,
		Useful:         true,
		Comment:        "Excellent fraud detection, very fast and accurate",
		CreatedAt:      fixedTime,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal Upvote: %v", err)
	}

	var decoded core.Upvote
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal Upvote: %v", err)
	}

	if !reflect.DeepEqual(original, decoded) {
		t.Errorf("Upvote round-trip mismatch\n  original: %+v\n  decoded:  %+v", original, decoded)
	}

	// Verify key fields survived.
	if decoded.KeyFingerprint != "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8" {
		t.Errorf("expected KeyFingerprint=%q, got %q",
			"SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8", decoded.KeyFingerprint)
	}
	if decoded.InvocationHash != "sha256:invocation789" {
		t.Errorf("expected InvocationHash=%q, got %q", "sha256:invocation789", decoded.InvocationHash)
	}
	if decoded.QualityScore != 5 {
		t.Errorf("expected QualityScore=5, got %d", decoded.QualityScore)
	}
	if !decoded.Useful {
		t.Errorf("expected Useful=true")
	}
	if decoded.Comment != "Excellent fraud detection, very fast and accurate" {
		t.Errorf("expected Comment to survive round-trip, got %q", decoded.Comment)
	}
	if decoded.InvocationID != "inv_rec_001" {
		t.Errorf("expected InvocationID=%q, got %q", "inv_rec_001", decoded.InvocationID)
	}
}

func TestInvocationRecordJSON(t *testing.T) {
	original := core.InvocationRecord{
		ID:             "inv_rec_001",
		ToolID:         "acme.com/fraud-detection",
		DefinitionHash: "sha256:abc123def456",
		KeyFingerprint: "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8",
		InputHash:      "sha256:input_aaa",
		OutputHash:     "sha256:output_bbb",
		LatencyMs:      142,
		Success:        true,
		CreatedAt:      fixedTime,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal InvocationRecord: %v", err)
	}

	var decoded core.InvocationRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal InvocationRecord: %v", err)
	}

	if !reflect.DeepEqual(original, decoded) {
		t.Errorf("InvocationRecord round-trip mismatch\n  original: %+v\n  decoded:  %+v", original, decoded)
	}

	// Verify specific fields survived.
	if decoded.LatencyMs != 142 {
		t.Errorf("expected LatencyMs=142, got %d", decoded.LatencyMs)
	}
	if !decoded.Success {
		t.Errorf("expected Success=true")
	}
	if decoded.KeyFingerprint != "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8" {
		t.Errorf("expected KeyFingerprint=%q, got %q",
			"SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8", decoded.KeyFingerprint)
	}
}

func TestToolDefinitionFromJSON(t *testing.T) {
	raw := `{
		"content_hash": "sha256:deadbeef",
		"provider": {
			"domain": "acme.com",
			"contact": "tools@acme.com"
		},
		"schema": {
			"input": {
				"transaction_id": { "type": "string" },
				"amount": { "type": "number" },
				"merchant_category": { "type": "string" }
			},
			"output": {
				"risk_score": { "type": "number", "min": 0, "max": 1 },
				"flags": {
					"type": "array",
					"items": { "type": "string" }
				}
			}
		},
		"invocation": {
			"protocol": "mcp",
			"endpoint": "https://tools.acme.com/mcp",
			"tool_name": "fraud-detection"
		},
		"capabilities": ["fraud", "ml", "financial", "real-time"],
		"created_at": "2026-03-01T00:00:00Z"
	}`

	var td core.ToolDefinition
	if err := json.Unmarshal([]byte(raw), &td); err != nil {
		t.Fatalf("failed to unmarshal ToolDefinition from raw JSON: %v", err)
	}

	// Provider
	if td.Provider.Domain != "acme.com" {
		t.Errorf("expected provider domain %q, got %q", "acme.com", td.Provider.Domain)
	}
	if td.Provider.Contact != "tools@acme.com" {
		t.Errorf("expected provider contact %q, got %q", "tools@acme.com", td.Provider.Contact)
	}

	// Content hash
	if td.ContentHash != "sha256:deadbeef" {
		t.Errorf("expected content_hash %q, got %q", "sha256:deadbeef", td.ContentHash)
	}

	// Schema input fields
	expectedInputFields := []string{"transaction_id", "amount", "merchant_category"}
	for _, field := range expectedInputFields {
		if _, ok := td.Schema.Input[field]; !ok {
			t.Errorf("expected input field %q to be present", field)
		}
	}
	if td.Schema.Input["transaction_id"].Type != "string" {
		t.Errorf("expected transaction_id type=string, got %q", td.Schema.Input["transaction_id"].Type)
	}
	if td.Schema.Input["amount"].Type != "number" {
		t.Errorf("expected amount type=number, got %q", td.Schema.Input["amount"].Type)
	}

	// Schema output: risk_score with min/max
	riskScore, ok := td.Schema.Output["risk_score"]
	if !ok {
		t.Fatalf("expected output field risk_score to be present")
	}
	if riskScore.Type != "number" {
		t.Errorf("expected risk_score type=number, got %q", riskScore.Type)
	}
	if riskScore.Min == nil || *riskScore.Min != 0.0 {
		t.Errorf("expected risk_score min=0, got %v", riskScore.Min)
	}
	if riskScore.Max == nil || *riskScore.Max != 1.0 {
		t.Errorf("expected risk_score max=1, got %v", riskScore.Max)
	}

	// Schema output: flags as array of strings
	flags, ok := td.Schema.Output["flags"]
	if !ok {
		t.Fatalf("expected output field flags to be present")
	}
	if flags.Type != "array" {
		t.Errorf("expected flags type=array, got %q", flags.Type)
	}
	if flags.Items == nil {
		t.Fatalf("expected flags.items to be non-nil")
	}
	if flags.Items.Type != "string" {
		t.Errorf("expected flags.items.type=string, got %q", flags.Items.Type)
	}

	// Invocation
	if td.Invocation.Protocol != "mcp" {
		t.Errorf("expected protocol %q, got %q", "mcp", td.Invocation.Protocol)
	}
	if td.Invocation.Endpoint != "https://tools.acme.com/mcp" {
		t.Errorf("expected endpoint %q, got %q", "https://tools.acme.com/mcp", td.Invocation.Endpoint)
	}
	if td.Invocation.ToolName != "fraud-detection" {
		t.Errorf("expected tool_name %q, got %q", "fraud-detection", td.Invocation.ToolName)
	}

	// Capabilities
	expectedCaps := []string{"fraud", "ml", "financial", "real-time"}
	if !reflect.DeepEqual(td.Capabilities, expectedCaps) {
		t.Errorf("expected capabilities %v, got %v", expectedCaps, td.Capabilities)
	}

	// CreatedAt
	if !td.CreatedAt.Equal(fixedTime) {
		t.Errorf("expected created_at %v, got %v", fixedTime, td.CreatedAt)
	}
}
