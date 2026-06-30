# /autoplan Review Report — qoe-test-harness spec

Dual-voice review (Claude subagent + Codex) across CEO, Eng, Design. 2026-06-30.
Source: `docs/spec.md`. DX phase skipped (no developer-facing surface).

## Consensus tables

### CEO (strategy / scope)
| Dimension | Claude | Codex | Consensus |
|---|---|---|---|
| Right problem? | reframe: engineer is real user | reframe: operator acceptance test, not consumer app | CONFIRMED (reframe) |
| Both apps day-one? | no, Android-first / iOS demand-gated | no, **critical**, Android-first | CONFIRMED |
| gomobile now? | premature debt | premature debt | CONFIRMED |
| WiFi field data valid? | no — **critical** confound | no — **critical**, wired for acceptance | CONFIRMED |
| KVM2 enough? | undersized, prototype only | prototype not validation infra | CONFIRMED |
| Make vs buy checked? | CableLabs/irtt not checked | use Ookla/etc as baselines, check refs | CONFIRMED |

### Eng (architecture / measurement validity)
| Dimension | Claude | Codex | Consensus |
|---|---|---|---|
| Bottleneck is the DOCSIS link? | NO — off-net VPS, **critical** (A2) | NO — not localized, **critical** | CONFIRMED — fatal |
| Load builds a standing queue? | NO — line-rate ≠ overshoot, **critical** (A1) | NO — fixed UDP may not, **high** | CONFIRMED |
| Downstream NQB survives transit? | likely bleached, **critical** (A3) | marking ≠ classification, **critical** | CONFIRMED |
| Negative control exists? | NO — **critical** (A4) | edge/inconclusive not first-class | CONFIRMED |
| Server is safe? | DDoS amplifier, **critical** (G1) | reflection/amplification, **critical** | CONFIRMED |
| gomobile sound for hot path? | GC jitter pollutes p99, **high** (B1) | premature, narrow JNI instead | CONFIRMED |
| base_rtt = min safe? | fragile, **high** (A6) | use p1/p5, pre/post idle, **high** | CONFIRMED |
| No-harm single-run valid? | weak | screening signal only, **high** | CONFIRMED |
| Gig tier on 1Gbps port? | physically impossible, **critical** (D1) | capacity soft | CONFIRMED |

### Design (UI/UX) — completeness 3/10 (both)
| Dimension | Claude | Codex | Consensus |
|---|---|---|---|
| Tester flow specified? | only verdict screen, **critical** | no preflight/in-progress, **high/crit** | CONFIRMED |
| State taxonomy designed? | 8 of 9 states undefined, **critical** | missing failure/recovery, **critical** | CONFIRMED |
| "No jargon" funded? | no translation layer, **high** | needs copy glossary, **high** | CONFIRMED |
| Dashboard default right? | CDF alone wrong; LL-vs-classic overlay | same — overlay + filters, **high** | CONFIRMED |
| Consent moment designed? | column not interaction, **high** | UX-absent, **medium** | CONFIRMED |
| Cross-platform UX contract? | will diverge, **critical** | state machine + copy table, **high** | CONFIRMED |

## Critical findings (fatal to validity or security — fix before field use)

1. **Bottleneck not localized to the DOCSIS link (A2).** From an off-net VPS, a standing queue can form at any WAN hop, where there is no L4S isolation. Then the LL probe sees the same queue, reads high, and a healthy LLD line is reported broken. `classic p99 > 50ms` proves *a* queue formed *somewhere*, not at the LL-capable AQM. **Fix: on-net server (operator AS / peering-adjacent) or a per-run bottleneck-localization gate.**
2. **Load = line rate won't build a queue (A1).** You must overshoot the bottleneck (~1.2-1.5x), measure deliverable capacity first, and confirm a *sustained* standing queue before scoring. Otherwise every honest run is `inconclusive`.
3. **Downstream NQB DSCP 45 bleached in transit (A3).** DSCP is routinely zeroed at AS edges; off-net it likely never reaches the CMTS classifier. **Fix: on-net + confirm the CMTS LL classifier config (ECT(1)? DSCP? app-id?) with the operator.**
4. **Server is a DDoS amplifier (G1).** A spoofed "start" packet → 500 Mbps flood at a victim. **Fix: return-routability cookie handshake before any high-rate flow; response ≤ request until validated; per-IP/per-code rate limits; concurrent-egress cap.**
5. **No negative control (A4).** Nothing proves the tool can tell LLD-on from LLD-off. **Fix: calibrate against a known LLD-off line (must fail/inconclusive) and a known-good line (must pass).**
6. **Wrong-link pollution (E1).** Cellular/VPN/guest-WiFi runs measure the wrong link and can falsely pass. **Fix: detect cellular/VPN, cross-check flow ASN vs expected operator, mark unverifiable runs inconclusive, never pass.**
7. **WiFi confound.** WiFi air-link bufferbloat masquerades as DOCSIS behavior. **Fix: wired Ethernet for verdict-grade; WiFi runs exploratory-only.**
8. **Gig tier impossible on a 1 Gbps port (D1).** Can't overshoot gig from a gig port; two concurrent 500 Mbps tests saturate the port itself. **Fix: MVP validates ≤ ~600 Mbps, ONE tester at a time (1 slot, not 1-2). Gig needs on-net/higher-capacity server.**

## High findings (correctness / honesty)

- **Marking survival ≠ queue engagement** — rename and separate the claims; survival is necessary, not sufficient.
- **base_rtt = min over 5s is fragile** — use p1/p5, continuous baseline stream, pre/post idle drift check.
- **No-harm single-run is a screening signal** — A/B/A repeated legs, latency (not just throughput ≥90%), variance bounds, inconclusive when noisy.
- **iOS fallback breaks write-once (B2)** — `Network.framework` is Swift-only, unreachable from Go; if iOS bleaches ECN you're writing Swift sockets anyway. Decide iOS networking from M0 before committing gomobile for iOS; share only pure compute (histograms/verdict/stats).
- **gomobile GC jitter (B1)** — stop-the-world pauses land in the p99 tail you score; M0 must benchmark timestamp jitter under load.
- **IPv6 marking** — spec only sets `IP_TOS` (v4); needs `IPV6_TCLASS`; record/pin address family.
- **Probe loss/reorder, NAT/CGNAT, slot lease/abandonment, ingest idempotency/partial upload** — all unspecified; add handling + tests.
- **PII** — `public_ip` stored unconditionally (only location is consent-gated); hash/truncate, document lawful basis.
- **UI** — pin a canonical 3-screen flow (preflight → running → verdict), a shared state machine, a copy/strings table, and verdict-locus = core (apps render, never re-derive). Dashboard default = LL-vs-classic CDF overlay with threshold lines + cohort filters; exclude inconclusive from fleet by default.

## Expand M0 — the kill-shots are currently un-spiked until M4

Add to Milestone 0 (before any app build), because each can independently sink the project:
- Downstream **DSCP/ECN transit survival** off-net vs on-net to a real line.
- **Queue-building** over a real line (can you overshoot and form a standing queue?).
- **Bottleneck localization** method (prove the access link is the bottleneck).
- **Server perf** at 500 Mbps with full TOS-echo + cmsg + timestamping on KVM2.
- **gomobile timestamp jitter** under load.
- **Amplification handshake** design validated.

## Convergent recommendation

Both models, independently, land on the same restructure: **make the wedge a wired, operator-run client against an on-net server, prove the methodology end-to-end (incl. a negative control), and gate mobile/iOS on demonstrated need** — rather than building two native apps over WiFi against an off-net VPS first.
