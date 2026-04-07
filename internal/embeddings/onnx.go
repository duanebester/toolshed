package embeddings

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"sync"

	"github.com/daulet/tokenizers"
	ort "github.com/yalue/onnxruntime_go"
)

var (
	ortInit    sync.Once
	ortInitErr error
)

// InitONNXRuntime initializes the ONNX Runtime environment. Must be called
// before creating any ONNXEmbedder. If libPath is empty, the runtime library
// is expected to be in the system's default library search path.
func InitONNXRuntime(libPath string) error {
	ortInit.Do(func() {
		if libPath != "" {
			ort.SetSharedLibraryPath(libPath)
		}
		ortInitErr = ort.InitializeEnvironment()
	})
	return ortInitErr
}

// DestroyONNXRuntime cleans up the ONNX Runtime global environment.
// Call on application shutdown.
func DestroyONNXRuntime() error {
	return ort.DestroyEnvironment()
}

// ONNXEmbedder implements Embedder using a local ONNX transformer model.
// It loads the model and tokenizer from disk and runs inference entirely
// locally — no external API calls, no data leaves the machine.
type ONNXEmbedder struct {
	tokenizer   *tokenizers.Tokenizer
	session     *ort.DynamicAdvancedSession
	modelName   string
	dims        int
	maxLen      int
	inputNames  []string
	outputNames []string
	mu          sync.Mutex // protects session.Run — may not be thread-safe
}

// ONNXOption configures an ONNXEmbedder.
type ONNXOption func(*ONNXEmbedder)

// WithONNXModelName sets the model name reported by Model().
func WithONNXModelName(name string) ONNXOption {
	return func(e *ONNXEmbedder) { e.modelName = name }
}

// WithONNXDimensions sets the embedding vector dimensionality.
func WithONNXDimensions(dims int) ONNXOption {
	return func(e *ONNXEmbedder) { e.dims = dims }
}

// WithMaxLength sets the maximum token sequence length. Inputs longer
// than this are truncated.
func WithMaxLength(maxLen int) ONNXOption {
	return func(e *ONNXEmbedder) { e.maxLen = maxLen }
}

// WithInputNames overrides the ONNX model input tensor names.
// Default: ["input_ids", "attention_mask", "token_type_ids"].
func WithInputNames(names []string) ONNXOption {
	return func(e *ONNXEmbedder) { e.inputNames = names }
}

// WithOutputNames overrides the ONNX model output tensor names.
// Default: ["last_hidden_state"].
func WithOutputNames(names []string) ONNXOption {
	return func(e *ONNXEmbedder) { e.outputNames = names }
}

// NewONNXEmbedder creates a local ONNX-based embedder. The modelDir must
// contain model.onnx and tokenizer.json. Call InitONNXRuntime() before this.
//
// Defaults are tuned for jinaai/jina-embeddings-v2-small-en (512 dims,
// 512 max tokens). Override with options for other models.
func NewONNXEmbedder(modelDir string, opts ...ONNXOption) (*ONNXEmbedder, error) {
	e := &ONNXEmbedder{
		modelName:   "jina-embeddings-v2-small-en",
		dims:        512,
		maxLen:      512,
		inputNames:  []string{"input_ids", "attention_mask", "token_type_ids"},
		outputNames: []string{"last_hidden_state"},
	}
	for _, opt := range opts {
		opt(e)
	}

	// Load the HuggingFace tokenizer.
	tokPath := filepath.Join(modelDir, "tokenizer.json")
	tk, err := tokenizers.FromFile(tokPath)
	if err != nil {
		return nil, fmt.Errorf("onnx: load tokenizer %s: %w", tokPath, err)
	}
	e.tokenizer = tk

	// Create the ONNX session with dynamic input shapes.
	modelPath := filepath.Join(modelDir, "model.onnx")
	session, err := ort.NewDynamicAdvancedSession(
		modelPath,
		e.inputNames,
		e.outputNames,
		nil, // default session options
	)
	if err != nil {
		tk.Close()
		return nil, fmt.Errorf("onnx: create session %s: %w", modelPath, err)
	}
	e.session = session

	return e, nil
}

func (e *ONNXEmbedder) Model() string   { return e.modelName }
func (e *ONNXEmbedder) Dimensions() int { return e.dims }

// Close releases the ONNX session and tokenizer resources.
func (e *ONNXEmbedder) Close() error {
	if e.session != nil {
		e.session.Destroy()
	}
	if e.tokenizer != nil {
		e.tokenizer.Close()
	}
	return nil
}

// Embed returns a vector embedding for a single text.
func (e *ONNXEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	results, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("onnx: no embedding returned")
	}
	return results[0], nil
}

// EmbedBatch returns embeddings for multiple texts. Each text is processed
// individually (no cross-sequence padding) to keep memory usage predictable.
func (e *ONNXEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		vec, err := e.embedSingle(text)
		if err != nil {
			return nil, fmt.Errorf("onnx: embed text %d: %w", i, err)
		}
		results[i] = vec
	}
	return results, nil
}

// embedSingle tokenizes one text, runs ONNX inference, and returns the
// mean-pooled, L2-normalized embedding vector.
func (e *ONNXEmbedder) embedSingle(text string) ([]float32, error) {
	// Tokenize with special tokens ([CLS], [SEP]).
	ids, _, err := e.tokenizer.EncodeErr(text, true)
	if err != nil {
		return nil, fmt.Errorf("onnx: tokenize: %w", err)
	}

	seqLen := len(ids)
	if seqLen == 0 {
		return make([]float32, e.dims), nil
	}
	if seqLen > e.maxLen {
		seqLen = e.maxLen
		ids = ids[:seqLen]
	}

	// Convert uint32 token IDs to int64 tensors.
	idsI64 := make([]int64, seqLen)
	maskI64 := make([]int64, seqLen)
	typeI64 := make([]int64, seqLen) // all zeros for single-sentence
	for j, id := range ids {
		idsI64[j] = int64(id)
		maskI64[j] = 1
	}

	// Create input tensors.
	shape := ort.NewShape(1, int64(seqLen))

	inputIDs, err := ort.NewTensor(shape, idsI64)
	if err != nil {
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}
	defer inputIDs.Destroy()

	attentionMask, err := ort.NewTensor(shape, maskI64)
	if err != nil {
		return nil, fmt.Errorf("create attention_mask tensor: %w", err)
	}
	defer attentionMask.Destroy()

	// Build input list — only include token_type_ids if the model expects 3 inputs.
	inputs := []ort.ArbitraryTensor{inputIDs, attentionMask}
	if len(e.inputNames) > 2 {
		tokenTypeIDs, err := ort.NewTensor(shape, typeI64)
		if err != nil {
			return nil, fmt.Errorf("create token_type_ids tensor: %w", err)
		}
		defer tokenTypeIDs.Destroy()
		inputs = append(inputs, tokenTypeIDs)
	}

	// Create output tensor — shape [1, seqLen, dims] for last_hidden_state.
	outShape := ort.NewShape(1, int64(seqLen), int64(e.dims))
	output, err := ort.NewEmptyTensor[float32](outShape)
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	defer output.Destroy()

	// Run inference (mutex-protected — ONNX Runtime sessions may not
	// be safe for concurrent Run() calls with dynamic shapes).
	e.mu.Lock()
	err = e.session.Run(inputs, []ort.ArbitraryTensor{output})
	e.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("onnx inference: %w", err)
	}

	// Mean pool over the sequence dimension using the attention mask.
	hidden := output.GetData()
	embedding := meanPool(hidden, maskI64, seqLen, e.dims)

	// L2 normalize for cosine similarity.
	l2Normalize(embedding)

	return embedding, nil
}

// meanPool computes the attention-weighted mean of hidden states along
// the sequence dimension.
func meanPool(hidden []float32, mask []int64, seqLen, dims int) []float32 {
	embedding := make([]float32, dims)
	var count float32

	for i := 0; i < seqLen; i++ {
		if mask[i] == 0 {
			continue
		}
		count++
		offset := i * dims
		for j := 0; j < dims; j++ {
			embedding[j] += hidden[offset+j]
		}
	}

	if count > 0 {
		for j := range embedding {
			embedding[j] /= count
		}
	}

	return embedding
}

// l2Normalize scales a vector to unit length.
func l2Normalize(v []float32) {
	var norm float64
	for _, f := range v {
		norm += float64(f) * float64(f)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range v {
			v[i] = float32(float64(v[i]) / norm)
		}
	}
}
