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
 iOS app (SwiftUI)  ─┐
                     ├─ shared Go core (gomobile) ──UDP test protocol──> VPS server (Go)
 Android (Compose) ──┘     sockets, ECT(1)/NQB marking,                  marks downstream ECT(1)/NQB,
                           both-direction flows, scalable CC,            echoes received TOS, timestamps,
                           timing, marking self-test                     ingests to Postgres,
                                                                          engineer web dashboard
```

| Component | Tech | Role |
|---|---|---|
| `server/` | Go (Hostinger KVM2) | UDP test endpoint, TOS echo, serialization, Postgres ingest, engineer dashboard |
| `core/` | Go (gomobile) | shared test engine: sockets, marking, flows, timing, histograms, self-test |
| `ios/` | SwiftUI + core (TestFlight) | field-tester verdict + engineer detail |
| `android/` | Compose + core (APK / Firebase) | field-tester verdict + engineer detail |

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

Pre-implementation. Full spec in [`docs/spec.md`](docs/spec.md), wire protocol in
[`docs/protocol.md`](docs/protocol.md), schema in
[`server/migrations/0001_init.sql`](server/migrations/0001_init.sql).

## Milestones

```
M0 iOS marking spike → M1 server + shared core → M2 Android app → M3 iOS app → M4 calibration + dashboard
```

M0 gates everything: if iOS bleaches ECN, the fallback (`Network.framework`
service classes or NQB-only) is decided before app work starts. Android leads
because it distributes and marks most easily.

## Out of scope (MVP)

Multi-gig (>1 Gbps) load, browser/desktop client, CMTS-side A/B toggling of LLD,
fine GPS location, automated headend config validation.
