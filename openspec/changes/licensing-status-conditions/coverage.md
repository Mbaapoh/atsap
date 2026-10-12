# Coverage — licensing-status-conditions

Every scenario in the delta spec, mapped to what proves it. **Tiers:** U = unit (`licensing/domain` pure rule; `licensing/application` through the real counter), W = wire (`licensing/rpc` handler), E = e2e (`internal/pbx/e2e/console_apis_test.go`, running app, real ringing calls holding channels).

## licensing/license-api

### Licence status reports the PRD condition, decided by the server

| Scenario | Proven by |
|---|---|
| A Free Community installation says so | **E** `TestConsoleAPIs_LicenceConditions/a Free Community licence says so` (dev-signed `free` token: condition and tier Free Community, state still Valid). **U** `TestStatus_FreeCommunityAtLowUsage`, `TestConditions_TierFromEditionNotChannels` (a 4-channel commercial licence stays Commercial) |
| Usage at 80% is a warning | **E** `…/usage crosses 80% then 100%` (5-channel licence: 3 held = Valid/Normal, 4 = Capacity Warning). **U** `TestConditions_CapacityBoundaries` (79/80 and 99/100 of 100; 7/8 of 10; 3/4 of 5), `TestStatus_WarningThenReachedThroughTheRealCounter` |
| Usage at 100% is reached, including the burst allowance | **E** same test (5 held = Capacity Reached). **U** `TestConditions_CapacityBoundaries` (11 of 10, inside the burst allowance, is Reached) |
| The most urgent condition wins and the other facts remain | **U** `TestStatus_UnverifiedAtCapacityReportsReachedAndKeepsTheState` (state Unverified, condition and capacity Reached), `TestConditions_Precedence`, `TestConditions_HeadlineAgreesWithFacts` (every state × tier × usage) |
| Degradation explains itself before capacity | **U** `TestConditions_Precedence/degraded beats reached; capacity still says reached` (measured against the 4-channel floor) |
| Setup has no capacity | **U** `TestConditions_SetupHasNoTierAndNoCapacity`, `TestStatus_SetupConditions`. **W** `TestGetLicenseStatus_ConditionsReachTheWire` (SETUP with unspecified tier and capacity) |

**Wire:** `TestGetLicenseStatus_ConditionsReachTheWire` maps every condition, tier and level, and the rendered status contains no licence material. `buf breaking` against develop is clean (additive fields 10–12). The console's generated TypeScript was regenerated and typechecks.

**Guard:** the verification-bypass guard flagged `ConditionUnverified`, the same legitimate grace state as the already-exempt `StateUnverified`; it and its wire enum are exempted by exact name, and a probe `AllowUnverified` is still caught.

## Gaps

| # | Gap | Severity | Note |
|---|---|---|---|
| G1 | Degraded and Unverified are proven at unit level here, not live: the e2e cannot move the clock 3 or 8 days | Low | Live grace transitions were proven in `licensing-capacity-grace`; the condition rule over them is pure and fully tabled |
| G2 | The condition changes only when status is read; nothing pushes it | Accepted | Design non-goal; the console refreshes status. A live feed is a later decision |
| G3 | Suspended is not reported here; a suspended tenant's users cannot sign in, and no operator view shows tenant status yet | Accepted | D-70: a tenant status, owned by identity; the operator console is not Phase A |
