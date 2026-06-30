// Package compute holds the pure measurement math: histograms, statistics, and
// the verdict. It has no I/O, no clock, and no network dependency by design (see
// docs/ARCHITECTURE.md and docs/ENGINEERING.md) so the numbers that drive a
// rollout decision can be tested exhaustively with plain values.
package compute

import "errors"

// DefaultEdgesMs are the fixed histogram lower-bin edges in milliseconds, dense
// below 30ms. Bin i covers [Edges[i], Edges[i+1]); the final bin covers
// [Edges[last], +Inf). 10 and 30 are edges so the pass/fail boundary lands on a
// bin edge. Histograms with identical edges are mergeable (sum the counts), which
// is how fleet-wide distributions are aggregated — you cannot average percentiles.
var DefaultEdgesMs = []float64{
	0, 0.5, 1, 1.5, 2, 3, 4, 5, 7.5, 10, 12.5, 15, 20, 25, 30, 40, 50, 75, 100, 150, 250, 500, 1000,
}

// ErrEdgesMismatch is returned when merging histograms with different edges.
var ErrEdgesMismatch = errors.New("compute: histogram edges differ; not mergeable")

// Histogram is a fixed-bin-edge histogram over latency values in milliseconds.
// Counts has the same length as Edges.
type Histogram struct {
	Edges  []float64 `json:"edges_ms"`
	Counts []uint64  `json:"counts"`
}

// NewHistogram returns a histogram over a copy of edges. Edges must be ascending
// and non-empty with edges[0] == 0; callers control the edge set so this is not
// re-validated on the hot path.
func NewHistogram(edges []float64) *Histogram {
	e := make([]float64, len(edges))
	copy(e, edges)
	return &Histogram{Edges: e, Counts: make([]uint64, len(edges))}
}

// NewDefaultHistogram returns a histogram over DefaultEdgesMs.
func NewDefaultHistogram() *Histogram { return NewHistogram(DefaultEdgesMs) }

// binIndex returns the bin for v via binary search. Values below Edges[0] clamp
// to bin 0.
func (h *Histogram) binIndex(v float64) int {
	lo, hi := 0, len(h.Edges)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if h.Edges[mid] <= v {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == 0 {
		return 0
	}
	return lo - 1
}

// Observe records one value in milliseconds.
func (h *Histogram) Observe(vMs float64) { h.Counts[h.binIndex(vMs)]++ }

// Total returns the number of observed samples.
func (h *Histogram) Total() uint64 {
	var t uint64
	for _, c := range h.Counts {
		t += c
	}
	return t
}

// Merge adds other's counts into h. Returns ErrEdgesMismatch if the edge sets
// differ. This is the fleet-aggregation primitive.
func (h *Histogram) Merge(other *Histogram) error {
	if !equalEdges(h.Edges, other.Edges) {
		return ErrEdgesMismatch
	}
	for i, c := range other.Counts {
		h.Counts[i] += c
	}
	return nil
}

// Quantile returns a histogram-approximate value v such that ~p of samples are
// <= v, with p in [0,1]. It linearly interpolates within the containing bin; the
// overflow bin returns its lower edge (no upper bound). Returns 0 for an empty
// histogram. The estimate is bounded by the bin width, which is sub-millisecond
// in the region that decides pass/fail.
func (h *Histogram) Quantile(p float64) float64 {
	total := h.Total()
	if total == 0 {
		return 0
	}
	switch {
	case p < 0:
		p = 0
	case p > 1:
		p = 1
	}
	target := p * float64(total)
	var cum float64
	for i, c := range h.Counts {
		if c == 0 {
			continue
		}
		prev := cum
		cum += float64(c)
		if cum >= target {
			lo := h.Edges[i]
			if i == len(h.Edges)-1 {
				return lo // overflow bin: no upper edge to interpolate to
			}
			frac := (target - prev) / float64(c)
			switch {
			case frac < 0:
				frac = 0
			case frac > 1:
				frac = 1
			}
			return lo + frac*(h.Edges[i+1]-lo)
		}
	}
	return h.Edges[len(h.Edges)-1]
}

func equalEdges(a, b []float64) bool {
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
