## 1. Domain

- [x] 1.1 Add `PreviousRotatedAt` to `Session`, the `RefreshGraceWindow` constant (30 s), the `RefreshRetried` outcome and `EventSessionRefreshRetried`. Set `PreviousRotatedAt` on a normal rotation; in `Refresh`, accept the previous digest within the window as a retry (rotate again; leave `PreviousDigest` and `PreviousRotatedAt` unchanged). Verify with domain unit tests for every scenario in the delta spec:
  - retry inside the window;
  - reuse after the window revokes;
  - a retry does not extend the window (retry at 10 s, reuse at 31 s revokes);
  - an older or unknown digest is refused without revoking;
  - a NULL `PreviousRotatedAt` (pre-migration session) revokes as today.
  Fault-inject a sliding window to prove the "does not extend" test catches it.

## 2. Storage

- [x] 2.1 Add migration `0011_session_refresh_grace` (`previous_rotated_at TIMESTAMPTZ NULL`; down drops it). Verify up, down and up again on the dev database, and that the migration test suite passes.
- [x] 2.2 Persist `previous_rotated_at` in the Postgres store (columns, scan, insert, update) and the in-memory store. Verify with the identity store integration tests: a rotation stores the time, a retry leaves it unchanged, and microsecond truncation does not break the window comparison.

## 3. Application

- [x] 3.1 Map `RefreshRetried` to a successful refresh in `RefreshSession`: record `session.refresh_retried` in `auth_events` and log at INFO (the WARN stays for real reuse). Verify with application tests:
  - a retry returns a token and a new secret;
  - one auth event is written;
  - no WARN is logged;
  - the secret from the lost response is then refused.

## 4. End to end

- [x] 4.1 Add an identity e2e test through the real RPC and cookie: refresh, drop the response, retry with the previous cookie inside the window and succeed; replay after the window and see the session revoked. Verify it runs green against the rig.
- [x] 4.2 Remove `test.fail` from the console's "a refresh interrupted in flight does not end the session" (`console/e2e/auth.rig.spec.ts`). Verify it passes against the Tilt rig, and fails again with the domain change reverted.
- [x] 4.3 Run every tier and record the results in this file:
  - backend: unit, integration, wire, e2e and UAT (`make ci`, `make uat-auto`);
  - console: component tests, Playwright including the rig specs.

## 5. Docs

- [x] 5.1 Amend D-62 with the grace window, its 30-second bound, the non-sliding rule and the accepted trade-off. Update LLD-02 §6 (sessions). Close the open gap in PROGRESS.md. Verify `make docs` passes.

**Results (2026-10-12):**
- **Backend:**
  - lint: 0 issues, after a `gofmt` fix of the new constant block;
  - unit: 22 packages ok, with the sliding-window fault injection caught by two domain tests;
  - integration (`-tags integration -p 1 -race`): every package ok. Each test migrates all the way down and up, so `0011` down and up run on every test;
  - e2e (`-tags e2e -p 1`): every suite ok, including the identity retry and replay-after-window subtests;
  - UAT: `uat-auto`, `uat-placement` and `uat-inbound` all PASS back to back;
  - `pbx reconcile`: no divergence.
- **Console** (`make console-check`):
  - 173 component tests, e2e 13, integration 9, UAT 1, audit 0;
  - the interrupted-refresh test passes on three consecutive runs and fails with the grace rule disabled (verified live through Tilt);
  - the server logged 6 retries and 0 replay revocations during those runs.
- **`govulncheck`** reports 10 advisories that predate this change. One is in `golang.org/x/text` v0.39.0 (fixed in v0.41.0). Nine are in the Go 1.25.14 standard library (fixed in Go 1.26.9). Both fixes are dependency or toolchain upgrades needing an owner decision, and they are reported separately.

