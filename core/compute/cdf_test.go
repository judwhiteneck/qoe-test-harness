package compute

import "testing"

func TestCDFEmpty(t *testing.T) {
	if pts := NewDefaultHistogram().CDF(); pts != nil {
		t.Fatalf("empty CDF = %v, want nil", pts)
	}
}

func TestCDFMonotoneAndNormalized(t *testing.T) {
	h := NewDefaultHistogram()
	for _, v := range []float64{0.3, 0.7, 1.2, 4, 4, 9, 60, 2000} {
		h.Observe(v)
	}
	pts := h.CDF()
	if len(pts) != len(h.Counts) {
		t.Fatalf("CDF len = %d, want %d", len(pts), len(h.Counts))
	}
	last := 0.0
	for i, p := range pts {
		if p.P < last-1e-9 {
			t.Fatalf("CDF not monotone at %d: %f < %f", i, p.P, last)
		}
		if p.P < 0 || p.P > 1+1e-9 {
			t.Fatalf("CDF out of range at %d: %f", i, p.P)
		}
		last = p.P
	}
	if got := pts[len(pts)-1].P; got != 1.0 {
		t.Fatalf("final CDF P = %f, want 1.0", got)
	}
}

func TestCDFMatchesMergeThenCDF(t *testing.T) {
	// Aggregation invariant: merging two testers then taking the CDF equals the CDF
	// of the combined sample set. This is why fixed-edge histograms are used.
	a := NewDefaultHistogram()
	b := NewDefaultHistogram()
	for _, v := range []float64{1, 2, 3} {
		a.Observe(v)
	}
	for _, v := range []float64{1, 50, 50} {
		b.Observe(v)
	}
	if err := a.Merge(b); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got := a.CDF()[len(a.CDF())-1].P; got != 1.0 {
		t.Fatalf("merged final P = %f, want 1.0", got)
	}
	if a.Total() != 6 {
		t.Fatalf("merged total = %d, want 6", a.Total())
	}
}
