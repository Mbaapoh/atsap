## 1. Domain

- [ ] 1.1 Add `PreviousRotatedAt` to `Session`, the `RefreshGraceWindow` constant (30 s), the `RefreshRetried` outcome and `EventSessionRefreshRetried`. Set `PreviousRotatedAt` on a normal rotation; in `Refresh`, accept the previous digest within the window as a retry (rotate again; leave `PreviousDigest` and `PreviousRotatedAt` unchanged). Verify with domain unit tests for every scenario in the delta spec:
  - retry inside the window;
  - reuse after the window revokes;
  - a retry does not extend the window (retry at 10 s, reuse at 31 s revokes);
  - an older or unknown digest is refused without revoking;
  - a NULL `PreviousRotatedAt` (pre-migration session) revokes as today.
  Fault-inject a sliding window to prove the "does not extend" test catches it.

## 2. Storage

- [ ] 2.1 Add migration `0011_session_refresh_grace` (`previous_rotated_at TIMESTAMPTZ NULL`; down drops it). Verify up, down and up again on the dev database, and that the migration test suite passes.
- [ ] 2.2 Persist `previous_rotated_at` in the Postgres store (columns, scan, insert, update) and the in-memory store. Verify with the identity store integration tests: a rotation stores the time, a retry leaves it unchanged, and microsecond truncation does not break the window comparison.

## 3. Application

- [ ] 3.1 Map `RefreshRetried` to a successful refresh in `RefreshSession`: record `session.refresh_retried` in `auth_events` and log at INFO (the WARN stays for real reuse). Verify with application tests:
  - a retry returns a token and a new secret;
  - one auth event is written;
  - no WARN is logged;
  - the secret from the lost response is then refused.

## 4. End to end

- [ ] 4.1 Add an identity e2e test through the real RPC and cookie: refresh, drop the response, retry with the previous cookie inside the window and succeed; replay after the window and see the session revoked. Verify it runs green against the rig.
- [ ] 4.2 Remove `test.fail` from the console's "a refresh interrupted in flight does not end the session" (`console/e2e/auth.rig.spec.ts`). Verify it passes against the Tilt rig, and fails again with the domain change reverted.
- [ ] 4.3 Run every tier and record the results in this file:
  - backend: unit, integration, wire, e2e and UAT (`make ci`, `make uat-auto`);
  - console: component tests, Playwright including the rig specs.

## 5. Docs

- [ ] 5.1 Amend D-62 with the grace window, its 30-second bound, the non-sliding rule and the accepted trade-off. Update LLD-02 §6 (sessions). Close the open gap in PROGRESS.md. Verify `make docs` passes.
