package compute

import "fmt"

// Thresholds are the calibrated pass/fail boundaries. Defaults come from the L4S
// targets; M0's negative control calibrates them against known LLD-off/on lines
// (see docs/m0-spec.md S7).
type Thresholds struct {
	LLP99MaxMs            float64 // LL probe p99 working-delta must be below this
	ClassicP99MinMs       float64 // classic probe p99 must exceed this (proves separation)
	MarkingSurvivalMin    float64 // fraction of marks that must survive
	NoHarmThroughputMin   float64 // classic-with-LL throughput as fraction of classic-solo
	NoHarmLatencyMarginMs float64 // max allowed classic p99 increase when LL is present
}

// DefaultThresholds returns the spec defaults (pre-calibration).
func DefaultThresholds() Thresholds {
	return Thresholds{
		LLP99MaxMs:            10,
		ClassicP99MinMs:       50,
		MarkingSurvivalMin:    0.99,
		NoHarmThroughputMin:   0.90,
		NoHarmLatencyMarginMs: 5,
	}
}

// DirectionInputs holds one direction's loaded-phase probe histograms.
type DirectionInputs struct {
	LL      *Histogram
	Classic *Histogram
}

// Inputs is everything the verdict needs, already reduced from raw samples. The
// validity gates encode the autoplan findings: if the measurement cannot be
// trusted, the verdict is Inconclusive regardless of the latency numbers.
type Inputs struct {
	// Validity gates.
	WrongLink       bool   // not on the expected wired LLD line (cellular/VPN/wrong ASN)
	WrongLinkReason string // optional human reason
	CapacityOK      bool   // delivered throughput reached the provisioned tier (bottleneck localized)
	StandingQueueOK bool   // overshoot built a sustained standing queue
	BaselineDrifted bool   // idle baseline moved during the run
	NoHarmNoisy     bool   // A/B/A variance exceeded the harm margin

	MarkingSurvival float64 // 0..1

	Down DirectionInputs
	Up   DirectionInputs

	// No-harm A/B/A reductions.
	ClassicSoloThroughputBps   float64
	ClassicWithLLThroughputBps float64
	ClassicSoloP99Ms           float64
	ClassicWithLLP99Ms         float64
}

// Evaluate applies the thresholds to the inputs and returns the Result. Validity
// gates take precedence: any untrustworthy condition yields Inconclusive with a
// caveat and no pass/fail sub-results.
func Evaluate(in Inputs, th Thresholds) Result {
	r := Result{Version: ResultVersion}

	var inc []string
	if in.WrongLink {
		inc = append(inc, orDefault(in.WrongLinkReason, "test not run over the expected wired LLD line"))
	}
	if !in.CapacityOK {
		inc = append(inc, "capacity gate failed: could not reach the provisioned tier, so the access link may not be the bottleneck")
	}
	if !in.StandingQueueOK {
		inc = append(inc, "no sustained standing queue formed under load")
	}
	if in.BaselineDrifted {
		inc = append(inc, "idle baseline drifted during the run")
	}
	if in.NoHarmNoisy {
		inc = append(inc, "no-harm legs too noisy to judge")
	}
	if len(inc) > 0 {
		r.Verdict = Inconclusive
		r.Caveats = inc
		return r
	}

	dStatus, dDetail := evalDirection("downstream", in.Down, th)
	uStatus, uDetail := evalDirection("upstream", in.Up, th)
	working := SubResult{
		Name:   "working",
		Status: bothPass(dStatus, uStatus),
		Detail: dDetail + "; " + uDetail,
	}

	marking := SubResult{Name: "marking_survival", Status: Fail}
	if in.MarkingSurvival >= th.MarkingSurvivalMin {
		marking.Status = Pass
	}
	marking.Detail = fmt.Sprintf("%.2f%% survived (min %.0f%%)",
		in.MarkingSurvival*100, th.MarkingSurvivalMin*100)

	thrRatio := 0.0
	if in.ClassicSoloThroughputBps > 0 {
		thrRatio = in.ClassicWithLLThroughputBps / in.ClassicSoloThroughputBps
	}
	latWorse := in.ClassicWithLLP99Ms - in.ClassicSoloP99Ms
	noharm := SubResult{Name: "no_harm", Status: Fail}
	if thrRatio >= th.NoHarmThroughputMin && latWorse <= th.NoHarmLatencyMarginMs {
		noharm.Status = Pass
	}
	noharm.Detail = fmt.Sprintf("classic throughput %.0f%% of solo (min %.0f%%), latency +%.1fms (max +%.1fms)",
		thrRatio*100, th.NoHarmThroughputMin*100, latWorse, th.NoHarmLatencyMarginMs)

	r.SubResults = []SubResult{working, marking, noharm}
	r.Verdict = allPass(working.Status, marking.Status, noharm.Status)
	return r
}

// evalDirection returns Pass only when the LL probe stayed low AND the classic
// probe inflated under the same load — the differentiation that proves the LL
// queue engaged.
func evalDirection(name string, d DirectionInputs, th Thresholds) (Verdict, string) {
	if d.LL == nil || d.Classic == nil || d.LL.Total() == 0 || d.Classic.Total() == 0 {
		return Fail, name + ": missing samples"
	}
	llP99 := d.LL.Quantile(0.99)
	clP99 := d.Classic.Quantile(0.99)
	status := Fail
	if llP99 < th.LLP99MaxMs && clP99 > th.ClassicP99MinMs {
		status = Pass
	}
	return status, fmt.Sprintf("%s LL p99 %.1fms (<%.0f), classic p99 %.1fms (>%.0f)",
		name, llP99, th.LLP99MaxMs, clP99, th.ClassicP99MinMs)
}

func bothPass(a, b Verdict) Verdict {
	if a == Pass && b == Pass {
		return Pass
	}
	return Fail
}

func allPass(vs ...Verdict) Verdict {
	for _, v := range vs {
		if v != Pass {
			return Fail
		}
	}
	return Pass
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
