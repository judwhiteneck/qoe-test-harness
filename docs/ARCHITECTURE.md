# Architecture & Design Guidance

How the system is decomposed and the rules that keep it clean as it grows. Read
this before adding a package. Engineering practices (how to write the code) live in
`docs/ENGINEERING.md`; this is about *shape and boundaries*.

## Design goals (in priority order)

1. **Measurement validity above all.** Every architectural choice is judged first by
   whether it preserves the integrity of the latency numbers. Determinism, low
   measurement jitter, and honest `inconclusive` beat features.
2. **Testable without hardware.** The bulk of the logic must be unit-testable on a
   laptop with no LLD line, no VPS, no root. Achieved by separating pure compute
   from I/O (see Boundaries).
3. **One source of truth for the verdict.** The verdict + sub-results are computed
   in one place (the core) and rendered verbatim everywhere. Clients never re-derive.
4. **Clients are thin and swappable.** CLI, Android, iOS are presentation over the
   same engine. Adding a client must not require touching measurement logic.

## Components

```
                    ┌──────────────────────────────────────────┐
                    │ core/  (shared Go engine)                 │
   clients ───────► │  compute/  PURE: no I/O, no clock, no net │ ◄─── server reuses
   (cli, android,   │            histograms, verdict, stats     │      compute + protocol
    ios)            │  protocol/ wire types, encode/decode,     │
                    │            handshake, TOS helpers (pure)   │
                    │  net/      sockets, marking, load gen,    │
                    │            probing, capacity, timing       │
                    │  engine/   orchestrates a run via net +    │
                    │            compute; emits Result           │
                    └──────────────────────────────────────────┘
                              ▲                         ▲
                              │ UDP test protocol        │
   ┌──────────────────────────┴───┐         ┌───────────┴───────────────────┐
   │ cli/   wired Go binary       │         │ server/  Hostinger, off-net     │
   │  flags, run engine, print,   │         │  udp endpoint (reuses net +     │
   │  --json upload               │         │  protocol), handshake, capacity │
   └──────────────────────────────┘         │  gate, 1-slot serialize,        │
   android/  Compose + core (gomobile)       │  ingest (Postgres), dashboard   │
   ios/      SwiftUI + native net +          └─────────────────────────────────┘
             shared compute (FFI, gated)
```

## Boundaries (the load-bearing rule)

**Pure compute must not import I/O.** `core/compute` and `core/protocol` have zero
dependencies on `net`, the clock, the filesystem, randomness, or time. They take
inputs and return outputs. This is what makes the math testable and deterministic.

Allowed import directions (a package may only import those below it):

```
clients (cli/android/ios)        ← may import engine, compute, protocol
  └─ engine/                      ← may import net, compute, protocol
       ├─ net/                    ← may import protocol; NEVER compute’s callers
       ├─ protocol/  (pure)       ← imports nothing internal
       └─ compute/   (pure)       ← imports nothing internal
server/                          ← may import net, protocol, compute (NOT engine/cli)
```

A dependency that points "up" this list is a design error. CI enforces it
(see ENGINEERING — import-boundary check).

### Why this split

The hot path (timestamping, socket reads) and the analysis (histograms, percentiles,
verdict) have opposite needs. The hot path is I/O-bound, jitter-sensitive, and hard
to test; the analysis is pure and must be exhaustively tested. Keeping them in
separate packages means a route change in how we read sockets never risks the
correctness of the verdict math, and the verdict math is tested with plain values.

## Key abstractions (interfaces, defined in core)

These exist so the I/O-bound parts are injectable and therefore testable. Concrete
implementations live in `net/`; tests use fakes.

- **`Clock`** — `Now() time.Time` / monotonic reads. Injected everywhere timing
  matters so tests are deterministic. Production = real monotonic clock; tests = a
  fake clock that advances on command. No package calls `time.Now()` directly.
- **`PacketConn`** — the marked-UDP socket: send/recv with TOS set and TOS observed,
  plus kernel receive timestamps. Real impl wraps `recvmmsg`/`IP_RECVTOS`/
  `SO_TIMESTAMPING`; test impl is an in-memory loopback that can inject loss,
  reorder, delay, and TOS bleaching.
- **`LoadController`** — drives offered load (overshoot ramp) and reports achieved
  rate. Test impl simulates a bottleneck + standing queue so engine logic is testable
  without a network.
- **`Sampler`** — emits probe RTT samples; pure consumers turn them into histograms.

A run is the `engine` wiring a `PacketConn` + `Clock` + `LoadController` into the
phase sequence, feeding samples to `compute`, and emitting a single `Result`.

## Data flow of one run

1. `engine` opens a `PacketConn`, does the **handshake** (`protocol`), then **wrong-link
   check** and **marking self-test**.
2. **Capacity gate** (`net` measures throughput; `compute` compares to tier) → may abort `inconclusive`.
3. **Baseline** (continuous probe stream; `compute` derives p1/p5 `base_rtt` + drift).
4. **Loaded phases** (`LoadController` overshoots; `compute` confirms standing queue;
   probes → `working_latency_delta`).
5. **No-harm A/B/A** legs.
6. `compute` builds fixed-bin histograms + the `Result{verdict, sub_results, caveats,
   per-phase histograms, marking survival, CE rate}`.
7. Client renders `Result`; uploads it (idempotent on `run.id`) to `server` ingest.

## The Result contract (single source of truth)

`compute` emits a versioned, serializable `Result`. Every surface (CLI text, app
screen, dashboard, Postgres row) is a projection of it. No surface contains
threshold logic. Adding a caveat or sub-result means changing the contract once.

## Technology choices & rationale

- **Go for core + server + CLI** — one language for the protocol on both ends; strong
  UDP/syscall control (`sendmmsg`/`recvmmsg`, cmsg); easy static binary for field laptops.
- **Pure-compute is plain Go, no deps** — portable to any client (incl. iOS via FFI)
  and trivially testable.
- **gomobile for Android only, later** — and only the engine/compute, not as a reason
  to route iOS through it. The iOS GC-jitter and `Network.framework`-fallback risks
  (see review) are why iOS shares *compute* but does its own sockets.
- **Postgres** — histograms stored as arrays (mergeable); raw samples for re-analysis.
- **Off-net Hostinger** is acceptable *because* the capacity gate localizes the
  bottleneck; this is an architectural assumption, not an accident — if the gate is
  removed, the off-net server becomes invalid.

## Extension points

- **New client:** implement presentation over `engine` + `Result`. Do not add
  measurement logic in the client.
- **New test phase:** add to `engine` sequence + a `compute` reducer; extend the
  `Result` contract; never special-case in a client.
- **New transport (e.g. on-net server):** new `PacketConn`/deploy target; protocol
  and compute unchanged.

## What must NOT happen

- Threshold/verdict logic in a client or the dashboard.
- `time.Now()` / `rand` / direct socket calls inside `compute` or `protocol`.
- A client talking to Postgres directly (always via server ingest).
- A "quick" throwaway path that bypasses the interfaces — M0 probes use the real
  `net` interfaces so the spike code becomes the foundation, not waste.
