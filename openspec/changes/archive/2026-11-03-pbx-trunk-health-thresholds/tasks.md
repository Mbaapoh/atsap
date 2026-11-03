## 1. Domain and schema

- [x] 1.1 Add T-6 constants and a pure `DecideHealth` in `pbx/domain`, plus `ReasonCallSetupFailures`. Unit-test every threshold, the call window, recovery, reset-on-failure and configuration failures. Verify `make test` and `make lint`.
- [x] 1.2 Migration 0017 adds `carrier_trunks.healthy_since`, `trunk_health_samples` (RLS, index), and the new reason in the check constraint, with a down migration. Verify up/down on `atsapbx_test`.
- [x] 1.3 Extend `TrunkStore` with the sample insert/aggregate/state write in one locked transaction, and update the trunk scan/view for `healthy_since`. Integration tests for 3 failures, a call window, recovery and reset. Verify the integration tier on `atsapbx_test`.

## 2. Prober and call health

- [x] 2.1 The prober records a PROBE sample and applies the decided state; the default interval becomes 10 s. Verify unit/integration tests and the existing prober suite.
- [x] 2.2 `ReportRouteHealth` records a CALL sample and applies the decided state instead of overwriting health from one outcome. Verify the placement/health integration tests.
- [x] 2.3 Configuration failures (host not found, address not allowed) remain Unhealthy at once; slow answers remain Degraded at once. Verify the domain and e2e tests.

## 3. BR-08 alert

- [x] 3.1 Add the `CarrierAlerter` port and the transition plumbing in the prober and `ReportRouteHealth`; disable it when no alerter is wired. Unit-test that only a failure transition triggers it.
- [x] 3.2 Implement the composition-root email adapter: tenant active TENANT_ADMINs plus `ATSAPBX_CARRIER_ALERT_EMAILS`, plain text, asynchronous, logged failures. Add config and tests for recipient selection.
- [x] 3.3 Rig e2e: a dead carrier degrades after 3 checks and one alert appears in Mailpit; no repeat while failed; a recovery sends none. Verify the e2e tier.

## 4. Console, docs and close

- [x] 4.1 Add the "Call setup failures" reason to the console catalogue and test it. Verify `make console-test`.
- [x] 4.2 Update `docs/lld/LLD-03-pbx-core.md` §7.5/§7.6, `docs/CONSOLE-DESIGN.md` if the reason wording changes, and `docs/PROGRESS.md`/`HANDOFF.md` on archive; record D-80.
- [x] 4.3 Update `coverage.md`, ratchet the floors after green, run `make docs` and every tier (unit, race, integration, e2e, console-check, phone UATs), then archive.

  *(done 2026-11-03: `coverage.md` maps every scenario; unit race+cover in the Go container, full pbx integration on `atsapbx_test`, full Go e2e (including the Mailpit alert), `make console-check`, and all three phone UATs pass. Unit coverage 79.3% >= 79.1; floors ratcheted. `make docs` and SCA pass; the host `-race` link remains blocked by the EIO GCC files (documented in HANDOFF §6). Migrations 0017 up/down were exercised by the integration tier.)*
