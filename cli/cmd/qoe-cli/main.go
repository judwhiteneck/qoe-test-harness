// Command qoe-cli is the wired diagnostic client. It handshakes with the server,
// measures an idle baseline, then runs marked probe bursts and reports marking
// survival and working-latency percentiles. With -load-mbps it also drives an
// upstream load flow on a separate socket so the probe bursts measure loaded
// latency (and prints the achieved bottleneck rate). NOTE: this is still a
// diagnostic, not a full pass/fail verdict — the calibrated thresholds and the
// downstream-load leg arrive with M0.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/judwhiteneck/qoe-test-harness/core/clock"
	"github.com/judwhiteneck/qoe-test-harness/core/compute"
	"github.com/judwhiteneck/qoe-test-harness/core/engine"
	cnet "github.com/judwhiteneck/qoe-test-harness/core/net"
	"github.com/judwhiteneck/qoe-test-harness/core/report"
)

func main() {
	serverAddr := flag.String("server", "", "test server host:port (required)")
	probes := flag.Int("probes", 200, "probes per marking class")
	loadMbps := flag.Int("load-mbps", 0, "if >0, run an upstream load flow at this rate during the probe bursts")
	downMbps := flag.Int("down-mbps", 0, "if >0, run a cookie-gated downstream load flow at this rate during the probe bursts")
	runFull := flag.Bool("run", false, "run the full engine phase sequence and print the verdict (Result)")
	downTier := flag.Int("down-tier-mbps", 500, "provisioned downstream tier, Mbps (used by -run)")
	upTier := flag.Int("up-tier-mbps", 50, "provisioned upstream tier, Mbps (used by -run)")
	jsonOut := flag.Bool("json", false, "with -run, print the Result as JSON")
	submitURL := flag.String("submit", "", "with -run, POST the full RunReport to this dashboard ingest URL")
	isp := flag.String("isp", "", "cohort tag: ISP name (recorded in the report)")
	region := flag.String("region", "", "cohort tag: region (recorded in the report)")
	flag.Parse()
	if *serverAddr == "" {
		log.Fatal("--server is required")
	}

	clk := clock.System{}

	if *runFull {
		runValidation(clk, runOpts{
			serverAddr: *serverAddr,
			downBps:    uint64(*downTier) * 1_000_000,
			upBps:      uint64(*upTier) * 1_000_000,
			asJSON:     *jsonOut,
			submitURL:  *submitURL,
			isp:        *isp,
			region:     *region,
		})
		return
	}
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

type runOpts struct {
	serverAddr     string
	downBps, upBps uint64
	asJSON         bool
	submitURL      string
	isp, region    string
}

// runValidation wires real sockets into the full engine phase sequence, prints
// the single-source-of-truth Result, and optionally submits the full RunReport to
// the dashboard. The engine is seam-injected (see docs/ARCHITECTURE.md); this is
// the composition root that hands it production I/O: a marked-UDP probe socket and
// the cookie-gated downstream load controller.
func runValidation(clk clock.Clock, o runOpts) {
	conn, err := cnet.DialUDP(clk, o.serverAddr)
	if err != nil {
		log.Fatalf("dial probe socket: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Handshake(1); err != nil {
		log.Printf("probe handshake failed (continuing): %v", err)
	}

	down, err := cnet.DialUDPDownLoad(clk, o.serverAddr, 2)
	if err != nil {
		log.Fatalf("dial downstream load: %v", err)
	}
	defer down.Close()

	startedAt := time.Now()
	eng := engine.New(engine.Config{
		Clock:              clk,
		Conn:               conn,
		Load:               down,
		ProvisionedDownBps: o.downBps,
		ProvisionedUpBps:   o.upBps,
	})
	rep, err := eng.RunFull()
	if err != nil {
		log.Fatalf("run: %v", err)
	}

	rr := report.RunReport{
		Schema: report.SchemaVersion,
		Meta: report.Meta{
			RunID:              fmt.Sprintf("cli-%d", startedAt.UnixNano()),
			StartedAtUnixMs:    startedAt.UnixMilli(),
			ISP:                o.isp,
			Region:             o.region,
			DeviceClass:        "cli",
			ProvisionedDownBps: o.downBps,
			ProvisionedUpBps:   o.upBps,
		},
		Result: rep.Result,
		Telemetry: report.Telemetry{
			BaseRTTms:            rep.BaseRTTms,
			MarkingSurvival:      rep.MarkingSurvival,
			CapacityAchievedBps:  rep.CapacityAchievedBps,
			OvershootAchievedBps: rep.OvershootAchievedBps,
			DownLL:               rep.DownLL,
			DownClassic:          rep.DownClassic,
		},
	}

	if o.asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rr); err != nil {
			log.Fatalf("encode: %v", err)
		}
	} else {
		fmt.Printf("qoe-cli validation run\n")
		fmt.Printf("  server:   %s\n", o.serverAddr)
		fmt.Printf("  tiers:    %d down / %d up Mbps\n", o.downBps/1_000_000, o.upBps/1_000_000)
		fmt.Printf("  base RTT: %.2f ms · marking survival: %.1f%%\n", rep.BaseRTTms, rep.MarkingSurvival*100)
		fmt.Printf("  VERDICT:  %s\n", rep.Result.Verdict)
		for _, sr := range rep.Result.SubResults {
			fmt.Printf("    - %-16s %-12s %s\n", sr.Name, sr.Status, sr.Detail)
		}
		for _, cv := range rep.Result.Caveats {
			fmt.Printf("    caveat: %s\n", cv)
		}
	}

	if o.submitURL != "" {
		if err := submit(o.submitURL, rr); err != nil {
			log.Fatalf("submit: %v", err)
		}
		fmt.Printf("  submitted run %s to %s\n", rr.Meta.RunID, o.submitURL)
	}
}

// submit POSTs the report JSON to the dashboard ingest endpoint.
func submit(url string, rr report.RunReport) error {
	body, err := json.Marshal(rr)
	if err != nil {
		return err
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("ingest returned %s", resp.Status)
	}
	return nil
}
