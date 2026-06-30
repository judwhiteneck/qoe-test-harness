// Command qoe-cli is the wired diagnostic client. It handshakes with the server
// and runs marked probe bursts, reporting marking survival and working-latency
// percentiles. NOTE: load generation is not built yet, so this is a probe-level
// diagnostic, not a full pass/fail validation run (that arrives with the load
// generator + the M0-calibrated thresholds).
package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/judwhiteneck/qoe-test-harness/core/clock"
	"github.com/judwhiteneck/qoe-test-harness/core/compute"
	"github.com/judwhiteneck/qoe-test-harness/core/engine"
	cnet "github.com/judwhiteneck/qoe-test-harness/core/net"
)

func main() {
	serverAddr := flag.String("server", "", "test server host:port (required)")
	probes := flag.Int("probes", 200, "probes per marking class")
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

	base, _, _, err := engine.BatchProbe(clk, conn, cnet.NotECT, 100)
	if err != nil {
		log.Fatalf("baseline: %v", err)
	}
	baseRTT := compute.BaseRTT(base, 0.05)

	llRTTs, llSurvival, _, err := engine.BatchProbe(clk, conn, cnet.LLMark, *probes)
	if err != nil {
		log.Fatalf("LL probes: %v", err)
	}
	clRTTs, _, _, err := engine.BatchProbe(clk, conn, cnet.NotECT, *probes)
	if err != nil {
		log.Fatalf("classic probes: %v", err)
	}

	llHist := compute.NewDefaultHistogram()
	for _, s := range llRTTs {
		llHist.Observe(compute.WorkingDelta(s, baseRTT))
	}
	clHist := compute.NewDefaultHistogram()
	for _, s := range clRTTs {
		clHist.Observe(compute.WorkingDelta(s, baseRTT))
	}

	fmt.Printf("qoe-cli probe diagnostic (no load generation yet)\n")
	fmt.Printf("  server:                 %s\n", *serverAddr)
	fmt.Printf("  base RTT (p5):          %.2f ms\n", baseRTT)
	fmt.Printf("  LL marking survival:    %.1f%% (%d probes)\n", llSurvival*100, len(llRTTs))
	fmt.Printf("  LL working-delta  p50/p99: %.2f / %.2f ms\n", llHist.Quantile(0.5), llHist.Quantile(0.99))
	fmt.Printf("  classic working-delta p50/p99: %.2f / %.2f ms\n", clHist.Quantile(0.5), clHist.Quantile(0.99))
}
