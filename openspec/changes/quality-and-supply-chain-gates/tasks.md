## 1. Decisions and prerequisites

- [x] 1.1 Record D-71 (headless quality and supply-chain gates: OSV-Scanner, gitleaks, Trivy upgrade with SBOM, expanded golangci-lint, coverage floors; SonarQube and OWASP Dependency-Check considered and rejected) and the D-68 amendment for the console dev dependencies, both after owner approval. Verify `make docs` passes and both decisions cite their alternatives.
- [x] 1.2 Resolve the existing govulncheck findings: bump `golang.org/x/text` to ≥ v0.41.0 and, on the owner's decision, the Go toolchain to the 1.26 line (`.mise.toml`, `go.mod`, the Dockerfiles). Verify `make vuln` passes and every Go tier still passes (unit, integration, e2e, the three UATs).

## 2. Go static analysis

- [x] 2.1 Enable gosec, errorlint, bodyclose, noctx, contextcheck, nilerr, rowserrcheck, sqlclosecheck, gocritic, revive, misspell, gocognit (30) and nolintlint (require-explanation) in `.golangci.yml`. Fix each finding or suppress it in place with a reason. Verify `make lint` reports 0 issues, and that a planted `http.Get` without a context and an unexplained `//nolint` each fail it.

## 3. Console static analysis

- [ ] 3.1 Add ESLint 9 with typescript-eslint (type-aware), react-hooks and jsx-a11y (exact versions, dev-only), `console/eslint.config.js`, and `npm run lint`, plus `make console-lint` in `console-check`. Fix all findings. Verify `make console-lint` is clean, that a planted conditional hook call and an `<img>` without alt text each fail it, and that `npm audit --omit=dev` stays at 0.

## 4. Coverage

- [x] 4.1 Go: produce one merged profile across unit and integration (`-coverpkg`, atomic), and add the e2e tier through a cover-instrumented dev image and `GOCOVERDIR`. Emit Cobertura. Verify the merged total exceeds the unit-only total and the report lists e2e-only paths (for example the ARI event loop) as covered.
- [x] 4.2 Console: enable `@vitest/coverage-v8` with LCOV and Cobertura output and thresholds. Verify `make console-test` writes the reports and fails when a threshold is raised above the measured value.
- [x] 4.3 Add `quality/coverage-floors.json` (overall, `*/domain` at 100%, console), a `make coverage` gate and `make coverage-ratchet`. Verify the gate passes at today's floors and fails when one floor is raised by one point.

## 5. Supply chain

- [x] 5.1 Pin osv-scanner in mise. Add `make sca` scanning `api/go.mod` and `console/package-lock.json` with the D-35 licence allowlist and `quality/osv-scanner.toml` for documented exceptions. Verify it passes, and that a planted GPL-licensed dependency and a known-vulnerable version each fail it.
- [x] 5.2 Pin gitleaks. Add `make secrets` with `quality/gitleaks.toml`, which allowlists only the synthetic fixtures, each with a comment. Run a one-off full-history scan and record the result. Verify a planted fake AWS key fails it.
- [ ] 5.3 Upgrade Trivy (digest-pinned) and add CycloneDX SBOM generation per image (`make sbom`). Verify both images scan with HIGH and CRITICAL gating and that the SBOMs list the Go modules and the Asterisk image's OS packages.

## 6. CI and docs

- [x] 6.1 Add `make quality` (lint, coverage, sca, secrets, audit for both stacks). Add Jenkins stages for the console (npm ci, typecheck, unit and coverage, lint, e2e, audit) and the quality stages, publishing JUnit, Cobertura and SARIF, report-only first and then failing. Verify a Jenkins run (or `make quality` locally where Jenkins is unavailable) is green, and that a planted failure in each stage turns it red.
- [x] 6.2 Update TOOLSET §1.3 and §3a, TESTING §4 (floors and ratchet) and §5 (CI mapping), and PROGRESS.md. Verify `make docs` passes.
- [x] 6.3 Run every tier once more on the finished gate: backend unit, integration, e2e and UATs; console unit, e2e, integration and UAT; `make quality`. Record the results here.

**Results (2026-10-13):**
- **`make quality`: exit 0.**
  - Go lint: 0 issues.
  - Go unit coverage: 76.4% (floor 76.3%).
  - Console coverage: 93.1% statements, 83.8% branches, above its floors.
  - OSV-Scanner: no vulnerabilities, licences within the allowlist.
  - govulncheck: none reachable.
  - gitleaks: no leaks (working tree and all 230 commits).
  - npm audit: 0.
- **Planted failures caught:**
  - a context-less `http.Get`, an unclosed response body and an unexplained `//nolint`;
  - Go and console floors raised by one point;
  - a GPL (tinymce 7) and a vulnerable (lodash 4.17.15) dependency;
  - a fake AWS key.
- **Jenkins** could not run from this machine; its stages call the same make targets.
- **Every Go tier under coverage (`make coverage-all`):** unit, integration and e2e all pass. Merged coverage is 82.6% of 6,120 statements, at its floor.
- **Backend UAT:** `uat-auto`, `uat-placement` and `uat-inbound` PASS; `pbx reconcile` reports no divergence.
- **Console:** e2e 13, integration 9, UAT 1, all pass.
- **Images:** the production API image is Trivy-clean at HIGH/CRITICAL after `golang.org/x/crypto` v0.55.0. The Asterisk image scan is still pending; on this network the vulnerability database is reachable only through GHCR, and the 3.3 GB image scans slowly.

