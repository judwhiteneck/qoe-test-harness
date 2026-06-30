package compute

// ResultVersion is the schema version of Result. Bump it on any breaking change
// to the contract; every surface (CLI, apps, dashboard, Postgres) renders Result
// verbatim, so this is the one place the shape is defined.
const ResultVersion = 1

// Verdict is the outcome of a run or sub-check.
type Verdict string

const (
	// Pass: the property was demonstrated.
	Pass Verdict = "pass"
	// Fail: the property was tested and not met.
	Fail Verdict = "fail"
	// Inconclusive: the measurement could not be trusted, so no pass/fail is
	// claimed. This is a first-class outcome, never a silent pass (see
	// docs/ENGINEERING.md §4).
	Inconclusive Verdict = "inconclusive"
)

// SubResult is one of the three things the tool reports: working, marking
// survival, no harm.
type SubResult struct {
	Name   string  `json:"name"`
	Status Verdict `json:"status"`
	Detail string  `json:"detail"`
}

// Result is the single source of truth for a run's outcome. Clients must render
// it and never re-derive verdict logic.
type Result struct {
	Version    int         `json:"version"`
	Verdict    Verdict     `json:"verdict"`
	SubResults []SubResult `json:"sub_results,omitempty"`
	Caveats    []string    `json:"caveats,omitempty"`
}
