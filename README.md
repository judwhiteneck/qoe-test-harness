# LLD/L4S Deployment Validation Tool

Field tool to validate a Low Latency DOCSIS (LLD) / L4S deployment from behind
newly-enabled subscriber equipment. It proves two things and collects the data to
back them up:

1. **It works** — the low-latency queue actually engages under load.
2. **It does no harm** — classic traffic is not degraded when LL traffic is present.

Browser speed tests (Ookla, Cloudflare, Apple `networkQuality`) measure loaded
latency but cannot mark packets ECT(1) or NQB DSCP 45, so they cannot steer
traffic into the LL queue, isolate LL vs classic behavior, or test upstream. This
tool uses native apps that mark packets plus a controlled server that marks
downstream and echoes back the truth.

## Architecture

```
 wired CLI (Go) ──┐
 Android (later) ─┼─ shared Go core ──UDP test protocol──> VPS server (Go, Hostinger, off-net)
 iOS (gated) ─────┘   pure-compute + sockets, marking,      marks downstream ECT(1)/NQB,
                      overshoot load, standing-queue          echoes received TOS, timestamps,
                      confirm, timing, histograms             return-routability handshake,
                                                              capacity gate, 1-slot serialize,
                                                              Postgres ingest, dashboard
```

| Component | Tech | Role |
|---|---|---|
| `server/` | Go (Hostinger KVM2, off-net) | UDP endpoint, TOS echo, return-routability handshake, capacity gate, 1-slot serialization, Postgres ingest, engineer dashboard |
| `core/` | Go | shared engine; `compute/` pure (histograms/verdict/stats) + `net/` sockets/marking/load |
| `cli/` | Go (wired) | primary wedge: technician runs wired to the modem |
| `android/` | Compose + core (gomobile/JNI) | field app, after CLI proves out |
| `ios/` | SwiftUI + native net + shared compute | demand-gated |

> v2 (post-autoplan): wired CLI-first instead of two native apps day-one — WiFi
> confounds the DOCSIS measurement and gomobile GC jitter pollutes the p99 tail.
> Server stays off-net, made valid by a per-run capacity-confirmation gate. See
> [`docs/autoplan-review.md`](docs/autoplan-review.md).

## Key measurement decisions

- **Metric is deviation from idle latency.** `working_latency_delta = max(0, sample_rtt − base_rtt)`,
  where `base_rtt` is the **min** RTT measured during the idle phase. RTT-based, so
  no clock sync is required.
- **Distributions, not scalars.** Each phase stores a fixed-bin-edge histogram so
  results merge across testers (you cannot average percentiles). Raw samples kept
  for re-analysis. Bin edges (ms), denser below 30 ms:
  `[0, 0.5, 1, 1.5, 2, 3, 4, 5, 7.5, 10, 12.5, 15, 20, 25, 30, 40, 50, 75, 100, 150, 250, 500, 1000, +∞]`
- **Marking survival is observable.** The server echoes the received TOS byte on
  every packet, so the client measures what fraction of ECT(1)/NQB marks survived
  and the CE-mark rate end-to-end.

## Pass / fail (defaults, calibrated in M0)

- **Working:** LL-probe `p99(working_latency_delta) < 10 ms` AND classic-probe `p99 > 50 ms` under the same load.
- **Marking survival:** ≥ 99% of ECT(1)/NQB packets arrive still marked.
- **No harm:** classic throughput with LL present ≥ 90% of classic-alone; classic latency not materially worse.

## Status

Core implemented and tested; clients (Android/iOS) pending; M0 spikes need
hardware. Built so far:

- `core/compute` — histogram, stats, verdict (pure, 93%+ coverage)
- `core/protocol` — wire format, return-routability handshake, load-stat report
- `core/clock`, `core/net` — injectable time + I/O seams, plus a loopback simulator
- `core/engine` — the phase sequence → verdict, tested across pass/fail/inconclusive,
  now wired to real sockets (`qoe-cli -run`) with a clock-driven load-settle so
  asynchronous real flows ramp before the capacity gate reads them
- `core/net` real UDP socket (Linux) with `IP_TOS`/`IP_RECVTOS` marking
- `core/net` load generator — clock-driven `Pacer` + throughput `Meter` (pure,
  unit-tested); a real upstream `UDPLoad` controller whose achieved rate is
  measured at the server; and a cookie-gated downstream `UDPDownLoad` (handshake
  → `Start` with cookie → server-paced flow), with the **anti-amplification gate
  tested** (a forged-cookie `Start` yields zero downstream bytes). Validated over
  loopback: paced rate ≈ achieved in both directions, probes run concurrently
- `server/` + `cli/` binaries, validated end to end over real UDP (marking
  survives; `qoe-cli -load-mbps N -down-mbps M` probes under live bidirectional load)
- `core/report` — the submitted/stored run record (verdict + mergeable
  histograms + cohort metadata), pure and JSON round-tripped
- `storage` — `Store` interface with an in-memory impl (tested) and a Postgres
  adapter that takes an injected `*sql.DB` (stdlib only; `storage/schema.sql`)
- `dashboard` — engineer view: `/ingest` (POST report), `/api/cdf` (cohort-merged
  LL-vs-classic CDF), `/api/runs` (filtered table), `/` (self-contained HTML
  overlay); tested with `httptest`. `qoe-cli -run -submit URL` posts a report end
  to end

Full spec in [`docs/spec.md`](docs/spec.md), M0 plan in [`docs/m0-spec.md`](docs/m0-spec.md),
architecture and required practices in [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) /
[`docs/ENGINEERING.md`](docs/ENGINEERING.md). Deploying (VPS server + tester CLI,
incl. Windows/WSL2): [`docs/DEPLOY.md`](docs/DEPLOY.md).

Planned (specced, not built): passive real-QoE monitor that observes real
gaming/VC/streaming sessions and correlates them with L4S marking —
[`docs/qoe-passive-spec.md`](docs/qoe-passive-spec.md), built on a separate x86
OpenWrt router platform [`docs/openwrt-router-spec.md`](docs/openwrt-router-spec.md).

## Build & run

Requires Go (Linux for the socket layer). The measurement core and simulator are
cross-platform; the real UDP server/CLI use `IP_TOS`/`IP_RECVTOS` and are Linux-only.

```
make ci                 # gofmt, vet, import-boundary, tests
go build ./...

# end to end over localhost:
go run ./server/cmd/qoe-server -addr 127.0.0.1:7700 -secret <secret> &
go run ./cli/cmd/qoe-cli -server 127.0.0.1:7700                              # idle probes
go run ./cli/cmd/qoe-cli -server 127.0.0.1:7700 -load-mbps 80 -down-mbps 120 # probes under load
go run ./cli/cmd/qoe-cli -server 127.0.0.1:7700 -run -down-tier-mbps 500     # field pass/fail
go run ./cli/cmd/qoe-cli -server 127.0.0.1:7700 -run -engineer               # + telemetry

# engineer dashboard (in-memory store) + submit a run to it:
go run ./dashboard/cmd/qoe-dashboard -addr :8080 &
go run ./cli/cmd/qoe-cli -server 127.0.0.1:7700 -run -isp Acme -region west \
  -submit http://127.0.0.1:8080/ingest
# then open http://127.0.0.1:8080 for the LL-vs-classic CDF overlay
```

The CLI is the wedge. Default mode is a probe-level diagnostic: handshake +
marked probe bursts → marking survival + working-latency percentiles, optionally
measured while upstream (`-load-mbps`) and/or cookie-gated downstream
(`-down-mbps`) load saturate the path. `-run` executes the full engine phase
sequence (baseline → capacity gate → overshoot → no-harm) over real sockets and
reports the verdict **role-switched** (spec "Both, role-switched"): a field tech
gets a clean pass/fail checklist with plain-language guidance; `-engineer` prints
the telemetry; `-json` emits the raw `RunReport` contract; `-submit URL` posts it
to the dashboard. The CLI marks packets itself (`IP_TOS`), so no separate helper
is needed — that was a requirement of the abandoned browser design.

`-run` already produces a real verdict; over loopback it correctly returns
`inconclusive` ("no sustained standing queue formed") because loopback has no
bottleneck. A genuine pass needs a real congested link plus the M0-calibrated
thresholds — standing-queue formation is M0/S3 on hardware.

## Milestones

```
M0 (spikes, gate all) → M1 server + core + wired CLI + dashboard → M2 Android → M3 iOS (gated) → M4 calibration + polish
```

M0 spikes every kill-shot before any client build: marking survival + on-wire
capture, DSCP/ECN transit survival on the target network, overshoot standing-queue
formation, capacity-confirmation method, server perf at ≤600 Mbps with TOS-echo,
return-routability handshake, and a negative control (LLD-off must not pass).

## Out of scope (MVP)

Multi-gig / gig-from-a-gig-port (needs on-net or bigger server), browser/desktop
GUI client, CMTS-side A/B toggling of LLD, fine GPS location, automated headend
config validation.
