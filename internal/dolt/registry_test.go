package dolt_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/toolshed/toolshed/internal/core"
	"github.com/toolshed/toolshed/internal/dolt"
)

const (
	registryDSN = "root@tcp(localhost:3306)/toolshed_registry?parseTime=true"
	ledgerDSN   = "root@tcp(localhost:3306)/toolshed_ledger?parseTime=true"

	// Seeded tool listing IDs from schema/registry/seed.sql.
	seededToolID      = "acme.com/fraud-detection"
	seededWordCountID = "toolshed.dev/word-count"

	// Seeded definition hashes — must match seed.sql.
	seededDefinitionHash = "sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	seededWordCountHash  = "sha256:d035f30e682cfefa3225540753f1c85f14d07bf2109bfde25a5e45d3b53a6928"

	// Seeded account fingerprints (SSH key identity).
	acmeProviderKey = "SHA256:test_acme_provider_key"
	agentKey        = "SHA256:test_agent_key"
	toolshedDevKey  = "SHA256:test_toolshed_dev_key"
)

func skipIfNoDolt(t *testing.T) {
	t.Helper()
	if os.Getenv("TOOLSHED_TEST_DOLT") == "" {
		t.Skip("skipping: set TOOLSHED_TEST_DOLT=1 to run Dolt integration tests")
	}
}

func newTestRegistry(t *testing.T) *dolt.Registry {
	t.Helper()
	skipIfNoDolt(t)

	reg, err := dolt.NewRegistry(registryDSN, ledgerDSN)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	t.Cleanup(func() {
		if err := reg.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return reg
}

// ---------------------------------------------------------------------------
// Connection
// ---------------------------------------------------------------------------

func TestNewRegistry(t *testing.T) {
	skipIfNoDolt(t)

	reg, err := dolt.NewRegistry(registryDSN, ledgerDSN)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := reg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Accounts
// ---------------------------------------------------------------------------

func TestGetAccount(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	acct, err := reg.GetAccount(ctx, acmeProviderKey)
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if acct == nil {
		t.Fatal("expected account, got nil")
	}

	if acct.ID != acmeProviderKey {
		t.Errorf("ID = %q, want %q", acct.ID, acmeProviderKey)
	}
	if acct.Domain != "acme.com" {
		t.Errorf("Domain = %q, want %q", acct.Domain, "acme.com")
	}
	if !acct.DomainVerified {
		t.Error("DomainVerified = false, want true")
	}
	if acct.DisplayName != "Acme Corp" {
		t.Errorf("DisplayName = %q, want %q", acct.DisplayName, "Acme Corp")
	}
	if !acct.IsProvider {
		t.Error("IsProvider = false, want true")
	}
	if acct.KeyType != "ssh-ed25519" {
		t.Errorf("KeyType = %q, want %q", acct.KeyType, "ssh-ed25519")
	}

	// Non-existent account returns nil.
	missing, err := reg.GetAccount(ctx, "SHA256:does_not_exist")
	if err != nil {
		t.Fatalf("GetAccount(nonexistent): %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for non-existent account, got %+v", missing)
	}
}

func TestGetOrCreateAccount(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	fingerprint := "SHA256:test_ephemeral_" + time.Now().Format("20060102150405.000")

	// First call creates the account.
	acct, err := reg.GetOrCreateAccount(ctx, fingerprint, "ssh-ed25519", "ssh-ed25519 AAAAephemeral test@test")
	if err != nil {
		t.Fatalf("GetOrCreateAccount (create): %v", err)
	}
	if acct == nil {
		t.Fatal("expected account after create, got nil")
	}
	if acct.ID != fingerprint {
		t.Errorf("ID = %q, want %q", acct.ID, fingerprint)
	}
	if acct.KeyType != "ssh-ed25519" {
		t.Errorf("KeyType = %q, want %q", acct.KeyType, "ssh-ed25519")
	}
	firstLastSeen := acct.LastSeen

	// Brief pause so the second call gets a different timestamp.
	time.Sleep(10 * time.Millisecond)

	// Second call updates last_seen.
	acct2, err := reg.GetOrCreateAccount(ctx, fingerprint, "ssh-ed25519", "ssh-ed25519 AAAAephemeral test@test")
	if err != nil {
		t.Fatalf("GetOrCreateAccount (update): %v", err)
	}
	if acct2 == nil {
		t.Fatal("expected account after update, got nil")
	}
	if !acct2.LastSeen.After(firstLastSeen) && !acct2.LastSeen.Equal(firstLastSeen) {
		t.Errorf("LastSeen was not bumped: first=%v, second=%v", firstLastSeen, acct2.LastSeen)
	}

	// Clean up.
	registryDB, err := sql.Open("mysql", registryDSN)
	if err != nil {
		t.Fatalf("open registry for cleanup: %v", err)
	}
	defer registryDB.Close()
	if _, err := registryDB.ExecContext(ctx, "DELETE FROM accounts WHERE id = ?", fingerprint); err != nil {
		t.Errorf("cleanup account: %v", err)
	}
}

func TestUpdateAccountDomain(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	// Create a fresh account to update.
	fingerprint := "SHA256:test_domain_upd_" + time.Now().Format("20060102150405.000")
	_, err := reg.GetOrCreateAccount(ctx, fingerprint, "ssh-ed25519", "ssh-ed25519 AAAAdomainupd test@test")
	if err != nil {
		t.Fatalf("GetOrCreateAccount: %v", err)
	}

	// Update its domain.
	if err := reg.UpdateAccountDomain(ctx, fingerprint, "example.org"); err != nil {
		t.Fatalf("UpdateAccountDomain: %v", err)
	}

	acct, err := reg.GetAccount(ctx, fingerprint)
	if err != nil {
		t.Fatalf("GetAccount after domain update: %v", err)
	}
	if acct.Domain != "example.org" {
		t.Errorf("Domain = %q, want %q", acct.Domain, "example.org")
	}
	if !acct.DomainVerified {
		t.Error("DomainVerified = false, want true after UpdateAccountDomain")
	}

	// Updating a non-existent account should error.
	if err := reg.UpdateAccountDomain(ctx, "SHA256:no_such_key", "nope.com"); err == nil {
		t.Error("expected error updating non-existent account, got nil")
	}

	// Clean up.
	registryDB, err := sql.Open("mysql", registryDSN)
	if err != nil {
		t.Fatalf("open registry for cleanup: %v", err)
	}
	defer registryDB.Close()
	if _, err := registryDB.ExecContext(ctx, "DELETE FROM accounts WHERE id = ?", fingerprint); err != nil {
		t.Errorf("cleanup account: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tool Definitions
// ---------------------------------------------------------------------------

func TestGetToolDefinition(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	def, err := reg.GetToolDefinition(ctx, seededDefinitionHash)
	if err != nil {
		t.Fatalf("GetToolDefinition: %v", err)
	}
	if def == nil {
		t.Fatal("expected definition, got nil")
	}

	if def.ContentHash != seededDefinitionHash {
		t.Errorf("ContentHash = %q, want %q", def.ContentHash, seededDefinitionHash)
	}
	if def.Provider.Domain != "acme.com" {
		t.Errorf("Provider.Domain = %q, want %q", def.Provider.Domain, "acme.com")
	}
	// v2: protocol is "rest", not "mcp".
	if def.Invocation.Protocol != "rest" {
		t.Errorf("Invocation.Protocol = %q, want %q", def.Invocation.Protocol, "rest")
	}
	if def.Invocation.Endpoint != "https://api.acme.com/fraud" {
		t.Errorf("Invocation.Endpoint = %q, want %q", def.Invocation.Endpoint, "https://api.acme.com/fraud")
	}
	if def.Invocation.ToolName != "fraud_check" {
		t.Errorf("Invocation.ToolName = %q, want %q", def.Invocation.ToolName, "fraud_check")
	}

	// Verify schema fields.
	if _, ok := def.Schema.Input["transaction_id"]; !ok {
		t.Error("expected schema input field 'transaction_id'")
	}
	if _, ok := def.Schema.Output["risk_score"]; !ok {
		t.Error("expected schema output field 'risk_score'")
	}

	// Verify capabilities.
	if len(def.Capabilities) == 0 {
		t.Fatal("expected non-empty capabilities")
	}
	capSet := make(map[string]bool, len(def.Capabilities))
	for _, c := range def.Capabilities {
		capSet[c] = true
	}
	for _, want := range []string{"fraud", "ml", "financial", "real-time"} {
		if !capSet[want] {
			t.Errorf("missing expected capability %q", want)
		}
	}

	// Verify nil return for non-existent hash.
	missing, err := reg.GetToolDefinition(ctx, "sha256:0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("GetToolDefinition(nonexistent): %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for non-existent definition, got %+v", missing)
	}
}

func TestRegisterToolDefinition(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	testHash := "sha256:test_register_def_" + time.Now().Format("20060102150405")

	def := core.ToolDefinition{
		ContentHash: testHash,
		Provider: core.Provider{
			Domain:  "test-provider.example",
			Contact: "SHA256:test_register_def_key",
		},
		Schema: core.Schema{
			Input:  map[string]core.FieldDef{"query": {Type: "string"}},
			Output: map[string]core.FieldDef{"result": {Type: "string"}},
		},
		Invocation: core.Invocation{
			Protocol: "rest",
			Endpoint: "https://test-provider.example/api",
			ToolName: "test_tool",
		},
		Capabilities: []string{"testing"},
		CreatedAt:    time.Now().UTC().Truncate(time.Second),
	}

	if err := reg.RegisterToolDefinition(ctx, def); err != nil {
		t.Fatalf("RegisterToolDefinition: %v", err)
	}

	// Read it back.
	got, err := reg.GetToolDefinition(ctx, testHash)
	if err != nil {
		t.Fatalf("GetToolDefinition after register: %v", err)
	}
	if got == nil {
		t.Fatal("expected definition after register, got nil")
	}
	if got.ContentHash != testHash {
		t.Errorf("ContentHash = %q, want %q", got.ContentHash, testHash)
	}
	if got.Provider.Domain != "test-provider.example" {
		t.Errorf("Provider.Domain = %q, want %q", got.Provider.Domain, "test-provider.example")
	}
	if got.Invocation.ToolName != "test_tool" {
		t.Errorf("Invocation.ToolName = %q, want %q", got.Invocation.ToolName, "test_tool")
	}

	// Idempotent: inserting the same hash again should not error.
	if err := reg.RegisterToolDefinition(ctx, def); err != nil {
		t.Fatalf("RegisterToolDefinition (idempotent): %v", err)
	}

	// Clean up.
	registryDB, err := sql.Open("mysql", registryDSN)
	if err != nil {
		t.Fatalf("open registry for cleanup: %v", err)
	}
	defer registryDB.Close()
	if _, err := registryDB.ExecContext(ctx, "DELETE FROM tool_definitions WHERE content_hash = ?", testHash); err != nil {
		t.Errorf("cleanup tool definition: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tool Listings
// ---------------------------------------------------------------------------

func TestGetToolListing(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	listing, err := reg.GetToolListing(ctx, seededToolID)
	if err != nil {
		t.Fatalf("GetToolListing: %v", err)
	}
	if listing == nil {
		t.Fatal("expected listing, got nil")
	}

	if listing.ID != seededToolID {
		t.Errorf("ID = %q, want %q", listing.ID, seededToolID)
	}
	if listing.DefinitionHash != seededDefinitionHash {
		t.Errorf("DefinitionHash = %q, want %q", listing.DefinitionHash, seededDefinitionHash)
	}
	if listing.Name != "Fraud Detection" {
		t.Errorf("Name = %q, want %q", listing.Name, "Fraud Detection")
	}
	if listing.VersionLabel != "3.1.0" {
		t.Errorf("VersionLabel = %q, want %q", listing.VersionLabel, "3.1.0")
	}

	// v2 fields.
	if listing.ProviderDomain != "acme.com" {
		t.Errorf("ProviderDomain = %q, want %q", listing.ProviderDomain, "acme.com")
	}
	if listing.ProviderAccount != acmeProviderKey {
		t.Errorf("ProviderAccount = %q, want %q", listing.ProviderAccount, acmeProviderKey)
	}
	if listing.Source != "push" {
		t.Errorf("Source = %q, want %q", listing.Source, "push")
	}

	// Pricing.
	if listing.Pricing.Model != "per_call" {
		t.Errorf("Pricing.Model = %q, want %q", listing.Pricing.Model, "per_call")
	}
	if listing.Pricing.Price != 0.005 {
		t.Errorf("Pricing.Price = %v, want %v", listing.Pricing.Price, 0.005)
	}

	// Payment.
	if len(listing.Payment.Methods) == 0 {
		t.Error("expected at least one payment method")
	} else if listing.Payment.Methods[0].Type != "stripe_connect" {
		t.Errorf("Payment.Methods[0].Type = %q, want %q", listing.Payment.Methods[0].Type, "stripe_connect")
	}

	// CreatedAt should be non-zero.
	if listing.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}

	// Verify nil return for non-existent listing.
	missing, err := reg.GetToolListing(ctx, "nonexistent-tool-id")
	if err != nil {
		t.Fatalf("GetToolListing(nonexistent): %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for non-existent listing, got %+v", missing)
	}
}

func TestRegisterToolListing(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	testID := "test.example/register-listing-" + time.Now().Format("20060102150405")
	now := time.Now().UTC().Truncate(time.Second)

	listing := core.ToolListing{
		ID:              testID,
		DefinitionHash:  seededDefinitionHash, // reuse existing definition
		ProviderAccount: acmeProviderKey,
		ProviderDomain:  "test.example",
		Name:            "Test Registered Listing",
		VersionLabel:    "0.1.0",
		Description:     "A listing created by RegisterToolListing test",
		Pricing:         core.Pricing{Model: "free", Price: 0, Currency: ""},
		Payment:         core.Payment{Methods: []core.PaymentMethod{{Type: "free"}}},
		Source:          "push",
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := reg.RegisterToolListing(ctx, listing); err != nil {
		t.Fatalf("RegisterToolListing: %v", err)
	}

	// Read it back.
	got, err := reg.GetToolListing(ctx, testID)
	if err != nil {
		t.Fatalf("GetToolListing after register: %v", err)
	}
	if got == nil {
		t.Fatal("expected listing after register, got nil")
	}
	if got.ID != testID {
		t.Errorf("ID = %q, want %q", got.ID, testID)
	}
	if got.Name != "Test Registered Listing" {
		t.Errorf("Name = %q, want %q", got.Name, "Test Registered Listing")
	}
	if got.ProviderDomain != "test.example" {
		t.Errorf("ProviderDomain = %q, want %q", got.ProviderDomain, "test.example")
	}
	if got.Source != "push" {
		t.Errorf("Source = %q, want %q", got.Source, "push")
	}

	// Upsert: update the name.
	listing.Name = "Updated Listing Name"
	listing.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	if err := reg.RegisterToolListing(ctx, listing); err != nil {
		t.Fatalf("RegisterToolListing (upsert): %v", err)
	}
	got2, err := reg.GetToolListing(ctx, testID)
	if err != nil {
		t.Fatalf("GetToolListing after upsert: %v", err)
	}
	if got2.Name != "Updated Listing Name" {
		t.Errorf("Name after upsert = %q, want %q", got2.Name, "Updated Listing Name")
	}

	// Clean up.
	registryDB, err := sql.Open("mysql", registryDSN)
	if err != nil {
		t.Fatalf("open registry for cleanup: %v", err)
	}
	defer registryDB.Close()
	if _, err := registryDB.ExecContext(ctx, "DELETE FROM tool_listings WHERE id = ?", testID); err != nil {
		t.Errorf("cleanup tool listing: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Search & List
// ---------------------------------------------------------------------------

func TestSearchTools(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	// Search by name/description keyword.
	results, err := reg.SearchTools(ctx, "fraud")
	if err != nil {
		t.Fatalf("SearchTools(fraud): %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'fraud', got %d", len(results))
	}
	if results[0].ID != seededToolID {
		t.Errorf("result ID = %q, want %q", results[0].ID, seededToolID)
	}

	// Search by capability keyword.
	results, err = reg.SearchTools(ctx, "real-time")
	if err != nil {
		t.Fatalf("SearchTools(real-time): %v", err)
	}
	if len(results) < 1 {
		t.Error("expected at least 1 result for capability search 'real-time'")
	}

	// Search for word-count tool.
	results, err = reg.SearchTools(ctx, "word")
	if err != nil {
		t.Fatalf("SearchTools(word): %v", err)
	}
	if len(results) < 1 {
		t.Error("expected at least 1 result for 'word'")
	}
	found := false
	for _, r := range results {
		if r.ID == seededWordCountID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %q in word search results", seededWordCountID)
	}

	// Search for something that doesn't exist.
	results, err = reg.SearchTools(ctx, "xyznonexistent999")
	if err != nil {
		t.Fatalf("SearchTools(nonexistent): %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for nonsense query, got %d", len(results))
	}

	// ----- Wildcard escaping -----
	// A bare "%" should NOT act as a SQL wildcard that dumps all rows.
	results, err = reg.SearchTools(ctx, "%")
	if err != nil {
		t.Fatalf("SearchTools(%%): %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for literal '%%' query, got %d (LIKE wildcards not escaped?)", len(results))
	}

	// A bare "_" should NOT match any single character.
	results, err = reg.SearchTools(ctx, "_")
	if err != nil {
		t.Fatalf("SearchTools(_): %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for literal '_' query, got %d (LIKE wildcards not escaped?)", len(results))
	}
}

func TestListToolsByProvider(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	// List tools for acme.com — should find fraud-detection.
	results, err := reg.ListToolsByProvider(ctx, "acme.com")
	if err != nil {
		t.Fatalf("ListToolsByProvider(acme.com): %v", err)
	}
	if len(results) < 1 {
		t.Fatal("expected at least 1 tool for acme.com")
	}
	found := false
	for _, r := range results {
		if r.ID == seededToolID {
			found = true
			if r.ProviderDomain != "acme.com" {
				t.Errorf("ProviderDomain = %q, want %q", r.ProviderDomain, "acme.com")
			}
		}
	}
	if !found {
		t.Errorf("expected %q in acme.com provider list", seededToolID)
	}

	// List tools for toolshed.dev — should find word-count.
	results, err = reg.ListToolsByProvider(ctx, "toolshed.dev")
	if err != nil {
		t.Fatalf("ListToolsByProvider(toolshed.dev): %v", err)
	}
	if len(results) < 1 {
		t.Fatal("expected at least 1 tool for toolshed.dev")
	}
	found = false
	for _, r := range results {
		if r.ID == seededWordCountID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %q in toolshed.dev provider list", seededWordCountID)
	}

	// Non-existent provider returns empty slice.
	results, err = reg.ListToolsByProvider(ctx, "no-such-provider.example")
	if err != nil {
		t.Fatalf("ListToolsByProvider(nonexistent): %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for non-existent provider, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// Reputation
// ---------------------------------------------------------------------------

func TestGetReputation(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	rep, err := reg.GetReputation(ctx, seededToolID)
	if err != nil {
		t.Fatalf("GetReputation: %v", err)
	}
	if rep == nil {
		t.Fatal("expected reputation, got nil")
	}

	if rep.ToolID != seededToolID {
		t.Errorf("ToolID = %q, want %q", rep.ToolID, seededToolID)
	}
	if rep.TotalUpvotes != 1 {
		t.Errorf("TotalUpvotes = %d, want %d", rep.TotalUpvotes, 1)
	}
	if rep.VerifiedUpvotes != 1 {
		t.Errorf("VerifiedUpvotes = %d, want %d", rep.VerifiedUpvotes, 1)
	}
	if rep.AvgQuality != 5.0 {
		t.Errorf("AvgQuality = %v, want %v", rep.AvgQuality, 5.0)
	}
	if rep.UniqueCallers != 1 {
		t.Errorf("UniqueCallers = %d, want %d", rep.UniqueCallers, 1)
	}
	if rep.TotalReports != 1 {
		t.Errorf("TotalReports = %d, want %d", rep.TotalReports, 1)
	}
	if rep.Trend != "new" {
		t.Errorf("Trend = %q, want %q", rep.Trend, "new")
	}
	if rep.ComputedAt.IsZero() {
		t.Error("ComputedAt is zero")
	}

	// Word-count reputation.
	wcRep, err := reg.GetReputation(ctx, seededWordCountID)
	if err != nil {
		t.Fatalf("GetReputation(word-count): %v", err)
	}
	if wcRep == nil {
		t.Fatal("expected word-count reputation, got nil")
	}
	if wcRep.AvgQuality != 4.0 {
		t.Errorf("word-count AvgQuality = %v, want %v", wcRep.AvgQuality, 4.0)
	}

	// Verify nil return for non-existent tool.
	missing, err := reg.GetReputation(ctx, "nonexistent-tool-id")
	if err != nil {
		t.Fatalf("GetReputation(nonexistent): %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for non-existent reputation, got %+v", missing)
	}
}

// ---------------------------------------------------------------------------
// Invocations (local ledger)
// ---------------------------------------------------------------------------

func TestWriteInvocation(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	invID := "inv_test_" + time.Now().Format("20060102150405.000")

	inv := core.InvocationRecord{
		ID:             invID,
		ToolID:         seededToolID,
		DefinitionHash: seededDefinitionHash,
		KeyFingerprint: agentKey,
		InputHash:      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		OutputHash:     "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		LatencyMs:      123,
		Success:        true,
		CreatedAt:      time.Now().UTC().Truncate(time.Second),
	}

	if _, err := reg.WriteInvocation(ctx, inv); err != nil {
		t.Fatalf("WriteInvocation: %v", err)
	}

	// Verify the row was written by querying the ledger directly.
	ledgerDB, err := sql.Open("mysql", ledgerDSN)
	if err != nil {
		t.Fatalf("open ledger for verification: %v", err)
	}
	defer ledgerDB.Close()

	var gotID string
	var gotFingerprint string
	var gotLatency int
	var gotSuccess bool
	err = ledgerDB.QueryRowContext(ctx,
		"SELECT id, key_fingerprint, latency_ms, success FROM invocations WHERE id = ?", invID,
	).Scan(&gotID, &gotFingerprint, &gotLatency, &gotSuccess)
	if err != nil {
		t.Fatalf("verify invocation row: %v", err)
	}
	if gotID != invID {
		t.Errorf("ID = %q, want %q", gotID, invID)
	}
	if gotFingerprint != agentKey {
		t.Errorf("KeyFingerprint = %q, want %q", gotFingerprint, agentKey)
	}
	if gotLatency != 123 {
		t.Errorf("LatencyMs = %d, want %d", gotLatency, 123)
	}
	if !gotSuccess {
		t.Error("Success = false, want true")
	}

	// Clean up the test row.
	if _, err := ledgerDB.ExecContext(ctx, "DELETE FROM invocations WHERE id = ?", invID); err != nil {
		t.Errorf("cleanup invocation row: %v", err)
	}
}

func TestGetInvocationByKeyAndTool(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	// Write an invocation so we have something to look up.
	invID := "inv_lookup_test_" + time.Now().Format("20060102150405.000")
	fingerprint := "SHA256:test_inv_lookup_key"

	inv := core.InvocationRecord{
		ID:             invID,
		ToolID:         seededToolID,
		DefinitionHash: seededDefinitionHash,
		KeyFingerprint: fingerprint,
		InputHash:      "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		OutputHash:     "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		LatencyMs:      42,
		Success:        true,
		CreatedAt:      time.Now().UTC().Truncate(time.Second),
	}

	if _, err := reg.WriteInvocation(ctx, inv); err != nil {
		t.Fatalf("WriteInvocation: %v", err)
	}

	// Look it up by key + tool.
	got, err := reg.GetInvocationByKeyAndTool(ctx, fingerprint, seededToolID)
	if err != nil {
		t.Fatalf("GetInvocationByKeyAndTool: %v", err)
	}
	if got == nil {
		t.Fatal("expected invocation record, got nil")
	}
	if got.ID != invID {
		t.Errorf("ID = %q, want %q", got.ID, invID)
	}
	if got.KeyFingerprint != fingerprint {
		t.Errorf("KeyFingerprint = %q, want %q", got.KeyFingerprint, fingerprint)
	}
	if got.ToolID != seededToolID {
		t.Errorf("ToolID = %q, want %q", got.ToolID, seededToolID)
	}
	if got.LatencyMs != 42 {
		t.Errorf("LatencyMs = %d, want %d", got.LatencyMs, 42)
	}

	// Non-existent combination returns nil.
	missing, err := reg.GetInvocationByKeyAndTool(ctx, "SHA256:no_such_key", seededToolID)
	if err != nil {
		t.Fatalf("GetInvocationByKeyAndTool(nonexistent): %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for non-existent invocation, got %+v", missing)
	}

	// Clean up.
	ledgerDB, err := sql.Open("mysql", ledgerDSN)
	if err != nil {
		t.Fatalf("open ledger for cleanup: %v", err)
	}
	defer ledgerDB.Close()
	if _, err := ledgerDB.ExecContext(ctx, "DELETE FROM invocations WHERE id = ?", invID); err != nil {
		t.Errorf("cleanup invocation row: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Upvotes
// ---------------------------------------------------------------------------

func TestWriteUpvote(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	upvoteID := "upv_test_" + time.Now().Format("20060102150405.000")

	upvote := core.Upvote{
		ID:             upvoteID,
		ToolID:         seededToolID,
		KeyFingerprint: agentKey,
		InvocationID:   "inv_fraud_001",
		InvocationHash: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		LedgerCommit:   "dolt:testcommithash1234567890abcdef",
		QualityScore:   4,
		Useful:         true,
		Comment:        "Integration test upvote",
		CreatedAt:      time.Now().UTC().Truncate(time.Second),
	}

	if err := reg.WriteUpvote(ctx, upvote); err != nil {
		t.Fatalf("WriteUpvote: %v", err)
	}

	// Verify the row was written by querying the registry directly.
	registryDB, err := sql.Open("mysql", registryDSN)
	if err != nil {
		t.Fatalf("open registry for verification: %v", err)
	}
	defer registryDB.Close()

	var gotID, gotToolID, gotFingerprint, gotComment string
	var gotQuality int
	var gotUseful bool
	err = registryDB.QueryRowContext(ctx,
		"SELECT id, tool_id, key_fingerprint, quality_score, useful, comment FROM upvotes WHERE id = ?", upvoteID,
	).Scan(&gotID, &gotToolID, &gotFingerprint, &gotQuality, &gotUseful, &gotComment)
	if err != nil {
		t.Fatalf("verify upvote row: %v", err)
	}
	if gotID != upvoteID {
		t.Errorf("ID = %q, want %q", gotID, upvoteID)
	}
	if gotToolID != seededToolID {
		t.Errorf("ToolID = %q, want %q", gotToolID, seededToolID)
	}
	if gotFingerprint != agentKey {
		t.Errorf("KeyFingerprint = %q, want %q", gotFingerprint, agentKey)
	}
	if gotQuality != 4 {
		t.Errorf("QualityScore = %d, want %d", gotQuality, 4)
	}
	if !gotUseful {
		t.Error("Useful = false, want true")
	}
	if gotComment != "Integration test upvote" {
		t.Errorf("Comment = %q, want %q", gotComment, "Integration test upvote")
	}

	// Clean up the test row.
	if _, err := registryDB.ExecContext(ctx, "DELETE FROM upvotes WHERE id = ?", upvoteID); err != nil {
		t.Errorf("cleanup upvote row: %v", err)
	}
}
