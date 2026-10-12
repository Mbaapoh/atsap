## 1. Decisions and prerequisites

- [ ] 1.1 Record D-71 (headless quality and supply-chain gates: OSV-Scanner, gitleaks, Trivy upgrade with SBOM, expanded golangci-lint, coverage floors; SonarQube and OWASP Dependency-Check considered and rejected) and the D-68 amendment for the console dev dependencies, both after owner approval. Verify `make docs` passes and both decisions cite their alternatives.
- [ ] 1.2 Resolve the existing govulncheck findings: bump `golang.org/x/text` to ≥ v0.41.0 and, on the owner's decision, the Go toolchain to the 1.26 line (`.mise.toml`, `go.mod`, the Dockerfiles). Verify `make vuln` passes and every Go tier still passes (unit, integration, e2e, the three UATs).

## 2. Go static analysis

- [ ] 2.1 Enable gosec, errorlint, bodyclose, noctx, contextcheck, nilerr, rowserrcheck, sqlclosecheck, gocritic, revive, misspell, gocognit (30) and nolintlint (require-explanation) in `.golangci.yml`. Fix each finding or suppress it in place with a reason. Verify `make lint` reports 0 issues, and that a planted `http.Get` without a context and an unexplained `//nolint` each fail it.

## 3. Console static analysis

- [ ] 3.1 Add ESLint 9 with typescript-eslint (type-aware), react-hooks and jsx-a11y (exact versions, dev-only), `console/eslint.config.js`, and `npm run lint`, plus `make console-lint` in `console-check`. Fix all findings. Verify `make console-lint` is clean, that a planted conditional hook call and an `<img>` without alt text each fail it, and that `npm audit --omit=dev` stays at 0.

## 4. Coverage

- [ ] 4.1 Go: produce one merged profile across unit and integration (`-coverpkg`, atomic), and add the e2e tier through a cover-instrumented dev image and `GOCOVERDIR`. Emit Cobertura. Verify the merged total exceeds the unit-only total and the report lists e2e-only paths (for example the ARI event loop) as covered.
- [ ] 4.2 Console: enable `@vitest/coverage-v8` with LCOV and Cobertura output and thresholds. Verify `make console-test` writes the reports and fails when a threshold is raised above the measured value.
- [ ] 4.3 Add `quality/coverage-floors.json` (overall, `*/domain` at 100%, console), a `make coverage` gate and `make coverage-ratchet`. Verify the gate passes at today's floors and fails when one floor is raised by one point.

## 5. Supply chain

- [ ] 5.1 Pin osv-scanner in mise. Add `make sca` scanning `api/go.mod` and `console/package-lock.json` with the D-35 licence allowlist and `quality/osv-scanner.toml` for documented exceptions. Verify it passes, and that a planted GPL-licensed dependency and a known-vulnerable version each fail it.
- [ ] 5.2 Pin gitleaks. Add `make secrets` with `quality/gitleaks.toml`, which allowlists only the synthetic fixtures, each with a comment. Run a one-off full-history scan and record the result. Verify a planted fake AWS key fails it.
- [ ] 5.3 Upgrade Trivy (digest-pinned) and add CycloneDX SBOM generation per image (`make sbom`). Verify both images scan with HIGH and CRITICAL gating and that the SBOMs list the Go modules and the Asterisk image's OS packages.

## 6. CI and docs

- [ ] 6.1 Add `make quality` (lint, coverage, sca, secrets, audit for both stacks). Add Jenkins stages for the console (npm ci, typecheck, unit and coverage, lint, e2e, audit) and the quality stages, publishing JUnit, Cobertura and SARIF, report-only first and then failing. Verify a Jenkins run (or `make quality` locally where Jenkins is unavailable) is green, and that a planted failure in each stage turns it red.
- [ ] 6.2 Update TOOLSET §1.3 and §3a, TESTING §4 (floors and ratchet) and §5 (CI mapping), and PROGRESS.md. Verify `make docs` passes.
- [ ] 6.3 Run every tier once more on the finished gate: backend unit, integration, e2e and UATs; console unit, e2e, integration and UAT; `make quality`. Record the results here.
