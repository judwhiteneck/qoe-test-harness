# Spec: LLD/L4S Deployment Validation Tool (server + wired CLI + mobile)

Status: v2, post-autoplan review (CEO/Eng/Design dual-voice). Pre-implementation.
Revised 2026-06-30. v1 history in git. Review report: `docs/autoplan-review.md`.

## Context

A cable operator is enabling Low Latency DOCSIS (LLD) / L4S on subscriber lines
and needs field proof that the deployment (1) actually engages the low-latency
queue and (2) does no harm to classic traffic. No subscriber-side tool marks
packets ECT(1)/NQB and reports per-queue behavior; browser speed tests (Ookla,
Cloudflare, Apple `networkQuality`) measure loaded latency but cannot mark
packets, isolate LL vs classic, or test upstream. They are used here as
corroborating baselines, not rebuilt.

Stakeholders: **field technicians** (operator staff) who run a wired test and get
a go/no-go; **network engineers** who need per-queue telemetry and fleet
distributions to sign off the rollout.

## What changed in v2 (autoplan outcome)

- **Client: wired Go CLI first**, not two native apps. Technicians run it on a
  laptop/SBC tethered to the modem. Removes WiFi confounding and gomobile risk.
  Android follows; iOS is demand-gated (only if consumer phone-on-WiFi testing is
  explicitly required).
- **Server stays off-net (Hostinger KVM2)**, made valid by a per-run
  **capacity-confirmation gate**: measured throughput must reach ~the tester's
  provisioned tier, proving the access link (not a WAN hop) is the bottleneck.
  DSCP 45 is known not to be bleached on the target network; still verified
  per-run via the TOS echo.
- **Overshoot to build a standing queue**, **return-routability handshake**
  (anti-amplification), **negative control**, **wrong-link detection**,
  **A/B/A no-harm**, **p1/p5 baseline**, **≤600 Mbps at 1 serialized slot**, and a
  pinned UI/dashboard contract — all folded in (see below).

## Architecture

```
 wired CLI (Go) ──┐
 Android (later) ─┼─ shared Go core ──UDP test protocol──> VPS server (Go, Hostinger, off-net)
 iOS (gated) ─────┘   pure-compute shared (histograms,      marks downstream ECT(1)/NQB DSCP45,
                      verdict, stats) + sockets;            echoes received TOS, timestamps,
                      marking, overshoot load,              return-routability handshake,
                      standing-queue confirm, timing        capacity gate, 1-slot serialize,
                                                            Postgres ingest, engineer dashboard
```

- **VPS server (Go, Hostinger KVM2, off-net):** UDP endpoint. Marks downstream
  ECT(1)/NQB DSCP 45, echoes the received TOS byte on every packet, timestamps
  both directions, counts CE marks. **Return-routability cookie handshake before
  any high-rate flow** (response ≤ request until the source IP is validated;
  per-code/per-IP rate limits; global egress cap). Serializes saturation tests to
  **one** active slot with a heartbeat lease (reclaim on missed heartbeats).
  Postgres ingest (HTTPS, idempotent on client `run.id`, resumable). Basic-auth
  engineer dashboard behind TLS + IP allowlist (real auth before wider use).
  Batched syscalls (`sendmmsg`/`recvmmsg` + `IP_RECVTOS` cmsg, `SO_TIMESTAMPING`).
- **Shared Go core:** protocol, sockets, ECT(1)/NQB marking (`IP_TOS` +
  `IPV6_TCLASS`), multi-flow **overshoot** load with standing-queue confirmation,
  optional saturating scalable-CC LL flow, high-resolution timing, fixed-bin
  histograms, verdict, marking self-test, wrong-link detection. **Pure-compute
  (histograms/verdict/stats) is a separate package** so iOS can reuse it via a
  thin FFI while doing native `Network.framework` sockets.
- **Wired CLI client (Go):** the wedge. Used wired, so the access link is the only
  variable. No gomobile. Emits human pass/fail/inconclusive + structured JSON.
- **Android app (Compose + core via gomobile/JNI):** after the CLI proves out.
- **iOS app (SwiftUI):** demand-gated. Native networking + shared pure-compute.

## Measurement

`working_latency_delta = rtt − base_rtt` (store unclamped; report `max(0, …)`).
RTT-based, no clock sync. `base_rtt` = **p1 (or p5)** of a **continuous low-rate
baseline stream** run throughout the test, with drift detection (compare pre/post
idle; flag route/radio changes). Probes: ~50/s, 32-byte, sequenced; server echoes
with received TOS + server timestamp. Per-seq matching with max-wait; lost probes
recorded as loss, excluded from the latency histogram; reorders handled.

Relative one-way jitter (from server timestamps, no absolute sync) kept as a
cross-check to disambiguate Phase 4 bidirectional deltas.

### Test sequence

| Phase | What | Notes |
|------|------|------|
| 0 — Preflight | Return-routability handshake; marking self-test (TOS echo survival, distinguishes path stripping); **wrong-link detection** (cellular/VPN refused or flagged; flow ASN cross-checked vs expected operator; wired required for verdict-grade). | abort/inconclusive on fail |
| 1 — Capacity + baseline | **Capacity-confirmation speed test**: delivered throughput must reach ~provisioned tier (within tolerance) → access link is the bottleneck → proceed; else `inconclusive`. Continuous baseline → `base_rtt`. | the localization gate |
| 2 — Downstream loaded | Greedy classic load **overshooting ~1.2-1.5× measured capacity**; confirm a **sustained** standing queue (classic delta persists, not transient) before scoring; LL-marked probe vs classic-marked probe. | |
| 3 — Upstream loaded | Same, device saturates upstream. | |
| 4 — Does no harm | **A/B/A repeated legs**: classic-alone vs classic+aggressive-LL; compare classic throughput, p95/p99 latency delta, and loss with variance bounds; `inconclusive` when variance exceeds the harm margin. | screening-grade, repeated |
| (continuous) | Marking survival / CE rate across phases 2-4. | survival ≠ classification |

**Negative control (calibration + acceptance):** a known LLD-off line (or
pre-rollout line) must yield fail/inconclusive; a known-good line must pass.
Thresholds are calibrated against both arms before field use.

### Pass / fail (calibrated in M0)

1. **Working:** capacity gate passed AND standing queue confirmed AND LL-probe
   `p99(delta) < 10 ms` AND classic-probe `p99(delta) > 50 ms`, per direction.
2. **Marking survival:** ≥ 99% of ECT(1)/NQB packets arrive still marked. Reported
   separately from queue engagement (survival is necessary, not sufficient).
3. **No harm:** across A/B/A, classic throughput ≥ 90% of solo AND classic p99
   delta not worse than solo beyond the calibrated margin.
Anything unverifiable (wrong link, capacity gate fail, no standing queue, baseline
drift, excess variance) → `inconclusive`, never `pass`.

### Capacity

KVM2 validates **≤ ~600 Mbps tiers, one tester at a time** (1 slot — two
concurrent tests would saturate the ~1 Gbps port and invalidate both). M1 proves
this with a server perf test (TOS-echo + cmsg + timestamping, GC-pause
distribution, kernel drop counters) before it's trusted. Gig / multi-gig is future
scope (on-net or higher-capacity server; you cannot overshoot a gig line from a
gig port).

## Data model (Postgres)

```sql
create table tester (
  id uuid primary key default gen_random_uuid(),
  access_code text unique not null,        -- high-entropy, bound to handshake, revocable
  expected_asn int, expected_cmts text,    -- wrong-link cross-check
  label text, created_at timestamptz not null default now()
);

create table run (
  id uuid primary key default gen_random_uuid(),
  tester_id uuid not null references tester(id),
  started_at timestamptz not null,
  client_kind text,                        -- cli | android | ios
  app_version text, os_version text, device_model text,
  conn_type text,                          -- wired | wifi | cellular(refused) | vpn(flagged)
  wifi_band text, wifi_rssi int,
  ip_family text,                          -- v4 | v6
  public_ip_hash text,                     -- hashed/truncated; consent-gated raw retained separately
  asn int, isp text, cmts_id text,
  modem_model text, modem_fw text,
  service_tier_down int, service_tier_up int,
  capacity_confirmed_down_bps bigint, capacity_confirmed_up_bps bigint,
  bottleneck_ok boolean,                   -- capacity gate result
  standing_queue_ok boolean,
  base_rtt_method text,                    -- p1 | p5
  base_rtt_down_us int, base_rtt_up_us int, base_rtt_drift_us int,
  geo_coarse text, consent_location boolean not null default false,
  upload_complete boolean not null default false,
  verdict text                             -- pass | fail | inconclusive
);
create index run_tester_idx on run(tester_id);
create index run_started_idx on run(started_at);

create table phase_result (
  id uuid primary key default gen_random_uuid(),
  run_id uuid not null references run(id) on delete cascade,
  phase text not null, flow_type text not null, direction text not null,
  throughput_bps bigint, loss_rate numeric, reorder_rate numeric,
  marking_survival numeric, ce_mark_rate numeric,
  hist_edges_ms numeric[] not null, hist_counts bigint[] not null, sample_count bigint not null
);
create index phase_run_idx on phase_result(run_id);

create table raw_sample (                  -- re-analysis; retention 180d then aggregate-only
  run_id uuid not null references run(id) on delete cascade,
  phase text not null, flow_type text, direction text,
  seq bigint, delta_us int, delta_unclamped_us int, lost boolean,
  tos_sent smallint, tos_echo smallint
);
create index raw_sample_run_idx on raw_sample(run_id);
```

Histograms (fixed edges below) are the mergeable fleet unit; sum `hist_counts`.
Edges (ms), dense < 30 ms:
`[0,0.5,1,1.5,2,3,4,5,7.5,10,12.5,15,20,25,30,40,50,75,100,150,250,500,1000,+∞]`.

## UI / dashboard

- **CLI:** clear `PASS / FAIL / INCONCLUSIVE` + the three sub-results + honest
  caveats (e.g. "NQB-only fallback", "WiFi — not verdict-grade"); `--json` for upload.
- **Apps (when built):** pinned canonical flow (preflight → running → verdict), a
  shared **state machine** covering every state (in-progress per phase, marking
  stripped, wrong link, WiFi, no server, partial upload, server busy/queued,
  inconclusive, interrupted), a shared **copy/strings table**, and verdict =
  core's structured output rendered verbatim (no client-side threshold logic).
  No jargon on the tester screen; numbers gated behind the engineer role.
- **Engineer dashboard:** default per-run view = **LL-vs-classic CDF overlay** per
  direction with 10 ms / 50 ms threshold lines drawn in (the proof is visual).
  Runs-table landing; **cohort filters** (tier, modem, firmware, CMTS, conn_type,
  client_kind, verdict); fleet charts require a cohort and **exclude inconclusive
  by default**, showing run/sample counts above every chart. Rollout-health
  scoreboard (pass/fail/inconclusive by segment).

## Security / privacy

Return-routability handshake before any rate; response ≤ request until validated;
per-code/per-IP rate limits; global egress cap; abuse telemetry. High-entropy
access codes bound to the handshake, revocable. Dashboard behind TLS + IP
allowlist (basic auth MVP; real auth before wider use). `public_ip` hashed/
truncated by default; raw IP + coarse location consent-gated with documented
lawful basis; restricted table access.

## Acceptance Criteria

1. M0 retires every kill-shot (see Milestones) with documented pass/fail before
   client build: client marking survival + on-wire capture; DSCP/ECN transit
   survival on the target network; overshoot builds a confirmed standing queue;
   capacity-confirmation method works; server perf at ≤600 Mbps with full
   TOS-echo; return-routability handshake blocks spoofed-source high-rate flows;
   negative control distinguishes LLD-off from LLD-on.
2. Wired CLI run on an LLD 500 Mbps line: capacity gate passes, standing queue
   confirmed, all phases + histograms + raw samples upload idempotently.
3. `working` fires only when capacity gate + standing queue + LL<10ms + classic>50ms
   all hold; under-loaded / wrong-link / drifted-baseline runs report `inconclusive`.
4. No-harm A/B/A fails when classic throughput < 90% solo or classic p99 worsens
   beyond margin; reports `inconclusive` when variance is too high.
5. Spoofed-source "start" packet does NOT elicit a high-rate flow (amplification test).
6. Cellular/VPN/wrong-ASN run is refused or marked `inconclusive`, never `pass`.
7. Dashboard renders LL-vs-classic overlay + cohort-filtered fleet distribution
   excluding inconclusive by default, behind auth.
8. Two concurrent saturation requests → second queues (1-slot serialization), with
   lease reclaim on a crashed/abandoned slot.
9. IPv6 path marks via `IPV6_TCLASS`; address family recorded.
10. Tests written and passing (incl. negative control, amplification, perf,
    loss/reorder, NAT/CGNAT, slot-abandonment, ingest idempotency, gomobile jitter
    once Android is in scope).

## Testing Plan

| Layer | What | Count |
|------|------|------|
| Unit | histogram binning, p1/p5 baseline + drift, delta, verdict, TOS encode/decode, handshake cookie | +14 |
| Integration | loopback run all phases; marking echo; 1-slot lock + lease reclaim; ingest idempotency/partial-upload; amplification refusal | +9 |
| Perf | server at 500-600 Mbps with TOS-echo + cmsg + timestamping; GC-pause + drop-counter capture | +2 |
| E2E | wired CLI on LLD line: full run → upload → dashboard; **negative control** (LLD-off ≠ pass) | +3 |

## Rollback Plan

Stateless server, single systemd unit → redeploy prior binary. Additive Postgres
migrations → drop new tables. CLI/app roll back to prior build. No shared
production data at risk.

## Effort Estimate (human / CC+gstack)

- Shared Go core (sockets, marking, overshoot+queue-confirm, histograms, verdict): ~6 days / ~1 day
- VPS server (UDP engine, handshake, capacity gate, 1-slot lease, ingest, dashboard): ~6 days / ~1.5 days
- Wired CLI client: ~2 days / ~0.5 day
- Expanded M0 spikes (marking, transit, queue-build, capacity, server perf, amplification, negative control): ~4 days / ~1 day (some need hardware)
- Android app (after CLI): ~3 days / ~0.5 day
- iOS app (demand-gated): ~4 days / ~0.5 day
- Threshold calibration on real lines incl. negative control: ~3 days / n/a (hardware)

## Files Reference (anticipated)

| Path | Contents |
|------|---------|
| `core/` | shared Go engine; `core/compute/` = pure (histograms/verdict/stats), `core/net/` = sockets/marking/load |
| `cli/` | wired Go CLI client |
| `server/` | VPS UDP server + handshake + ingest + dashboard |
| `server/migrations/` | Postgres schema |
| `android/` | Compose app + core (gomobile/JNI) |
| `ios/` | SwiftUI app + native net + shared compute (demand-gated) |
| `docs/protocol.md` | wire protocol incl. handshake |

## Out of Scope

- Multi-gig / gig from a gig port — needs on-net / higher-capacity server.
- Browser/desktop GUI client — cannot mark packets.
- CMTS-side A/B toggling of LLD — no-harm is single-run A/B/A from the subscriber side.
- Fine GPS — coarse + consent only.
- Automated CMTS/headend config validation — subscriber-side only (but record the
  CMTS LL classifier config per run).

## Milestones

```
M0 (spikes, gate all) → M1 server + core + wired CLI + dashboard → M2 Android → M3 iOS (gated) → M4 calibration + polish
```

M0 spikes every kill-shot before client build: client marking survival + capture,
DSCP/ECN transit survival on the target network, overshoot standing-queue
formation, capacity-confirmation method, server perf at ≤600 Mbps with TOS-echo,
return-routability handshake, and the negative control. If M0 can't show marking
survival + repeatable LL/classic separation on a real LLD line with the bottleneck
localized, stop and move to lab/operator-side validation before building clients.
