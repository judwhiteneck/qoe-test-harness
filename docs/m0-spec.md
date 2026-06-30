# M0 Spec — De-risk the methodology before building clients

Status: ready to execute. Derived from `docs/spec.md` v2 and `docs/autoplan-review.md`.
M0 is a gate: it proves the measurement is trustworthy *before* any client is built.
If M0 fails, we stop and move to lab / operator-side validation instead of shipping UI.

## Why M0 exists

The autoplan review found the project's real risk is not the apps — it is **causal
attribution**: proving the congestion we measure forms at the CMTS dual-queue AQM,
not at a WAN hop, WiFi, or the VPS. Six findings could each turn a healthy LLD line
into a "broken" verdict or vice versa. M0 retires every one with a small, cheap
experiment before we spend effort on clients.

## What you need

- The Hostinger KVM2 VPS (root), reachable by UDP on the test port.
- One wired test machine (laptop/SBC) on an **LLD-enabled** line, known provisioned tier.
- One **LLD-disabled** (or pre-rollout) line on the same plant for the negative control.
- `tcpdump`/`wireshark` on both ends; `iperf3` for capacity cross-checks.
- The throwaway Go probe binaries built from `core/net` (no full client needed).

## Spikes

Each spike: **goal → method → pass/fail → artifact**. Run roughly in order; S1-S4
gate S7.

### S1 — Client marking on the wire
**De-risks:** that the client can actually set ECT(1) + NQB DSCP 45 and they leave the NIC.
**Method:** From the wired Go probe, send UDP with `IP_TOS`/`IPV6_TCLASS` set to (a) ECT(1),
(b) DSCP 45, (c) both. Capture on the client NIC with tcpdump (`ip[1]` byte). Server echoes
the received TOS.
**Pass:** capture shows the intended bits on the wire AND server echo reports ≥99% survival on
a clean local path, IPv4 and IPv6.
**Artifact:** `docs/m0/s1-marking.md` with capture snippets + survival numbers per family.

### S2 — DSCP/ECN transit survival to the target line
**De-risks:** that marks survive Hostinger → transit → operator core → CMTS (the A3 critical).
You believe DSCP 45 is not bleached on this network; this measures it.
**Method:** Server (off-net) sends downstream ECT(1)+DSCP45 to the wired tester; tester captures
received TOS and reports survival. Repeat upstream (tester→server). Compare against a Not-ECT/DSCP0
control flow.
**Pass:** downstream + upstream survival ≥99% to/from the real line. If <99%, record *where*
(client capture vs server echo localizes WAN-vs-CM) and decide ECT(1)-only or NQB-only per the
operator's actual CMTS classifier (see S0 note).
**Artifact:** `docs/m0/s2-transit.md` with per-direction survival + the CMTS classifier config
confirmed with the operator.

### S3 — Overshoot builds a sustained standing queue
**De-risks:** that load = line rate won't congest (A1). You must overshoot to fill the buffer.
**Method:** With multi-flow UDP, ramp offered downstream load from 0.8× to ~1.5× the measured
capacity. Watch the classic-probe `working_latency_delta` over time.
**Pass:** at some overshoot factor the classic delta rises and **stays elevated** (a standing
queue, not a transient spike) for the full phase, and recovers when load stops. Record the factor
that reliably builds the queue without loss-collapse.
**Artifact:** `docs/m0/s3-queue.md` with the load-vs-standing-delta curve and the chosen overshoot factor.

### S4 — Capacity confirmation = bottleneck localization
**De-risks:** that the access link (not a WAN hop) is the bottleneck (A2). This is your speed-test gate.
**Method:** Measure deliverable downstream/upstream throughput VPS↔tester (multi-flow, plus an
iperf3 cross-check). Compare to the tester's provisioned tier.
**Pass:** delivered throughput reaches the provisioned tier within tolerance (define it, e.g.
≥90% of tier) → the WAN path has headroom and the access link is the binding constraint → runs
may score. Below tolerance → `inconclusive`. Validate the tolerance against S3 (queue must form
at the access link, confirmed by S2 survival to the CM).
**Artifact:** `docs/m0/s4-capacity.md` with the tolerance value and its justification.

### S5 — Server performance on KVM2
**De-risks:** that 2 vCPU / ~1 Gbps can deliver clean ≤600 Mbps with full TOS-echo + timestamping.
**Method:** Drive the server at 300/500/600 Mbps with the real path (recvmmsg + `IP_RECVTOS` cmsg +
`SO_TIMESTAMPING` + per-packet echo). Capture: achieved pps/bps, CPU, kernel drop counters
(`/proc/net/udp`, `ethtool -S`), GC pause distribution (`GODEBUG=gctrace=1`), inter-timestamp jitter.
**Pass:** sustained target rate with <X% drops and GC pauses that do not contaminate the p99 tail
(server-side jitter well under the 10 ms decision boundary). One slot only.
**Artifact:** `docs/m0/s5-serverperf.md` with the rate/CPU/drop/jitter table and the safe max rate.

### S6 — Return-routability handshake (anti-amplification)
**De-risks:** the DDoS-amplifier hole (G1 critical) — a spoofed "start" must not elicit a flow.
**Method:** Implement the cookie handshake in the protocol: server replies to an unvalidated
source only with a small cookie (response ≤ request); high-rate flow starts only after the client
echoes the cookie from its real source. Test with a spoofed-source start packet.
**Pass:** spoofed-source start elicits at most one ≤-request-size packet, never a high-rate flow;
per-IP/per-code rate limits and the global egress cap hold under a burst of fake starts.
**Artifact:** `docs/m0/s6-handshake.md` + the handshake added to `docs/protocol.md`.

### S7 — Negative control (ground truth)
**De-risks:** that the tool can tell LLD-on from LLD-off (A4). Without this every verdict is suspect.
**Method:** Run the full S1-S6 pipeline on a **known LLD-off / pre-rollout line** and on a
**known-good LLD line**. Calibrate the 10 ms / 50 ms / 99% / 90% thresholds against both arms.
**Pass:** LLD-off yields `fail`/`inconclusive`; LLD-on yields `pass`; thresholds cleanly separate
the two with margin. Record the calibrated values back into `docs/spec.md`.
**Artifact:** `docs/m0/s7-negative-control.md` with both arms' distributions and the calibrated thresholds.

## M0 gate (GO / NO-GO)

**GO** to build clients only if: S1+S2 marking survives end to end; S3 reliably builds a standing
queue; S4 localizes the bottleneck to the access link; S5 server delivers clean rate on KVM2;
S6 blocks amplification; **S7 separates LLD-off from LLD-on with margin.**

**NO-GO** (any kill-shot fails): stop client work. If marking/queue/attribution can't be shown on
a real line, move to lab validation and operator-side CMTS counters before building UI. Document
the failing spike and the decision.

## Acceptance criteria

1. All seven `docs/m0/sN-*.md` artifacts exist with real numbers (no TODOs).
2. The GO/NO-GO decision is recorded with the evidence behind it.
3. On GO: calibrated thresholds are written back into `docs/spec.md` §Pass/fail.
4. The throwaway probe code lives under `core/net` behind the same interfaces the
   real clients will use (no throwaway-only abstractions) — see `docs/ENGINEERING.md`.

## Effort

~4 days human / ~1 day CC for the code + analysis; plus real-line + VPS access time
(some spikes need the hardware and a maintenance/test window on the plant).

## Out of scope for M0

Any client UI, Android/iOS, the dashboard, fleet aggregation, Postgres beyond a
scratch table for spike data. M0 is methodology proof only.
