// Package embeddings provides vector embedding support for semantic search
// in the ToolShed registry. It defines the Embedder interface and utility
// functions for computing similarity between embedding vectors.
package embeddings

import (
	"context"
	"encoding/binary"
	"math"
	"sort"
	"strings"
)

// Embedder generates vector embeddings from text. Implementations may call
// external APIs (OpenAI, Ollama) or run models locally.
type Embedder interface {
	// Embed returns a vector embedding for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch returns embeddings for multiple texts in a single call.
	// Implementations should batch API calls when possible.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Model returns the model identifier (e.g. "text-embedding-3-small").
	Model() string

	// Dimensions returns the dimensionality of the embedding vectors.
	Dimensions() int
}

// ToolEmbedding pairs a tool listing ID with its embedding vector and metadata.
type ToolEmbedding struct {
	ToolID     string
	Embedding  []float32
	Model      string
	Dimensions int
	TextHash   string // SHA-256 of the text that was embedded (staleness detection)
}

// ScoredResult pairs a tool ID with its cosine similarity score.
type ScoredResult struct {
	ToolID string
	Score  float64
}

// CosineSimilarity computes the cosine similarity between two vectors.
// Returns a value in [-1, 1] where 1 means identical direction.
// Returns 0 if either vector has zero magnitude.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// RankByCosineSimilarity ranks a set of tool embeddings against a query embedding.
// Returns results sorted by descending similarity score. Results below the
// given threshold are excluded.
func RankByCosineSimilarity(query []float32, embeddings []ToolEmbedding, threshold float64) []ScoredResult {
	results := make([]ScoredResult, 0, len(embeddings))

	for _, te := range embeddings {
		score := CosineSimilarity(query, te.Embedding)
		if score >= threshold {
			results = append(results, ScoredResult{
				ToolID: te.ToolID,
				Score:  score,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

// BuildToolText constructs the text to embed for a tool. This concatenates
// the tool's name, description, capabilities, and provider domain into a
// single string optimized for semantic search.
func BuildToolText(name, description, providerDomain string, capabilities []string) string {
	text := name
	if description != "" {
		text += "\n" + description
	}
	if len(capabilities) > 0 {
		text += "\ncapabilities: " + strings.Join(capabilities, ", ")
	}
	if providerDomain != "" {
		text += "\nprovider: " + providerDomain
	}
	return text
}

// EncodeEmbedding encodes a float32 slice to bytes for storage.
// Uses little-endian IEEE 754 binary encoding (4 bytes per float32).
func EncodeEmbedding(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// DecodeEmbedding decodes bytes back to a float32 slice.
func DecodeEmbedding(data []byte) []float32 {
	if len(data)%4 != 0 {
		return nil
	}
	v := make([]float32, len(data)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return v
}
