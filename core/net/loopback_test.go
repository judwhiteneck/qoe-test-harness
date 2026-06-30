package net

import (
	"testing"
	"time"

	"github.com/judwhiteneck/qoe-test-harness/core/clock"
)

func newLB(cfg LoopbackConfig) (*clock.Fake, *Loopback) {
	clk := clock.NewFake(time.Unix(1_700_000_000, 0))
	return clk, NewLoopback(clk, cfg)
}

func TestLoopbackBaseDelayAndClockAdvance(t *testing.T) {
	clk, lb := newLB(LoopbackConfig{BaseRTT: 20 * time.Millisecond})
	start := clk.Now()
	if err := lb.SendProbe(Probe{Seq: 1, SentAt: start, Marking: NotECT}); err != nil {
		t.Fatal(err)
	}
	e, err := lb.RecvEcho()
	if err != nil {
		t.Fatal(err)
	}
	if got := e.RecvAt.Sub(start); got != 20*time.Millisecond {
		t.Fatalf("rtt = %v, want 20ms", got)
	}
	if !clk.Now().Equal(e.RecvAt) {
		t.Fatal("clock not advanced to delivery instant")
	}
}

func TestLoopbackDualQueueOrdering(t *testing.T) {
	clk, lb := newLB(LoopbackConfig{
		BaseRTT:           20 * time.Millisecond,
		LLQueueDelay:      3 * time.Millisecond,
		ClassicQueueDelay: 100 * time.Millisecond,
	})
	_ = lb.Load().SetRateBps(1) // loaded
	start := clk.Now()
	_ = lb.SendProbe(Probe{Seq: 1, SentAt: start, Marking: NotECT}) // classic, slow
	_ = lb.SendProbe(Probe{Seq: 2, SentAt: start, Marking: LLMark}) // LL, fast
	first, _ := lb.RecvEcho()                                       // earliest RecvAt = LL
	second, _ := lb.RecvEcho()
	if first.Seq != 2 {
		t.Fatalf("first delivered seq = %d, want 2 (LL is faster)", first.Seq)
	}
	if d := first.RecvAt.Sub(start); d != 23*time.Millisecond {
		t.Fatalf("LL rtt = %v, want 23ms", d)
	}
	if d := second.RecvAt.Sub(start); d != 120*time.Millisecond {
		t.Fatalf("classic rtt = %v, want 120ms", d)
	}
}

func TestLoopbackDrop(t *testing.T) {
	clk, lb := newLB(LoopbackConfig{BaseRTT: 10 * time.Millisecond, DropEvery: 2})
	start := clk.Now()
	for i := uint64(1); i <= 4; i++ {
		_ = lb.SendProbe(Probe{Seq: i, SentAt: start, Marking: NotECT})
	}
	n := 0
	for {
		if _, err := lb.RecvEcho(); err == ErrNoEcho {
			break
		}
		n++
	}
	if n != 2 { // seq 2 and 4 dropped
		t.Fatalf("delivered %d echoes, want 2", n)
	}
}

func TestLoopbackBleachAndCE(t *testing.T) {
	clk, lb := newLB(LoopbackConfig{BaseRTT: 10 * time.Millisecond, Bleach: true, CEWhenLoaded: true})
	_ = lb.Load().SetRateBps(1)
	start := clk.Now()
	_ = lb.SendProbe(Probe{Seq: 1, SentAt: start, Marking: LLMark})
	e, _ := lb.RecvEcho()
	if e.TOSObserved != NotECT {
		t.Fatalf("bleached observed = %#x, want NotECT", e.TOSObserved)
	}
	if !e.CESeen {
		t.Fatal("expected CE under load")
	}
}

func TestLoopbackEmpty(t *testing.T) {
	_, lb := newLB(LoopbackConfig{})
	if _, err := lb.RecvEcho(); err != ErrNoEcho {
		t.Fatalf("err = %v, want ErrNoEcho", err)
	}
	if err := lb.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
}

func TestLoopbackLoadControllerCap(t *testing.T) {
	_, lb := newLB(LoopbackConfig{MaxAchievableBps: 300})
	lc := lb.Load()
	_ = lc.SetRateBps(500)
	if got := lc.AchievedBps(); got != 300 {
		t.Fatalf("capped achieved = %d, want 300", got)
	}
	_ = lc.SetRateBps(100)
	if got := lc.AchievedBps(); got != 100 {
		t.Fatalf("achieved = %d, want 100", got)
	}
	lc.Stop()
	if got := lc.AchievedBps(); got != 0 {
		t.Fatalf("after Stop achieved = %d, want 0", got)
	}
}
