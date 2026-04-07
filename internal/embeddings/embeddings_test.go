package embeddings

import (
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float64
		tol  float64
	}{
		{
			name: "identical vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{1, 0, 0},
			want: 1.0,
			tol:  1e-6,
		},
		{
			name: "orthogonal vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{0, 1, 0},
			want: 0.0,
			tol:  1e-6,
		},
		{
			name: "opposite vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{-1, 0, 0},
			want: -1.0,
			tol:  1e-6,
		},
		{
			name: "similar vectors",
			a:    []float32{1, 1, 0},
			b:    []float32{1, 0, 0},
			want: 1.0 / math.Sqrt(2),
			tol:  1e-6,
		},
		{
			name: "empty vectors",
			a:    []float32{},
			b:    []float32{},
			want: 0.0,
			tol:  1e-6,
		},
		{
			name: "different lengths",
			a:    []float32{1, 2},
			b:    []float32{1, 2, 3},
			want: 0.0,
			tol:  1e-6,
		},
		{
			name: "zero vector",
			a:    []float32{0, 0, 0},
			b:    []float32{1, 2, 3},
			want: 0.0,
			tol:  1e-6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > tt.tol {
				t.Errorf("CosineSimilarity(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestRankByCosineSimilarity(t *testing.T) {
	query := []float32{1, 0, 0}
	embs := []ToolEmbedding{
		{ToolID: "exact", Embedding: []float32{1, 0, 0}},          // score = 1.0
		{ToolID: "similar", Embedding: []float32{0.9, 0.1, 0}},    // score ~ 0.994
		{ToolID: "orthogonal", Embedding: []float32{0, 1, 0}},     // score = 0.0
		{ToolID: "somewhat", Embedding: []float32{0.5, 0.5, 0.5}}, // score ~ 0.577
	}

	results := RankByCosineSimilarity(query, embs, 0.3)

	// Should exclude "orthogonal" (score 0.0 < 0.3).
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Should be sorted by score descending: exact, similar, somewhat.
	if results[0].ToolID != "exact" {
		t.Errorf("results[0] = %q, want %q", results[0].ToolID, "exact")
	}
	if results[1].ToolID != "similar" {
		t.Errorf("results[1] = %q, want %q", results[1].ToolID, "similar")
	}
	if results[2].ToolID != "somewhat" {
		t.Errorf("results[2] = %q, want %q", results[2].ToolID, "somewhat")
	}

	// With high threshold, only exact and similar should remain.
	results = RankByCosineSimilarity(query, embs, 0.99)
	if len(results) != 2 {
		t.Fatalf("expected 2 results with threshold 0.99, got %d", len(results))
	}
}

func TestEncodeDecodeEmbedding(t *testing.T) {
	original := []float32{1.0, -2.5, 3.14159, 0.0, -0.001}
	encoded := EncodeEmbedding(original)

	if len(encoded) != len(original)*4 {
		t.Fatalf("encoded length = %d, want %d", len(encoded), len(original)*4)
	}

	decoded := DecodeEmbedding(encoded)
	if len(decoded) != len(original) {
		t.Fatalf("decoded length = %d, want %d", len(decoded), len(original))
	}

	for i := range original {
		if decoded[i] != original[i] {
			t.Errorf("decoded[%d] = %v, want %v", i, decoded[i], original[i])
		}
	}
}

func TestDecodeEmbeddingInvalidLength(t *testing.T) {
	// Not divisible by 4.
	result := DecodeEmbedding([]byte{1, 2, 3})
	if result != nil {
		t.Errorf("expected nil for invalid length, got %v", result)
	}
}

func TestBuildToolText(t *testing.T) {
	text := BuildToolText(
		"Fraud Detection",
		"Real-time transaction fraud scoring with ML",
		"acme.com",
		[]string{"fraud", "ml", "financial", "real-time"},
	)

	// Should contain all components.
	if text == "" {
		t.Fatal("BuildToolText returned empty string")
	}

	// Check the components are present.
	want := "Fraud Detection\nReal-time transaction fraud scoring with ML\ncapabilities: fraud, ml, financial, real-time\nprovider: acme.com"
	if text != want {
		t.Errorf("BuildToolText = %q, want %q", text, want)
	}
}

func TestBuildToolTextMinimal(t *testing.T) {
	text := BuildToolText("My Tool", "", "", nil)
	if text != "My Tool" {
		t.Errorf("BuildToolText minimal = %q, want %q", text, "My Tool")
	}
}
