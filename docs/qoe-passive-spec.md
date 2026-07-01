# Passive Real-QoE Monitor — spec (MVP)

Companion to the active L4S detector. Status: specced, not built. Authored via
`/spec`.

## Context
The harness today is **active-only**: `qoe-cli` fires synthetic marked probes and
`core/engine` proves the low-latency queue *can* work (verdict in `core/compute`).
It has zero visibility into whether **real** gaming / video-conference / streaming
sessions on the line are actually good, or whether they are even getting L4S
treatment. This adds a **passive** companion: observe real flows, score their
experience from metadata, and correlate each flow with its ECN/DSCP marks. The
payoff for a rollout is the claim that matters — *"this real Zoom/Valorant/YouTube
session landed in the LL queue and its experience was good"* — not just "a probe
says the queue exists."

## Dependencies
- **Live deployment requires the x86 OpenWrt router platform — a separate project**
  (see [`docs/openwrt-router-spec.md`](openwrt-router-spec.md)). This spec targets a
  generic capable in-path Linux host that runs Zeek+nDPI and sees all household
  traffic; the OpenWrt x86 router is the intended production instance of that host.
- **MVP development and all tests need only pcap fixtures** — no router required.
  The two projects are decoupled for development; the router is needed only to run
  live.

## Current state (verified, this repo)
- `core/report/report.go` — `RunReport{Schema, Meta, Result, Telemetry}` is the
  submitted/stored contract; `Meta` carries cohort tags (ISP/region/device).
- `core/compute/` — `Histogram` (fixed mergeable edges), `CDF()`,
  `WorkingDelta(sample, base)`, `BaseRTT(samples, pct)`. Reusable latency math.
- `storage/store.go` — `Store{Save, List}` + `Filter`; in-memory + Postgres.
- `dashboard/dashboard.go` — `/ingest`, `/api/runs`, `/api/cdf`, `/`.
- No passive capture, per-flow records, or app classification exist yet.

## Proposed change (MVP)
A **passive monitor** runs on the in-path Linux host, sees all household traffic,
and emits one **`SessionReport`** per observed flow into the existing
dashboard/storage.

```
 Zeek + nDPI plugin                qoe-monitor (Go shim)            existing dashboard/storage
 ── reads iface OR pcap ──►  conn/ssl/quic/l4s JSON logs ──►  parse → SessionReport ──► POST /ingest/session
    app-ID (nDPI), SNI,         (tailed / read -r)            + QoE score (3 classes)   (new session store)
    RTT, retransmits,                                          + L4S correlation
    per-flow ECN tally
    (l4s.zeek script)
```

Three pieces:
1. **Zeek config + `l4s.zeek`** — per-flow records with app-ID (nDPI), SNI/ASN,
   byte/packet counts, TCP-handshake RTT, retransmits, and a **per-connection ECN
   codepoint tally** (ECT(0)/ECT(1)/CE) + observed DSCP. Runs live on an interface
   *and* offline via `zeek -r fixture.pcap` (what makes it hardware-free to test).
2. **`cmd/qoe-monitor` (Go)** — reads Zeek JSON logs (tail live / read offline),
   converts each flow to a `SessionReport`, computes the QoE score, POSTs to the
   dashboard.
3. **`core/session` + dashboard/storage extension** — the `SessionReport`
   contract, a session store, `/ingest/session`, `/api/sessions`, a sessions view.

### The `SessionReport` contract (`core/session/session.go`, pure)
```go
type SessionReport struct {
    Schema      int
    SessionID   string   // stable hash of 5-tuple + start
    StartedAtUnixMs, DurationMs int64
    AppClass    string   // gaming | video_conf | streaming | other
    AppName     string   // nDPI/SNI best-effort: "zoom","youtube","valorant"
    Confidence  float64
    Transport   string   // tcp | udp | quic
    RemoteASN   int      // NOT the raw remote IP (privacy)
    BytesUp, BytesDown, PktsUp, PktsDown uint64

    // L4S correlation (from the IP header, cleartext)
    ECT1Frac, ECT0Frac, CEFrac float64
    DSCP        int            // observed (e.g. 45 = NQB)

    // latency / loss signals
    RTTms              *compute.Histogram // reuse the fixed-edge histogram
    LoadLatencyDeltaMs *compute.Histogram // working-delta during saturated windows
    RetransFrac, JitterMs float64

    // QoE
    QoEScore   float64  // 0..100
    QoERating  string   // good | fair | poor
    Reasons    []string // human-readable drivers
    Meta       report.Meta // reuse cohort tags
}
```

### QoE scoring (v1 = honest heuristics, validated on pcaps, calibrated later)
A shared, deterministic, unit-tested function maps metadata to a 0–100 score +
rating per app class:
- **Gaming** (small-packet UDP): dominated by **jitter + loss**, then
  load-latency-delta.
- **Video conferencing** (RTP-like UDP): **loss bursts + freeze proxy (cadence
  gaps) + jitter**.
- **Streaming** (TLS/QUIC bulk): **stall proxy** (throughput-starvation gaps in the
  download cadence) + sustained goodput vs a resolution threshold. True
  Requet/BUFFEST rebuffer modeling is follow-on.
- **Cross-cutting:** `LoadLatencyDeltaMs` reuses `compute.WorkingDelta` +
  `Histogram`/`CDF` — the same math the active side uses — measured on real flows
  during high-utilization windows.

### L4S correlation
`l4s.zeek` tallies ECN codepoints and DSCP from the IP header per flow. The
`SessionReport` exposes `ECT1Frac`/`CEFrac`/`DSCP`, so the dashboard can answer per
app class: *are real-time flows actually marked ECT(1)/NQB, and do marked flows
show lower `LoadLatencyDeltaMs` than unmarked ones?*

### Dashboard/storage extension
`storage.Store` gains session methods (or a parallel `SessionStore`); dashboard
adds `/ingest/session`, `/api/sessions` (cohort + app-class filters), and a view
overlaying marked-vs-unmarked load-latency per app class. Reuses `report.Meta`
filters.

## Acceptance criteria (MVP)
1. `zeek -r testdata/<fixture>.pcap` + `qoe-monitor` produces a `SessionReport` per
   flow with populated app-class, ECN fractions, byte/packet counts, and RTT.
2. A fixture with **one L4S-marked flow and one unmarked flow under load** yields
   two sessions where the **marked flow's `LoadLatencyDeltaMs` p99 is measurably
   lower** than the unmarked flow's.
3. A **QoE score + rating is produced for all three app classes** (gaming,
   video_conf, streaming) from at least one representative pcap each, with
   documented scoring formulas.
4. `SessionReport` round-trips JSON and is accepted by `/ingest/session`;
   `/api/sessions` returns it filtered by app-class.
5. **No payloads are ever written** — metadata-only; raw remote IPs reduced to ASN
   (test asserts absence).
6. Unit tests on scoring (table-driven over synthetic features) + an offline
   end-to-end test (pcap → session → ingest). Pure `core/session` + scoring ≥90%
   coverage; monitor ≥70%.

## Testing plan
| Layer | What | Count |
|---|---|---|
| Unit | QoE scoring per app class over synthetic feature vectors | +9 |
| Unit | `SessionReport` JSON round-trip + privacy (no IP/payload) | +2 |
| Integration | `zeek -r fixture.pcap` → shim → `SessionReport` (3 app fixtures + 1 marked/unmarked-under-load) | +4 |
| Integration | POST `/ingest/session` → `/api/sessions` filter | +2 |

Fixtures are small captured/synthesized pcaps in `testdata/` — no live hardware to
develop.

## Technical risks
- **ECN extraction in Zeek.** `conn.log` lacks ECN by default; `l4s.zeek` must tally
  codepoints via packet-level events (perf cost at line rate). Fallback: a
  `gopacket` ECN tally in the shim over the same pcap/interface.
- **QUIC RTT.** Spin bit is often disabled; QUIC RTT/loss may be unavailable —
  degrade gracefully (score without it, lower confidence).
- **nDPI-plugin build for Zeek** on the host image is real setup effort (belongs to
  the router spec's platform image).

## Effort estimate
- `core/session` contract + scoring framework + tests: ~4h
- `l4s.zeek` (flow + ECN/DSCP tally) + fixtures: ~5h
- `cmd/qoe-monitor` shim (log parse → session → POST): ~4h
- storage/dashboard session extension + view: ~5h
- ≈ **18h / 2–3 days** (excludes the router platform build)

## Files reference
| File | Change |
|---|---|
| `core/session/session.go` (new) | `SessionReport` + validation, pure |
| `core/session/qoe.go` (new) | scoring per app class (reuses `core/compute`) |
| `cmd/qoe-monitor/main.go` (new) | Zeek-log → session → `/ingest/session` |
| `zeek/l4s.zeek` (new) | per-flow ECN/DSCP tally + app-ID export |
| `storage/store.go` | session Save/List (or `SessionStore`) |
| `dashboard/dashboard.go` | `/ingest/session`, `/api/sessions`, view |
| `testdata/*.pcap` (new) | fixtures: 3 app classes + marked/unmarked-under-load |

## Out of scope (MVP)
Building the OpenWrt x86 router platform (separate spec); real-time alerting;
multi-household fleet aggregation; defeating ECH/encrypted-DNS; deep per-app models
(true rebuffer/Requet, MOS); payload storage of any kind; port-mirror/inline-bridge
vantages (the x86 in-path host is the one supported vantage for v1).

## Follow-on epic (later)
Real rebuffer/bitrate inference (Requet/BUFFEST); spin-bit / active latency anchor
per flow; VoIP MOS; mirror/bridge vantages; fleet rollup + alerting; ECH-resilient
app-ID.
