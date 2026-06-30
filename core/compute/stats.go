package compute

import "sort"

// BaseRTT returns the pth percentile (p in [0,1]) of raw RTT samples in ms — the
// idle-latency floor used as the baseline. Use a low percentile (p1/p5) rather
// than the raw min, which is too sensitive to a single lucky sample (see
// docs/spec.md §Measurement). Returns 0 for no samples. Does not mutate input.
func BaseRTT(samplesMs []float64, p float64) float64 {
	n := len(samplesMs)
	if n == 0 {
		return 0
	}
	s := make([]float64, n)
	copy(s, samplesMs)
	sort.Float64s(s)
	switch {
	case p <= 0:
		return s[0]
	case p >= 1:
		return s[n-1]
	}
	idx := p * float64(n-1)
	lo := int(idx)
	frac := idx - float64(lo)
	if lo+1 < n {
		return s[lo] + frac*(s[lo+1]-s[lo])
	}
	return s[lo]
}

// WorkingDelta is the latency added over the idle baseline, clamped at 0. A
// negative raw delta is sampling/clock noise, not negative queuing.
func WorkingDelta(rttMs, baseRTTMs float64) float64 {
	if d := rttMs - baseRTTMs; d > 0 {
		return d
	}
	return 0
}
