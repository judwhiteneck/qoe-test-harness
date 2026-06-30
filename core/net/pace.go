package net

import (
	"time"

	"github.com/judwhiteneck/qoe-test-harness/core/clock"
)

// Pacer schedules packet emission to hold a target bitrate. It is clock-driven
// and self-correcting: Due() returns how many fixed-size packets should be sent
// right now to stay on the target rate given elapsed time and what's been sent.
// Pure logic over the injected Clock, so it is deterministic under test.
type Pacer struct {
	clk         clock.Clock
	start       time.Time
	bytesPerPkt int
	rateBps     uint64
	sent        uint64
}

// NewPacer starts a pacer for rateBps using bytesPerPkt-sized packets.
func NewPacer(clk clock.Clock, bytesPerPkt int, rateBps uint64) *Pacer {
	return &Pacer{clk: clk, start: clk.Now(), bytesPerPkt: bytesPerPkt, rateBps: rateBps}
}

// Due returns how many packets to send now to catch up to the target rate.
func (p *Pacer) Due() int {
	if p.bytesPerPkt <= 0 || p.rateBps == 0 {
		return 0
	}
	elapsed := p.clk.Since(p.start).Seconds()
	if elapsed <= 0 {
		return 0
	}
	target := uint64(float64(p.rateBps) / 8.0 * elapsed / float64(p.bytesPerPkt))
	if target <= p.sent {
		return 0
	}
	return int(target - p.sent)
}

// MarkSent records n packets as sent.
func (p *Pacer) MarkSent(n int) {
	if n > 0 {
		p.sent += uint64(n)
	}
}

// Meter measures achieved throughput from observed byte counts over clock time.
type Meter struct {
	clk   clock.Clock
	start time.Time
	bytes uint64
}

// NewMeter starts a throughput meter.
func NewMeter(clk clock.Clock) *Meter { return &Meter{clk: clk, start: clk.Now()} }

// Add records n received bytes.
func (m *Meter) Add(n int) {
	if n > 0 {
		m.bytes += uint64(n)
	}
}

// Bps returns the average bits/second since start (or last Reset).
func (m *Meter) Bps() uint64 {
	elapsed := m.clk.Since(m.start).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return uint64(float64(m.bytes) * 8.0 / elapsed)
}

// Reset restarts the measurement window.
func (m *Meter) Reset() {
	m.start = m.clk.Now()
	m.bytes = 0
}
