package net

import (
	"errors"
	"sort"
	"time"

	"github.com/judwhiteneck/qoe-test-harness/core/clock"
)

// ErrNoEcho means there are no more echoes to deliver (the in-memory equivalent
// of "would block"). The engine drains a batch until it sees this.
var ErrNoEcho = errors.New("net: no echo available")

// LoopbackConfig models a simulated path so the engine can be tested end to end
// with no network or hardware. Delays are deterministic functions of the probe,
// so runs are reproducible.
type LoopbackConfig struct {
	BaseRTT           time.Duration // idle floor, both markings
	LLQueueDelay      time.Duration // added to LL (ECT(1)/NQB) probes when loaded
	ClassicQueueDelay time.Duration // added to classic probes when loaded
	JitterStepMs      int           // spreads samples: + (seq%5)*step ms
	DropEvery         int           // drop every Nth probe (0 = no loss)
	Bleach            bool          // strip marking on the wire (observed = NotECT)
	CEWhenLoaded      bool          // set CE on LL probes under load
	MaxAchievableBps  uint64        // cap on delivered throughput (0 = unlimited)
}

// Loopback is an in-memory PacketConn that simulates a DOCSIS-like path with a
// dual-queue: under load, LL probes get LLQueueDelay while classic probes get the
// larger ClassicQueueDelay. It owns a fake clock and advances it to each echo's
// delivery instant on RecvEcho.
type Loopback struct {
	clk     *clock.Fake
	cfg     LoopbackConfig
	loaded  bool
	bps     uint64
	pending []Echo
}

// NewLoopback returns a simulator driven by clk.
func NewLoopback(clk *clock.Fake, cfg LoopbackConfig) *Loopback {
	return &Loopback{clk: clk, cfg: cfg}
}

// SendProbe queues an echo (unless the probe is dropped by the loss model).
func (l *Loopback) SendProbe(p Probe) error {
	if l.cfg.DropEvery > 0 && p.Seq%uint64(l.cfg.DropEvery) == 0 {
		return nil // dropped: no echo
	}
	delay := l.cfg.BaseRTT
	if l.loaded {
		if isLL(p.Marking) {
			delay += l.cfg.LLQueueDelay
		} else {
			delay += l.cfg.ClassicQueueDelay
		}
	}
	if l.cfg.JitterStepMs > 0 {
		delay += time.Duration(int(p.Seq%5)*l.cfg.JitterStepMs) * time.Millisecond
	}
	tos := p.Marking
	if l.cfg.Bleach {
		tos = NotECT
	}
	l.pending = append(l.pending, Echo{
		Seq:          p.Seq,
		SentAt:       p.SentAt,
		RecvAt:       p.SentAt.Add(delay),
		ServerRecvAt: p.SentAt.Add(delay / 2),
		TOSObserved:  tos,
		CESeen:       l.loaded && l.cfg.CEWhenLoaded && isLL(p.Marking),
	})
	return nil
}

// RecvEcho delivers the earliest pending echo and advances the clock to its
// arrival instant. Returns ErrNoEcho when the queue is empty.
func (l *Loopback) RecvEcho() (Echo, error) {
	if len(l.pending) == 0 {
		return Echo{}, ErrNoEcho
	}
	sort.SliceStable(l.pending, func(i, j int) bool {
		return l.pending[i].RecvAt.Before(l.pending[j].RecvAt)
	})
	e := l.pending[0]
	l.pending = l.pending[1:]
	l.clk.Set(e.RecvAt)
	return e, nil
}

// Close is a no-op for the simulator.
func (l *Loopback) Close() error { return nil }

// Load returns a LoadController bound to this simulator.
func (l *Loopback) Load() LoadController { return loopbackLoad{l} }

type loopbackLoad struct{ l *Loopback }

func (c loopbackLoad) SetRateBps(bps uint64) error {
	c.l.bps = bps
	c.l.loaded = bps > 0
	return nil
}

func (c loopbackLoad) AchievedBps() uint64 {
	if c.l.cfg.MaxAchievableBps > 0 && c.l.bps > c.l.cfg.MaxAchievableBps {
		return c.l.cfg.MaxAchievableBps
	}
	return c.l.bps
}

func (c loopbackLoad) Stop() {
	c.l.bps = 0
	c.l.loaded = false
}

func isLL(m Marking) bool { return m&ECT1 != 0 }

// Ensure the simulator satisfies the production interfaces.
var (
	_ PacketConn     = (*Loopback)(nil)
	_ LoadController = loopbackLoad{}
)
