# Coverage — pbx-trunk-health-thresholds

**Tiers:**
- **U**: `pbx/domain` pure decisions and the alert adapter.
- **I**: `pbx/postgres` samples/aggregates, `pbx/application` prober and call-health window.
- **E**: `pbx/e2e` on the rig, including Mailpit for the BR-08 alert.
- **A**: the three phone UATs as regression (the call path is unchanged).

## pbx-core/trunk-routing — trunk health

| Scenario | Proven by |
|---|---|
| A new trunk is not yet checked | **U**; **I** `TestTrunkProber_ACreatedTrunkIsCheckedAtOnce`; **E** `TestTrunkHealth_IsWhatTheCarrierSays` |
| A carrier that answers is healthy | **U** `TestDecideHealth_HealthyProbe`; **I/E** the same two |
| A slow carrier is degraded at once | **U** `TestDecideHealth_ConfigurationAndSlow`; **I** selection/call-window tests |
| Three failed checks degrade | **U** `TestDecideHealth_ConsecutiveFailures`; **I** `TestTrunkProber_ThresholdsRecoveryAndAlert`; **E** the TEST-NET trunk |
| Two failed checks do not degrade | **U** `TestDecideHealth_ConsecutiveFailures`; **I** the prober test |
| A failed call window degrades | **U** `TestDecideHealth_CallWindow`; **I** `TestReportRouteHealth_CallWindowDegradesAndAlertsOnce`, `TestPlaceCall_CallSetupWindowDegradesAndSuccessDoesNotRestore` |
| A small or healthy call window changes nothing | **U** `TestDecideHealth_CallWindow`; **I** the call-window test |
| A host that does not resolve is unhealthy at once | **U** `TestDecideHealth_ConfigurationAndSlow`; **I/E** `TestTrunkProber_*`, `TestTrunkHealth_IsWhatTheCarrierSays` |
| A name resolving only to non-carrier addresses | **U** (reason maps to Unhealthy); **I** `TestTrunkProber_AddressNotAllowedKeepsOnlyTheRanges` |
| A missed check does not degrade at once | **U** the three-check test; **I** the prober threshold test |
| Five healthy minutes recover | **U** `TestDecideHealth_Recovery`; **I** `TestTrunkProber_ThresholdsRecoveryAndAlert` (clock) |
| A failure during recovery restarts the window | **U** `TestDecideHealth_Recovery`; **I** the prober test resets on failure |
| Changing the host is checked at once | **I** `TestTrunkProber_ANewAddressIsCheckedAgain`; **E** the typo-fix step |
| Health cannot be set by configuration | **I** `TestRoutePlanner_SelectionAndObservedHealth` |
| Reported failure degrades future selection only | **I** the selection tests; **E** the existing tiers |

## pbx-core/trunk-routing — BR-08 alert

| Scenario | Proven by |
|---|---|
| A carrier failure alerts its tenant administrators | **U** `TestCarrierAlertRecipientsAndBody`; **I** `TestTrunkProber_ThresholdsRecoveryAndAlert` and `TestReportRouteHealth_CallWindowDegradesAndAlertsOnce`; **E** `TestCarrierAlert_FailureReachesTheOperatorOnce` (Mailpit) |
| A configuration failure alerts at once | **E** the same test (host not found) |
| No repeated alert while failed | **I/E** both tests wait and count |
| Recovery is silent | **I** the prober test counts after recovery |
| A mail failure changes nothing | **U** `TestCarrierAlertMailFailureIsReturned`; the application logs and ignores |

**Executed 2026-11-03:** unit/race in the Go container; full `pbx/application` and `pbx/postgres` integration on `atsapbx_test`; full Go e2e (trunk health, the Mailpit alert, and every other suite); `make console-check`; all three phone UATs; `make docs`; SCA. Unit coverage 79.3% >= 79.1% and the console floors were left at their measured values.

**Found and fixed on the way:**
- Migration 0017's down script restored the old reason constraint while
  rows still held `CALL_SETUP_FAILURES`, and RLS hid those rows from the
  update. The down now lifts RLS for that update (as 0014 does) and maps
  the reason to `NO_RESPONSE` first, so the migration is reversible and
  the integration tier can reset the scratch database.

## Gaps

| # | Gap | Severity | Note |
|---|---|---|---|
| G1 | The BR-08 alert is email-only | Accepted | Webhooks are LLD-05; the console still shows health |
| G2 | A Degraded trunk is still selected when no healthy trunk can take the call | Accepted | T-6 says Degraded, not skipped; this is the honest failover rule |
| G3 | Recovery is proven with a test clock, not by waiting five minutes in e2e | Accepted | Waiting would make the e2e tier five minutes slower per case; the clock drives the same code |
| G4 | The installation operator must set `ATSAPBX_CARRIER_ALERT_EMAILS`; otherwise only tenant administrators are reached | Accepted | Configuration is documented; the tenant administrator is the primary recipient |
