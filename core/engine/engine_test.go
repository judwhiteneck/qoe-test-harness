package engine_test

import (
	"testing"
	"time"

	"github.com/judwhiteneck/qoe-test-harness/core/clock"
	"github.com/judwhiteneck/qoe-test-harness/core/compute"
	"github.com/judwhiteneck/qoe-test-harness/core/engine"
	cnet "github.com/judwhiteneck/qoe-test-harness/core/net"
)

func runWith(t *testing.T, cfg cnet.LoopbackConfig) compute.Result {
	t.Helper()
	clk := clock.NewFake(time.Unix(1_700_000_000, 0))
	lo := cnet.NewLoopback(clk, cfg)
	eng := engine.New(engine.Config{
		Clock:              clk,
		Conn:               lo,
		Load:               lo.Load(),
		ProvisionedDownBps: 500_000_000,
		ProvisionedUpBps:   50_000_000,
		BaselineProbes:     200,
		LoadedProbes:       500,
	})
	r, err := eng.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return r
}

// healthy is a path where the dual-queue isolates LL latency under heavy classic load.
func healthy() cnet.LoopbackConfig {
	return cnet.LoopbackConfig{
		BaseRTT:           20 * time.Millisecond,
		LLQueueDelay:      3 * time.Millisecond,
		ClassicQueueDelay: 120 * time.Millisecond,
		JitterStepMs:      1,
	}
}

func sub(r compute.Result, name string) compute.Verdict {
	for _, s := range r.SubResults {
		if s.Name == name {
			return s.Status
		}
	}
	return ""
}

func TestRunHealthyPass(t *testing.T) {
	r := runWith(t, healthy())
	if r.Verdict != compute.Pass {
		t.Fatalf("verdict = %s, want pass (caveats=%v subs=%+v)", r.Verdict, r.Caveats, r.SubResults)
	}
	for _, name := range []string{"working", "marking_survival", "no_harm"} {
		if sub(r, name) != compute.Pass {
			t.Errorf("%s = %s, want pass", name, sub(r, name))
		}
	}
}

func TestRunBleachedMarkingFails(t *testing.T) {
	cfg := healthy()
	cfg.Bleach = true
	r := runWith(t, cfg)
	if r.Verdict != compute.Fail || sub(r, "marking_survival") != compute.Fail {
		t.Fatalf("verdict=%s marking=%s, want fail/fail", r.Verdict, sub(r, "marking_survival"))
	}
}

func TestRunLLNotIsolatedFails(t *testing.T) {
	cfg := healthy()
	cfg.LLQueueDelay = 40 * time.Millisecond // LL no longer kept low under load
	r := runWith(t, cfg)
	if r.Verdict != compute.Fail || sub(r, "working") != compute.Fail {
		t.Fatalf("verdict=%s working=%s, want fail/fail", r.Verdict, sub(r, "working"))
	}
}

func TestRunNoStandingQueueInconclusive(t *testing.T) {
	cfg := healthy()
	cfg.ClassicQueueDelay = 5 * time.Millisecond // load never built a queue
	r := runWith(t, cfg)
	if r.Verdict != compute.Inconclusive {
		t.Fatalf("verdict = %s, want inconclusive", r.Verdict)
	}
}

func TestRunCapacityGateInconclusive(t *testing.T) {
	cfg := healthy()
	cfg.MaxAchievableBps = 300_000_000 // below 90% of the 500 Mbps tier
	r := runWith(t, cfg)
	if r.Verdict != compute.Inconclusive {
		t.Fatalf("verdict = %s, want inconclusive (capacity gate)", r.Verdict)
	}
}

func TestRunToleratesLoss(t *testing.T) {
	cfg := healthy()
	cfg.DropEvery = 10 // ~10% loss
	r := runWith(t, cfg)
	if r.Verdict != compute.Pass {
		t.Fatalf("verdict = %s, want pass under 10%% loss", r.Verdict)
	}
}
