// Package storage persists run reports for the engineer view. The Store interface
// has two implementations: an in-memory one (tests, local dashboard demo) and a
// Postgres adapter that takes an injected *sql.DB so this package depends only on
// the standard library — the concrete driver is wired in at the binary level.
package storage

import (
	"context"
	"sort"
	"sync"

	"github.com/judwhiteneck/qoe-test-harness/core/report"
)

// DefaultLimit caps a List result when the filter leaves Limit unset.
const DefaultLimit = 1000

// Filter selects and bounds runs for the engineer view. Empty string fields are
// ignored (no constraint); SinceUnixMs of 0 is ignored.
type Filter struct {
	ISP         string
	Region      string
	DeviceClass string
	Verdict     string
	SinceUnixMs int64
	Limit       int
}

func (f Filter) limit() int {
	if f.Limit <= 0 || f.Limit > DefaultLimit {
		return DefaultLimit
	}
	return f.Limit
}

func (f Filter) matches(r report.RunReport) bool {
	if f.ISP != "" && r.Meta.ISP != f.ISP {
		return false
	}
	if f.Region != "" && r.Meta.Region != f.Region {
		return false
	}
	if f.DeviceClass != "" && r.Meta.DeviceClass != f.DeviceClass {
		return false
	}
	if f.Verdict != "" && string(r.Result.Verdict) != f.Verdict {
		return false
	}
	if f.SinceUnixMs != 0 && r.Meta.StartedAtUnixMs < f.SinceUnixMs {
		return false
	}
	return true
}

// Store persists and queries run reports.
type Store interface {
	Save(ctx context.Context, r report.RunReport) error
	List(ctx context.Context, f Filter) ([]report.RunReport, error)
}

// Memory is an in-memory Store. Safe for concurrent use.
type Memory struct {
	mu   sync.Mutex
	runs map[string]report.RunReport // keyed by run id (last write wins)
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory { return &Memory{runs: make(map[string]report.RunReport)} }

// Save stores r, replacing any earlier run with the same id.
func (m *Memory) Save(_ context.Context, r report.RunReport) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[r.Meta.RunID] = r
	return nil
}

// List returns matching runs newest-first, bounded by the filter's limit.
func (m *Memory) List(_ context.Context, f Filter) ([]report.RunReport, error) {
	m.mu.Lock()
	out := make([]report.RunReport, 0, len(m.runs))
	for _, r := range m.runs {
		if f.matches(r) {
			out = append(out, r)
		}
	}
	m.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].Meta.StartedAtUnixMs > out[j].Meta.StartedAtUnixMs
	})
	if lim := f.limit(); len(out) > lim {
		out = out[:lim]
	}
	return out, nil
}

var _ Store = (*Memory)(nil)
