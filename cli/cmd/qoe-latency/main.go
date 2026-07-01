// Command qoe-latency reports ABSOLUTE RTT for the classic and low-latency
// (ECT(1)+NQB) marking classes, idle and under load. Probes of the two classes
// are INTERLEAVED so neither systematically sees a more-soaked buffer. It runs
// each load direction with BOTH classic-marked and LL-marked load, so an L4S
// L-queue is actually filled when the load is LL. For the LL probe class it
// reports, per leg (up = client->server, down = server->client echo), the
// survival of the ECN ECT(1) bit AND the NQB DSCP-45 value separately, plus CE.
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/judwhiteneck/qoe-test-harness/core/clock"
	"github.com/judwhiteneck/qoe-test-harness/core/compute"
	cnet "github.com/judwhiteneck/qoe-test-harness/core/net"
)

type rtt struct{ p50, p90, p99, max float64 }

type mark struct{ upECT, upNQB, upCE, dnECT, dnNQB, dnCE float64 }

type scenario struct {
	name        string
	classic, ll rtt
	nCl, nLl    int
	m           mark
}

// probe sends nPerClass*2 interleaved probes (odd seq = LL, even = classic),
// drains the echoes, buckets absolute RTTs by class, and for LL computes per-leg
// ECT / NQB / CE rates.
func probe(clk clock.Clock, conn cnet.PacketConn, name string, nPerClass int) scenario {
	total := nPerClass * 2
	for i := 1; i <= total; i++ {
		m := cnet.NotECT
		if i%2 == 1 {
			m = cnet.LLMark
		}
		if err := conn.SendProbe(cnet.Probe{Seq: uint64(i), SentAt: clk.Now(), Marking: m}); err != nil {
			log.Fatalf("%s send: %v", name, err)
		}
	}
	var cl, ll []float64
	var upECT, upNQB, upCE, dnECT, dnNQB, dnCE int
	for {
		e, err := conn.RecvEcho()
		if err == cnet.ErrNoEcho {
			break
		}
		if err != nil {
			log.Fatalf("%s recv: %v", name, err)
		}
		r := float64(e.RecvAt.Sub(e.SentAt).Nanoseconds()) / 1e6
		if e.Seq%2 == 1 { // LL
			ll = append(ll, r)
			if e.TOSObserved&cnet.ECT1 != 0 {
				upECT++
			}
			if e.TOSObserved>>2 == 45 {
				upNQB++
			}
			if e.CESeen {
				upCE++
			}
			if e.DownTOSObserved&cnet.ECT1 != 0 {
				dnECT++
			}
			if e.DownTOSObserved>>2 == 45 {
				dnNQB++
			}
			if e.DownCE {
				dnCE++
			}
		} else {
			cl = append(cl, r)
		}
	}
	pct := func(c int) float64 {
		if len(ll) == 0 {
			return 0
		}
		return float64(c) / float64(len(ll)) * 100
	}
	q := func(r []float64) rtt {
		return rtt{compute.BaseRTT(r, 0.5), compute.BaseRTT(r, 0.9), compute.BaseRTT(r, 0.99), compute.BaseRTT(r, 1)}
	}
	return scenario{
		name: name, classic: q(cl), ll: q(ll), nCl: len(cl), nLl: len(ll),
		m: mark{pct(upECT), pct(upNQB), pct(upCE), pct(dnECT), pct(dnNQB), pct(dnCE)},
	}
}

func main() {
	server := flag.String("server", "", "test server host:port (required)")
	probes := flag.Int("probes", 300, "probes per class per scenario")
	downMbps := flag.Int("down-mbps", 400, "downstream load rate")
	upMbps := flag.Int("up-mbps", 45, "upstream load rate")
	settleMs := flag.Int("settle-ms", 2000, "warmup after starting a load flow before probing")
	llFirst := flag.Bool("ll-first", false, "order-swap control: run LL-marked load before classic-marked load in each direction")
	flag.Parse()
	if *server == "" {
		log.Fatal("-server is required")
	}
	clk := clock.System{}
	conn, err := cnet.DialUDP(clk, *server)
	if err != nil {
		log.Fatalf("dial probe socket: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Handshake(1); err != nil {
		log.Printf("handshake (continuing): %v", err)
	}
	settle := time.Duration(*settleMs) * time.Millisecond

	downScen := func(name string, lm cnet.Marking) scenario {
		d, err := cnet.DialUDPDownLoad(clk, *server, 4)
		if err != nil {
			log.Fatalf("dial down load: %v", err)
		}
		d.SetMarking(lm)
		if err := d.SetRateBps(uint64(*downMbps) * 1_000_000); err != nil {
			log.Fatalf("down load: %v", err)
		}
		time.Sleep(settle)
		ach := float64(d.AchievedBps()) / 1e6
		s := probe(clk, conn, fmt.Sprintf("%s %.0fM", name, ach), *probes)
		d.Stop()
		d.Close()
		time.Sleep(700 * time.Millisecond)
		return s
	}
	upScen := func(name string, lm cnet.Marking) scenario {
		u, err := cnet.DialUDPLoad(clk, *server)
		if err != nil {
			log.Fatalf("dial up load: %v", err)
		}
		if err := u.SetMarking(lm); err != nil {
			log.Fatalf("up mark: %v", err)
		}
		if err := u.SetRateBps(uint64(*upMbps) * 1_000_000); err != nil {
			log.Fatalf("up load: %v", err)
		}
		time.Sleep(settle)
		ach := float64(u.AchievedBps()) / 1e6
		s := probe(clk, conn, fmt.Sprintf("%s %.0fM", name, ach), *probes)
		u.Stop()
		u.Close()
		time.Sleep(700 * time.Millisecond)
		return s
	}

	scen := []scenario{probe(clk, conn, "idle", *probes)}
	if *llFirst {
		scen = append(scen, downScen("DOWN/LL-load", cnet.LLMark), downScen("DOWN/cl-load", cnet.NotECT))
		scen = append(scen, upScen("UP/LL-load", cnet.LLMark), upScen("UP/cl-load", cnet.NotECT))
	} else {
		scen = append(scen, downScen("DOWN/cl-load", cnet.NotECT), downScen("DOWN/LL-load", cnet.LLMark))
		scen = append(scen, upScen("UP/cl-load", cnet.NotECT), upScen("UP/LL-load", cnet.LLMark))
	}

	fmt.Printf("\nserver %s, %d probes/class/cell, interleaved, settle=%dms, down=%d up=%d Mbps\n\n",
		*server, *probes, *settleMs, *downMbps, *upMbps)

	fmt.Println("ABSOLUTE RTT (ms)")
	fmt.Printf("%-18s %8s %8s %8s %8s\n", "scenario", "p50", "p90", "p99", "max")
	for _, s := range scen {
		fmt.Printf("%-14s cl   %8.2f %8.2f %8.2f %8.2f\n", s.name, s.classic.p50, s.classic.p90, s.classic.p99, s.classic.max)
		fmt.Printf("%-14s LL   %8.2f %8.2f %8.2f %8.2f\n", "", s.ll.p50, s.ll.p90, s.ll.p99, s.ll.max)
	}

	fmt.Println("\nLL-CLASS MARKING (percent of LL probes) — up = client->server, dn = server->client echo")
	fmt.Printf("%-18s %7s %7s %6s   %7s %7s %6s\n", "scenario", "up_ECT", "up_NQB", "up_CE", "dn_ECT", "dn_NQB", "dn_CE")
	for _, s := range scen {
		fmt.Printf("%-18s %6.1f%% %6.1f%% %5.1f%%   %6.1f%% %6.1f%% %5.1f%%\n",
			s.name, s.m.upECT, s.m.upNQB, s.m.upCE, s.m.dnECT, s.m.dnNQB, s.m.dnCE)
	}
}
