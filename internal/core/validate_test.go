package core_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/toolshed/toolshed/internal/core"
)

// Helper to build the fraud-detection schema used across tests.
func fraudDetectionSchema() core.Schema {
	min0 := 0.0
	max1 := 1.0
	return core.Schema{
		Input: map[string]core.FieldDef{
			"transaction_id":    {Type: "string"},
			"amount":            {Type: "number"},
			"merchant_category": {Type: "string"},
		},
		Output: map[string]core.FieldDef{
			"risk_score": {Type: "number", Min: &min0, Max: &max1},
			"flags":      {Type: "array", Items: &core.FieldDef{Type: "string"}},
		},
	}
}

func TestValidateInputValid(t *testing.T) {
	schema := fraudDetectionSchema()
	input := map[string]any{
		"transaction_id":    "txn_001",
		"amount":            float64(99.95),
		"merchant_category": "electronics",
	}

	if err := core.ValidateInput(schema, input); err != nil {
		t.Fatalf("expected valid input to pass, got: %v", err)
	}
}

func TestValidateInputValidWithInt(t *testing.T) {
	schema := fraudDetectionSchema()
	input := map[string]any{
		"transaction_id":    "txn_002",
		"amount":            42,
		"merchant_category": "grocery",
	}

	if err := core.ValidateInput(schema, input); err != nil {
		t.Fatalf("expected int for number field to pass, got: %v", err)
	}
}

func TestValidateInputValidWithJSONNumber(t *testing.T) {
	schema := fraudDetectionSchema()

	// Simulate what json.Decoder with UseNumber produces.
	raw := `{"transaction_id":"txn_003","amount":250.00,"merchant_category":"travel"}`
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()

	var input map[string]any
	if err := dec.Decode(&input); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}

	if err := core.ValidateInput(schema, input); err != nil {
		t.Fatalf("expected json.Number for number field to pass, got: %v", err)
	}
}

func TestValidateInputMissingField(t *testing.T) {
	schema := fraudDetectionSchema()
	input := map[string]any{
		"transaction_id": "txn_004",
		// "amount" is missing
		"merchant_category": "food",
	}

	err := core.ValidateInput(schema, input)
	if err == nil {
		t.Fatal("expected error for missing field, got nil")
	}
	if !containsSubstring(err.Error(), "amount") {
		t.Fatalf("expected error to mention 'amount', got: %v", err)
	}
}

func TestValidateInputWrongType(t *testing.T) {
	schema := fraudDetectionSchema()
	input := map[string]any{
		"transaction_id":    "txn_005",
		"amount":            "not-a-number",
		"merchant_category": "retail",
	}

	err := core.ValidateInput(schema, input)
	if err == nil {
		t.Fatal("expected error for wrong type, got nil")
	}
	if !containsSubstring(err.Error(), "amount") {
		t.Fatalf("expected error to mention 'amount', got: %v", err)
	}
}

func TestValidateInputNumberRange(t *testing.T) {
	min10 := 10.0
	max100 := 100.0
	schema := core.Schema{
		Input: map[string]core.FieldDef{
			"score": {Type: "number", Min: &min10, Max: &max100},
		},
	}

	tests := []struct {
		name    string
		value   any
		wantErr bool
	}{
		{"below min", float64(5), true},
		{"above max", float64(200), true},
		{"at min", float64(10), false},
		{"at max", float64(100), false},
		{"in range", float64(50), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := map[string]any{"score": tt.value}
			err := core.ValidateInput(schema, input)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for value %v, got nil", tt.value)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error for value %v, got: %v", tt.value, err)
			}
		})
	}
}

func TestValidateInputNumberRangeMinOnly(t *testing.T) {
	min0 := 0.0
	schema := core.Schema{
		Input: map[string]core.FieldDef{
			"count": {Type: "number", Min: &min0},
		},
	}

	err := core.ValidateInput(schema, map[string]any{"count": float64(-1)})
	if err == nil {
		t.Fatal("expected error for value below min, got nil")
	}

	err = core.ValidateInput(schema, map[string]any{"count": float64(999)})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateInputArrayValidation(t *testing.T) {
	schema := core.Schema{
		Input: map[string]core.FieldDef{
			"tags": {Type: "array", Items: &core.FieldDef{Type: "string"}},
		},
	}

	// Valid: all strings.
	err := core.ValidateInput(schema, map[string]any{
		"tags": []any{"alpha", "beta"},
	})
	if err != nil {
		t.Fatalf("expected valid array to pass, got: %v", err)
	}

	// Invalid: contains a non-string.
	err = core.ValidateInput(schema, map[string]any{
		"tags": []any{"alpha", float64(42)},
	})
	if err == nil {
		t.Fatal("expected error for array with wrong item type, got nil")
	}
	if !containsSubstring(err.Error(), "tags") {
		t.Fatalf("expected error to mention 'tags', got: %v", err)
	}
}

func TestValidateInputArrayWithoutItems(t *testing.T) {
	schema := core.Schema{
		Input: map[string]core.FieldDef{
			"data": {Type: "array"},
		},
	}

	// Should pass — no Items means no element-level checking.
	err := core.ValidateInput(schema, map[string]any{
		"data": []any{1, "two", true},
	})
	if err != nil {
		t.Fatalf("expected array without Items constraint to pass, got: %v", err)
	}
}

func TestValidateInputExtraFieldsOK(t *testing.T) {
	schema := fraudDetectionSchema()
	input := map[string]any{
		"transaction_id":    "txn_006",
		"amount":            float64(42.0),
		"merchant_category": "books",
		"extra_field":       "should be ignored",
		"another_extra":     float64(123),
	}

	if err := core.ValidateInput(schema, input); err != nil {
		t.Fatalf("expected extra fields to be accepted, got: %v", err)
	}
}

func TestValidateInputBooleanField(t *testing.T) {
	schema := core.Schema{
		Input: map[string]core.FieldDef{
			"enabled": {Type: "boolean"},
		},
	}

	err := core.ValidateInput(schema, map[string]any{"enabled": true})
	if err != nil {
		t.Fatalf("expected valid bool to pass, got: %v", err)
	}

	err = core.ValidateInput(schema, map[string]any{"enabled": "true"})
	if err == nil {
		t.Fatal("expected error for string instead of bool, got nil")
	}
}

func TestValidateInputObjectField(t *testing.T) {
	schema := core.Schema{
		Input: map[string]core.FieldDef{
			"metadata": {Type: "object"},
		},
	}

	err := core.ValidateInput(schema, map[string]any{
		"metadata": map[string]any{"key": "value"},
	})
	if err != nil {
		t.Fatalf("expected valid object to pass, got: %v", err)
	}

	err = core.ValidateInput(schema, map[string]any{
		"metadata": "not-an-object",
	})
	if err == nil {
		t.Fatal("expected error for string instead of object, got nil")
	}
}

func TestValidateOutputValid(t *testing.T) {
	schema := fraudDetectionSchema()
	output := map[string]any{
		"risk_score": float64(0.85),
		"flags":      []any{"high_amount", "new_merchant"},
	}

	if err := core.ValidateOutput(schema, output); err != nil {
		t.Fatalf("expected valid output to pass, got: %v", err)
	}
}

func TestValidateOutputWrongType(t *testing.T) {
	schema := fraudDetectionSchema()
	output := map[string]any{
		"risk_score": "not-a-number",
		"flags":      []any{"ok"},
	}

	err := core.ValidateOutput(schema, output)
	if err == nil {
		t.Fatal("expected error for wrong output type, got nil")
	}
	if !containsSubstring(err.Error(), "risk_score") {
		t.Fatalf("expected error to mention 'risk_score', got: %v", err)
	}
}

func TestValidateOutputRangeViolation(t *testing.T) {
	schema := fraudDetectionSchema()
	output := map[string]any{
		"risk_score": float64(1.5),
		"flags":      []any{},
	}

	err := core.ValidateOutput(schema, output)
	if err == nil {
		t.Fatal("expected error for risk_score out of range, got nil")
	}
	if !containsSubstring(err.Error(), "risk_score") {
		t.Fatalf("expected error to mention 'risk_score', got: %v", err)
	}
}

func TestValidateOutputMissingField(t *testing.T) {
	schema := fraudDetectionSchema()
	output := map[string]any{
		"risk_score": float64(0.5),
		// "flags" is missing
	}

	err := core.ValidateOutput(schema, output)
	if err == nil {
		t.Fatal("expected error for missing output field, got nil")
	}
	if !containsSubstring(err.Error(), "flags") {
		t.Fatalf("expected error to mention 'flags', got: %v", err)
	}
}

func TestValidateOutputArrayItemValidation(t *testing.T) {
	schema := fraudDetectionSchema()
	output := map[string]any{
		"risk_score": float64(0.5),
		"flags":      []any{"ok", float64(42)},
	}

	err := core.ValidateOutput(schema, output)
	if err == nil {
		t.Fatal("expected error for array item type mismatch, got nil")
	}
	if !containsSubstring(err.Error(), "flags") {
		t.Fatalf("expected error to mention 'flags', got: %v", err)
	}
}

// containsSubstring checks if s contains substr (case-insensitive not needed here).
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
