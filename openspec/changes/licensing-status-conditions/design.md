## Context

The design of record is **LLD-08 §6.1** (written with this change) and **D-70**. Today:
- `licensing/domain.Entitlement` carries a `State`: Setup, Valid, Unverified or Degraded.
- `FreeEntitlement()` builds the Free Community tier: edition `free`, 4 channels, 10 extensions, 1 tenant, and state Valid.
- The `Counter` reports channels in use and admits a 10% burst for up to 15 minutes.
- `Service.Status` returns entitlement, usage and dates; `licensing/rpc` maps them to `LicenseStatus` (`state`, `edition`, `channels`, `channels_in_use`, caps, dates, `reason`).

Nothing reports the tier or the capacity level, so a client would have to infer both. D-70 forbids that.

## Goals / Non-Goals

**Goals:**
- One server-side rule that turns entitlement and usage into the PRD §11.2 condition, the tier and the capacity level.
- Every combination and boundary unit-tested, in one place.
- Additive wire change; no change to how calls are admitted.

**Non-Goals:**
- Changing the counter, the burst allowance, grace evaluation or enforcement at call setup.
- A Suspended licence state: suspension is a tenant status (identity). A suspended tenant's users cannot sign in, so the console never shows it to them; an operator sees it on the tenant later (operator console, not Phase A).
- Pushing condition changes in real time. The console reads status on load and refreshes it; a live feed is a later decision.
- The console's licence screen itself (`console-foundation` task 2.3).

## Decisions

**D1 — Three facts and one headline, not one flattened state.**
PRD §11.2 lists the states flat, but they overlap: a Free Community installation can be at Capacity Warning, and an Unverified one at Capacity Reached. The status therefore reports:
- `state`: the entitlement state, unchanged;
- `tier`: `FREE_COMMUNITY` when the edition is `free`, otherwise `COMMERCIAL`; unspecified in Setup;
- `capacity`: `NORMAL`, `WARNING` or `REACHED`; unspecified in Setup;
- `condition`: the one headline the console shows.

*Rejected:* replacing `state` with a single eight-value enum. It loses facts (Unverified at capacity becomes one or the other) and breaks the wire.

**D2 — The headline precedence is the server's, most urgent first.**
Setup › Degraded › Capacity Reached › Entitlement Unverified › Capacity Warning › Free Community › Valid.
- Degraded comes before Reached because it explains why capacity is low and how to restore it.
- Reached comes before Unverified because calls are being declined now, whereas Unverified is a deadline.
- Free Community only shows when nothing needs attention.

**D3 — The rule is a pure function in `licensing/domain`.**
`Condition(e Entitlement, inUse int) Status` returns condition, tier and capacity. It has no clock and no I/O, and every combination of state × tier × usage is table-tested, including the exact thresholds. It uses integer arithmetic so no float edge exists:
- Warning when `inUse*100 >= 80*channels`;
- Reached when `inUse >= channels`.

Degraded uses its floor (4 channels). Setup (0 channels) has no capacity level. Usage above `channels`, inside the burst allowance, is Reached.

**D4 — Wire: three new enums and fields on `LicenseStatus`, numbered after the existing ones.**
`LicenseCondition condition = 10`, `LicenseTier tier = 11`, `CapacityLevel capacity = 12`. The `reason` comment gains "set for Degraded and Setup", unchanged in behaviour. Both `GetLicenseStatus` and `ApplyLicenseKey` return them, since both return `LicenseStatus`. The console's generated TypeScript is regenerated in the same change (`npm run gen`), so the console sees the new fields without a hand-written type.

**D5 — The edition `free` identifies the tier.**
`FreeEntitlement()` and the licence issuer already use edition `free` for Free Community (BR-LIC-01). The tier is derived from that one constant in `licensing/domain`, not from channel counts. A commercial 4-channel licence is still Commercial.

## Risks / Trade-offs

- **[The headline changes as calls start and end]** Expected. Capacity is live usage. The console refreshes status, and the full facts stay available so a pill flickering between Warning and Normal can be explained.
- **[Precedence is a product choice]** It is written once (D2), here and in LLD-08 §6.1, and tested. Changing it is a one-line, recorded decision.
- **[Burst counted as Reached]** An installation absorbing a busy hour shows Reached. That is accurate: it is beyond its entitlement, and calls past the allowance are being declined.

## Migration Plan

No schema change. Additive proto fields; old clients ignore them. Rollback is a code revert.
