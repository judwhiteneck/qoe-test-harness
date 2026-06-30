//go:build linux

package server

import (
	"context"
	stdnet "net"
	"testing"
	"time"

	"github.com/judwhiteneck/qoe-test-harness/core/clock"
	cnet "github.com/judwhiteneck/qoe-test-harness/core/net"
	"github.com/judwhiteneck/qoe-test-harness/core/protocol"
)

// TestEndToEndOverLoopback runs a real server on 127.0.0.1 and a real UDP client
// against it: handshake + a burst of marked probes echoed back. This exercises
// the production socket path (no hardware, no LLD line). Marking-on-the-wire is
// checked when the platform delivers IP TOS on loopback; the authoritative
// on-wire check is M0/S1 on real hardware.
func TestEndToEndOverLoopback(t *testing.T) {
	srv, err := Listen("127.0.0.1:0", []byte("test-secret"))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	clk := clock.System{}
	conn, err := cnet.DialUDP(clk, srv.Addr().String())
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer conn.Close()
	conn.SetReadTimeout(300 * time.Millisecond)

	cookie, err := conn.Handshake(1234)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if len(cookie) == 0 {
		t.Fatal("handshake returned empty cookie")
	}

	const n = 20
	for i := 1; i <= n; i++ {
		if err := conn.SendProbe(cnet.Probe{Seq: uint64(i), SentAt: clk.Now(), Marking: cnet.LLMark}); err != nil {
			t.Fatalf("SendProbe: %v", err)
		}
	}

	got, tosSeen := 0, 0
	for {
		e, rerr := conn.RecvEcho()
		if rerr == cnet.ErrNoEcho {
			break
		}
		if rerr != nil {
			t.Fatalf("RecvEcho: %v", rerr)
		}
		got++
		if e.RecvAt.Sub(e.SentAt) <= 0 {
			t.Errorf("seq %d: non-positive RTT", e.Seq)
		}
		if e.TOSObserved != 0 {
			tosSeen++
			if e.TOSObserved != cnet.LLMark {
				t.Errorf("seq %d: TOS observed %#x, want %#x", e.Seq, e.TOSObserved, cnet.LLMark)
			}
		}
	}
	if got == 0 {
		t.Fatal("no echoes received over loopback")
	}
	if got < n/2 {
		t.Errorf("only %d/%d echoes returned", got, n)
	}
	if tosSeen == 0 {
		t.Log("note: platform did not deliver IP TOS on loopback; on-wire marking is validated in M0/S1 on hardware")
	}
}

// TestUDPLoadAchievesTargetOverLoopback runs the real load controller against the
// real server sink. Loopback has no bottleneck, so the achieved rate (measured at
// the server from its byte-count reports) should track the paced target. This
// validates the pacer + measurement plumbing end to end; standing-queue formation
// under a real bottleneck is M0/S3 on hardware.
func TestUDPLoadAchievesTargetOverLoopback(t *testing.T) {
	srv, err := Listen("127.0.0.1:0", []byte("test-secret"))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	ld, err := cnet.DialUDPLoad(clock.System{}, srv.Addr().String())
	if err != nil {
		t.Fatalf("DialUDPLoad: %v", err)
	}
	defer ld.Close()

	const target = 50_000_000 // 50 Mbps
	if err := ld.SetRateBps(target); err != nil {
		t.Fatalf("SetRateBps: %v", err)
	}
	time.Sleep(700 * time.Millisecond) // warm up + several report intervals
	got := ld.AchievedBps()
	ld.Stop()

	if got == 0 {
		t.Fatal("achieved rate is 0: no load-stat reports made it back")
	}
	// Wide tolerance: loopback scheduling and the 50 ms report window add jitter.
	if got < target/2 || got > target*2 {
		t.Errorf("achieved %d bps, want within 2x of target %d", got, target)
	}
}

// TestDownstreamLoadAchievesTargetWithCookie exercises the full cookie-gated
// downstream path: handshake -> Start (with cookie) -> server-paced flow ->
// client measures the received rate. Loopback has no bottleneck, so the achieved
// rate tracks the paced target.
func TestDownstreamLoadAchievesTargetWithCookie(t *testing.T) {
	srv, err := Listen("127.0.0.1:0", []byte("test-secret"))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	dl, err := cnet.DialUDPDownLoad(clock.System{}, srv.Addr().String(), 1)
	if err != nil {
		t.Fatalf("DialUDPDownLoad: %v", err)
	}
	defer dl.Close()

	const target = 50_000_000
	if err := dl.SetRateBps(target); err != nil {
		t.Fatalf("SetRateBps: %v", err)
	}
	time.Sleep(700 * time.Millisecond)
	got := dl.AchievedBps()
	dl.Stop()

	if got == 0 {
		t.Fatal("achieved 0: no downstream flow arrived despite a valid cookie")
	}
	if got < target/2 || got > target*2 {
		t.Errorf("achieved %d bps, want within 2x of target %d", got, target)
	}
}

// TestDownstreamLoadRefusedWithoutCookie is the anti-amplification guarantee
// (spec G1): a Start with no valid cookie must produce NO downstream flow, so the
// server cannot be turned into a reflector against a spoofed victim. We send a
// Start with a bogus cookie and confirm not a single packet comes back.
func TestDownstreamLoadRefusedWithoutCookie(t *testing.T) {
	srv, err := Listen("127.0.0.1:0", []byte("test-secret"))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	raddr, err := stdnet.ResolveUDPAddr("udp4", srv.Addr().String())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	c, err := stdnet.DialUDP("udp4", nil, raddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// A Start with a forged (all-zero) cookie — the attacker never did the handshake.
	start := protocol.Start{Header: protocol.Header{Session: 1}, RateBps: 50_000_000, DurationMs: 5000}
	out := make([]byte, protocol.StartSize)
	if _, err := start.Marshal(out); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := c.Write(out); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Expect silence: read should time out with zero bytes.
	_ = c.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	buf := make([]byte, 2048)
	if n, err := c.Read(buf); err == nil {
		t.Fatalf("server sent %d bytes for a forged-cookie Start; amplification gate breached", n)
	}
}
