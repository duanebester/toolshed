package embeddings

import (
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// meanPool
// ---------------------------------------------------------------------------

func TestMeanPool_SingleToken(t *testing.T) {
	// One token, 3 dims — mean pool of a single vector is itself.
	hidden := []float32{1.0, 2.0, 3.0}
	mask := []int64{1}
	got := meanPool(hidden, mask, 1, 3)
	want := []float32{1.0, 2.0, 3.0}
	assertFloat32Slice(t, "single token", got, want, 1e-6)
}

func TestMeanPool_MultipleTokens(t *testing.T) {
	// Two tokens, 3 dims — should average across the sequence dimension.
	hidden := []float32{
		2.0, 4.0, 6.0, // token 0
		4.0, 8.0, 2.0, // token 1
	}
	mask := []int64{1, 1}
	got := meanPool(hidden, mask, 2, 3)
	want := []float32{3.0, 6.0, 4.0}
	assertFloat32Slice(t, "two tokens", got, want, 1e-6)
}

func TestMeanPool_MaskedTokensExcluded(t *testing.T) {
	// Three tokens but the last two are masked out (padding).
	hidden := []float32{
		1.0, 2.0, // token 0 (active)
		9.0, 9.0, // token 1 (masked — should be ignored)
		9.0, 9.0, // token 2 (masked — should be ignored)
	}
	mask := []int64{1, 0, 0}
	got := meanPool(hidden, mask, 3, 2)
	want := []float32{1.0, 2.0}
	assertFloat32Slice(t, "masked tokens", got, want, 1e-6)
}

func TestMeanPool_PartialMask(t *testing.T) {
	// 4 tokens, only first two are active.
	hidden := []float32{
		2.0, 4.0, 6.0, // token 0 (active)
		6.0, 8.0, 2.0, // token 1 (active)
		0.0, 0.0, 0.0, // token 2 (masked)
		0.0, 0.0, 0.0, // token 3 (masked)
	}
	mask := []int64{1, 1, 0, 0}
	got := meanPool(hidden, mask, 4, 3)
	want := []float32{4.0, 6.0, 4.0}
	assertFloat32Slice(t, "partial mask", got, want, 1e-6)
}

func TestMeanPool_AllMasked(t *testing.T) {
	// All tokens masked — result should be all zeros (no division by zero).
	hidden := []float32{9.0, 9.0, 9.0}
	mask := []int64{0}
	got := meanPool(hidden, mask, 1, 3)
	want := []float32{0.0, 0.0, 0.0}
	assertFloat32Slice(t, "all masked", got, want, 1e-6)
}

func TestMeanPool_ZeroSeqLen(t *testing.T) {
	// Empty sequence — should return zero vector.
	got := meanPool(nil, nil, 0, 4)
	want := []float32{0.0, 0.0, 0.0, 0.0}
	assertFloat32Slice(t, "zero seqLen", got, want, 1e-6)
}

func TestMeanPool_SingleDimension(t *testing.T) {
	// Multiple tokens, 1 dim — degenerate but valid.
	hidden := []float32{2.0, 6.0, 10.0}
	mask := []int64{1, 1, 1}
	got := meanPool(hidden, mask, 3, 1)
	want := []float32{6.0}
	assertFloat32Slice(t, "single dim", got, want, 1e-6)
}

// ---------------------------------------------------------------------------
// l2Normalize
// ---------------------------------------------------------------------------

func TestL2Normalize_UnitVector(t *testing.T) {
	// Already unit length — should be unchanged.
	v := []float32{1.0, 0.0, 0.0}
	l2Normalize(v)
	assertFloat32Slice(t, "unit vector", v, []float32{1.0, 0.0, 0.0}, 1e-6)
}

func TestL2Normalize_ScalesDown(t *testing.T) {
	// [3, 4] has L2 norm = 5 → normalized = [0.6, 0.8].
	v := []float32{3.0, 4.0}
	l2Normalize(v)
	assertFloat32Slice(t, "scale down", v, []float32{0.6, 0.8}, 1e-6)
}

func TestL2Normalize_ResultHasUnitNorm(t *testing.T) {
	v := []float32{1.0, 2.0, 3.0, 4.0, 5.0}
	l2Normalize(v)

	var norm float64
	for _, f := range v {
		norm += float64(f) * float64(f)
	}
	norm = math.Sqrt(norm)

	if math.Abs(norm-1.0) > 1e-5 {
		t.Errorf("norm after l2Normalize = %v, want ~1.0", norm)
	}
}

func TestL2Normalize_NegativeValues(t *testing.T) {
	v := []float32{-3.0, 4.0}
	l2Normalize(v)
	assertFloat32Slice(t, "negative values", v, []float32{-0.6, 0.8}, 1e-6)
}

func TestL2Normalize_ZeroVector(t *testing.T) {
	// Zero vector — should stay zero (no division by zero / NaN).
	v := []float32{0.0, 0.0, 0.0}
	l2Normalize(v)
	for i, f := range v {
		if f != 0.0 || math.IsNaN(float64(f)) {
			t.Errorf("l2Normalize(zero)[%d] = %v, want 0.0", i, f)
		}
	}
}

func TestL2Normalize_Empty(t *testing.T) {
	// Empty slice — should not panic.
	v := []float32{}
	l2Normalize(v)
	if len(v) != 0 {
		t.Errorf("expected empty slice, got len %d", len(v))
	}
}

func TestL2Normalize_VerySmallValues(t *testing.T) {
	// Near-zero but non-zero values — should still normalize to unit length.
	v := []float32{1e-20, 1e-20}
	l2Normalize(v)

	var norm float64
	for _, f := range v {
		norm += float64(f) * float64(f)
	}
	norm = math.Sqrt(norm)

	if math.Abs(norm-1.0) > 1e-5 {
		t.Errorf("norm of tiny values after l2Normalize = %v, want ~1.0", norm)
	}
}

// ---------------------------------------------------------------------------
// meanPool + l2Normalize integration
// ---------------------------------------------------------------------------

func TestMeanPoolThenNormalize(t *testing.T) {
	// Simulates the embedSingle pipeline: mean pool then L2 normalize.
	hidden := []float32{
		3.0, 0.0, 4.0, // token 0
		5.0, 0.0, 0.0, // token 1
	}
	mask := []int64{1, 1}
	pooled := meanPool(hidden, mask, 2, 3)
	// pooled = [4.0, 0.0, 2.0], norm = sqrt(16+0+4) = sqrt(20)
	l2Normalize(pooled)

	norm20 := float32(math.Sqrt(20.0))
	want := []float32{4.0 / norm20, 0.0, 2.0 / norm20}
	assertFloat32Slice(t, "pool+normalize", pooled, want, 1e-6)

	// Verify unit length.
	var norm float64
	for _, f := range pooled {
		norm += float64(f) * float64(f)
	}
	norm = math.Sqrt(norm)
	if math.Abs(norm-1.0) > 1e-5 {
		t.Errorf("norm after pool+normalize = %v, want ~1.0", norm)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assertFloat32Slice(t *testing.T, label string, got, want []float32, tol float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: len(got)=%d, len(want)=%d", label, len(got), len(want))
	}
	for i := range want {
		if math.Abs(float64(got[i]-want[i])) > tol {
			t.Errorf("%s: [%d] = %v, want %v", label, i, got[i], want[i])
		}
	}
}
