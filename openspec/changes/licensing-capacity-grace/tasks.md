# Tasks — licensing-capacity-grace

Groups are ordered by dependency. Groups 1–3 build `licensing` in
isolation and cannot break anything; group 4 is the cutover, where the
stub is deleted and enforcement becomes real; groups 5–6 prove the
invariants against the live stack.

## 1. Gate first, so the boundary is enforced before there is code to enforce it

- [x] 1.1 Add a `licensing-imports-nothing` depguard rule to `.golangci.yml` denying `atsap-api/internal/telephony`, `atsap-api/internal/pbx` and `atsap-api/internal/identity` from `**/internal/licensing/**`, citing HLD 04 §10.1; verify `mise run lint` still reports 0 issues *(done — rule added before the package exists, deliberately; lint reports 0 issues)*
- [x] 1.2 Fault-inject the rule: add the forbidden import to a scratch file under `internal/licensing/`, confirm `mise run lint` fails naming the rule, then remove it — record the observed failure text in this task (LLD-08 DoD 9, D-28) *(done — a scratch `internal/licensing/domain` importing all three peers produced **3 issues**, one per deny entry, so each is independently live rather than the first shadowing the rest. Observed:*

  ```
  internal/licensing/domain/zz_faultinject.go:4:2: import 'atsap-api/internal/identity/domain'
    is not allowed from list 'licensing-imports-nothing': licensing is Tier 0 and depends on
    nothing (HLD 04 §10.1). identity and licensing are peers that never call each other …
  internal/licensing/domain/zz_faultinject.go:5:2: import 'atsap-api/internal/pbx/domain' …
  internal/licensing/domain/zz_faultinject.go:6:2: import 'atsap-api/internal/telephony/domain' …
  3 issues:
  ```
  *Tree clean again after removal: 0 issues.)*
- [x] 1.3 Add `atsap-api/internal/licensing` to `internal/archtest`'s scan as a package that may not import other contexts, with a fault-injection test proving it rejects, mirroring `TestScanModule_DetectsInjectedPbxViolation`; verify `go test ./internal/archtest/...` passes *(done — needed a new rule shape, not a list entry: `forbiddenImports` answers "who may import X" and cannot express "X may import nothing", since importing telephony is only wrong* inside *licensing. Added `isolatedContexts` (depending prefix → forbidden prefixes) and `checkIsolation`, kept pure like `checkImport`. Covered by `TestCheckIsolation` (7 cases, including that a context may import its own subpackages and that `licensingx` is unaffected by the `licensing` prefix) and `TestScanModule_DetectsInjectedLicensingViolation` through the real scanner. Adding the other Tier-0 contexts is one line each — see the note below.)*

## 2. Domain — pure, no I/O, no dependencies

- [ ] 2.1 Create `internal/licensing/domain` with `LicenseToken`, `CapacityVerdict`, `GraceStatus` and `EntitlementStatus` types per LLD-08 §3; verify `go build ./...` passes and `go list -deps` shows the package importing nothing from `internal/` outside `shared/domain`
- [ ] 2.2 Implement `VerifyToken(payload, signature []byte, keys KeySet) (LicenseToken, error)` so that signature verification structurally precedes parsing (design D7); verify unit tests generate an ephemeral Ed25519 keypair in-process — no committed key material (D-39) — and cover valid, altered, untrusted-key, malformed-signature, empty and truncated payloads (spec `license-token`)
- [ ] 2.3 Assert in test that no exported function in `domain` parses a payload without verifying it — enumerate the exported surface, so adding an unverified parser later fails this test (design D7)
- [ ] 2.4 Implement `GraceState(now, lastConfirmedAt time.Time) GraceStatus` as a pure function: Valid → EntitlementUnverified (warnings from day 2) → Degraded after 7 days; verify table-driven tests cover the boundaries at day 1, day 2, day 6, day 7 and day 30 with an injected clock (spec `entitlement-grace`)
- [ ] 2.5 Assert `GraceState` depends only on elapsed time — same inputs give same output, failure count and process uptime are not parameters (spec `entitlement-grace`, "Repeated failures do not accelerate degradation")
- [ ] 2.6 Define `FreeChannels = 4`, `FreeMaxExtensions = 10` and `FreeMaxTenants = 1` as named domain constants with the BR-LIC-01 / D-49 reasoning in their doc comments; verify tests assert the free entitlement and the post-grace floor both resolve to 4 channels and that the published entitlement carries all three caps (design D5a, D6)
- [ ] 2.6a Model **Setup** (no entitlement on record) as a state distinct from Degraded, with its own refusal reason; verify a test asserts Setup permits **no** calls, Degraded permits 4, and neither can be produced from the other (design D5a, LLD-08 DoD 11, spec `capacity-enforcement`)
- [ ] 2.7 Verify that reducing an entitlement never removes or disables an existing extension or tenant — the entitlement is published data and constrains only new call capacity (AC-06.13, spec `capacity-enforcement`)
- [ ] 2.8 Implement the concurrent-channel counter with the 10% burst allowance and its bounded window; verify a `-race` test at 10× expected setup rate shows zero drift and a return to zero (AC-06.10, spec `capacity-enforcement`)
- [ ] 2.9 Verify the counter's refusal reasons are distinct constants — over-capacity, not-activated, expired and tampered are each separable in telemetry (spec `capacity-enforcement`, AC-06.6)
- [ ] 2.10 Implement the trusted `KeySet`: the production key as a package constant, plus a development key from an `-ldflags` variable that is **empty by default**; verify a test builds with no ldflags and asserts exactly one trusted key, and that a development-signed token is rejected (D-54, spec `license-token`, risk R-14)

## 3. Persistence and the provider port

- [ ] 3.1 Write `api/migrations/0005_licensing.{up,down}.sql` creating `licensing_state` per HLD 03 §5 — including `signed_payload`, `signature`, `max_tenants` and `max_extensions` — with **no `tenant_id` and no RLS**, granted to `atsapbx_app` only, never `asterisk_engine`; verify `mise run migrate` up and down both succeed cleanly
- [ ] 3.2 Add an integration test asserting `licensing_state` has no `tenant_id` column and no row-level security enabled, deliberately, so the exception cannot be "corrected" later; verify it fails if either is added (LLD-08 DoD 6, spec `capacity-enforcement`)
- [ ] 3.3 Create `internal/licensing/ports` with the four-method `LicenseManager` and licensing's own `CapacityVerdict`; verify it does not import `telephony/ports` (design D2, enforced by task 1.1)
- [ ] 3.4 Implement `internal/licensing/postgres` reading and writing `licensing_state` with parameterized queries only, treating `signed_payload`/`signature` as the entitlement of record and re-verifying on load; verify integration tests cover a present row, a missing row, and an expired licence (D-53)
- [ ] 3.4a Prove the claim columns are a cache and nothing more: alter `capacity`, `max_tenants` and `edition` directly in the database and verify **no** entitlement decision changes; then break the signature and verify the installation degrades to 4 channels reporting tampering rather than honouring the row (D-53, AC-06.16, LLD-08 DoD 10)
- [ ] 3.5 Implement `internal/licensing/application` wiring counter, grace tracker and store into the four port methods; verify a missing row resolves to **Setup with no call path** and the not-activated reason code — not to a capacity floor (design D5a, spec `capacity-enforcement`)
- [ ] 3.5a Implement `ApplyLicenseKey` as a domain operation plus `ATSAPBX_LICENSE_TOKEN` intake at startup; verify a token supplied by configuration activates the installation, takes effect with no restart and without interrupting a call in progress, and that an invalid token leaves the entitlement in force untouched (spec `license-token`, re-slice)
- [ ] 3.5b Make `ApplyLicenseKey` idempotent: applying the identical token again succeeds, changes no entitlement, interrupts no call, and does **not** refresh `last_confirmed_at` — re-applying a token must not restart the grace clock, since startup intake runs on every boot (spec `license-token`, D-58)
- [ ] 3.6 Implement `VerifyDailyEntitlement` as a scheduled operation that never blocks call setup; verify a test asserts an unreachable and a hanging entitlement service both leave call setup unaffected (spec `entitlement-grace`, AC-06.3)
- [ ] 3.7 Verify no configuration value, environment variable or build option can skip verification — enumerate the config surface and assert every combination still verifies (spec `license-token`, D-54)

## 4. Cutover — the stub goes and enforcement becomes real

- [ ] 4.1 Add `ReleaseCapacity(ctx context.Context, callID shareddomain.CallID) error` to `telephony/ports.LicenseManager` — keyed by call, not by a channel count, so the operation can be idempotent (D-58); verify `go build ./...` fails until every implementation is updated, then passes (design D1, D3)
- [ ] 4.2 Give `ReleaseCapacity` a call identifier so a repeat release for the same call is a no-op rather than a decrement, then call it in `finalizeTermination` for calls that left Screening permitted; verify unit tests cover normal hangup, setup failure, screened-out (no release) and a direct double release (count unchanged, no error) (spec `telephony-core/call-lifecycle`, design D3, D-58)
- [ ] 4.3 Write the composition-root adapter in `cmd/atsap-api` translating `licensing/ports.CapacityVerdict` to `telephony/ports.CapacityVerdict`; verify it is the only file importing both contexts (design D2)
- [ ] 4.4 Delete `internal/telephony/application/stub_license.go` and wire the real adapter; verify `go build ./...` and the full unit suite pass with no stub remaining (LLD-08 DoD 7)
- [ ] 4.5 Verify `git diff --stat api/internal/telephony/` lists only `ports/ports.go` and `application/service.go` — any third file means scope crept into another context (LLD-08 DoD 7, design D1)
- [ ] 4.6 Amend `docs/lld/LLD-08-licensing.md` §1 and DoD 7 to record that the tripwire fired, why `ReleaseCapacity` was missing, and the narrowed rule; verify `mise run docs` passes

## 5. Integration — against a real database

- [ ] 5.1 Verify capacity is installation-scoped, not tenant-scoped: two tenants drawing on one entitlement of 2 leaves the third setup refused regardless of which tenant asks (spec `capacity-enforcement`)
- [ ] 5.2 Verify the burst allowance absorbs a short overage and refuses a sustained one, with the over-capacity reason code distinguishable from the compliance refusal already in the Screening path
- [ ] 5.3 Verify a restart while calls are up resets the in-use count and self-corrects as those calls end, and record the observed behaviour — this is design D4's stated consequence, not a defect to fix here
- [ ] 5.4 Verify the grace state machine end to end with an injected clock: valid → unverified with day-2 warnings → degraded at day 7 → valid again on confirmation, with no restart (spec `entitlement-grace`)
- [ ] 5.5 Verify no licence key, signature, or fingerprint value appears in any log line or error returned to a caller, across accept and reject paths (D-39, spec `license-token`)

## 6. End to end — the invariants, against live Asterisk

- [ ] 6.0 Supply the committed development token to the dev stack and both e2e suites through `ATSAPBX_LICENSE_TOKEN`, signed by the development key; verify the suites activate through the real path rather than bypassing it, and that the same token is rejected by a binary built with no ldflags (D-52 cost, D-54)
- [ ] 6.1 Verify a real call is placed and completed with capacity enforced, and the counter returns to zero afterwards — the walking skeleton still passes with the stub gone
- [ ] 6.2 Verify **no active call is dropped** across every licence transition — valid → over-capacity → unverified → degraded — with a live call held throughout and media still flowing at the end (INV-03, LLD-08 DoD 4)
- [ ] 6.3 Verify an emergency destination connects with capacity exhausted, while degraded, with an expired licence, and with no licence at all (INV-01, AC-06.7, spec `capacity-enforcement`)
- [ ] 6.4 Verify a capacity increase takes effect without a restart and without interrupting a call in progress (AC-06.8, spec `capacity-enforcement`)
- [ ] 6.5 Run the full gate in order — `mise run docs`, `mise run lint`, `mise run test` at all three tiers, `mise run diagrams` — and record each result; then write `coverage.md` mapping every scenario in the four delta specs to the test that proves it, naming any gap in the G-series convention rather than leaving it unmentioned
