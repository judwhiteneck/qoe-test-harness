// Package net is the I/O layer: marked-UDP sockets, load generation, and probing.
// Real implementations use raw sockets (recvmmsg / IP_RECVTOS / SO_TIMESTAMPING);
// tests use in-memory fakes that inject loss, reorder, delay, and TOS bleaching.
// The pure packages compute and protocol never import this one (see the import
// rules in docs/ARCHITECTURE.md, enforced by scripts/check-imports.sh).
package net

import "time"

// Marking is the IP TOS byte the client/server set: ECN bits plus DSCP.
type Marking uint8

const (
	// NotECT marks classic, non-L4S traffic.
	NotECT Marking = 0x00
	// ECT1 is the L4S ECN codepoint (ECT(1)).
	ECT1 Marking = 0x01
	// DSCPNQB is Non-Queue-Building, DSCP 45, placed in the high 6 bits of the byte.
	DSCPNQB Marking = 45 << 2
	// LLMark is what a low-latency probe/flow sets: NQB + ECT(1).
	LLMark Marking = DSCPNQB | ECT1
)

// Probe is one timestamped, sequenced measurement packet sent by the client.
type Probe struct {
	Seq     uint64
	SentAt  time.Time
	Marking Marking
}

// Echo is the server's reply, carrying the TOS byte the server actually observed
// so the client can measure marking survival and CE marking end to end.
type Echo struct {
	Seq          uint64
	SentAt       time.Time // original client send time, echoed back (for per-probe RTT)
	RecvAt       time.Time // client receive time (for RTT)
	ServerRecvAt time.Time // server receive time (for relative one-way cross-check)
	TOSObserved  Marking   // server-observed TOS: marking survival on the client->server (upstream) leg
	CESeen       bool      // CE observed by the server on the upstream probe

	// DownTOSObserved is the TOS byte the CLIENT observed on the received echo,
	// i.e. the server->client (downstream) leg, when the server reflects the mark
	// onto the return path. DownCE is whether that echo arrived CE-marked (the
	// direct signal that a downstream L4S AQM acted). Both are zero if the client
	// socket could not read the received TOS.
	DownTOSObserved Marking
	DownCE          bool
}

// PacketConn is the marked-UDP transport seam. The real implementation wraps raw
// sockets; tests use a loopback fake.
type PacketConn interface {
	SendProbe(p Probe) error
	RecvEcho() (Echo, error)
	Close() error
}

// LoadController drives offered load (the overshoot ramp) and reports the rate it
// is actually achieving, so the engine can confirm a standing queue formed.
type LoadController interface {
	SetRateBps(bps uint64) error
	AchievedBps() uint64
	Stop()
}
