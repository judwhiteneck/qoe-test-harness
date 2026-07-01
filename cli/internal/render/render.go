// Package render turns a RunReport into the two role-switched views the spec
// calls for: a field technician sees a clean pass/fail checklist with plain-
// language guidance; an engineer sees the full telemetry. Keeping this out of
// main makes the wording testable.
package render

import (
	"fmt"
	"strings"

	"github.com/judwhiteneck/qoe-test-harness/core/compute"
	"github.com/judwhiteneck/qoe-test-harness/core/report"
)

// friendlyCheck maps a sub-result name to the field-facing label.
var friendlyCheck = map[string]string{
	"working":          "Low latency is working",
	"marking_survival": "Low-latency tags survived the network",
	"no_harm":          "Classic traffic is unharmed",
}

// fieldOrder fixes the checklist order regardless of map iteration.
var fieldOrder = []string{"working", "marking_survival", "no_harm"}

// friendlyCaveat rewrites engine caveats into action-oriented field guidance.
// Unknown caveats pass through unchanged.
var friendlyCaveat = map[string]string{
	"capacity gate failed: could not reach the provisioned tier, so the access link may not be the bottleneck": "Couldn't confirm your line is the bottleneck — connect wired to the modem and close other downloads, then retry.",
	"no sustained standing queue formed under load":                                                            "The test couldn't load the link hard enough to judge — make sure you're wired directly to the modem, then retry.",
}

func glyph(v compute.Verdict) string {
	switch v {
	case compute.Pass:
		return "✓"
	case compute.Fail:
		return "✗"
	default:
		return "—"
	}
}

func mbps(bps uint64) float64 { return float64(bps) / 1e6 }

func hasCapacityCaveat(r compute.Result) bool {
	for _, c := range r.Caveats {
		if strings.Contains(c, "capacity gate failed") {
			return true
		}
	}
	return false
}

// Field renders the technician view: a checklist and a single headline result.
func Field(rr report.RunReport) string {
	var b strings.Builder
	res := rr.Result
	subs := map[string]compute.Verdict{}
	for _, s := range res.SubResults {
		subs[s.Name] = s.Status
	}

	fmt.Fprintf(&b, "LLD / L4S validation\n")

	// Path-clear line: the bottleneck-localization gate, in plain terms.
	pathGlyph := "✓"
	if hasCapacityCaveat(res) {
		pathGlyph = "✗"
	}
	if tier := rr.Meta.ProvisionedDownBps; tier > 0 {
		fmt.Fprintf(&b, "  %s  Path clear (your line is the bottleneck): %.0f / %.0f Mbps\n",
			pathGlyph, mbps(rr.Telemetry.CapacityAchievedBps), mbps(tier))
	} else {
		fmt.Fprintf(&b, "  %s  Path clear (your line is the bottleneck)\n", pathGlyph)
	}

	for _, name := range fieldOrder {
		v, ok := subs[name]
		if !ok {
			v = compute.Inconclusive // not determined this run
		}
		fmt.Fprintf(&b, "  %s  %s\n", glyph(v), friendlyCheck[name])
	}

	fmt.Fprintf(&b, "  ──────────────────────────────\n")
	fmt.Fprintf(&b, "  RESULT: %s\n", strings.ToUpper(string(res.Verdict)))

	for _, c := range res.Caveats {
		msg := c
		if f, ok := friendlyCaveat[c]; ok {
			msg = f
		}
		fmt.Fprintf(&b, "    • %s\n", msg)
	}
	return b.String()
}

// Engineer renders the full telemetry view.
func Engineer(rr report.RunReport) string {
	var b strings.Builder
	res := rr.Result
	t := rr.Telemetry

	fmt.Fprintf(&b, "LLD / L4S validation — engineer view\n")
	fmt.Fprintf(&b, "  run:                 %s  (%s/%s)\n", rr.Meta.RunID, orDash(rr.Meta.ISP), orDash(rr.Meta.Region))
	fmt.Fprintf(&b, "  tiers:               %.0f down / %.0f up Mbps\n", mbps(rr.Meta.ProvisionedDownBps), mbps(rr.Meta.ProvisionedUpBps))
	fmt.Fprintf(&b, "  base RTT:            %.2f ms\n", t.BaseRTTms)
	fmt.Fprintf(&b, "  marking survival:    %.1f%%\n", t.MarkingSurvival*100)
	fmt.Fprintf(&b, "  capacity achieved:   %.1f / %.0f Mbps\n", mbps(t.CapacityAchievedBps), mbps(rr.Meta.ProvisionedDownBps))
	fmt.Fprintf(&b, "  overshoot achieved:  %.1f Mbps\n", mbps(t.OvershootAchievedBps))

	if t.DownLL != nil && t.DownClassic != nil {
		fmt.Fprintf(&b, "  working-delta (downstream), ms:\n")
		fmt.Fprintf(&b, "    %-8s %8s %8s %8s %8s\n", "", "p50", "p90", "p99", "p99.9")
		writePcts(&b, "    low-lat", t.DownLL)
		writePcts(&b, "    classic", t.DownClassic)
	}

	fmt.Fprintf(&b, "  VERDICT: %s\n", strings.ToUpper(string(res.Verdict)))
	for _, s := range res.SubResults {
		fmt.Fprintf(&b, "    - %-16s %-12s %s\n", s.Name, s.Status, s.Detail)
	}
	for _, c := range res.Caveats {
		fmt.Fprintf(&b, "    caveat: %s\n", c)
	}
	return b.String()
}

func writePcts(b *strings.Builder, label string, h *compute.Histogram) {
	fmt.Fprintf(b, "%-8s %8.2f %8.2f %8.2f %8.2f\n",
		label, h.Quantile(0.50), h.Quantile(0.90), h.Quantile(0.99), h.Quantile(0.999))
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
