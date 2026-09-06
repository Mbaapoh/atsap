---
name: acceptance-test-authoring
description: Write acceptance tests from PRD acceptance criteria (AC-xx) and OpenSpec delta-spec scenarios. Use when a change needs black-box tests proving each AC, or when asked to cover acceptance criteria with tests.
---

Write acceptance tests that prove the product behaves as specified — one test
per acceptance criterion, traceable by ID, black-box wherever possible.

**Lens:** system design — contracts and observable behavior, with a QA bar:
every AC proven by a test or explicitly listed as uncovered.

## Ground truth (read before writing)

- `docs/PRD.md` — the acceptance criteria (`AC-xx.x`) under each epic; the
  user stories (`US-xx.x`) that own them. ACs are the contract. Never invent
  new ones; if behaviour has no AC, say so and stop.
- `openspec/changes/<name>/specs/` — delta-spec scenarios for the change in
  progress; each scenario maps to one or more ACs.
- `openspec/specs/` — living specs from archived changes (regression surface).
- `docs/hld/08-performance.md` — SLOs a test may assert (P95 setup latency,
  CPS, MOS targets). `docs/hld/09-failure-model.md` — degradation behaviour a
  test must expect (e.g. recording fails → call continues, admin alerted).
- `docs/TOOLSET.md` + `go.mod` — approved test libraries only.

## Method

1. **Extract the AC list** for the change: IDs, exact wording, owning epic.
   Reject vague ACs back to the author — an untestable AC is a spec defect,
   not a test problem.
2. **One test per AC**, named after it (`TestAC_10_1_...`). A test asserts the
   observable outcome in the AC wording — API responses over ConnectRPC,
   NATS events published, rows written — never internals (no Asterisk channel
   IDs, per TRD anti-corruption rule).
3. **Black-box first.** Drive the public ConnectRPC API with a tenant-scoped
   identity, exactly as the portal or a partner would (D-24, API-first). Drop
   to ARI/AMI fixtures or `sipgo` endpoints (D-25 rig) only where the AC is
   about the media path itself.
4. **Compliance ACs assert prevention, not warnings** (PRD INV-09): the
   unlawful call is refused, the over-limit campaign stops. Test the refusal.
5. **Failure-model ACs assert the degraded path** (hld/09): what the caller
   hears, what the agent sees, what alert fires — all three.
6. **Audit/observability ACs assert the record** (12-factor XI): emitted
   log and audit entries carry the D-39 fields; the compliance report
   (AC-15.5 pattern) shows rules applied, calls blocked, and the reason
   for each.
7. **Emit a traceability table** test → AC → epic at the end. Any AC without
   a test is listed as uncovered, explicitly.

## Constraints

- Domain language only: `Call`, `Participant`/`CallParticipant`. `Leg` is
  banned (D-32); fixed "caller/agent" parties are banned (D-18).
- `tenant_id` on every fixture row (D-24, seam 1). Cross-tenant leakage tests
  belong wherever isolation ACs exist (PRD AC-10.4 pattern).
- No secrets, tokens, audio, or PII in fixtures or assertions (D-39).
- Tests must pass the machine gates (D-28): `golangci`, `govulncheck`, and
  the repo's CI test suite. A test that cannot run in CI is a draft, not done.
- Testing that uncovers a missing architectural decision (new pattern,
  contract, or policy needed) stops at the finding and routes it through
  the architectural-decision-records skill — tests prove decisions, they
  don't take them.
