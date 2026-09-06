---
name: test-driven-development
description: Guide Red-Green-Refactor development on the Go/Asterisk stack. Use when implementing behaviour, or when asked to build something test-first.
---

Red writes the contract, Green meets it, Refactor keeps it. On this stack the
cycle is disciplined by the architecture: pure domain units first, ports
mocked at boundaries, the media path proven on the D-25 rig — never the
reverse.

**Lens:** the senior engineer writing the code, backed by DevOps gates and
system-design SLOs — red first, green second, clean third.

## The cycle, project-tuned

1. **Red.** Write the failing test from the AC or delta-spec scenario
   (acceptance-test-authoring skill for form). Start with the pure core:
   compliance verdicts (D-21), IVR fold transitions (D-22), usage accrual
   across a channel swap (D-17) — exhaustive tables, no I/O, no mocks.
2. **Green.** Minimal implementation. New domain code speaks ports the
   application layer defines; adapters (`ari`/`ami`, Postgres, NATS,
   carriers) implement them. Domain packages import no adapter, ever.
3. **Refactor.** Only on green — toward small pure functions with
   canonical names (glossary skill); D-21/D-22 are the shape to imitate.
   Delete dead code outright; never comment it out. D-25 spike code is
   deleted, never merged. Then prove the gates: `golangci`,
   `govulncheck`, full `go test`. D-28 — machine gates, not self-review.
4. **Integrate outward** in dependency order (D-26, TRD build graph):
   unit → port-contract tests with stub adapters (LLD-01 §5 pattern:
   stub `LicenseManager`/`ComplianceEngine` from day one) → walking-skeleton
   path on the local rig (extensions 1001–1999, `sipgo` caller, WSS softphone)
   → SIPp load shapes from `08-performance.md` where the AC carries an SLO.

## What gets tested (and how)

- **Invariants as tests**: `tenant_id` present on every row/object;
  Participant identity unbroken across channel replacement; usage never
  double-counts nor gaps on handover; compliance refusal with no override
  (INV-09); emergency calls skip Screening (INV-01).
- **ACL behaviour**: channel created/answered/transferred/replaced/hung-up
  and bridge entered/left events each map to exactly one
  Participant/Call transition; raw channel IDs never escape (assert their
  absence in API responses).
- **Failure paths first-class** (hld/09): recording fails → call continues +
  alert; carrier route fails → 5s failover; Asterisk node lost → calls end,
  UI reflects within SLA.
- **ConnectRPC contracts**: `buf breaking` clean; generated clients compile;
  JSON-over-HTTP/1.1 and gRPC-over-HTTP/2 both exercised where streaming
  ACs exist.

## Constraints

- Libraries from `go.mod` / `.mise.toml` / TOOLSET.md only (pgx/v5,
  ConnectRPC, `nats.go`, `slog` — no ORMs, no unapproved frameworks).
- Config arrives via environment only (12-factor III): no hardcoded
  endpoints, credentials, or environment assumptions in code or tests.
  `.env.example` documents the keys; `.env` is never read in tests and
  never committed (openspec-git-discipline skill).
- Human-in-the-loop stop rule: stop and ask on ambiguous ACs, spec-vs-code
  conflicts, anything beyond the change's task list, and anything needing
  a new dependency, skill, pattern, or decision (D-28/D-30/D-37 gates).
  The solo architect decides; the agent drafts and implements.
- No test commits secrets, tokens, audio, or PII (D-39); fixtures use
  synthetic tenants and masked numbers (`+1555****0199` pattern).
- A test that cannot run in CI is a draft. Performance/soak tests
  (08-performance §load framework) are marked long-running and gated
  separately — they never block the unit gate.
