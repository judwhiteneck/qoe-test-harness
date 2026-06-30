package compute

// CDFPoint is one step of a cumulative distribution: the fraction P of samples at
// or below Ms milliseconds. The overflow bin is reported at the last edge with
// P = 1.0 (it has no upper bound to plot).
type CDFPoint struct {
	Ms float64 `json:"ms"`
	P  float64 `json:"p"`
}

// CDF returns the cumulative distribution as step points keyed on each bin's
// upper edge. It is the engineer view's primitive: merge per-tester histograms
// (same edges) into one, then CDF the result to overlay LL vs classic across a
// cohort. Empty histograms return nil.
func (h *Histogram) CDF() []CDFPoint {
	total := h.Total()
	if total == 0 {
		return nil
	}
	pts := make([]CDFPoint, 0, len(h.Counts))
	var cum uint64
	for i, c := range h.Counts {
		cum += c
		ms := h.Edges[i]
		if i+1 < len(h.Edges) {
			ms = h.Edges[i+1] // step rises at the bin's upper edge
		}
		pts = append(pts, CDFPoint{Ms: ms, P: float64(cum) / float64(total)})
	}
	return pts
}
