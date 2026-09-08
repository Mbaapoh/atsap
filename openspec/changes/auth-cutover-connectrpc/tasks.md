## 1. Establish the baseline before changing anything

- [ ] 1.1 Run both e2e suites and the integration tier and record them green, so a test that turns red during the cutover is distinguishable from one that was already failing; verify by capturing the pass output
- [ ] 1.2 Confirm no caller outside the repo depends on unauthenticated access: grep `scripts/`, `deploy/`, and the UAT rig for `TelephonyService`; verify by recording what was found (expected: nothing)

## 2. Cut over

- [ ] 2.1 Add a telephony `TenantMatcher` alongside `identityrpc.TenantID`, returning the request's `tenant_id` for `GetCall`; verify a unit test shows it extracts the field and reports absent for a message that has none
- [ ] 2.2 Mount `TelephonyService` with the auth interceptor in `cmd/atsap-api`, deleting the comment recording the concession; verify the app starts and an unauthenticated `GetCall` returns `401` against the running binary
- [ ] 2.3 Confirm `AuthInterceptor` itself needed no modification; if it did, stop and surface it — needing to change the interceptor means the abstraction was wrong (design Non-Goals)

## 3. Update the callers that relied on the concession

- [ ] 3.1 Update `internal/server/server_test.go` so its `GetCall` request authenticates rather than expecting an unauthenticated route; verify the package's tests pass
- [ ] 3.2 Update `internal/telephony/e2e/walkingskeleton_test.go` to obtain a real token via the fixture path (seed tenant + operator, call `AuthenticateUser`) rather than being exempted; verify the walking-skeleton e2e passes end to end against the live rig
- [ ] 3.3 Verify no `ExemptProcedure` call was added for anything but `AuthenticateUser`; verify by asserting the interceptor's exemption is exactly that one procedure in a test

## 4. Prove it cannot silently regress

- [ ] 4.1 Add a test asserting an unauthenticated `GetCall` is refused, mirroring the guard `pbx-e2e-projection` added for `PbxService`; verify by fault injection — unmount the interceptor, see the test fail, remount, see it pass
- [ ] 4.2 Add a test asserting a caller cannot read a call by naming another tenant while holding a valid token for their own; verify it fails with the mismatch refusal, not a `not found`

## 5. Documentation and close-out

- [ ] 5.1 Update `docs/API.md` §1: remove the "TelephonyService remains unauthenticated" concession and state the API-wide posture; verify `mise run docs` passes
- [ ] 5.2 Record in LLD-02 §9 that `identity-bootstrap` is closed as unnecessary, with the reason (the fixture path obtains a real token; a second minting path is a second thing to keep secure); verify the docs gate passes
- [ ] 5.3 Run the full gate: `mise run docs`, `mise run lint`, `mise run proto` (must stay green — no proto change), `mise run test`, integration `-p 1`, both e2e suites; verify all pass
- [ ] 5.4 Write `coverage.md` mapping each delta-spec scenario to its test, naming any gap rather than counting it covered
