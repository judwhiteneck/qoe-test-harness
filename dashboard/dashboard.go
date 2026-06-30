// Package dashboard is the engineer view: an HTTP ingest endpoint for submitted
// run reports and a read API that merges per-tester histograms into fleet-wide
// LL-vs-classic CDF overlays with cohort filters. It is storage-agnostic (any
// storage.Store) so it is fully testable with the in-memory store via httptest.
package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/judwhiteneck/qoe-test-harness/core/compute"
	"github.com/judwhiteneck/qoe-test-harness/core/report"
	"github.com/judwhiteneck/qoe-test-harness/storage"
)

// Server serves the engineer view over a Store.
type Server struct{ store storage.Store }

// New returns a dashboard over store.
func New(store storage.Store) *Server { return &Server{store: store} }

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ingest", s.ingest)
	mux.HandleFunc("/api/runs", s.apiRuns)
	mux.HandleFunc("/api/cdf", s.apiCDF)
	mux.HandleFunc("/", s.index)
	return mux
}

// ingest accepts a POSTed RunReport and stores it after validation.
func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var rep report.RunReport
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&rep); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !rep.Valid() {
		http.Error(w, "invalid report: need schema, run_id, verdict", http.StatusBadRequest)
		return
	}
	if err := s.store.Save(r.Context(), rep); err != nil {
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func filterFromQuery(r *http.Request) storage.Filter {
	q := r.URL.Query()
	f := storage.Filter{
		ISP:         q.Get("isp"),
		Region:      q.Get("region"),
		DeviceClass: q.Get("device"),
		Verdict:     q.Get("verdict"),
	}
	if v, err := strconv.ParseInt(q.Get("since"), 10, 64); err == nil {
		f.SinceUnixMs = v
	}
	if v, err := strconv.Atoi(q.Get("limit")); err == nil {
		f.Limit = v
	}
	return f
}

// runRow is the trimmed per-run view for the table.
type runRow struct {
	RunID           string  `json:"run_id"`
	StartedAtUnixMs int64   `json:"started_at_unix_ms"`
	ISP             string  `json:"isp"`
	Region          string  `json:"region"`
	DeviceClass     string  `json:"device_class"`
	Verdict         string  `json:"verdict"`
	BaseRTTms       float64 `json:"base_rtt_ms"`
	MarkingSurvival float64 `json:"marking_survival"`
}

func (s *Server) apiRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.List(r.Context(), filterFromQuery(r))
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	rows := make([]runRow, 0, len(runs))
	for _, run := range runs {
		rows = append(rows, runRow{
			RunID:           run.Meta.RunID,
			StartedAtUnixMs: run.Meta.StartedAtUnixMs,
			ISP:             run.Meta.ISP,
			Region:          run.Meta.Region,
			DeviceClass:     run.Meta.DeviceClass,
			Verdict:         string(run.Result.Verdict),
			BaseRTTms:       run.Telemetry.BaseRTTms,
			MarkingSurvival: run.Telemetry.MarkingSurvival,
		})
	}
	writeJSON(w, map[string]any{"runs": rows})
}

// cdfResponse is the merged LL-vs-classic overlay for the cohort.
type cdfResponse struct {
	Runs      int                `json:"runs"`
	SamplesLL uint64             `json:"samples_ll"`
	SamplesCl uint64             `json:"samples_classic"`
	LL        []compute.CDFPoint `json:"ll"`
	Classic   []compute.CDFPoint `json:"classic"`
}

func (s *Server) apiCDF(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.List(r.Context(), filterFromQuery(r))
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	ll := compute.NewDefaultHistogram()
	cl := compute.NewDefaultHistogram()
	for _, run := range runs {
		// Merge ignores histograms with mismatched edges; all testers use the
		// default edge set, so this aggregates the whole cohort.
		if run.Telemetry.DownLL != nil {
			_ = ll.Merge(run.Telemetry.DownLL)
		}
		if run.Telemetry.DownClassic != nil {
			_ = cl.Merge(run.Telemetry.DownClassic)
		}
	}
	writeJSON(w, cdfResponse{
		Runs:      len(runs),
		SamplesLL: ll.Total(),
		SamplesCl: cl.Total(),
		LL:        ll.CDF(),
		Classic:   cl.CDF(),
	})
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
