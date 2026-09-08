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
| **integration** | `//go:build integration` | live dev Postgres (+ NATS where the test says so) | explicitly, local + CI-gated stage | `go test -tags integration -p 1 ./... -race` |
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
- **`integration` always runs with `-p 1` across `./...`.** `go test`
  runs different packages' test binaries in parallel by default; once
  more than one package's integration tests migrate the same shared dev
  database (as `internal/postgres` and `internal/telephony/postgres`
  both do from task 6.3 onward), that parallelism produces real
  Postgres deadlocks on concurrent `DROP TABLE`/`CREATE TABLE` — not a
  flaky test, a genuine cross-package schema race. `-p 1` serializes
  package test binaries (tests within one package still ran
  sequentially already, `-p 1` only changes cross-package scheduling).

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

`mise run ci` runs `docs → lint → vuln → test` (unit). The CI pipeline —
whatever provider implements it (`Jenkinsfile` today) — runs the same
stages in the same order, then builds images and scans them (Trivy). The `integration` and `e2e`
suites run in their own stages once they exist — the walking skeleton's
tasks 2.x/5.6/6.3/7.x (integration) and 10.2 (e2e, local rig **and** CI)
are the first occupants.

## 6. Reproducing any suite

Same commands locally and in CI — that is the point. Start the services
first (`docker compose -f deploy/docker-compose.yml up -d postgres nats`
at minimum; the full stack for `e2e`), then:

| Suite | Command | Services needed | Env overrides (defaults shown) |
|---|---|---|---|
| unit | `cd api && go test ./... -race -cover` | none (hermetic by rule §1) | — |
| integration | `cd api && go test -tags integration -p 1 ./... -race` | Postgres, NATS (the **app container competes**, see below) | `DATABASE_URL` (`postgres://atsapbx_app:devpassword123@localhost:15432/atsapbx?sslmode=disable`), `NATS_URL` (`nats://127.0.0.1:4222`) |
| e2e (telephony-core) | `cd api && go test -tags e2e ./internal/telephony/e2e/ -count=1` | full dev stack (Asterisk + Postgres + NATS + app) | above plus ARI `http://127.0.0.1:8088/ari`, SIP `127.0.0.1:5060/udp`, app API `http://127.0.0.1:8080` |
| e2e (pbx-core projection) | `cd api && go test -tags e2e ./internal/pbx/e2e/ -count=1` | full dev stack; the **app container must carry `PbxService`** — rebuild it (`docker compose -f deploy/docker-compose.yml build app && ... up -d app`) after any change to the API, or every call 404s | as above; SIP UAs run in-process (REGISTER only, so ephemeral ports are fine and no firewall change is needed) |
| automated UAT | `mise run uat:auto` | full dev stack + host baresip phones (script manages them) | — (script owns setup/teardown; see `docs/uat/walking-skeleton.md`) |

> **The integration tier shares its database with the running app.**
> `deploy-app-1` runs its own outbox worker against the same `outbox`
> table and continuously claims unpublished rows — measured: 20 rows
> inserted, 20 unpublished at t+0, **0 unpublished at t+3**, with nothing
> else polling. It also has the schema reset underneath it by
> `resetSchema`. So an integration test must assert only what it owns: no
> test may assume it is the sole consumer of the outbox, and a failure
> that appears only with the stack up is a rig condition before it is a
> product one. Stop the app container if a test genuinely needs exclusive
> access — and say in the test why.

`DATABASE_URL`/`NATS_URL` defaults target the compose-published host
ports, so a default dev stack works with zero extra flags. If a suite
cannot run hermetically (unit) or against these defaults, that is a
defect in the suite, not in the machine.

### 6.1 There is no seeded dev tenant

Migration `0002_dev_tenant_seed` once inserted a fixed-UUID tenant behind
`ATSAPBX_SEED_DEV_TENANT`. Both the gate and the seed are gone: tenants
are provisioned through `identity` (openspec change
`identity-auth-rbac`), and no migration conjures one in any environment.
`0002` remains as an empty tombstone purely so `golang-migrate` can still
resolve the chain on a database already at that version — deleting the
files strands every existing dev stack.

Tests create the tenants they need, which they already did. A test that
assumed the seeded UUID would now find nothing, correctly: provisioning
is a call, not a fixture.

## 7. Results & records

- **Per-run records live in CI, never in git.** The Test stage publishes
  JUnit XML (`junit` step: per-build history, trends, flake tracking) and
  archives the coverage profile. Committing test output to git is an
  anti-pattern: it conflicts, rots daily, and duplicates what CI already
  versions per build.
- **Per-task evidence lives in `tasks.md`.** A ticked task keeps its
  verify proof in parens (command + outcome, e.g. "100% coverage",
  "Verified via `TestOutboxWorkerRole`"). A tick without evidence is
  indistinguishable from untested — treat it as such in review.
- **Per-change traceability lives in the living spec.** At archive, the
  acceptance-test-authoring skill emits the test → AC → epic table
  (with the explicit uncovered-AC list) alongside the merged spec. That
  table — not scattered logs — is how progress is tracked release to
  release.
- **The agent feedback loop is therefore:** CI failure (or local red) →
  reproduce with §6 (same command, same env) → fix → re-run the gate
  (`mise run ci`, plus the affected suite) → record evidence in
  `tasks.md`. Findings that change design — not just code — go through
  `/opsx-update` into proposal/design/specs, so the next session inherits
  the decision instead of rediscovering the failure. This is how AI
  agents (or humans) turn any red build into a fix without tribal
  knowledge: the failing artifact, the reproduction recipe, and the
  decision trail are all in the repo or one click away in CI.
