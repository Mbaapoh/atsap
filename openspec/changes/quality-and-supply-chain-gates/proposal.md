## Why

Code quality and supply-chain safety are project goals, but today only part of them is enforced by a machine:
- golangci-lint runs 5 linters, none of them security-focused.
- Coverage is recorded for Go unit tests but never gated, and never measured for integration or e2e.
- The console is not in CI at all: no tests, no coverage, no linter, no audit.
- D-35's "permissive licences only" rule is enforced by nobody.
- Nothing scans for committed secrets.
- Trivy is pinned to a 2024 release.

The owner asked for SonarQube- or OWASP-style SCA and coverage, run headless, for both the backend and the frontend.

## What Changes

One headless, CLI-only gate for both stacks, run by `make quality` locally and by Jenkins. Every tool is open source with a permissive licence (D-35), pinned through mise, and needs no server.

- **Coverage, with floors that only ratchet up:**
  - Go coverage merged across the unit, integration and e2e tiers. The e2e tier uses a coverage-instrumented app binary (`go build -cover`, `GOCOVERDIR`).
  - Console coverage from Vitest (`@vitest/coverage-v8`).
  - Both export Cobertura/LCOV for Jenkins.
  - The build fails if coverage falls below a per-area floor; pure domain packages stay at 100%, as TESTING §4 already requires.
- **Static analysis:**
  - Go: golangci-lint gains gosec, errorlint, bodyclose, noctx, contextcheck, nilerr, rowserrcheck, sqlclosecheck, gocritic, revive, misspell and gocognit. These ship inside golangci-lint, so no new tool is added.
  - Console: ESLint with typescript-eslint, react-hooks and jsx-a11y.
- **Software composition analysis (SCA):**
  - **OSV-Scanner** (OpenSSF, Apache-2.0) over `api/go.mod` and `console/package-lock.json`, with a licence policy that enforces D-35's allowlist.
  - govulncheck (call-reachability for Go) and `npm audit` stay.
- **Containers and SBOM:**
  - Trivy moves to a current pinned release.
  - It also emits a CycloneDX SBOM for each image, archived per build.
- **Secrets:** **gitleaks** (MIT) scans the working tree on every build and the full history once.
- **CI:** Jenkins gains console stages (install, typecheck, test with coverage, lint, e2e, audit) and the new quality stages. JUnit, coverage and SARIF reports are published, so Jenkins becomes the dashboard.
- **Not adopted as the gate:**
  - **SonarQube** needs a server and database, and the free Community Build lacks branch and PR analysis.
  - **OWASP Dependency-Check** matches by CPE, which is slow and noisy for Go and npm, and it needs an NVD API key.
  Both are recorded as considered and rejected in the decision. SonarQube Community stays possible later as a read-only dashboard fed by the same reports.

## Capabilities

### New Capabilities

None. This is tooling: no product behaviour changes (`skip_specs: true`).

### Modified Capabilities

None.

## Impact

- **Build and CI:** `Makefile` (`quality`, `coverage`, `sca`, `secrets`, `sbom` targets and console lint), `.mise.toml` (osv-scanner, gitleaks, trivy pins), `.golangci.yml`, `Jenkinsfile` (console stages, quality stages, report publishing).
- **Console:** `console/package.json` adds ESLint 9, typescript-eslint, eslint-plugin-react-hooks, eslint-plugin-jsx-a11y and @vitest/coverage-v8, all dev-only. This needs a **D-68 amendment** approved by the owner (console brief: no dependency without a recorded decision). It also adds `console/eslint.config.js` and a coverage block in `vite.config.ts`.
- **Go:**
  - newly enabled linters will report existing findings. Each is fixed or explicitly suppressed with a reason; nothing is blanket-disabled;
  - `api/Dockerfile.dev` gains a cover-instrumented build for e2e coverage.
- **Repository:** `quality/` holds the coverage floors, the OSV licence policy and the gitleaks allowlist (synthetic fixture credentials such as the D-39 test hash).
- **Docs:**
  - a new decision **D-71**, which amends D-35 with the toolset and its rejected alternatives;
  - TOOLSET §1.3 and §3a;
  - TESTING §4 (coverage floors and ratchet) and §5 (CI mapping).
- **Known findings to resolve first:**
  - govulncheck already fails on Go 1.25.14 standard-library advisories (fixed in Go 1.26.9) and `golang.org/x/text` v0.39.0 (fixed in v0.41.0);
  - the toolchain bump is an owner decision, taken before this gate turns red-on-fail in CI.
