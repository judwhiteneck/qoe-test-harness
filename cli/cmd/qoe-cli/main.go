// Command qoe-cli is the wired diagnostic client. It handshakes with the server,
// measures an idle baseline, then runs marked probe bursts and reports marking
// survival and working-latency percentiles. With -load-mbps it also drives an
// upstream load flow on a separate socket so the probe bursts measure loaded
// latency (and prints the achieved bottleneck rate). NOTE: this is still a
// diagnostic, not a full pass/fail verdict — the calibrated thresholds and the
// downstream-load leg arrive with M0.
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/judwhiteneck/qoe-test-harness/core/clock"
	"github.com/judwhiteneck/qoe-test-harness/core/compute"
	"github.com/judwhiteneck/qoe-test-harness/core/engine"
	cnet "github.com/judwhiteneck/qoe-test-harness/core/net"
)

func main() {
	serverAddr := flag.String("server", "", "test server host:port (required)")
	probes := flag.Int("probes", 200, "probes per marking class")
	loadMbps := flag.Int("load-mbps", 0, "if >0, run an upstream load flow at this rate during the probe bursts")
	downMbps := flag.Int("down-mbps", 0, "if >0, run a cookie-gated downstream load flow at this rate during the probe bursts")
	flag.Parse()
	if *serverAddr == "" {
		log.Fatal("--server is required")
	}

	clk := clock.System{}
	conn, err := cnet.DialUDP(clk, *serverAddr)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Handshake(1); err != nil {
		log.Printf("handshake failed (continuing with probes): %v", err)
	}

	// Idle baseline first, before any load, so working-delta is relative to a
	// genuinely unloaded path.
	base, _, _, err := engine.BatchProbe(clk, conn, cnet.NotECT, 100)
	if err != nil {
		log.Fatalf("baseline: %v", err)
	}
	baseRTT := compute.BaseRTT(base, 0.05)

	// Optionally saturate the path on separate sockets while we probe.
	var load *cnet.UDPLoad
	if *loadMbps > 0 {
		load, err = cnet.DialUDPLoad(clk, *serverAddr)
		if err != nil {
			log.Fatalf("dial load: %v", err)
		}
		defer load.Close()
		if err := load.SetRateBps(uint64(*loadMbps) * 1_000_000); err != nil {
			log.Fatalf("start load: %v", err)
		}
	}
	var down *cnet.UDPDownLoad
	if *downMbps > 0 {
		down, err = cnet.DialUDPDownLoad(clk, *serverAddr, 2)
		if err != nil {
			log.Fatalf("dial downstream load: %v", err)
		}
		defer down.Close()
		if err := down.SetRateBps(uint64(*downMbps) * 1_000_000); err != nil {
			log.Fatalf("start downstream load: %v", err)
		}
	}
	if load != nil || down != nil {
		time.Sleep(500 * time.Millisecond) // let the queue (if any) build before probing
	}

	llRTTs, llSurvival, _, err := engine.BatchProbe(clk, conn, cnet.LLMark, *probes)
	if err != nil {
		log.Fatalf("LL probes: %v", err)
	}
	clRTTs, _, _, err := engine.BatchProbe(clk, conn, cnet.NotECT, *probes)
	if err != nil {
		log.Fatalf("classic probes: %v", err)
	}

	var upAchieved, downAchieved uint64
	if load != nil {
		upAchieved = load.AchievedBps()
		load.Stop()
	}
	if down != nil {
		downAchieved = down.AchievedBps()
		down.Stop()
	}

	llHist := compute.NewDefaultHistogram()
	for _, s := range llRTTs {
		llHist.Observe(compute.WorkingDelta(s, baseRTT))
	}
	clHist := compute.NewDefaultHistogram()
	for _, s := range clRTTs {
		clHist.Observe(compute.WorkingDelta(s, baseRTT))
	}

	mode := "idle"
	switch {
	case *loadMbps > 0 && *downMbps > 0:
		mode = fmt.Sprintf("under %d up / %d down Mbps load", *loadMbps, *downMbps)
	case *loadMbps > 0:
		mode = fmt.Sprintf("under %d Mbps upstream load", *loadMbps)
	case *downMbps > 0:
		mode = fmt.Sprintf("under %d Mbps downstream load", *downMbps)
	}
	fmt.Printf("qoe-cli probe diagnostic (%s; not a full verdict)\n", mode)
	fmt.Printf("  server:                        %s\n", *serverAddr)
	fmt.Printf("  base RTT (p5):                 %.2f ms\n", baseRTT)
	if load != nil {
		fmt.Printf("  upstream load achieved:        %.1f Mbps (target %d)\n", float64(upAchieved)/1e6, *loadMbps)
	}
	if down != nil {
		fmt.Printf("  downstream load achieved:      %.1f Mbps (target %d)\n", float64(downAchieved)/1e6, *downMbps)
	}
	fmt.Printf("  LL marking survival:           %.1f%% (%d probes)\n", llSurvival*100, len(llRTTs))
	fmt.Printf("  LL working-delta  p50/p99:     %.2f / %.2f ms\n", llHist.Quantile(0.5), llHist.Quantile(0.99))
	fmt.Printf("  classic working-delta p50/p99: %.2f / %.2f ms\n", clHist.Quantile(0.5), clHist.Quantile(0.99))
}
