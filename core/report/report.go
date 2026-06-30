// Package report defines the run record submitted by a tester and stored for the
// engineer view: the verdict (compute.Result) plus the telemetry behind it
// (mergeable histograms + scalars) and cohort metadata. It is pure (imports only
// compute) so the wire/storage shape is defined and tested in one place, the way
// compute.Result is the one definition of the verdict.
package report

import "github.com/judwhiteneck/qoe-test-harness/core/compute"

// SchemaVersion is the version of the RunReport shape. Bump on breaking changes.
const SchemaVersion = 1

// Meta is the cohort/context for a run: who ran it, where, on what tier. These
// are the dashboard's filter dimensions.
type Meta struct {
	RunID              string `json:"run_id"`
	StartedAtUnixMs    int64  `json:"started_at_unix_ms"`
	ISP                string `json:"isp,omitempty"`
	Region             string `json:"region,omitempty"`
	DeviceClass        string `json:"device_class,omitempty"` // cli | android | ios
	SoftwareVersion    string `json:"software_version,omitempty"`
	ProvisionedDownBps uint64 `json:"provisioned_down_bps,omitempty"`
	ProvisionedUpBps   uint64 `json:"provisioned_up_bps,omitempty"`
}

// Telemetry is the measured evidence behind the verdict. Histograms carry fixed
// bin edges so they merge across testers (the engineer CDF overlay). Up* are
// optional; the current engine measures the downstream dual-queue distinctly.
type Telemetry struct {
	BaseRTTms            float64            `json:"base_rtt_ms"`
	MarkingSurvival      float64            `json:"marking_survival"`
	CapacityAchievedBps  uint64             `json:"capacity_achieved_bps"`
	OvershootAchievedBps uint64             `json:"overshoot_achieved_bps"`
	DownLL               *compute.Histogram `json:"down_ll,omitempty"`
	DownClassic          *compute.Histogram `json:"down_classic,omitempty"`
	UpLL                 *compute.Histogram `json:"up_ll,omitempty"`
	UpClassic            *compute.Histogram `json:"up_classic,omitempty"`
}

// RunReport is the full submitted/stored record for one validation run.
type RunReport struct {
	Schema    int            `json:"schema"`
	Meta      Meta           `json:"meta"`
	Result    compute.Result `json:"result"`
	Telemetry Telemetry      `json:"telemetry"`
}

// Valid reports whether r is well-formed enough to store: a known schema, a run
// id, and a verdict. Ingest rejects anything else rather than persisting junk.
func (r RunReport) Valid() bool {
	return r.Schema == SchemaVersion && r.Meta.RunID != "" && r.Result.Verdict != ""
}
