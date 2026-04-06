package core_test

import (
	"strings"
	"testing"

	"github.com/toolshed/toolshed/internal/core"
)

const validProviderYAML = `
version: "0.1"
provider:
  domain: acme.com
  contact: tools@acme.com

tools:
  - name: Fraud Detection
    description: Real-time fraud scoring for transactions
    version: "1.0.0"
    capabilities: [fraud-detection, real-time, fintech]
    invoke:
      protocol: rest
      endpoint: https://api.acme.com/fraud
      tool_name: fraud_detection
    schema:
      input:
        transaction_id: { type: string }
        amount: { type: number, min: 0 }
        merchant_category: { type: string }
      output:
        risk_score: { type: number, min: 0, max: 100 }
        flags: { type: array, items: { type: string } }
    pricing:
      model: free
`

const multiToolYAML = `
version: "0.1"
provider:
  domain: acme.com
  contact: tools@acme.com

tools:
  - name: Fraud Detection
    description: Real-time fraud scoring for transactions
    version: "1.0.0"
    capabilities: [fraud-detection, real-time, fintech]
    invoke:
      protocol: rest
      endpoint: https://api.acme.com/fraud
      tool_name: fraud_detection
    schema:
      input:
        transaction_id: { type: string }
      output:
        risk_score: { type: number }
    pricing:
      model: per_call
      price: 0.005
      currency: usd
    payment:
      methods:
        - type: stripe_connect
          account_id: acct_acme_abc123
        - type: free
          limit: 100/month

  - name: Sentiment Analysis
    description: Analyze text sentiment
    version: "2.0.0"
    capabilities: [nlp, sentiment, text]
    invoke:
      protocol: rest
      endpoint: https://api.acme.com/sentiment
      tool_name: sentiment_analysis
    schema:
      input:
        text: { type: string }
      output:
        sentiment: { type: string }
        score: { type: number, min: -1, max: 1 }
    pricing:
      model: free
`

func TestParseProviderFileFromReader(t *testing.T) {
	pf, err := core.ParseProviderFileFromReader(strings.NewReader(validProviderYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pf.Version != "0.1" {
		t.Errorf("Version = %q, want %q", pf.Version, "0.1")
	}
	if pf.Provider.Domain != "acme.com" {
		t.Errorf("Provider.Domain = %q, want %q", pf.Provider.Domain, "acme.com")
	}
	if pf.Provider.Contact != "tools@acme.com" {
		t.Errorf("Provider.Contact = %q, want %q", pf.Provider.Contact, "tools@acme.com")
	}
	if len(pf.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(pf.Tools))
	}

	tool := pf.Tools[0]
	if tool.Name != "Fraud Detection" {
		t.Errorf("Tool.Name = %q, want %q", tool.Name, "Fraud Detection")
	}
	if tool.Description != "Real-time fraud scoring for transactions" {
		t.Errorf("Tool.Description = %q, want %q", tool.Description, "Real-time fraud scoring for transactions")
	}
	if tool.Version != "1.0.0" {
		t.Errorf("Tool.Version = %q, want %q", tool.Version, "1.0.0")
	}
	if len(tool.Capabilities) != 3 {
		t.Fatalf("expected 3 capabilities, got %d", len(tool.Capabilities))
	}
	if tool.Invoke.Protocol != "rest" {
		t.Errorf("Invoke.Protocol = %q, want %q", tool.Invoke.Protocol, "rest")
	}
	if tool.Invoke.Endpoint != "https://api.acme.com/fraud" {
		t.Errorf("Invoke.Endpoint = %q, want %q", tool.Invoke.Endpoint, "https://api.acme.com/fraud")
	}
	if tool.Invoke.ToolName != "fraud_detection" {
		t.Errorf("Invoke.ToolName = %q, want %q", tool.Invoke.ToolName, "fraud_detection")
	}
	if tool.Pricing.Model != "free" {
		t.Errorf("Pricing.Model = %q, want %q", tool.Pricing.Model, "free")
	}

	// Schema input fields.
	if len(tool.Schema.Input) != 3 {
		t.Fatalf("expected 3 input fields, got %d", len(tool.Schema.Input))
	}
	if fd, ok := tool.Schema.Input["amount"]; !ok {
		t.Error("missing input field 'amount'")
	} else {
		if fd.Type != "number" {
			t.Errorf("amount.Type = %q, want %q", fd.Type, "number")
		}
		if fd.Min == nil || *fd.Min != 0 {
			t.Errorf("amount.Min = %v, want 0", fd.Min)
		}
	}

	// Schema output fields.
	if len(tool.Schema.Output) != 2 {
		t.Fatalf("expected 2 output fields, got %d", len(tool.Schema.Output))
	}
	if fd, ok := tool.Schema.Output["risk_score"]; !ok {
		t.Error("missing output field 'risk_score'")
	} else {
		if fd.Min == nil || *fd.Min != 0 {
			t.Errorf("risk_score.Min = %v, want 0", fd.Min)
		}
		if fd.Max == nil || *fd.Max != 100 {
			t.Errorf("risk_score.Max = %v, want 100", fd.Max)
		}
	}
	if fd, ok := tool.Schema.Output["flags"]; !ok {
		t.Error("missing output field 'flags'")
	} else {
		if fd.Type != "array" {
			t.Errorf("flags.Type = %q, want %q", fd.Type, "array")
		}
		if fd.Items == nil || fd.Items.Type != "string" {
			t.Error("flags.Items should be {type: string}")
		}
	}
}

func TestParseProviderFileMultiTool(t *testing.T) {
	pf, err := core.ParseProviderFileFromReader(strings.NewReader(multiToolYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pf.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(pf.Tools))
	}

	// First tool: fraud detection with payment methods.
	fraud := pf.Tools[0]
	if fraud.Pricing.Model != "per_call" {
		t.Errorf("fraud Pricing.Model = %q, want %q", fraud.Pricing.Model, "per_call")
	}
	if fraud.Pricing.Price != 0.005 {
		t.Errorf("fraud Pricing.Price = %v, want %v", fraud.Pricing.Price, 0.005)
	}
	if fraud.Pricing.Currency != "usd" {
		t.Errorf("fraud Pricing.Currency = %q, want %q", fraud.Pricing.Currency, "usd")
	}
	if len(fraud.Payment.Methods) != 2 {
		t.Fatalf("expected 2 payment methods, got %d", len(fraud.Payment.Methods))
	}
	if fraud.Payment.Methods[0].Type != "stripe_connect" {
		t.Errorf("payment[0].Type = %q, want %q", fraud.Payment.Methods[0].Type, "stripe_connect")
	}
	if fraud.Payment.Methods[0].AccountID != "acct_acme_abc123" {
		t.Errorf("payment[0].AccountID = %q, want %q", fraud.Payment.Methods[0].AccountID, "acct_acme_abc123")
	}
	if fraud.Payment.Methods[1].Type != "free" {
		t.Errorf("payment[1].Type = %q, want %q", fraud.Payment.Methods[1].Type, "free")
	}
	if fraud.Payment.Methods[1].Limit != "100/month" {
		t.Errorf("payment[1].Limit = %q, want %q", fraud.Payment.Methods[1].Limit, "100/month")
	}

	// Second tool: sentiment analysis.
	sent := pf.Tools[1]
	if sent.Name != "Sentiment Analysis" {
		t.Errorf("Name = %q, want %q", sent.Name, "Sentiment Analysis")
	}
	if sent.Invoke.ToolName != "sentiment_analysis" {
		t.Errorf("Invoke.ToolName = %q, want %q", sent.Invoke.ToolName, "sentiment_analysis")
	}
	if _, ok := sent.Schema.Output["score"]; !ok {
		t.Error("missing output field 'score'")
	}
}

func TestParseProviderFileMissingVersion(t *testing.T) {
	yaml := `
provider:
  domain: acme.com
tools:
  - name: Test
    description: A test tool
    capabilities: [test]
    invoke:
      protocol: rest
      endpoint: https://example.com/api
    schema:
      input: {}
      output: {}
`
	_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for missing version, got nil")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("expected error to mention 'version', got: %v", err)
	}
}

func TestParseProviderFileMissingDomain(t *testing.T) {
	yaml := `
version: "0.1"
provider:
  contact: tools@acme.com
tools:
  - name: Test
    description: A test tool
    capabilities: [test]
    invoke:
      protocol: rest
      endpoint: https://example.com/api
    schema:
      input: {}
      output: {}
`
	_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for missing domain, got nil")
	}
	if !strings.Contains(err.Error(), "domain") {
		t.Errorf("expected error to mention 'domain', got: %v", err)
	}
}

func TestParseProviderFileNoTools(t *testing.T) {
	yaml := `
version: "0.1"
provider:
  domain: acme.com
tools: []
`
	_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for empty tools list, got nil")
	}
	if !strings.Contains(err.Error(), "at least one tool") {
		t.Errorf("expected error to mention 'at least one tool', got: %v", err)
	}
}

func TestParseProviderFileMissingToolName(t *testing.T) {
	yaml := `
version: "0.1"
provider:
  domain: acme.com
tools:
  - description: A tool without a name
    capabilities: [test]
    invoke:
      protocol: rest
      endpoint: https://example.com/api
    schema:
      input: {}
      output: {}
`
	_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for missing tool name, got nil")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("expected error to mention 'name', got: %v", err)
	}
}

func TestParseProviderFileMissingDescription(t *testing.T) {
	yaml := `
version: "0.1"
provider:
  domain: acme.com
tools:
  - name: Test Tool
    capabilities: [test]
    invoke:
      protocol: rest
      endpoint: https://example.com/api
    schema:
      input: {}
      output: {}
`
	_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for missing description, got nil")
	}
	if !strings.Contains(err.Error(), "description") {
		t.Errorf("expected error to mention 'description', got: %v", err)
	}
}

func TestParseProviderFileMissingCapabilities(t *testing.T) {
	yaml := `
version: "0.1"
provider:
  domain: acme.com
tools:
  - name: Test Tool
    description: A tool
    invoke:
      protocol: rest
      endpoint: https://example.com/api
    schema:
      input: {}
      output: {}
`
	_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for missing capabilities, got nil")
	}
	if !strings.Contains(err.Error(), "capabilities") {
		t.Errorf("expected error to mention 'capabilities', got: %v", err)
	}
}

func TestParseProviderFileInvalidProtocol(t *testing.T) {
	yaml := `
version: "0.1"
provider:
  domain: acme.com
tools:
  - name: Test Tool
    description: A tool
    capabilities: [test]
    invoke:
      protocol: soap
      endpoint: https://example.com/api
    schema:
      input: {}
      output: {}
`
	_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for invalid protocol, got nil")
	}
	if !strings.Contains(err.Error(), "protocol") {
		t.Errorf("expected error to mention 'protocol', got: %v", err)
	}
}

func TestParseProviderFileMissingEndpoint(t *testing.T) {
	yaml := `
version: "0.1"
provider:
  domain: acme.com
tools:
  - name: Test Tool
    description: A tool
    capabilities: [test]
    invoke:
      protocol: rest
    schema:
      input: {}
      output: {}
`
	_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for missing endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("expected error to mention 'endpoint', got: %v", err)
	}
}

func TestParseProviderFileInvalidPricingModel(t *testing.T) {
	yaml := `
version: "0.1"
provider:
  domain: acme.com
tools:
  - name: Test Tool
    description: A tool
    capabilities: [test]
    invoke:
      protocol: rest
      endpoint: https://example.com/api
    schema:
      input: {}
      output: {}
    pricing:
      model: barter
`
	_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for invalid pricing model, got nil")
	}
	if !strings.Contains(err.Error(), "pricing.model") {
		t.Errorf("expected error to mention 'pricing.model', got: %v", err)
	}
}

func TestParseProviderFileInvalidPaymentType(t *testing.T) {
	yaml := `
version: "0.1"
provider:
  domain: acme.com
tools:
  - name: Test Tool
    description: A tool
    capabilities: [test]
    invoke:
      protocol: rest
      endpoint: https://example.com/api
    schema:
      input: {}
      output: {}
    payment:
      methods:
        - type: bitcoin_cash
`
	_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for invalid payment type, got nil")
	}
	if !strings.Contains(err.Error(), "type") {
		t.Errorf("expected error to mention 'type', got: %v", err)
	}
}

func TestParseProviderFileInvalidFieldType(t *testing.T) {
	yaml := `
version: "0.1"
provider:
  domain: acme.com
tools:
  - name: Test Tool
    description: A tool
    capabilities: [test]
    invoke:
      protocol: rest
      endpoint: https://example.com/api
    schema:
      input:
        data: { type: datetime }
      output: {}
`
	_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for invalid field type, got nil")
	}
	if !strings.Contains(err.Error(), "type") {
		t.Errorf("expected error to mention 'type', got: %v", err)
	}
}

func TestParseProviderFileAllProtocols(t *testing.T) {
	for _, proto := range []string{"rest", "mcp", "grpc", "graphql"} {
		yaml := `
version: "0.1"
provider:
  domain: acme.com
tools:
  - name: Test Tool
    description: A tool
    capabilities: [test]
    invoke:
      protocol: ` + proto + `
      endpoint: https://example.com/api
    schema:
      input: {}
      output: {}
`
		_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
		if err != nil {
			t.Errorf("protocol %q should be valid, got error: %v", proto, err)
		}
	}
}

func TestParseProviderFileAllPricingModels(t *testing.T) {
	for _, model := range []string{"free", "per_call", "subscription", "contact"} {
		yaml := `
version: "0.1"
provider:
  domain: acme.com
tools:
  - name: Test Tool
    description: A tool
    capabilities: [test]
    invoke:
      protocol: rest
      endpoint: https://example.com/api
    schema:
      input: {}
      output: {}
    pricing:
      model: ` + model + `
`
		_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
		if err != nil {
			t.Errorf("pricing model %q should be valid, got error: %v", model, err)
		}
	}
}

func TestParseProviderFileAllPaymentTypes(t *testing.T) {
	for _, typ := range []string{"free", "stripe_connect", "api_key", "l402", "cashu", "mpp"} {
		yaml := `
version: "0.1"
provider:
  domain: acme.com
tools:
  - name: Test Tool
    description: A tool
    capabilities: [test]
    invoke:
      protocol: rest
      endpoint: https://example.com/api
    schema:
      input: {}
      output: {}
    payment:
      methods:
        - type: ` + typ + `
`
		_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
		if err != nil {
			t.Errorf("payment type %q should be valid, got error: %v", typ, err)
		}
	}
}

func TestParseProviderFileDefaultPricing(t *testing.T) {
	yaml := `
version: "0.1"
provider:
  domain: acme.com
tools:
  - name: Test Tool
    description: A tool
    capabilities: [test]
    invoke:
      protocol: rest
      endpoint: https://example.com/api
    schema:
      input: {}
      output: {}
`
	pf, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// pricing.model is empty in the parsed YAML, but ConvertToRecords defaults it to "free"
	if pf.Tools[0].Pricing.Model != "" {
		t.Errorf("expected empty pricing model before conversion, got %q", pf.Tools[0].Pricing.Model)
	}
}

// --- ToolID / slugify ---

func TestToolID(t *testing.T) {
	tests := []struct {
		domain string
		name   string
		want   string
	}{
		{"acme.com", "Fraud Detection", "acme.com/fraud-detection"},
		{"toolshed.dev", "Word Count", "toolshed.dev/word-count"},
		{"example.com", "my-tool", "example.com/my-tool"},
		{"example.com", "ML Scoring v2.0", "example.com/ml-scoring-v2-0"},
		{"example.com", "  Spaces Everywhere  ", "example.com/spaces-everywhere"},
		{"example.com", "ALLCAPS", "example.com/allcaps"},
		{"example.com", "under_scores", "example.com/under-scores"},
		{"example.com", "dots.and.more", "example.com/dots-and-more"},
	}

	for _, tt := range tests {
		got := core.ToolID(tt.domain, tt.name)
		if got != tt.want {
			t.Errorf("ToolID(%q, %q) = %q, want %q", tt.domain, tt.name, got, tt.want)
		}
	}
}

// --- ConvertToRecords ---

func TestConvertToRecords(t *testing.T) {
	pf, err := core.ParseProviderFileFromReader(strings.NewReader(multiToolYAML))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	providerAccount := "SHA256:testkey123"
	defs, listings, err := core.ConvertToRecords(pf, providerAccount)
	if err != nil {
		t.Fatalf("ConvertToRecords error: %v", err)
	}

	if len(defs) != 2 {
		t.Fatalf("expected 2 definitions, got %d", len(defs))
	}
	if len(listings) != 2 {
		t.Fatalf("expected 2 listings, got %d", len(listings))
	}

	// First definition: fraud detection.
	fraudDef := defs[0]
	if fraudDef.ContentHash == "" {
		t.Error("expected non-empty content hash for fraud definition")
	}
	if !strings.HasPrefix(fraudDef.ContentHash, "sha256:") {
		t.Errorf("content hash should start with 'sha256:', got %q", fraudDef.ContentHash)
	}
	if fraudDef.Provider.Domain != "acme.com" {
		t.Errorf("Provider.Domain = %q, want %q", fraudDef.Provider.Domain, "acme.com")
	}
	if fraudDef.Invocation.Protocol != "rest" {
		t.Errorf("Invocation.Protocol = %q, want %q", fraudDef.Invocation.Protocol, "rest")
	}

	// Second definition: sentiment.
	sentDef := defs[1]
	if sentDef.ContentHash == "" {
		t.Error("expected non-empty content hash for sentiment definition")
	}
	if sentDef.ContentHash == fraudDef.ContentHash {
		t.Error("different tools should have different content hashes")
	}

	// First listing: fraud detection.
	fraudListing := listings[0]
	if fraudListing.ID != "acme.com/fraud-detection" {
		t.Errorf("ID = %q, want %q", fraudListing.ID, "acme.com/fraud-detection")
	}
	if fraudListing.DefinitionHash != fraudDef.ContentHash {
		t.Errorf("DefinitionHash = %q, want %q", fraudListing.DefinitionHash, fraudDef.ContentHash)
	}
	if fraudListing.ProviderAccount != providerAccount {
		t.Errorf("ProviderAccount = %q, want %q", fraudListing.ProviderAccount, providerAccount)
	}
	if fraudListing.ProviderDomain != "acme.com" {
		t.Errorf("ProviderDomain = %q, want %q", fraudListing.ProviderDomain, "acme.com")
	}
	if fraudListing.Name != "Fraud Detection" {
		t.Errorf("Name = %q, want %q", fraudListing.Name, "Fraud Detection")
	}
	if fraudListing.VersionLabel != "1.0.0" {
		t.Errorf("VersionLabel = %q, want %q", fraudListing.VersionLabel, "1.0.0")
	}
	if fraudListing.Pricing.Model != "per_call" {
		t.Errorf("Pricing.Model = %q, want %q", fraudListing.Pricing.Model, "per_call")
	}
	if fraudListing.Pricing.Price != 0.005 {
		t.Errorf("Pricing.Price = %v, want %v", fraudListing.Pricing.Price, 0.005)
	}
	if fraudListing.Source != "push" {
		t.Errorf("Source = %q, want %q", fraudListing.Source, "push")
	}
	if len(fraudListing.Payment.Methods) != 2 {
		t.Fatalf("expected 2 payment methods, got %d", len(fraudListing.Payment.Methods))
	}

	// Second listing: sentiment.
	sentListing := listings[1]
	if sentListing.ID != "acme.com/sentiment-analysis" {
		t.Errorf("ID = %q, want %q", sentListing.ID, "acme.com/sentiment-analysis")
	}
	if sentListing.Pricing.Model != "free" {
		t.Errorf("Pricing.Model = %q, want %q", sentListing.Pricing.Model, "free")
	}
}

func TestConvertToRecordsDefaultPricingModel(t *testing.T) {
	yaml := `
version: "0.1"
provider:
  domain: example.com
tools:
  - name: Simple Tool
    description: A simple tool with no pricing
    capabilities: [test]
    invoke:
      protocol: rest
      endpoint: https://example.com/api
    schema:
      input: {}
      output: {}
`
	pf, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, listings, err := core.ConvertToRecords(pf, "SHA256:testkey")
	if err != nil {
		t.Fatalf("ConvertToRecords error: %v", err)
	}

	if listings[0].Pricing.Model != "free" {
		t.Errorf("expected default pricing model 'free', got %q", listings[0].Pricing.Model)
	}
}

func TestContentHashFromEntry(t *testing.T) {
	pf, err := core.ParseProviderFileFromReader(strings.NewReader(validProviderYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hash, err := core.ContentHashFromEntry(pf.Tools[0], pf.Provider.Domain)
	if err != nil {
		t.Fatalf("ContentHashFromEntry error: %v", err)
	}
	if !strings.HasPrefix(hash, "sha256:") {
		t.Errorf("expected sha256: prefix, got %q", hash)
	}

	// Verify determinism: calling again produces the same hash.
	hash2, err := core.ContentHashFromEntry(pf.Tools[0], pf.Provider.Domain)
	if err != nil {
		t.Fatalf("ContentHashFromEntry second call error: %v", err)
	}
	if hash != hash2 {
		t.Errorf("non-deterministic: %q != %q", hash, hash2)
	}

	// Different domain → different hash.
	hash3, err := core.ContentHashFromEntry(pf.Tools[0], "evil.com")
	if err != nil {
		t.Fatalf("ContentHashFromEntry error: %v", err)
	}
	if hash == hash3 {
		t.Error("different domain should produce a different hash")
	}
}

// --- MarshalYAML ---

func TestMarshalYAML(t *testing.T) {
	result := core.SearchResponse{
		Results: []core.SearchResult{
			{
				Name:           "Fraud Detection",
				ID:             "acme.com/fraud-detection",
				DefinitionHash: "sha256:abc123",
				Description:    "Real-time fraud scoring",
				Capabilities:   []string{"fraud", "ml"},
				Invoke: core.Invocation{
					Protocol: "rest",
					Endpoint: "https://api.acme.com/fraud",
					ToolName: "fraud_detection",
				},
				Schema: core.Schema{
					Input:  map[string]core.FieldDef{"id": {Type: "string"}},
					Output: map[string]core.FieldDef{"score": {Type: "number"}},
				},
				Pricing: core.Pricing{Model: "free"},
				Provider: core.ProviderInfo{
					Domain:   "acme.com",
					Verified: true,
				},
			},
		},
		Total: 1,
	}

	data, err := core.MarshalYAML(result)
	if err != nil {
		t.Fatalf("MarshalYAML error: %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "fraud-detection") {
		t.Error("expected YAML output to contain 'fraud-detection'")
	}
	if !strings.Contains(output, "acme.com") {
		t.Error("expected YAML output to contain 'acme.com'")
	}
	if !strings.Contains(output, "sha256:abc123") {
		t.Error("expected YAML output to contain 'sha256:abc123'")
	}
	if !strings.Contains(output, "total: 1") {
		t.Error("expected YAML output to contain 'total: 1'")
	}
}

func TestParseProviderFileFromBytes(t *testing.T) {
	pf, err := core.ParseProviderFileFromBytes([]byte(validProviderYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pf.Provider.Domain != "acme.com" {
		t.Errorf("Provider.Domain = %q, want %q", pf.Provider.Domain, "acme.com")
	}
	if len(pf.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(pf.Tools))
	}
}

func TestParseProviderFileMissingProtocol(t *testing.T) {
	yaml := `
version: "0.1"
provider:
  domain: acme.com
tools:
  - name: Test
    description: A tool
    capabilities: [test]
    invoke:
      endpoint: https://example.com/api
    schema:
      input: {}
      output: {}
`
	_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for missing protocol, got nil")
	}
	if !strings.Contains(err.Error(), "protocol") {
		t.Errorf("expected error to mention 'protocol', got: %v", err)
	}
}

func TestParseProviderFileMissingSchemaInput(t *testing.T) {
	yaml := `
version: "0.1"
provider:
  domain: acme.com
tools:
  - name: Test
    description: A tool
    capabilities: [test]
    invoke:
      protocol: rest
      endpoint: https://example.com/api
    schema:
      output: {}
`
	_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for missing schema.input, got nil")
	}
	if !strings.Contains(err.Error(), "schema.input") {
		t.Errorf("expected error to mention 'schema.input', got: %v", err)
	}
}

func TestParseProviderFileMissingSchemaOutput(t *testing.T) {
	yaml := `
version: "0.1"
provider:
  domain: acme.com
tools:
  - name: Test
    description: A tool
    capabilities: [test]
    invoke:
      protocol: rest
      endpoint: https://example.com/api
    schema:
      input: {}
`
	_, err := core.ParseProviderFileFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for missing schema.output, got nil")
	}
	if !strings.Contains(err.Error(), "schema.output") {
		t.Errorf("expected error to mention 'schema.output', got: %v", err)
	}
}
