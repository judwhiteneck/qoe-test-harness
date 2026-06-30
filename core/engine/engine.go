// Package engine orchestrates one validation run: it drives the phase sequence
// over a PacketConn + LoadController + Clock, feeds samples to the pure compute
// package, and returns a single compute.Result. It holds no measurement math of
// its own (that lives in compute) and no sockets (those are net), so it is fully
// testable against the in-memory loopback simulator.
package engine

import (
	"time"

	"github.com/judwhiteneck/qoe-test-harness/core/clock"
	"github.com/judwhiteneck/qoe-test-harness/core/compute"
	cnet "github.com/judwhiteneck/qoe-test-harness/core/net"
)

// DefaultLoadSettle is how long Run waits after a rate change before reading the
// achieved rate / probing, so an asynchronous real load flow has time to ramp and
// produce a measurement. The Fake clock makes this instantaneous in tests.
const DefaultLoadSettle = 500 * time.Millisecond

// Config parameterises a run. Clock, Conn, and Load are required; the rest take
// sensible defaults via New.
type Config struct {
	Clock clock.Clock
	Conn  cnet.PacketConn
	Load  cnet.LoadController

	Thresholds         compute.Thresholds
	ProvisionedDownBps uint64
	ProvisionedUpBps   uint64

	BaselineProbes        int
	LoadedProbes          int
	OvershootBps          uint64        // default: 1.3x ProvisionedDownBps
	CapacityToleranceFrac float64       // delivered/tier needed to localize the bottleneck
	StandingQueueMinMs    float64       // classic median delta to call a standing queue
	BaselinePct           float64       // percentile for base_rtt (e.g. 0.05 = p5)
	LoadSettle            time.Duration // wait after a rate change before reading/probing
}

// DefaultConfig holds the non-required defaults.
func DefaultConfig() Config {
	return Config{
		Thresholds:            compute.DefaultThresholds(),
		BaselineProbes:        200,
		LoadedProbes:          500,
		CapacityToleranceFrac: 0.9,
		StandingQueueMinMs:    30,
		BaselinePct:           0.05,
		LoadSettle:            DefaultLoadSettle,
	}
}

// Engine runs validations.
type Engine struct{ cfg Config }

// New fills unset fields from DefaultConfig and returns an Engine.
func New(cfg Config) *Engine {
	d := DefaultConfig()
	if cfg.Thresholds == (compute.Thresholds{}) {
		cfg.Thresholds = d.Thresholds
	}
	if cfg.BaselineProbes == 0 {
		cfg.BaselineProbes = d.BaselineProbes
	}
	if cfg.LoadedProbes == 0 {
		cfg.LoadedProbes = d.LoadedProbes
	}
	if cfg.CapacityToleranceFrac == 0 {
		cfg.CapacityToleranceFrac = d.CapacityToleranceFrac
	}
	if cfg.StandingQueueMinMs == 0 {
		cfg.StandingQueueMinMs = d.StandingQueueMinMs
	}
	if cfg.BaselinePct == 0 {
		cfg.BaselinePct = d.BaselinePct
	}
	if cfg.LoadSettle == 0 {
		cfg.LoadSettle = d.LoadSettle
	}
	if cfg.OvershootBps == 0 {
		cfg.OvershootBps = cfg.ProvisionedDownBps * 13 / 10
	}
	return &Engine{cfg: cfg}
}

// Report is a run's verdict plus the telemetry behind it, for submission to the
// engineer view. Run returns just the verdict; RunFull returns this.
type Report struct {
	Result               compute.Result
	BaseRTTms            float64
	MarkingSurvival      float64
	CapacityAchievedBps  uint64
	OvershootAchievedBps uint64
	DownLL               *compute.Histogram
	DownClassic          *compute.Histogram
}

// Run executes the phase sequence and returns the verdict. Validity gates
// (capacity, standing queue) are computed here and handed to compute.Evaluate,
// which applies inconclusive-precedence.
func (e *Engine) Run() (compute.Result, error) {
	rep, err := e.RunFull()
	return rep.Result, err
}

// RunFull executes the phase sequence and returns the verdict together with the
// telemetry behind it (mergeable histograms + scalars).
func (e *Engine) RunFull() (Report, error) {
	c := e.cfg
	c.Load.Stop()

	// Phase 1: idle baseline -> base_rtt floor.
	baseSamples, _, _, err := BatchProbe(c.Clock, c.Conn, cnet.NotECT, c.BaselineProbes)
	if err != nil {
		return Report{}, err
	}
	baseRTT := compute.BaseRTT(baseSamples, c.BaselinePct)

	// Capacity gate: can we reach the provisioned tier? If so the access link is
	// the bottleneck (bottleneck localization).
	if err := c.Load.SetRateBps(c.ProvisionedDownBps); err != nil {
		return Report{}, err
	}
	c.Clock.Sleep(c.LoadSettle) // let an async flow ramp before reading achieved
	achieved := c.Load.AchievedBps()
	capacityOK := float64(achieved) >= c.CapacityToleranceFrac*float64(c.ProvisionedDownBps)

	// Phase 2: loaded (overshoot to build a standing queue). LL probe vs classic
	// probe under the same load.
	if err := c.Load.SetRateBps(c.OvershootBps); err != nil {
		return Report{}, err
	}
	c.Clock.Sleep(c.LoadSettle) // let the queue (if any) build before probing
	llHist, llSurvival, _, err := e.histBatch(cnet.LLMark, c.LoadedProbes, baseRTT)
	if err != nil {
		return Report{}, err
	}
	classicHist, _, _, err := e.histBatch(cnet.NotECT, c.LoadedProbes, baseRTT)
	if err != nil {
		return Report{}, err
	}
	standingQueueOK := classicHist.Quantile(0.5) >= c.StandingQueueMinMs

	// Phase 4: no-harm A/B/A (simplified) — classic alone vs classic with LL present.
	soloThroughput := c.Load.AchievedBps()
	soloHist, _, _, err := e.histBatch(cnet.NotECT, c.LoadedProbes, baseRTT)
	if err != nil {
		return Report{}, err
	}
	withLLThroughput := c.Load.AchievedBps()
	withHist, _, _, err := e.histBatch(cnet.NotECT, c.LoadedProbes, baseRTT)
	if err != nil {
		return Report{}, err
	}
	c.Load.Stop()

	in := compute.Inputs{
		CapacityOK:                 capacityOK,
		StandingQueueOK:            standingQueueOK,
		MarkingSurvival:            llSurvival,
		Down:                       compute.DirectionInputs{LL: llHist, Classic: classicHist},
		Up:                         compute.DirectionInputs{LL: llHist, Classic: classicHist},
		ClassicSoloThroughputBps:   float64(soloThroughput),
		ClassicWithLLThroughputBps: float64(withLLThroughput),
		ClassicSoloP99Ms:           soloHist.Quantile(0.99),
		ClassicWithLLP99Ms:         withHist.Quantile(0.99),
	}
	return Report{
		Result:               compute.Evaluate(in, c.Thresholds),
		BaseRTTms:            baseRTT,
		MarkingSurvival:      llSurvival,
		CapacityAchievedBps:  achieved,
		OvershootAchievedBps: withLLThroughput,
		DownLL:               llHist,
		DownClassic:          classicHist,
	}, nil
}

// BatchProbe sends n probes of the given marking through conn and drains all
// echoes, returning RTT samples (ms), LL marking survival, and CE rate. Each
// probe is timestamped at send and matched to the send time the echo carries
// back, so RTT needs no clock sync. Exported for diagnostic clients (e.g. the CLI).
func BatchProbe(clk clock.Clock, conn cnet.PacketConn, marking cnet.Marking, n int) (rttsMs []float64, survival, ce float64, err error) {
	for i := 0; i < n; i++ {
		p := cnet.Probe{Seq: uint64(i + 1), SentAt: clk.Now(), Marking: marking}
		if err = conn.SendProbe(p); err != nil {
			return nil, 0, 0, err
		}
	}
	var survived, total, ceCount int
	for {
		echo, rerr := conn.RecvEcho()
		if rerr == cnet.ErrNoEcho {
			break
		}
		if rerr != nil {
			return nil, 0, 0, rerr
		}
		rttsMs = append(rttsMs, float64(echo.RecvAt.Sub(echo.SentAt).Nanoseconds())/1e6)
		total++
		if markingSurvived(marking, echo.TOSObserved) {
			survived++
		}
		if echo.CESeen {
			ceCount++
		}
	}
	if total > 0 {
		survival = float64(survived) / float64(total)
		ce = float64(ceCount) / float64(total)
	}
	return rttsMs, survival, ce, nil
}

// histBatch is BatchProbe + a working-delta histogram over baseRTT.
func (e *Engine) histBatch(marking cnet.Marking, n int, baseRTT float64) (*compute.Histogram, float64, float64, error) {
	samples, survival, ce, err := BatchProbe(e.cfg.Clock, e.cfg.Conn, marking, n)
	if err != nil {
		return nil, 0, 0, err
	}
	h := compute.NewDefaultHistogram()
	for _, s := range samples {
		h.Observe(compute.WorkingDelta(s, baseRTT))
	}
	return h, survival, ce, nil
}

// markingSurvived reports whether an LL probe's ECT(1) bit survived. Classic
// probes carry nothing to survive, so they always count as survived.
func markingSurvived(intended, observed cnet.Marking) bool {
	if intended&cnet.ECT1 == 0 {
		return true
	}
	return observed&cnet.ECT1 != 0
}
