# server — VPS test endpoint + ingest + dashboard

Go. Runs on Hostinger KVM2 (~2 vCPU, 8 GB, ~1 Gbps port). Single systemd unit.

Responsibilities:
- UDP test endpoint: marks downstream ECT(1)/NQB, echoes received TOS on every
  packet, timestamps both directions, counts CE marks.
- Serializes saturation tests (1-2 active slots; queue the rest) so the single
  port/CPU is not split. Batched syscalls (`sendmmsg`/`recvmmsg`) to hit rate.
- Result ingest → Postgres (`migrations/0001_init.sql`).
- Basic-auth engineer web dashboard: per-run CDFs from `hist_counts` + fleet-wide
  merged distribution.
- Issues per-tester access codes.

See `../docs/protocol.md` and `../docs/spec.md`.

Status: not started. First built in M1.
