# Engineering Practices (Required)

These are requirements, not suggestions. "MUST" items are enforced in review and CI;
a PR that violates one does not merge. The goal is code that is **clean, testable,
and trustworthy enough to base a rollout decision on.** Architecture/boundaries live
in `docs/ARCHITECTURE.md`; this is how we write the code inside them.

## 0. The prime directive

This tool's output is a go/no-go on a network rollout. A subtle bug that biases the
latency math is worse than a crash, because it produces a *confident wrong answer*.
Therefore: **the measurement math is pure, deterministic, and exhaustively tested,
and nothing that can be tested with values is tested with a network.**

## 1. Testability (the core requirement)

1. **Pure compute, no I/O.** `core/compute` and `core/protocol` MUST NOT import
   `net`, `os`, `time` (for the clock), or `math/rand`. They take inputs, return
   outputs. This is what makes the verdict testable without hardware.
2. **Inject time.** No production code outside `net` calls `time.Now()` /
   `time.Since()`. Pass a `Clock` interface. Tests use a fake clock that advances
   deterministically. Rationale: timing code tested against the wall clock is flaky
   and untestable for edge cases (drift, route change, slow phase).
3. **Inject I/O.** Sockets, load generation, and the network are behind interfaces
   (`PacketConn`, `LoadController` — see ARCHITECTURE). The real impls live in `net`;
   tests use an in-memory loopback fake that can inject **loss, reorder, delay,
   jitter, TOS bleaching, and a simulated standing queue.** Every measurement edge
   case is reproduced this way, not hoped for in the field.
4. **Inject randomness.** Pass a seeded source; never call global `rand`. Tests are
   reproducible.
5. **Determinism is a MUST for compute.** Same inputs → same `Result`, bit for bit.
   No map-iteration-order leakage into outputs, no time, no goroutine races in the
   pure path.

## 2. Testing standards

- **Layers (all required):** unit (pure compute + protocol), integration (engine
  over the loopback fake, full phase sequence), perf (server at rate, separate tag),
  E2E (real line — manual/M0, not in CI).
- **Table-driven tests** are the default for compute. One test function, many cases.
- **Golden tests** for the verdict and histogram outputs: a set of known sample
  streams → an asserted `Result`. These are the regression net for the math; changing
  a golden requires justifying the math change in the PR.
- **Property tests** (e.g. `testing/quick` or rapid) for invariants that must hold for
  all inputs: histogram bin counts sum to sample count; merging two histograms equals
  histogramming the concatenation (the mergeability the fleet rollup depends on);
  `working_latency_delta` never negative after clamp; percentile monotonicity.
- **Adversarial cases are mandatory, not optional.** Every reducer is tested with
  empty input, single sample, all-identical, all-lost, reordered, and out-of-range
  values. "Happy path only" fails review (see `docs/spec.md` integrity goals).
- **No network or sleep in unit tests.** A unit test that opens a socket or calls
  `time.Sleep` is an integration test in the wrong place — move it and use the fakes.
- **Coverage:** `core/compute` and `core/protocol` MUST be ≥90% line coverage and
  cover the adversarial cases above. `net`/`engine` SHOULD be ≥70%. Coverage is a
  floor signal, not the goal — golden + property tests matter more.
- **Tests are deterministic.** No flakes tolerated; a flaky test is a failing test.
  If it depends on timing, it's using the real clock — fix it (item 1.2).

## 3. Hot-path discipline (measurement integrity)

- The probe/timestamp path MUST avoid per-packet heap allocation: preallocate buffer
  pools, reuse `[]byte`, batch with `recvmmsg`/`sendmmsg`. Allocation → GC → pauses →
  contaminated p99.
- Use kernel timestamps (`SO_TIMESTAMPING`) where available; record the timestamp
  source per run.
- Keep the hot path out of the GC's way: no logging, no map writes, no interface
  boxing in the inner loop. Measure with `GODEBUG=gctrace=1` and a jitter histogram
  (this is an M0 deliverable, S5).
- Hot-path changes MUST include a benchmark (`testing.B`) and report alloc/op and
  ns/op deltas in the PR.

## 4. Error handling & honesty

- **`inconclusive` is a first-class result, not an error.** Any condition that makes
  the measurement untrustworthy (wrong link, capacity gate fail, no standing queue,
  baseline drift, excess no-harm variance, marking stripped) MUST surface as
  `inconclusive` with a caveat, never as a silent `pass` and never swallowed.
- Wrap errors with context (`fmt.Errorf("...: %w", err)`); never discard with `_`
  except where genuinely irrelevant, and comment why.
- No `panic` in library code (`core`, `server`); return errors. `panic` only for
  truly-impossible invariants, with a message.
- Validate all wire input as untrusted (it crosses the network): bounds-check
  lengths, reject malformed packets, never index from attacker-controlled offsets.

## 5. Concurrency

- Protect shared state with the simplest correct tool; prefer channels/ownership over
  shared mutable state. The 1-slot serialization and slot lease are concurrency:
  test them with the race detector.
- **`go test -race` MUST pass in CI** and is required locally before pushing
  concurrency changes.
- Every goroutine has a clear owner and a shutdown path (context cancellation); no
  goroutine leaks. Long-lived loops select on `ctx.Done()`.

## 6. Code style & structure

- `gofmt` + `goimports` clean (CI-enforced). `go vet` clean. `golangci-lint` (errcheck,
  staticcheck, gosec, ineffassign, revive) clean — config in `.golangci.yml`.
- Explicit over clever: a new contributor should read a function in 30 seconds.
  Prefer a 10-line obvious version to a 200-line abstraction.
- Small packages with clear responsibility; no `util`/`common` grab-bags.
- Exported symbols have doc comments. Comments explain *why*, not *what*.
- DRY against what exists — search before adding; don't reinvent a reducer or a codec.

## 7. Security (this is a public UDP service)

- No high-rate flow before the return-routability handshake validates the source
  (anti-amplification, see spec G1). Reviewer MUST check this on any server path change.
- Response ≤ request size until the source is validated; enforce per-IP/per-code rate
  limits and a global egress cap.
- Treat every wire field as hostile (item 4).
- **No secrets in the repo.** Tokens, access codes, DB creds come from env / mounted
  config, never committed. `.gitignore` covers `.env`/`config.local.*`; a pre-commit
  secret scan SHOULD run. PII (`public_ip`) is hashed/truncated and consent-gated per spec.

## 8. CI gates (a PR merges only if all pass)

1. `gofmt`/`goimports` clean, `go vet`, `golangci-lint`.
2. `go test ./... -race` green.
3. Coverage thresholds (compute/protocol ≥90%, net/engine ≥70%).
4. **Import-boundary check** — a script asserting `compute`/`protocol` import no I/O
   and the dependency direction in ARCHITECTURE holds. (A simple `go list -deps` test.)
5. Golden + property tests green.
6. Benchmarks run (not gated on perf, but must compile and report).

## 9. Commits & PRs

- Small, focused commits; imperative subject; body explains *why*.
- One logical change per PR; PR description states what changed, why, and how it was
  tested (which layers).
- A PR touching the measurement math MUST call that out and show the golden-test diff.
- Never commit on `main` directly for code — branch, PR, review, merge.
- Co-author/footer trailers per repo convention.

## 10. Definition of done (per change)

- [ ] Behavior covered by the right test layer (pure → unit+golden+property).
- [ ] Adversarial inputs tested (empty/single/loss/reorder/out-of-range).
- [ ] Deterministic (no real clock/rand/net in unit tests).
- [ ] Import boundaries respected; lints + `-race` clean.
- [ ] Errors wrapped; untrustworthy conditions → `inconclusive` with caveat.
- [ ] Hot-path changes have a benchmark + alloc report.
- [ ] No secrets; PII handled per spec.
- [ ] Docs/contract updated if the `Result` shape changed.
