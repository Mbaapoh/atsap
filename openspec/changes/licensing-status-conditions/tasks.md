# Tasks — licensing-status-conditions

Implements LLD-08 §6.1 and D-70, ahead of `console-foundation` task 2.3. Every task ends with a verification.

## 1. Domain

- [ ] 1.1 Add `Condition`, `Tier` and `CapacityLevel` to `licensing/domain`, and the pure function `Condition(e Entitlement, inUse int)` (design D1–D3, D5). Verify table-driven tests over every entitlement state × tier × usage, including 79%/80% and 99%/100% boundaries, usage inside the burst allowance, Degraded's 4-channel floor, Setup (no capacity, no tier), and a 4-channel commercial licence staying Commercial

## 2. Application and wire

- [ ] 2.1 Add the three values to `ports.LicenseStatus` and compute them in `Service.Status` from the entitlement and live usage. Verify unit tests: Free Community at low usage; Warning and Reached as reservations are taken through the real counter; Unverified at capacity reports Reached with state Unverified
- [ ] 2.2 Add `LicenseCondition`, `LicenseTier` and `CapacityLevel` to `licensing.proto` (design D4), regenerate the Go stubs with the pinned plugins and the console's TypeScript (`make console-gen`), and map them in `licensing/rpc`. Verify `buf lint`, `buf breaking` against develop clean, the console typecheck passing, and handler tests asserting each field and that no key material renders

## 3. Docs and gates

- [ ] 3.1 Update `docs/API.md`'s `GetLicenseStatus` row, HLD-19 §6, and `docs/CONSOLE-DESIGN.md`'s licence section (the condition drives the pill; Suspended is a tenant status). Verify `make docs` passes
- [ ] 3.2 Run `make lint`, build and vet under every tag, unit with `-race`, integration with `-race -p 1`, and `make console-test`. Verify all green and no change outside `internal/licensing`, the protos, generated code and docs

## 4. End to end

- [ ] 4.1 Extend the console-APIs e2e with a dev-key-signed `free` licence (condition and tier Free Community) and a 5-channel licence while holding ringing calls: 3 calls Normal, 4 Warning, 5 Reached. Restore the original licence in cleanup. Verify each assertion; every existing e2e suite still green
- [ ] 4.2 Write `coverage.md` mapping each scenario in the delta spec to its test, with any gap in the G-series convention
