package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/judwhiteneck/qoe-test-harness/core/compute"
	"github.com/judwhiteneck/qoe-test-harness/core/report"
	"github.com/judwhiteneck/qoe-test-harness/storage"
)

func report1(id, isp, verdict string, llVals, clVals []float64) report.RunReport {
	ll := compute.NewDefaultHistogram()
	cl := compute.NewDefaultHistogram()
	for _, v := range llVals {
		ll.Observe(v)
	}
	for _, v := range clVals {
		cl.Observe(v)
	}
	return report.RunReport{
		Schema: report.SchemaVersion,
		Meta:   report.Meta{RunID: id, StartedAtUnixMs: 1_700_000_000_000, ISP: isp, DeviceClass: "cli"},
		Result: compute.Result{Version: compute.ResultVersion, Verdict: compute.Verdict(verdict)},
		Telemetry: report.Telemetry{
			BaseRTTms: 20, MarkingSurvival: 0.99, DownLL: ll, DownClassic: cl,
		},
	}
}

func newSrv() http.Handler { return New(storage.NewMemory()).Handler() }

func TestIngestThenList(t *testing.T) {
	h := newSrv()

	body, _ := json.Marshal(report1("r1", "Acme", "pass", []float64{0.5, 1, 2}, []float64{40, 60, 120}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ingest", bytes.NewReader(body)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("ingest = %d, want 204 (%s)", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("runs = %d", rec.Code)
	}
	var got struct {
		Runs []map[string]any `json:"runs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Runs) != 1 || got.Runs[0]["run_id"] != "r1" {
		t.Fatalf("runs = %+v", got.Runs)
	}
}

func TestIngestRejectsBad(t *testing.T) {
	h := newSrv()
	// Wrong method.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ingest", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /ingest = %d, want 405", rec.Code)
	}
	// Invalid report (no verdict/run_id).
	bad, _ := json.Marshal(report.RunReport{Schema: report.SchemaVersion})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ingest", bytes.NewReader(bad)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid report = %d, want 400", rec.Code)
	}
}

func TestCDFMergesCohortAndFilters(t *testing.T) {
	h := newSrv()
	post := func(r report.RunReport) {
		body, _ := json.Marshal(r)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ingest", bytes.NewReader(body)))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("ingest %s = %d", r.Meta.RunID, rec.Code)
		}
	}
	post(report1("a", "Acme", "pass", []float64{1, 1}, []float64{50}))
	post(report1("b", "Acme", "pass", []float64{2, 2}, []float64{60}))
	post(report1("c", "Beta", "fail", []float64{3}, []float64{70}))

	get := func(qs string) cdfResponse {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/cdf"+qs, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("cdf %q = %d", qs, rec.Code)
		}
		var resp cdfResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}

	all := get("")
	if all.Runs != 3 || all.SamplesLL != 5 || all.SamplesCl != 3 {
		t.Fatalf("all: runs=%d ll=%d cl=%d, want 3/5/3", all.Runs, all.SamplesLL, all.SamplesCl)
	}
	if n := len(all.LL); n == 0 || all.LL[n-1].P != 1.0 {
		t.Fatalf("merged LL CDF not normalized: %v", all.LL)
	}

	acme := get("?isp=Acme")
	if acme.Runs != 2 || acme.SamplesLL != 4 {
		t.Fatalf("acme filter: runs=%d ll=%d, want 2/4", acme.Runs, acme.SamplesLL)
	}
}

func TestIndexServesHTML(t *testing.T) {
	h := newSrv()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("index = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "Engineer View") {
		t.Error("index missing expected title")
	}
}
