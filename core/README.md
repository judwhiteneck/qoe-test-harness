# core — shared Go test engine

Built once, embedded in both apps via gomobile (`.aar` for Android, `.xcframework`
for iOS) and reused server-side for the protocol. Tailscale-style shared core.

Responsibilities:
- Raw UDP sockets with `IP_TOS` / `IPV6_TCLASS` marking (ECT(1), NQB DSCP 45) and
  `IP_RECVTOS` for reading observed TOS.
- Multi-flow saturation (default 4 flows) and scalable congestion response.
- High-resolution monotonic timing; `base_rtt` (min) and `working_latency_delta`.
- Fixed-bin-edge histogram builder (mergeable bin counts).
- Marking self-test (phase 0).

See `../docs/protocol.md` and `../docs/spec.md`.

Status: not started. First built in M1.
