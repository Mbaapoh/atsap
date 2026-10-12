## Why

The console's licence screen must show the installation's real licence and capacity state: PRD §11.2 names eight. `GetLicenseStatus` reports four: Setup, Valid, Entitlement Unverified and Degraded. Free Community, Capacity Warning and Capacity Reached exist in the PRD but not on the wire. A Free Community installation reads as plain "Valid", and an installation declining calls at full capacity also reads "Valid".

D-70 decided that the server reports every state and the browser never derives one. Thresholds computed in two places drift, and a console that says "Valid" while calls are being refused is the failure PRD AC-10.9 exists to prevent. This change must land before `console-foundation` task 2.3, the licence screen.

## What Changes

- `GetLicenseStatus` (and `ApplyLicenseKey`'s response, which returns the same status) gains three fields, all computed server-side:
  - **`condition`**: the one PRD §11.2 state for the console's status pill, chosen by a fixed precedence. In order: Setup, Degraded, Capacity Reached, Entitlement Unverified, Capacity Warning, Free Community, Valid.
  - **`tier`**: Free Community or Commercial.
  - **`capacity`**: Normal, Warning (at least 80% of the channels in force) or Reached (100% or more, including calls in the burst allowance). None in Setup.
- The rule is one pure function in `licensing/domain`, tested exhaustively.
- **Additive on the wire.** The existing `state` field keeps its meaning, and `buf breaking` stays clean.
- **Suspended is not added.** It is a tenant's status, owned by identity, and a suspended tenant's users cannot sign in. That is recorded, not changed.
- **Not changed:** the capacity counter, the burst allowance, the grace state machine, the enforcement at call setup, and every other context.

## Capabilities

### New Capabilities

_(none)_

### Modified Capabilities

- `licensing/license-api`: licence status reports the PRD condition, the tier and the capacity level, all decided by the server.

## Impact

- **Code:** `internal/licensing/domain` (a pure condition function and its types), `licensing/application` (`Status` adds them), `licensing/ports` (`LicenseStatus` gains three fields), `licensing/rpc` (mapping), `licensing.proto` and regenerated Go and console TypeScript stubs.
- **Unchanged:** identity, pbx-core and telephony-core. Licensing still imports nothing.
- **Docs:** LLD-08 §6.1 (written with this proposal), `docs/API.md` §1's `GetLicenseStatus` row, HLD-19 §6, and `docs/CONSOLE-DESIGN.md`'s licence section (Suspended belongs to the tenant).
- **Tests:** unit (every combination of entitlement state, tier and usage, including each threshold's boundary); wire (the fields render, and no key material); e2e against the running app (a Free Community licence reports Free Community; a 5-channel licence reports Warning with 4 calls held and Reached with 5).
