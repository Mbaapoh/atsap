# AtsaPBX Testing Conventions

How tests are organized, named, and run in this project. Fills the "test
strategy" section TRD defers here. Read alongside the
`test-driven-development` and `acceptance-test-authoring` skills, which
define the Red-Green-Refactor cycle and the per-AC form.

Traceability: PRD §17 (Definition of Ready evidence); LLD-01 §8 (first
Definition of Done); TOOLSET.md §1 (gates).

---

## 1. The three suites

| Suite | Build tag | Needs | Runs where | Command |
|---|---|---|---|---|
| **unit** | none (default) | nothing — pure logic, `httptest` fakes, scripted fixtures | every `go test`, local + CI | `go test ./... -race -cover` |
| **integration** | `//go:build integration` | live dev Postgres (+ NATS where the test says so) | explicitly, local + CI-gated stage | `go test -tags integration ./... -race` |
| **e2e** | `//go:build e2e` | full rig: Asterisk + Postgres + NATS (dev compose) | explicitly, local rig + CI e2e stage | `go test -tags e2e ./...` |

Rules:

- **Default (`go test ./...`) is unit only and must stay hermetic.** A test
  that needs a socket, a container, or the network belongs behind
  `integration` or `e2e` — a unit gate that cannot run on a laptop is a
  draft, not done.
- `-race` is non-negotiable on unit and integration. Anything touching the
  ACL correlation registry, the outbox worker, or NATS callbacks is
  exercised concurrently.
- Long/soak and load shapes (HLD `08-performance.md`) are marked
  long-running and gated separately — they never block the unit gate.

## 2. Layout

- Tests live next to code: `foo_test.go` beside `foo.go`, same package
  (white-box) or `foo_test` package (black-box) where the boundary matters.
- Table-driven tests with `t.Run` subtests; failure messages state input,
  got, and want (`t.Errorf("Foo(%q) = %d; want %d", ...)`).
- `testify` (`assert`/`require`) is approved for readability; plain
  `testing` is fine for small cases.
- Shared fixtures (synthetic tenants, masked numbers like `+1555****0199`)
  live in the owning package's `*_test.go`, never in production files.
- No secrets, tokens, audio, or PII in fixtures or assertions (D-39).

## 3. Naming & traceability

- Acceptance tests: one per PRD criterion, named after it —
  `TestAC_10_1_...`, `TestAC_15_3_...` — driven black-box through the
  public ConnectRPC API as the portal or a partner would (D-24).
- Each change ends with a test → AC → epic table; every AC without a test
  is listed as uncovered, explicitly.
- Invariants get permanent regression tests: `tenant_id` on every
  row/object, Participant identity across channel replacement, usage never
  double-counts nor gaps, compliance refusal with no override (INV-09),
  emergency calls skip Screening (INV-01).
- Failure-model tests assert the degraded path (HLD `09-failure-model.md`):
  what the caller hears, what the agent sees, what alert fires — all three.

## 4. Coverage bar

- `go test ./... -race -cover` must pass; new behavior ships with tests
  that fail first (Red) and an uncovered-AC list of zero for the change's
  scope.
- Coverage is a tripwire, not a target: 100% on pure domain units
  (transitions, compliance verdicts, usage accrual), honest everywhere
  else. A coverage number bought with assertion-free tests is a defect in
  the test, not credit.

## 5. CI mapping

`mise run ci` runs `docs → lint → vuln → test` (unit). Jenkins mirrors it,
then builds images and scans them (Trivy). The `integration` and `e2e`
suites run in their own stages once they exist — the walking skeleton's
tasks 2.x/5.6/6.3/7.x (integration) and 10.2 (e2e, local rig **and** CI)
are the first occupants.
