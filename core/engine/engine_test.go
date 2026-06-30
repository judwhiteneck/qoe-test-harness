package engine_test

import (
	"strings"
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

// asyncLoad models a real load controller: AchievedBps reads 0 until the flow has
// ramped for rampMs of clock time after SetRateBps. It proves Run's LoadSettle
// gives an asynchronous flow time to produce a measurement before the capacity
// gate reads it.
type asyncLoad struct {
	clk    clock.Clock
	rampMs int
	setAt  time.Time
	rate   uint64
}

func (a *asyncLoad) SetRateBps(bps uint64) error {
	a.rate, a.setAt = bps, a.clk.Now()
	return nil
}

func (a *asyncLoad) AchievedBps() uint64 {
	if a.clk.Since(a.setAt) < time.Duration(a.rampMs)*time.Millisecond {
		return 0 // not ramped yet
	}
	return a.rate
}

func (a *asyncLoad) Stop() { a.rate = 0 }

func hasCaveat(r compute.Result, substr string) bool {
	for _, c := range r.Caveats {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

func TestRunSettleLetsAsyncLoadRamp(t *testing.T) {
	const tier = 500_000_000
	run := func(settle time.Duration) compute.Result {
		clk := clock.NewFake(time.Unix(1_700_000_000, 0))
		lo := cnet.NewLoopback(clk, healthy())
		eng := engine.New(engine.Config{
			Clock:              clk,
			Conn:               lo,
			Load:               &asyncLoad{clk: clk, rampMs: 200},
			ProvisionedDownBps: tier,
			ProvisionedUpBps:   50_000_000,
			BaselineProbes:     50,
			LoadedProbes:       50,
			LoadSettle:         settle,
		})
		r, err := eng.Run()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		return r
	}

	// Settle shorter than the ramp: the gate reads 0, capacity is unconfirmed.
	short := run(1 * time.Millisecond)
	if !hasCaveat(short, "capacity gate failed") {
		t.Errorf("short settle: expected capacity caveat, got %v", short.Caveats)
	}
	// Settle longer than the ramp: the flow has measured, capacity confirmed (so
	// the only remaining caveat is the absent standing queue, not capacity).
	long := run(500 * time.Millisecond)
	if hasCaveat(long, "capacity gate failed") {
		t.Errorf("long settle: capacity should be confirmed, got %v", long.Caveats)
	}
}
