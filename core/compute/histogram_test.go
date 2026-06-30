package compute

import (
	"math"
	"testing"
	"testing/quick"
)

func histFrom(edges []float64, samples ...float64) *Histogram {
	h := NewHistogram(edges)
	for _, s := range samples {
		h.Observe(s)
	}
	return h
}

func TestObserveAndTotal(t *testing.T) {
	h := NewDefaultHistogram()
	for _, v := range []float64{0, 0.4, 5, 9.9, 1000, 99999} {
		h.Observe(v)
	}
	if got := h.Total(); got != 6 {
		t.Fatalf("Total = %d, want 6", got)
	}
}

func TestBinPlacement(t *testing.T) {
	h := NewDefaultHistogram()
	cases := []struct {
		v   float64
		bin int
	}{
		{-5, 0},    // below first edge clamps to bin 0
		{0, 0},     // exactly first edge
		{0.4, 0},   // [0,0.5)
		{0.5, 1},   // exactly an edge -> next bin
		{5, 7},     // edge 5 is index 7
		{9.9, 8},   // [7.5,10)
		{10, 9},    // edge 10
		{2000, 22}, // overflow bin (last index)
	}
	for _, c := range cases {
		if got := h.binIndex(c.v); got != c.bin {
			t.Errorf("binIndex(%v) = %d, want %d", c.v, got, c.bin)
		}
	}
}

func TestQuantileEmpty(t *testing.T) {
	if got := NewDefaultHistogram().Quantile(0.99); got != 0 {
		t.Fatalf("empty Quantile = %v, want 0", got)
	}
}

func TestQuantileConcentrated(t *testing.T) {
	// All samples land in [5,7.5); p99 must be within that bin.
	h := NewDefaultHistogram()
	for i := 0; i < 1000; i++ {
		h.Observe(6)
	}
	q := h.Quantile(0.99)
	if q < 5 || q >= 7.5 {
		t.Fatalf("p99 = %v, want within [5,7.5)", q)
	}
}

func TestQuantileOverflowBin(t *testing.T) {
	h := NewDefaultHistogram()
	for i := 0; i < 100; i++ {
		h.Observe(5000) // overflow
	}
	if q := h.Quantile(0.99); q != 1000 {
		t.Fatalf("overflow p99 = %v, want 1000 (lower edge of overflow bin)", q)
	}
}

func TestQuantileMonotonic(t *testing.T) {
	h := NewDefaultHistogram()
	for _, v := range []float64{1, 2, 5, 8, 12, 40, 120, 600} {
		for i := 0; i < 50; i++ {
			h.Observe(v)
		}
	}
	prev := -1.0
	for p := 0.0; p <= 1.0; p += 0.05 {
		q := h.Quantile(p)
		if q < prev {
			t.Fatalf("Quantile not monotonic: p=%.2f gave %v after %v", p, q, prev)
		}
		prev = q
	}
}

// TestMergeEquivalence is the property the fleet rollup depends on: merging two
// histograms equals histogramming the concatenation. You cannot average
// percentiles across testers, so this must hold exactly.
func TestMergeEquivalence(t *testing.T) {
	f := func(a, b []float64) bool {
		clean := func(xs []float64) []float64 {
			out := make([]float64, 0, len(xs))
			for _, x := range xs {
				if math.IsNaN(x) || math.IsInf(x, 0) {
					continue
				}
				out = append(out, math.Abs(x))
			}
			return out
		}
		as, bs := clean(a), clean(b)
		h1 := histFrom(DefaultEdgesMs, as...)
		h2 := histFrom(DefaultEdgesMs, bs...)
		if err := h1.Merge(h2); err != nil {
			return false
		}
		combined := histFrom(DefaultEdgesMs, append(append([]float64{}, as...), bs...)...)
		return equalCounts(h1.Counts, combined.Counts)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestMergeEdgesMismatch(t *testing.T) {
	h1 := NewDefaultHistogram()
	h2 := NewHistogram([]float64{0, 1, 2})
	if err := h1.Merge(h2); err != ErrEdgesMismatch {
		t.Fatalf("Merge err = %v, want ErrEdgesMismatch", err)
	}
}

// TestTotalEqualsObservations is a property: Total always equals the number of
// Observe calls.
func TestTotalEqualsObservations(t *testing.T) {
	f := func(xs []float64) bool {
		h := NewDefaultHistogram()
		n := 0
		for _, x := range xs {
			if math.IsNaN(x) || math.IsInf(x, 0) {
				continue
			}
			h.Observe(math.Abs(x))
			n++
		}
		return h.Total() == uint64(n)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func equalCounts(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
