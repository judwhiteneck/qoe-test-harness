# OpenWrt x86 Router + Monitoring Platform — build spec

Prerequisite platform for the [passive QoE monitor](qoe-passive-spec.md). A
**separate project**: build the in-path x86 router the monitor runs on. This is a
hardware + networking build, not application code. Authored via `/spec`.

## Goal
Stand up an x86 mini-PC as the household's OpenWrt router that (a) routes the line
reliably and (b) meets the passive monitor's platform contract: sees all traffic,
runs Zeek+nDPI, and passes ECN/DSCP through unmodified. "Router first; the monitor
builds on it."

## Decisions (locked)
- **Hardware:** buy a new N100-class dual/quad-NIC mini-PC (BOM below).
- **Upstream:** standalone cable modem → clean L2 handoff, box does all routing (no
  double-NAT).
- **Wi-Fi:** a dedicated wired AP in AP mode (x86 Wi-Fi is not worth fighting).
- **Scope:** finish as a working router AND verify monitoring-readiness (Zeek+nDPI
  installed, capture path exposed, ECN verified). Building the monitor itself is the
  other spec.

## The #1 risk, surfaced up front: Zeek on OpenWrt
Zeek + the nDPI plugin are **not in OpenWrt's package feeds** (OpenWrt is a minimal
embedded distro). Do not assume `opkg install zeek`. Two supported paths, decide in
Phase 1 with a spike:
- **Primary — containerize Zeek on OpenWrt.** Install `dockerd`/`podman` (available
  for OpenWrt x86), run an official Zeek container with host networking / the
  capture interface mapped in. OpenWrt stays the router; Zeek lives in the
  container. Needs the RAM/NVMe in the BOM.
- **Fallback — Debian as the router.** If the container path is too fiddly, run
  Debian on the same box (nftables + systemd-networkd for routing, Zeek installs
  natively from apt). You lose the OpenWrt UI; you keep the exact same topology,
  NICs, and monitor contract. **Nothing downstream depends on it being OpenWrt.**

Run the spike (can Zeek see the WAN iface from a container on OpenWrt x86?) before
committing the full build. If it fights back for more than an afternoon, take the
Debian fallback.

## Bill of materials
| Item | Spec | Why | ~Cost |
|---|---|---|---|
| Mini-PC | Intel **N100/N150**, fanless, **≥2× 2.5GbE Intel i226-V** (4-port fine), **16GB** DDR5, **256GB** NVMe | N100 has AES-NI + easily routes 1Gbps; 16GB/256GB gives Zeek headroom + log space; i226 is well-supported | $180–280 |
| Wi-Fi AP | Wi-Fi 6 AP (or a router you run in AP mode) | Clean wireless; x86 box has none | $60–130 |
| Switch (optional) | Small unmanaged 2.5G/1G switch | Only if you need more wired ports than the box provides | $20–40 |
| USB stick | 8GB+ | OpenWrt installer | on hand |
| Cables | Cat6 ×3+ | modem↔box↔AP/switch | on hand |

Target: comfortably handle the provisioned line rate (assume up to 1Gbps) with Zeek
running. N100 + i226 clears that with headroom.

## Topology
```
 cable modem ──[WAN: eth0]── OpenWrt x86 router ──[LAN: eth1]── switch ─┬─ wired devices
   (bridge/pure)                (routing/DHCP/FW)                        └─ Wi-Fi AP (AP mode) ~)))  clients
                                     │
                                     └─ Zeek+nDPI (container) sniffs WAN/LAN iface → logs → (later) qoe-monitor
```

## Build plan (phased, with a safe cutover)
**Phase 0 — procure + prep (~1–2h active).** Order BOM. Download OpenWrt **x86_64**
combined-EFI image. Read your modem's swap behavior (many cable modems bind to one
device MAC — plan to reboot the modem, or MAC-clone, at cutover).

**Phase 1 — Zeek spike + OS install (~3h).** FIRST do the Zeek-on-OpenWrt container
spike on the bench (not in-path). If green, continue OpenWrt; if not, switch to the
Debian fallback. Then flash OpenWrt x86 to the NVMe (boot the USB installer or `dd`
the image), expand the root partition, first boot, set a password, confirm both
NICs enumerate (i226 driver present).

**Phase 2 — routing, off-line bench (~2h).** Configure WAN (DHCP from modem) on
eth0, LAN + DHCP server on eth1, firewall (default deny inbound), basic hardening
(SSH keys, no WAN admin). Validate on the bench with a laptop on LAN before touching
the house.

**Phase 3 — cutover (~1h, keep the fallback).** Swap the box in: modem → box WAN,
box LAN → switch/AP. Reboot the modem so it re-provisions to the new device.
**Keep the old router on the shelf** — reverting is: modem → old router, 5 minutes.
Verify internet + a speed test lands near the provisioned tier.

**Phase 4 — Wi-Fi (~1h).** Put the dedicated AP in AP/bridge mode on the LAN (DHCP
from the OpenWrt box), one SSID, WPA3/2. Confirm clients connect and roam.

**Phase 5 — monitoring readiness (~3h).** Install the container runtime (or on
Debian, apt Zeek + nDPI plugin). Point Zeek at the capture interface; run `zeek -r`
on a sample pcap to confirm it emits `conn.log`/`ssl.log`. Set log rotation + a size
cap on the NVMe. **Disable NIC offloads that corrupt capture** (GRO/LRO on the
sniffed iface) so packet boundaries and ECN bits are seen truthfully.

**Phase 6 — validate the monitor contract (~1h).** Prove the platform doesn't
bleach marks: from a wired Linux host (or WSL2) behind the box, run the existing
active tool through it — `qoe-cli -server <VPS>:7700` — and confirm **LL marking
survival ~100%**. This reuses what's already built to certify the platform for the
monitor.

## Acceptance criteria
1. Household routes through the box: WAN up, LAN DHCP hands out leases, internet
   works; a speed test reaches ≥90% of the provisioned tier.
2. Wi-Fi via the dedicated AP: one SSID, clients connect, WPA2/3.
3. **Fallback tested once:** documented modem→old-router revert works in ≤5 min.
4. Zeek+nDPI runs on the box and processes a sample pcap into logs; log rotation
   caps disk use.
5. **ECN/DSCP integrity:** active `qoe-cli` run through the box shows marking
   survival ~100% (the platform contract for the monitor).
6. Line-rate sanity: box sustains the provisioned rate with Zeek running, with CPU
   headroom (no dropped packets on the capture iface at target load).

## Reliability / rollback
The box is now the critical path — if it dies, the internet dies. Mitigations:
old router kept as a 5-minute fallback (criterion 3); routing config kept simple and
**separate from the monitoring stack** so a Zeek change can't take the line down;
container for Zeek so it's isolated from routing; back up the OpenWrt/Debian config
after Phase 4.

## Risks
- **Zeek-on-OpenWrt** (primary risk) → container path, Debian fallback (see above).
- **i226 2.5GbE support** → verify on the current stable OpenWrt release before
  buying; i226 is supported in recent kernels but confirm.
- **Cable-modem MAC binding** → reboot modem / MAC-clone at cutover.
- **Capture fidelity** → offloads (GRO/LRO/checksum) can mangle what the sniffer
  sees; disable on the capture iface.

## Cost / effort
Hardware ≈ **$260–450**. Build ≈ **a weekend** (~12h active across phases), most of
it in the Zeek spike and the cutover.

## Out of scope
Everything in the passive-monitor spec (that builds on this); multi-site; HA/failover
routing; VPN/ad-block/other router extras (add later, they don't affect the monitor
contract).

## Relationship
[`qoe-passive-spec.md`](qoe-passive-spec.md) is developed and tested against pcap
fixtures **without** this platform; this platform is required only to run the monitor
**live**. The two are decoupled — build them in either order.
