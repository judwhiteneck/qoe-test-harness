package net

import (
	"testing"
	"time"

	"github.com/judwhiteneck/qoe-test-harness/core/clock"
)

func TestPacerCatchesUpToRate(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	// 8000 bps = 1000 B/s; 100 B packets -> 10 pkt/s.
	p := NewPacer(clk, 100, 8000)

	if d := p.Due(); d != 0 {
		t.Fatalf("Due at t0 = %d, want 0", d)
	}
	clk.Advance(time.Second)
	if d := p.Due(); d != 10 {
		t.Fatalf("Due after 1s = %d, want 10", d)
	}
	p.MarkSent(10)
	if d := p.Due(); d != 0 {
		t.Fatalf("Due immediately after sending the quota = %d, want 0", d)
	}
	clk.Advance(500 * time.Millisecond)
	if d := p.Due(); d != 5 {
		t.Fatalf("Due after +0.5s = %d, want 5", d)
	}
	// Falling behind: don't send, let two more seconds elapse, expect a full catch-up.
	p.MarkSent(5)
	clk.Advance(2 * time.Second)
	if d := p.Due(); d != 20 {
		t.Fatalf("Due after falling 2s behind = %d, want 20", d)
	}
}

func TestPacerZeroInputs(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	clk.Advance(time.Second)
	if d := NewPacer(clk, 0, 1000).Due(); d != 0 {
		t.Errorf("zero packet size: Due = %d, want 0", d)
	}
	if d := NewPacer(clk, 100, 0).Due(); d != 0 {
		t.Errorf("zero rate: Due = %d, want 0", d)
	}
}

func TestMeterBps(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	m := NewMeter(clk)
	if bps := m.Bps(); bps != 0 {
		t.Fatalf("Bps with no elapsed time = %d, want 0", bps)
	}
	m.Add(1000) // bytes
	clk.Advance(time.Second)
	if bps := m.Bps(); bps != 8000 {
		t.Fatalf("Bps = %d, want 8000", bps)
	}

	m.Reset()
	m.Add(500)
	clk.Advance(time.Second)
	if bps := m.Bps(); bps != 4000 {
		t.Fatalf("Bps after reset = %d, want 4000", bps)
	}
}
