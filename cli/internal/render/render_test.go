package render

import (
	"strings"
	"testing"

	"github.com/judwhiteneck/qoe-test-harness/core/compute"
	"github.com/judwhiteneck/qoe-test-harness/core/report"
)

func passReport() report.RunReport {
	ll := compute.NewDefaultHistogram()
	cl := compute.NewDefaultHistogram()
	for _, v := range []float64{1, 2, 3} {
		ll.Observe(v)
	}
	for _, v := range []float64{40, 60, 120} {
		cl.Observe(v)
	}
	return report.RunReport{
		Schema: report.SchemaVersion,
		Meta:   report.Meta{RunID: "r1", ISP: "Acme", Region: "west", ProvisionedDownBps: 500_000_000},
		Result: compute.Result{
			Version: compute.ResultVersion,
			Verdict: compute.Pass,
			SubResults: []compute.SubResult{
				{Name: "working", Status: compute.Pass},
				{Name: "marking_survival", Status: compute.Pass},
				{Name: "no_harm", Status: compute.Pass},
			},
		},
		Telemetry: report.Telemetry{
			BaseRTTms: 20, MarkingSurvival: 0.995,
			CapacityAchievedBps: 487_000_000, DownLL: ll, DownClassic: cl,
		},
	}
}

func TestFieldPassIsCleanAndHidesTelemetry(t *testing.T) {
	out := Field(passReport())
	if !strings.Contains(out, "RESULT: PASS") {
		t.Errorf("missing headline result:\n%s", out)
	}
	if strings.Count(out, "✓") < 4 { // path-clear + 3 checks
		t.Errorf("expected 4 passing checks:\n%s", out)
	}
	if !strings.Contains(out, "Low latency is working") {
		t.Errorf("missing friendly label:\n%s", out)
	}
	// Field view must NOT leak engineer telemetry.
	for _, leak := range []string{"base RTT", "p99", "marking survival", "Mbps\n  overshoot"} {
		if strings.Contains(out, leak) {
			t.Errorf("field view leaked telemetry %q:\n%s", leak, out)
		}
	}
}

func TestFieldInconclusiveShowsGuidance(t *testing.T) {
	rr := passReport()
	rr.Result = compute.Result{
		Version: compute.ResultVersion,
		Verdict: compute.Inconclusive,
		Caveats: []string{"no sustained standing queue formed under load"},
	}
	out := Field(rr)
	if !strings.Contains(out, "RESULT: INCONCLUSIVE") {
		t.Errorf("missing headline:\n%s", out)
	}
	if strings.Contains(out, "no sustained standing queue") {
		t.Errorf("raw caveat should be rewritten for the field:\n%s", out)
	}
	if !strings.Contains(out, "wired directly to the modem") {
		t.Errorf("missing friendly guidance:\n%s", out)
	}
	// No sub-results -> all three checks render as the inconclusive glyph.
	if strings.Count(out, "—") < 3 {
		t.Errorf("expected 3 undetermined checks:\n%s", out)
	}
}

func TestFieldCapacityFailMarksPathNotClear(t *testing.T) {
	rr := passReport()
	rr.Result = compute.Result{
		Version: compute.ResultVersion,
		Verdict: compute.Inconclusive,
		Caveats: []string{"capacity gate failed: could not reach the provisioned tier, so the access link may not be the bottleneck"},
	}
	out := Field(rr)
	if !strings.Contains(out, "✗  Path clear") {
		t.Errorf("capacity failure should mark path not clear:\n%s", out)
	}
	if !strings.Contains(out, "connect wired to the modem") {
		t.Errorf("missing capacity guidance:\n%s", out)
	}
}

func TestEngineerShowsTelemetryAndPercentiles(t *testing.T) {
	out := Engineer(passReport())
	for _, want := range []string{"engineer view", "base RTT", "marking survival", "p99.9", "low-lat", "classic", "VERDICT: PASS"} {
		if !strings.Contains(out, want) {
			t.Errorf("engineer view missing %q:\n%s", want, out)
		}
	}
}
