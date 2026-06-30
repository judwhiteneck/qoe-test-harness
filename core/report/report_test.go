package report

import (
	"encoding/json"
	"testing"

	"github.com/judwhiteneck/qoe-test-harness/core/compute"
)

func sample() RunReport {
	ll := compute.NewDefaultHistogram()
	cl := compute.NewDefaultHistogram()
	for _, v := range []float64{0.5, 1, 2} {
		ll.Observe(v)
	}
	for _, v := range []float64{40, 60, 120} {
		cl.Observe(v)
	}
	return RunReport{
		Schema: SchemaVersion,
		Meta: Meta{
			RunID:           "run-1",
			StartedAtUnixMs: 1_700_000_000_000,
			ISP:             "Acme",
			Region:          "us-west",
			DeviceClass:     "cli",
		},
		Result: compute.Result{Version: compute.ResultVersion, Verdict: compute.Pass},
		Telemetry: Telemetry{
			BaseRTTms:       20,
			MarkingSurvival: 0.99,
			DownLL:          ll,
			DownClassic:     cl,
		},
	}
}

func TestRunReportRoundTrip(t *testing.T) {
	in := sample()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out RunReport
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Meta.RunID != in.Meta.RunID || out.Result.Verdict != in.Result.Verdict {
		t.Fatalf("scalar mismatch: got %+v", out)
	}
	if out.Telemetry.DownLL.Total() != 3 || out.Telemetry.DownClassic.Total() != 3 {
		t.Fatalf("histogram totals lost across JSON: %+v", out.Telemetry)
	}
	// The CDF must survive the round trip (it is what the dashboard renders).
	if got := out.Telemetry.DownClassic.CDF(); len(got) == 0 || got[len(got)-1].P != 1.0 {
		t.Fatalf("CDF lost across round trip: %v", got)
	}
}

func TestRunReportValid(t *testing.T) {
	if !sample().Valid() {
		t.Error("sample should be valid")
	}
	bad := sample()
	bad.Meta.RunID = ""
	if bad.Valid() {
		t.Error("missing run id should be invalid")
	}
	bad = sample()
	bad.Result.Verdict = ""
	if bad.Valid() {
		t.Error("missing verdict should be invalid")
	}
	bad = sample()
	bad.Schema = 999
	if bad.Valid() {
		t.Error("unknown schema should be invalid")
	}
}
