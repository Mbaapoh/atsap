# Tasks — licensing-status-conditions

Implements LLD-08 §6.1 and D-70, ahead of `console-foundation` task 2.3. Every task ends with a verification.

## 1. Domain

- [x] 1.1 Add `Condition`, `Tier` and `CapacityLevel` to `licensing/domain`, and the pure function `Condition(e Entitlement, inUse int)` (design D1–D3, D5). Verify table-driven tests over every entitlement state × tier × usage, including 79%/80% and 99%/100% boundaries, usage inside the burst allowance, Degraded's 4-channel floor, Setup (no capacity, no tier), and a 4-channel commercial licence staying Commercial *(done — `domain/condition.go`: `Conditions(e, inUse)` returns condition, tier and capacity level; integer thresholds; `FreeEdition` now names the constant `FreeEntitlement` uses. Table tests cover the boundaries, burst, Degraded's floor, Setup, and every state × tier × usage.)*

## 2. Application and wire

- [x] 2.1 Add the three values to `ports.LicenseStatus` and compute them in `Service.Status` from the entitlement and live usage. Verify unit tests: Free Community at low usage; Warning and Reached as reservations are taken through the real counter; Unverified at capacity reports Reached with state Unverified *(done — `ports.LicenseStatus.Conditions`, computed in `Service.Status` from live usage. Unit tests through the real counter: Free Community, Valid→Warning→Reached at 3/4/5 of 5, Unverified at capacity reports Reached with state Unverified, Setup. **Guard fired, handled:** the bypass guard flagged `ConditionUnverified`; it and its wire enum are exempted by exact name like `StateUnverified`, and a probe `AllowUnverified` is still caught.)*
- [x] 2.2 Add `LicenseCondition`, `LicenseTier` and `CapacityLevel` to `licensing.proto` (design D4), regenerate the Go stubs with the pinned plugins and the console's TypeScript (`make console-gen`), and map them in `licensing/rpc`. Verify `buf lint`, `buf breaking` against develop clean, the console typecheck passing, and handler tests asserting each field and that no key material renders *(done — `LicenseCondition`, `LicenseTier`, `CapacityLevel` as fields 10–12; Go stubs regenerated with the pinned plugins and the console's TypeScript with `npm run gen`; handler mapping. `buf lint` clean; `buf breaking` against develop clean; console typecheck and tests green; wire test maps every value and renders no licence material.)*

## 3. Docs and gates

- [x] 3.1 Update `docs/API.md`'s `GetLicenseStatus` row, HLD-19 §6, and `docs/CONSOLE-DESIGN.md`'s licence section (the condition drives the pill; Suspended is a tenant status). Verify `make docs` passes *(done — `docs/API.md`'s `GetLicenseStatus` row, HLD-19 §6, and `docs/CONSOLE-DESIGN.md`'s licence section: the pill shows the server's condition; tier, capacity and state alongside; Suspended is a tenant status. `make docs` passes.)*
- [x] 3.2 Run `make lint`, build and vet under every tag, unit with `-race`, integration with `-race -p 1`, and `make console-test`. Verify all green and no change outside `internal/licensing`, the protos, generated code and docs *(done — `make lint` 0 issues; build and vet under every tag; unit `-race` and integration `-race -p 1` green; console typecheck and tests green. No change outside licensing, the protos, generated code, the e2e and docs.)*

## 4. End to end

- [x] 4.1 Extend the console-APIs e2e with a dev-key-signed `free` licence (condition and tier Free Community) and a 5-channel licence while holding ringing calls: 3 calls Normal, 4 Warning, 5 Reached. Restore the original licence in cleanup. Verify each assertion; every existing e2e suite still green *(done — `TestConsoleAPIs_LicenceConditions`: a dev-signed `free` licence reads Free Community (state still Valid); a 5-channel licence with ringing calls held reads Valid/Normal at 3, Capacity Warning at 4, Capacity Reached at 5, tier Commercial. The original licence is restored in cleanup (the 16-channel licence test passes after it). Every e2e suite (pbx, telephony, identity) green.)*
- [x] 4.2 Write `coverage.md` mapping each scenario in the delta spec to its test, with any gap in the G-series convention *(done — `coverage.md` maps all six scenarios; gaps G1 (Degraded/Unverified unit-level here), G2 (no push), G3 (Suspended is a tenant status).)*
