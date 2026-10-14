## 1. Decisions and prerequisites

- [x] 1.1 Record D-71 (headless quality and supply-chain gates: OSV-Scanner, gitleaks, Trivy upgrade with SBOM, expanded golangci-lint, coverage floors; SonarQube and OWASP Dependency-Check considered and rejected) and the D-68 amendment for the console dev dependencies, both after owner approval. Verify `make docs` passes and both decisions cite their alternatives.
- [x] 1.2 Resolve the existing govulncheck findings: bump `golang.org/x/text` to ≥ v0.41.0 and, on the owner's decision, the Go toolchain to the 1.26 line (`.mise.toml`, `go.mod`, the Dockerfiles). Verify `make vuln` passes and every Go tier still passes (unit, integration, e2e, the three UATs).

## 2. Go static analysis

- [x] 2.1 Enable gosec, errorlint, bodyclose, noctx, contextcheck, nilerr, rowserrcheck, sqlclosecheck, gocritic, revive, misspell, gocognit (30) and nolintlint (require-explanation) in `.golangci.yml`. Fix each finding or suppress it in place with a reason. Verify `make lint` reports 0 issues, and that a planted `http.Get` without a context and an unexplained `//nolint` each fail it.

## 3. Console static analysis

- [x] 3.1 Add ESLint 9 with typescript-eslint (type-aware), react-hooks and jsx-a11y (exact versions, dev-only), `console/eslint.config.js`, and `npm run lint`, plus `make console-lint` in `console-check`. Fix all findings. Verify `make console-lint` is clean, that a planted conditional hook call and an `<img>` without alt text each fail it, and that `npm audit --omit=dev` stays at 0.

  *(done 2026-10-14, as **Oxlint**, approved by the owner (D-68 amendment, D-71 resolved): the ESLint set cannot run on TypeScript 7.
  - `oxlint` 1.86.0 and `oxlint-tsgolint` 7.0.2003 are installed exact and dev-only, the newest releases at least two weeks old. Both are MIT.
  - `console/.oxlintrc.json` makes the correctness and suspicious categories errors, plus named rules: rules-of-hooks, exhaustive-deps, alt-text, no-floating-promises, no-misused-promises, await-thenable and switch-exhaustiveness.
  - Four rules are off, each with its reason in the config: react-in-jsx-scope, prefer-tag-over-role, no-unstable-nested-components and memo-dependencies. Two are off in tests only.
  - `npm run lint` and `make console-lint` exist, and the lint runs in `console-check`, `make quality` and the Jenkins console stage.

  All 843 first findings were resolved:
  - 682 came from the React-19-irrelevant rule;
  - the rest were fixed in code.

  The code fixes:
  - **Async handlers:** eleven async form handlers are now explicitly voided.
  - **Effects:** effects return their cleanups consistently.
  - **Dialogs:** five dialogs mount their form only while open, instead of resetting state in an effect. This also fixes the inbound dialog wiping a typed number when the extension list refetched.
  - **Navigation drawer:** it closes by path, without an effect.
  - **Call filters:** they follow the address during render, keeping focus on Apply.
  - **Modal:** its ref is no longer written during render.
  - **Unchecked casts:** they are replaced with real checks.
  - **Real defects found:**
    - `SelectTenantPage` followed an unvalidated return path. It now uses `returnPath`, which refuses `//elsewhere`, with a new test.
    - It also showed a raw error message. It now shows console copy via `toApiError`.
  - **Switches:** they name every case.
  - **String lengths:** they are measured in code points, matching the server's `utf8.RuneCountInString`, through one `codePoints` helper.

  Verified:
  - `make console-lint` is clean.
  - Planted defects each fail it: a conditional `useState`, an `<img>` without alt, and a floating promise.
  - `make console-check` passes (component 395, e2e 24, integration 24, UAT 3, audit 0, secrets clean).
  - `make sca` passes.
  - The console floors were ratcheted to 96.3 / 88.2 / 98.3 / 97.5.)*

## 4. Coverage

- [x] 4.1 Go: produce one merged profile across unit and integration (`-coverpkg`, atomic), and add the e2e tier through a cover-instrumented dev image and `GOCOVERDIR`. Emit Cobertura. Verify the merged total exceeds the unit-only total and the report lists e2e-only paths (for example the ARI event loop) as covered.
- [x] 4.2 Console: enable `@vitest/coverage-v8` with LCOV and Cobertura output and thresholds. Verify `make console-test` writes the reports and fails when a threshold is raised above the measured value.
- [x] 4.3 Add `quality/coverage-floors.json` (overall, `*/domain` at 100%, console), a `make coverage` gate and `make coverage-ratchet`. Verify the gate passes at today's floors and fails when one floor is raised by one point.

## 5. Supply chain

- [x] 5.1 Pin osv-scanner in mise. Add `make sca` scanning `api/go.mod` and `console/package-lock.json` with the D-35 licence allowlist and `quality/osv-scanner.toml` for documented exceptions. Verify it passes, and that a planted GPL-licensed dependency and a known-vulnerable version each fail it.
- [x] 5.2 Pin gitleaks. Add `make secrets` with `quality/gitleaks.toml`, which allowlists only the synthetic fixtures, each with a comment. Run a one-off full-history scan and record the result. Verify a planted fake AWS key fails it.
- [ ] 5.3 Upgrade Trivy (digest-pinned) and add CycloneDX SBOM generation per image (`make sbom`). Verify both images scan with HIGH and CRITICAL gating and that the SBOMs list the Go modules and the Asterisk image's OS packages. *(partial, 2026-10-13: Trivy 0.75.0 pinned and in Jenkins; the CycloneDX SBOM of the API image lists its Go modules (27 components, `x/crypto` v0.55.0, stdlib 1.26.9); the API image is clean. **Blocked:** the Asterisk image fails, with 20 fixable HIGH findings (python3.11 deb12u8 → deb12u9) and 440 HIGH/CRITICAL findings Debian has not fixed yet. The image is single-stage and ships its build toolchain (build-essential, git, subversion, wget, Python). That needs an owner decision: a multi-stage runtime image (its own change, regression on the phone UATs), and whether the gate ignores findings Debian has not fixed (`--ignore-unfixed`).)*

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

