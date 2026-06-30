# ios — SwiftUI app

Thin UI over the shared Go core (`core/`, embedded as an `.xcframework` via
gomobile). Distributed through TestFlight.

Screens:
- Field tester: one pass/fail/inconclusive verdict + three sub-results, no jargon.
- Engineer detail: per-run telemetry, links to dashboard.

Risk owned here: **iOS may bleach ECN / override DSCP.** M0 spike confirms whether
`IP_TOS` on a POSIX UDP socket survives to the VPS; fallback is
`Network.framework` service classes or NQB-only. M0 must pass before this app is
built (M3).

Status: not started. M0 spike first, app in M3.
