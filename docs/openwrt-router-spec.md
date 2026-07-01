# OpenWrt x86 Router Platform — spec (stub)

Prerequisite platform for the [passive QoE monitor](qoe-passive-spec.md). This is a
**separate project**: build the in-path host that the monitor runs on. Stub only —
flesh out when the work actually starts.

## Why
The passive monitor needs a vantage point that sees **all** household traffic and
has the horsepower to run Zeek + nDPI. Consumer-router OpenWrt (MIPS/ARM, tiny
RAM/flash) cannot run Zeek. An x86 mini-PC running OpenWrt routes the house *and*
has the CPU/RAM for line-rate analysis, and it's a thing worth owning regardless.

## What the platform must provide (the contract the monitor depends on)
1. **In-path visibility:** the box is the gateway (modem → box → LAN/AP), so it sees
   every household flow natively — no port mirror or tap needed.
2. **A capture interface** the monitor/Zeek can read (the WAN or bridge interface),
   with ECN/DSCP bits intact (no rewriting/bleaching of the IP TOS byte on the box).
3. **Enough compute:** handle the provisioned line rate (target 500 Mbps–1 Gbps)
   while running Zeek + nDPI. N100-class x86 is the reference.
4. **Room to run Zeek + the nDPI plugin + a Go binary** (persistent storage for
   logs/binaries; ability to install packages or run a container).
5. **Stable routing/firewall/DHCP** for the household (this becomes the production
   router).

## Reliability note (the in-path tradeoff)
Because the box is now the router, **if it breaks, the internet breaks.** Mitigate:
keep the previous router on hand as a 5-minute fallback, and keep the routing config
separate/simple from the monitoring stack so monitoring changes can't take the line
down.

## Rough scope (to detail later)
- Hardware selection (N100 mini-PC, dual NIC).
- OpenWrt x86 install + base routing/firewall/DHCP (+ Wi-Fi via a separate AP).
- Package/container path for Zeek + nDPI plugin + the Go monitor.
- Verify ECN/DSCP survive the box unmodified (a quick check with the existing active
  tool: run `qoe-cli` through it and confirm marking survival stays ~100%).
- Fallback/rollback procedure.

## Out of scope
Everything in the passive-monitor spec (that builds on this); multi-site; HA.

## Relationship
`qoe-passive-spec.md` is **developed and tested against pcap fixtures without this
platform**; the platform is required only to run the monitor **live**.
