## Why

Trunk health is checked every 30 s and one observation decides (D-74),
but HLD **T-6** requires a new behaviour: checks every 10 s, `Degraded`
only after **3 consecutive failed checks** or a **call-setup failure rate
above 20 % over a rolling 60 s window (minimum 5 calls)**, and recovery
after **5 continuous minutes** of healthy checks. **BR-08** also requires
the operator to be told when a carrier fails; today health is only visible
in the console. This change implements T-6 exactly as written and adds the
BR-08 email alert.

## What Changes

- Checks run every **10 s** by default (`ATSAPBX_TRUNK_PROBE_INTERVAL` can
  still tune it, but the default changes from 30 s to 10 s).
- A carrier is **Degraded** after 3 consecutive failed OPTIONS checks, or
  when its call-setup failure rate in the last 60 s is above 20 % with at
  least 5 call attempts.
- A carrier returns to **Healthy** only after 5 continuous minutes of
  healthy checks. A failed check or a call-setup failure resets that
  window.
- Configuration failures (host not found, address not allowed) remain
  **Unhealthy** immediately: waiting for three checks would only delay a
  configuration error the platform already knows is not a carrier.
- A new reason, **call setup failures**, explains a health change driven by
  calls, not by pings.
- **BR-08 alert:** on the transition from healthy/unknown to Degraded or
  Unhealthy, the platform emails the affected tenant's active tenant
  administrators, plus any installation-level addresses in
  `ATSAPBX_CARRIER_ALERT_EMAILS`. One alert per transition; recovery sends
  no alert. Mail uses the existing mailer (Mailpit on the rig).
- The observations are kept in a new `trunk_health_samples` table so the
  rolling window is exact across restarts; `carrier_trunks` gains
  `healthy_since` for the recovery window.
- Docs and the console health reason table gain the new reason. D-80
  records the decision.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `pbx-core/trunk-routing`: trunk health thresholds and the recovery
  window change from "one observation decides" to T-6; a new requirement
  covers the operator alert for a failing carrier.

## Impact

- **Migration:** `0017_trunk_health_thresholds` adds
  `carrier_trunks.healthy_since`, `trunk_health_samples`, its RLS policy
  and index, and widens the health reason check.
- **pbx-core:** `domain` gains the pure threshold/decision logic and the
  new reason; `application` records samples and detects transitions;
  `postgres` gains sample/aggregate reads; `ports` gains a `CarrierAlerter`
  and the sample/aggregate store methods.
- **Composition:** a `carrierAlerter` adapter in `cmd/atsap-api` sends mail
  through the existing SMTP configuration to tenant administrators and the
  configured platform addresses.
- **Console:** the health reason table shows "Call setup failures".
- **Tests:** unit threshold/decision tests, integration store/prober tests
  with a fake clock, e2e degradation and alert in Mailpit, and the existing
  tiers as regression.
- **Config:** default check interval 10 s; new
  `ATSAPBX_CARRIER_ALERT_EMAILS` (comma-separated, optional).
- **Dependencies:** none.
