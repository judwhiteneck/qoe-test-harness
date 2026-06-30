package storage

import (
	"context"
	"testing"

	"github.com/judwhiteneck/qoe-test-harness/core/compute"
	"github.com/judwhiteneck/qoe-test-harness/core/report"
)

func mk(id string, atMs int64, isp, region, verdict string) report.RunReport {
	return report.RunReport{
		Schema: report.SchemaVersion,
		Meta:   report.Meta{RunID: id, StartedAtUnixMs: atMs, ISP: isp, Region: region, DeviceClass: "cli"},
		Result: compute.Result{Version: compute.ResultVersion, Verdict: compute.Verdict(verdict)},
	}
}

func TestMemorySaveListNewestFirst(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	_ = m.Save(ctx, mk("a", 100, "Acme", "west", "pass"))
	_ = m.Save(ctx, mk("b", 300, "Acme", "east", "fail"))
	_ = m.Save(ctx, mk("c", 200, "Beta", "west", "pass"))

	got, err := m.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"b", "c", "a"} // newest-first by started_at
	if len(got) != 3 {
		t.Fatalf("got %d runs, want 3", len(got))
	}
	for i, id := range want {
		if got[i].Meta.RunID != id {
			t.Errorf("position %d = %s, want %s", i, got[i].Meta.RunID, id)
		}
	}
}

func TestMemoryFilters(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	_ = m.Save(ctx, mk("a", 100, "Acme", "west", "pass"))
	_ = m.Save(ctx, mk("b", 300, "Acme", "east", "fail"))
	_ = m.Save(ctx, mk("c", 200, "Beta", "west", "pass"))

	cases := []struct {
		name string
		f    Filter
		want []string
	}{
		{"by isp", Filter{ISP: "Acme"}, []string{"b", "a"}},
		{"by region", Filter{Region: "west"}, []string{"c", "a"}},
		{"by verdict", Filter{Verdict: "pass"}, []string{"c", "a"}},
		{"isp+region", Filter{ISP: "Acme", Region: "east"}, []string{"b"}},
		{"since", Filter{SinceUnixMs: 150}, []string{"b", "c"}},
		{"limit", Filter{Limit: 1}, []string{"b"}},
		{"no match", Filter{ISP: "Nope"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := m.List(ctx, tc.f)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d, want %d (%v)", len(got), len(tc.want), tc.want)
			}
			for i, id := range tc.want {
				if got[i].Meta.RunID != id {
					t.Errorf("pos %d = %s, want %s", i, got[i].Meta.RunID, id)
				}
			}
		})
	}
}

func TestMemorySaveUpsert(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	_ = m.Save(ctx, mk("a", 100, "Acme", "west", "pass"))
	_ = m.Save(ctx, mk("a", 100, "Acme", "west", "fail")) // same id, new verdict
	got, _ := m.List(ctx, Filter{})
	if len(got) != 1 {
		t.Fatalf("got %d runs, want 1 after upsert", len(got))
	}
	if got[0].Result.Verdict != compute.Fail {
		t.Errorf("verdict = %s, want fail (last write wins)", got[0].Result.Verdict)
	}
}
