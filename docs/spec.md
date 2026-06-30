# Spec: LLD/L4S Deployment Validation Tool (server + iOS + Android)

Status: approved draft, pre-implementation. Authored via `/spec` 2026-06-30.

## Context

A cable operator is enabling Low Latency DOCSIS (LLD) / L4S on subscriber lines
and needs field proof that the deployment (1) actually engages the low-latency
queue and (2) does no harm to classic traffic. No tool exists for this today.
Browser-based speed tests (Ookla, Cloudflare, Apple `networkQuality`) measure
loaded latency but cannot mark packets ECT(1) or NQB DSCP 45, so they cannot
steer traffic into the LL queue, isolate LL vs classic behavior, or test
upstream. This tool fills that gap with native apps that mark packets and a
controlled server that marks downstream and reports the truth back.

Stakeholders: **field testers** (technicians/recruited users behind LLD-enabled
equipment) who need a one-tap go/no-go; **network engineers** who need full
per-queue telemetry and fleet-wide distributions to sign off on the rollout.

## Current State

Greenfield. No prior code. Verified 2026-06-30.

Domain constraints:
- Downstream LL marking is set by the **server**; the CMTS classifies into the LL
  queue. A controlled VPS can drive this.
- Upstream LL marking must be set by the **client**, which a browser cannot do.
  Native apps can via `setsockopt(IP_TOS)`.
- A native client with a custom UDP protocol can have the receiver **echo the
  observed TOS byte**, giving direct evidence of marking survival and CE marking.

## Proposed Change

Three components against one shared protocol:

```
 iOS app (SwiftUI)  ─┐
                     ├─ shared Go core (gomobile) ──UDP test protocol──> VPS server (Go)
 Android (Compose) ──┘     sockets, ECT(1)/NQB marking,                  marks downstream ECT(1)/NQB,
                           both-direction flows, scalable CC,            echoes received TOS, timestamps,
                           timing, marking self-test                     ingests to Postgres,
                                                                          engineer web dashboard
```

- **VPS server (Go, Hostinger KVM2):** UDP endpoint that marks downstream packets
  ECT(1)/NQB DSCP 45, echoes the received TOS byte on every packet, timestamps
  both directions, counts CE marks, and serializes saturation tests (1-2 active
  slots, queue the rest). Ingests results to Postgres. Serves a basic-auth
  engineer web dashboard. Issues per-tester access codes. Uses batched syscalls
  (`sendmmsg`/`recvmmsg`) to hit target rate on 2 vCPU.
- **Shared Go core (gomobile, Tailscale-style):** the test engine. Raw UDP
  sockets, ECT(1)/NQB marking, multi-flow saturation, scalable congestion
  response, high-resolution timing, the marking self-test, and the bin-edge
  histogram builder. Written once, embedded in both apps and reused server-side.
- **iOS (SwiftUI, TestFlight) + Android (Compose, APK / Firebase App
  Distribution):** thin UIs. Field-tester verdict + engineer detail. Upload
  results to the VPS.

### Test sequence

| Phase | What | Duration |
|------|------|----------|
| 0 — Marking self-test | Send ECT(1) + NQB probes; server echoes TOS. Confirm marks survive before scoring. If stripped, warn and fall back (iOS: `Network.framework` service classes or NQB-only). | ~3 s |
| 1 — Idle baseline | Low-rate probes both directions. `base_rtt = min RTT` per direction. | ~5 s |
| 2 — Downstream loaded latency | Greedy **classic** flow (Not-ECT/ECT(0)) saturates downstream at target rate via 4 parallel UDP flows to fill the WAN path. Concurrent **LL-marked probe** and **classic-marked probe** measure `working_latency_delta`. | ~20 s |
| 3 — Upstream loaded latency | Same, device saturates upstream. | ~20 s |
| 4 — Does no harm | Leg A: classic flow alone, measure throughput + latency. Leg B: classic flow + aggressive LL flow concurrently. Compare classic degradation. Single run, no LLD A/B toggle. | ~20 s |
| (continuous) — Marking survival / CE | TOS echo + CE counting run across phases 2-4. | — |

`working_latency_delta = max(0, sample_rtt − base_rtt)`. RTT-based, robust to
clock offset (no time sync). Probes: ~50/s, 32-byte, sequenced, server echoes
with received TOS + server timestamp.

The load that builds the classic queue is a greedy classic-marked flow; the LL
probe is ECT(1) and should stay low because the dual-queue coupled AQM isolates
it. Classic-high under load is the validity check that the link was actually
congested. Exact congestion-control and rate tuning are calibrated in M0/M1.

### Pass/fail thresholds (calibrated in M0, defaults below)

1. **Working:** downstream LL-probe `p99(working_latency_delta) < 10 ms` AND
   classic-probe `p99 > 50 ms` under the same load. Same for upstream.
2. **Marking survival:** ≥ 99% of ECT(1)/NQB packets arrive still marked.
3. **No harm:** classic throughput in Leg B ≥ 90% of Leg A; classic
   `working_latency_delta` p99 in Leg B not worse than Leg A by more than a
   calibrated margin.

### Capacity

MVP targets **500 Mbps** (most testers' tier), well within KVM2's port and CPU
budget (~50k pps). Gig tier supported one-at-a-time via serialization. Multi-gig
is future scope (larger or multi-region server).

### Data model (Postgres)

```sql
create table tester (
  id            uuid primary key,
  access_code   text unique not null,
  label         text,
  created_at    timestamptz not null default now()
);

create table run (
  id                uuid primary key,
  tester_id         uuid not null references tester(id),
  started_at        timestamptz not null,
  app_version       text, os_version text, device_model text,
  conn_type         text,          -- wifi | wired
  wifi_band         text, wifi_rssi int,
  modem_model       text, modem_fw text,
  cmts_id           text, isp text, asn int,
  service_tier_down int, service_tier_up int,   -- Mbps
  geo_coarse        text,          -- city/ZIP, consented
  public_ip         inet,
  consent_location  boolean not null default false,
  verdict           text,          -- pass | fail | inconclusive
  base_rtt_down_us  int, base_rtt_up_us int
);

create table phase_result (
  id            uuid primary key,
  run_id        uuid not null references run(id),
  phase         text not null,         -- baseline | down_loaded | up_loaded | no_harm
  flow_type     text not null,         -- ll | classic
  direction     text not null,         -- down | up
  throughput_bps bigint,
  marking_survival numeric,            -- 0..1
  ce_mark_rate     numeric,            -- 0..1
  hist_edges_ms  numeric[] not null,   -- the fixed edge array
  hist_counts    bigint[] not null,    -- mergeable bin counts on working_latency_delta
  sample_count   bigint not null
);

create table raw_sample (              -- kept for re-analysis (~MB scale at <100 testers)
  run_id        uuid not null references run(id),
  phase         text not null,
  flow_type     text, direction text,
  seq           bigint,
  delta_us      int,                   -- working_latency_delta
  tos_sent      smallint, tos_echo smallint
);
```

Histograms are the mergeable unit for fleet rollups (sum `hist_counts`); raw
samples allow recomputation. Retention: raw 180 days, then aggregate-only.
Location coarse + consent-gated by default.

## Acceptance Criteria

1. M0: an iOS device sends ECT(1) and NQB DSCP 45 UDP to the VPS, and the
   server's TOS echo confirms ≥99% survival; if iOS bleaches ECN, the documented
   fallback is exercised and recorded. Pass/fail documented before apps are built.
2. Android app marks ECT(1)/NQB, capture-verified on the wire, ≥99% survival on a
   clean path.
3. A full run on an LLD-enabled 500 Mbps line produces a verdict and uploads all
   four phases + histograms + raw samples to Postgres.
4. The "working" verdict fires only when LL p99 delta < 10 ms AND classic p99
   delta > 50 ms; a deliberately under-loaded run reports `inconclusive`, not a
   false pass.
5. The "no harm" verdict compares Leg A vs Leg B and fails when classic
   throughput drops below 90%.
6. Engineer dashboard renders per-run CDFs from `hist_counts` and a fleet-wide
   merged distribution across all testers, behind basic auth.
7. Field-tester screen shows a single pass/fail/inconclusive with the three
   sub-results, no jargon.
8. Saturation tests serialize: a second concurrent request queues rather than
   splitting the port.
9. Tests written and passing.

## Testing Plan

| Layer | What | Count |
|------|------|------|
| Unit | histogram binning on fixed edges, `base_rtt` min calc, `working_latency_delta`, verdict logic, TOS encode/decode | +12 |
| Integration | loopback UDP run through all phases; marking echo; serialization slot lock | +6 |
| E2E | one real device per platform on an LLD line: full run → upload → dashboard | +2 |

## Rollback Plan

Stateless server behind a single systemd unit; rollback = redeploy prior binary.
Postgres migrations additive (new tables only); rollback = drop new tables. Apps
roll back via TestFlight/Firebase to the prior build. No shared production data at
risk.

## Effort Estimate (human / CC+gstack)

- Protocol + shared Go core: ~5 days / ~1 day
- VPS server (UDP engine, TOS echo, serialization, ingest, dashboard): ~4 days / ~1 day
- M0 iOS marking spike: ~1 day / ~2 h
- iOS app (SwiftUI + core bridge + TestFlight): ~3 days / ~0.5 day
- Android app (Compose + core bridge + Firebase): ~3 days / ~0.5 day
- Threshold calibration on real lines: ~2 days / n/a (needs hardware)

## Files Reference (anticipated)

| Path | Contents |
|------|---------|
| `core/` | shared Go test engine (sockets, marking, flows, histogram) |
| `server/` | VPS UDP server + ingest + dashboard |
| `server/migrations/` | Postgres schema |
| `ios/` | SwiftUI app + gomobile xcframework |
| `android/` | Compose app + gomobile aar |
| `docs/protocol.md` | wire protocol spec |

## Out of Scope

- Multi-gig (>1 Gbps) load — needs a larger/multi-region server.
- Browser/desktop client — cannot mark packets.
- Toggling LLD on/off at the CMTS for A/B — no-harm is measured single-run.
- Fine GPS location — coarse + consent only.
- Automated CMTS/headend config validation — subscriber-side only.

## Milestones

```
M0 iOS marking spike → M1 server + shared core → M2 Android app → M3 iOS app → M4 calibration + dashboard polish
```

M0 gates everything: if iOS can't mark, the fallback path is decided before app
work starts. Android leads because it distributes and marks most easily.
