//go:build linux

package server

import (
	"context"
	"testing"
	"time"

	"github.com/judwhiteneck/qoe-test-harness/core/clock"
	cnet "github.com/judwhiteneck/qoe-test-harness/core/net"
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
