# Wire Protocol (draft)

UDP, custom. One protocol shared by `core/` (client) and `server/`. Designed so
the receiver can report the exact TOS byte it observed, which is how marking
survival and CE marking are measured end to end.

## Packet header (common)

All packets start with a fixed header:

| Field | Bytes | Notes |
|---|---|---|
| `magic` | 2 | protocol id |
| `version` | 1 | |
| `type` | 1 | `PROBE`, `PROBE_ECHO`, `LOAD`, `CTRL`, `CTRL_ACK` |
| `session_id` | 8 | per-run id |
| `seq` | 8 | per-flow monotonic |
| `t_send_client_us` | 8 | client monotonic send time (probe) |

## TOS / marking

The client sets the IP TOS byte via `setsockopt(IP_TOS)` (IPv4) /
`IPV6_TCLASS` (IPv6):

- **LL probe / LL load:** ECT(1) (ECN `01`) and/or NQB DSCP 45.
- **Classic probe / classic load:** Not-ECT (`00`) or ECT(0) (`10`).

The server reads the received TOS via `IP_RECVTOS` / `IPV6_RECVTCLASS` and copies
it into the echo.

## PROBE → PROBE_ECHO

Client sends `PROBE` (ECT(1)/NQB or classic). Server replies `PROBE_ECHO` adding:

| Field | Bytes | Notes |
|---|---|---|
| `tos_observed` | 1 | TOS byte the server actually received |
| `t_recv_server_us` | 8 | server receive time |
| `ce_seen` | 1 | 1 if ECN CE (`11`) observed on this packet |

Client computes:
- `rtt = t_recv_echo_client - t_send_client` (no clock sync needed)
- `working_latency_delta = max(0, rtt - base_rtt)`
- marking survival = fraction of echoes whose `tos_observed` still carries the
  intended ECT(1)/NQB
- CE-mark rate = fraction of echoes with `ce_seen = 1`

## LOAD

Saturation traffic. 4 parallel flows by default. Downstream LOAD is server→client
(server sets TOS); upstream LOAD is client→server (client sets TOS). Rate-paced to
the target (500 Mbps MVP, configurable per tier). Payload ~1200-1400 B.

## CTRL

Run setup/teardown: access code auth, tier, phase transitions, server-side
serialization slot grant/queue, result upload handle.

## Open items (resolve in M0/M1)

- Whether iOS honors `IP_TOS` on a POSIX UDP socket or requires
  `Network.framework` service classes; fallback to NQB-only if ECN is bleached.
- Exact congestion-control response for the LL load flow.
- Slot-grant backoff/queue messaging when the server is busy.
