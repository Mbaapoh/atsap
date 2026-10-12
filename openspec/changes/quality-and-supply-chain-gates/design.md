## Context

The gates today are `docs → lint → vuln → test` (D-28, D-59), run by `make ci` and Jenkins.
- Lint is golangci-lint with 5 linters.
- `vuln` is govulncheck.
- Unit tests archive a coverage profile; nothing reads it.
- Trivy 0.52.2 scans images.

The console has four test tiers (`make console-check`), but none runs in Jenkins. D-35 requires permissive licences and continuous supply-chain scanning. D-68 freezes the console's dependencies until a decision amends it. See proposal.md for the motivation.

## Goals / Non-Goals

**Goals:**
- Every gate runs headless from one command (`make quality`), the same locally and in Jenkins, with no server to operate.
- Coverage is measured across every Go tier and for the console, and can only go up.
- D-35's licence rule and "no committed secrets" become machine checks.
- Reports are standard formats (JUnit, Cobertura/LCOV, SARIF, CycloneDX) that Jenkins renders and any dashboard could read later.

**Non-Goals:**
- A quality dashboard server (SonarQube, Dependency-Track). This can be added later as a consumer of the same reports.
- Mutation testing or fuzzing beyond what exists.
- Replacing govulncheck or `npm audit`: OSV-Scanner complements them.
- Container runtime security or DAST. Asterisk and SIP exposure are covered by the security HLD and penetration tests, not this change.

## Decisions

### 1. OSV-Scanner for SCA, not OWASP Dependency-Check

OSV-Scanner reads the exact lockfiles (`go.mod`/`go.sum`, `package-lock.json`) and queries OSV.dev. OSV.dev aggregates the Go vulnerability database, GitHub Security Advisories and npm advisories, so a match is by package and version, not by guessed CPE. It is a single static binary under Apache-2.0, an OpenSSF project, with SARIF output and a built-in licence scan (`--licenses=<allowlist>`), which enforces D-35 in the same pass.

**Alternative considered: OWASP Dependency-Check.** It is a Java tool that matches by CPE: noisy for Go modules and npm, slow on first run, and it needs an NVD API key to be usable. Rejected as the gate. "OWASP SCA" as a practice is met; that particular tool is not the best fit for these ecosystems.

**Alternative considered: Trivy `fs` for SCA as well.** Viable, and kept for images. OSV-Scanner is preferred for source lockfiles because its licence policy is first-class and its database is the Go vulnerability database's upstream.

### 2. Coverage measured in every tier, gated with ratcheting floors

- **Go unit and integration:**
  - run with `-coverpkg=./internal/...,./cmd/...` and `-covermode=atomic`, so a package exercised by another package's tests counts;
  - profiles are merged with `go tool covdata` (text format via `go tool cover`);
  - Cobertura output comes from a small in-repo converter or `gocover-cobertura` (MIT, pinned), decided at apply time by licence and maintenance.
- **Go e2e:** Tilt's dev image builds the app with `go build -cover`. The e2e target runs it with `GOCOVERDIR` mounted, then merges, so the code that only real Asterisk and Postgres exercise is counted too.
- **Console:** `@vitest/coverage-v8`, the coverage provider Vitest ships for the same version (4.1.x), produces LCOV and Cobertura.
- **Gate:**
  - `quality/coverage-floors.json` lists per-area floors: overall, `*/domain` at 100%, and the console;
  - the gate fails below a floor;
  - floors start at today's measured values rounded down, and a separate target raises them when coverage improves, so the number never silently falls.

**Alternative considered: SonarQube's quality gate.** It needs a server, and the free edition analyses only the main branch. Rejected for now.

### 3. Go linters: enable what golangci-lint already ships

Enable gosec (security), errorlint (`%w` and `errors.Is`, which D-40 already mandates), bodyclose, noctx (requests without a context, which D-40 forbids), contextcheck, nilerr, rowserrcheck and sqlclosecheck (pgx correctness), gocritic, revive (house style), misspell and gocognit (cognitive complexity, threshold 30). No new binary is added. Existing findings are fixed in this change, or suppressed in place with `//nolint:<linter> // <reason>`. A suppression without a reason fails lint (`nolintlint` with `require-explanation`).

### 4. Console ESLint, by D-68 amendment

ESLint 9 (flat config) with typescript-eslint (type-aware rules on the strict tsconfig), eslint-plugin-react-hooks (hook rules, which `tsc` cannot check) and eslint-plugin-jsx-a11y (accessibility, which supports HLD-19 §7). All are MIT and dev-only, so none ships in the bundle. Exact versions are pinned with `npm install --save-exact`, and `npm audit` stays clean. This needs the owner's approval, recorded as a D-68 amendment before install.

### 5. Secrets: gitleaks

gitleaks (MIT, single binary) runs `gitleaks dir` on the working tree in every build, and `gitleaks git` over the full history once in this change. The allowlist (`quality/gitleaks.toml`) names only the synthetic fixtures that are meant to be public (D-39), such as the Argon2id fixture hash and the dev-rig passwords in `.env.example`, each with a comment.

### 6. Trivy upgraded, plus SBOM

Pin a current Trivy release by digest in the Jenkinsfile and through mise locally. Scan both images as today (HIGH and CRITICAL fail), and emit a CycloneDX SBOM per image as a build artefact (`trivy image --format cyclonedx`). Generating the SBOM needs no extra tool.

### 7. One entry point, the same everywhere

`make quality` runs, in order:
1. lint (Go and console);
2. coverage (unit and integration, plus e2e when the rig is up);
3. `sca` (OSV-Scanner and govulncheck);
4. `secrets`;
5. `audit`.

Jenkins calls the same targets stage by stage and publishes JUnit, Cobertura and SARIF. The console gets its own stages: `npm ci`, typecheck, unit with coverage, lint, e2e (Chromium in the agent image) and audit. Its integration and UAT tiers stay on the rig stage, as the Go e2e does.

## Risks / Trade-offs

- **[Risk] New linters flood the first run.** → Fix them in this change, package by package. Suppress in place only with a stated reason. Never disable a linter wholesale to get green.
- **[Risk] Coverage floors push assertion-free tests.** → TESTING §4 stands: coverage is a tripwire, not a target. Floors start at today's values; only `make coverage-ratchet` raises them, and it is a deliberate commit reviewed like code.
- **[Risk] OSV-Scanner and govulncheck disagree.** → govulncheck's reachability is authoritative for Go: an unreachable OSV finding is documented in `quality/osv-scanner.toml` with an expiry date, never silently ignored.
- **[Risk] The gate is red on day one** because of the existing Go 1.25 standard-library and `x/text` advisories. → Resolve first: the owner decides the Go 1.26 toolchain bump, and `x/text` is bumped. Until then the `vuln` stage reports these by ID and the change's tasks track them.
- **[Trade-off] No dashboard.** Jenkins' coverage and warnings views are the dashboard. A SonarQube Community instance can be added later and fed these reports without changing any gate.

## Migration Plan

1. Land the D-68 amendment and D-71 after owner approval.
2. Add the tools behind `make quality` while the CI stages only report.
3. Fix the findings until `make quality` is green.
4. Then switch the CI stages to fail the build.

Rollback is per stage: a stage can return to report-only while its findings are fixed, and each such fallback is recorded in PROGRESS.md.

## Open Questions

- The exact pinned versions of osv-scanner, gitleaks, Trivy and the npm packages are resolved at apply time: the latest release that is at least two weeks old, with its licence checked. This does not change the approach or the tasks.
