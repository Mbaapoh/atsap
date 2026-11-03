## Context

See `proposal.md` — Why. D-74 built the prober and its single-observation
health. `ObserveProbe` and `ObserveHealth` already map one observation to
a health and reason; the application writes it straight to
`carrier_trunks`. T-6 changes the aggregation, not the probe itself:
checks every 10 s, 3 consecutive failures, a 60 s call window, and a 5 min
recovery. BR-08 adds an operator notification on the failure transition.

The console already renders `health_status`, `health_reason` and
`health_observed_at`; only the reason catalogue needs the new value. The
identity store can list a tenant's principals with their email and role,
and the app already has an SMTP mailer (Mailpit on the rig).

## Goals / Non-Goals

**Goals:**

- Implement HLD T-6 exactly: 10 s checks, 3 consecutive failed checks or
  >20 % call-setup failures over 60 s (min 5) -> Degraded; 5 continuous
  healthy minutes -> Healthy.
- Keep the decision pure and testable in `pbx/domain`; the database stores
  samples and the current state.
- Tell the operator (BR-08) once per failure transition, by email.
- Keep the console honest: the reason says whether pings or calls caused
  the state.

**Non-Goals:**

- Changing the probe protocol or adding a SIP library (D-74).
- Alerting on every transient failure, or on recovery.
- Per-call routing changes: selection already prefers Healthy over
  Degraded and skips Unhealthy.
- Changing the API shape (the trunk already reports health/reason/time).
- Push/webhook alerts (LLD-05 is later); email is the Phase A channel.

## Decisions

### D1. Samples in a table, decision in the domain

Every observation is a row in `trunk_health_samples` (`PROBE` or `CALL`,
healthy, reason, at). A pure `domain.DecideHealth` takes the current
state, the latest observation, the consecutive probe failures, the 60 s
call counts and the recovery start, and returns the next state/reason and
`healthy_since`. The store records the sample and applies the decision in
one transaction with the trunk row locked. This makes the rolling window
exact across restarts and the rules unit-testable without a clock.

**Alternative rejected:** in-memory counters. A restart forgets the
window and the 5 min recovery; two app instances would disagree.

**Alternative rejected:** computing everything in SQL. The thresholds and
precedence are domain rules and need direct unit tests; SQL would duplicate
them and the console could see a state no test explains.

### D2. What is a failed check

- `No response`, `carrier error` (5xx) and a failed name resolution are
  failed checks. Configuration failures (`host not found`,
  `address not allowed`) set Unhealthy **at once**: the platform knows it
  cannot be a carrier, and waiting three checks only delays the truth.
- A slow answer is Degraded at once, as today; it is a real service
  problem but not a transport failure.
- Any other final SIP response is a healthy check, including 403/404 to an
  unauthenticated OPTIONS (D-74).

### D3. Thresholds and recovery

- Checks default to every **10 s** (`ATSAPBX_TRUNK_PROBE_INTERVAL` can
  override; the minimum stays 5 s).
- Three consecutive failed checks, or a call window with at least 5
  attempts and failures/total > 20 %, set Degraded with the triggering
  reason.
- From Degraded/Unhealthy, the first healthy check starts
  `healthy_since`; only 5 continuous minutes later is the trunk Healthy.
  A failed check or a call failure resets it. Recovery is reported as
  Healthy with "answering".
- Unknown keeps its current meaning until the first decision: a healthy
  check makes it Healthy, a failed check leaves it Unknown until the
  third (or a call window triggers).
- A call success counts in the denominator but never degrades on its own;
  only the rate over the window does.

### D4. Alert on a failure transition, by email

A transition from Healthy/Unknown to Degraded or Unhealthy triggers one
alert. The prober and the call-health path detect it from the store's
returned previous/next state and call a `CarrierAlerter` port. The
composition-root adapter lists the tenant's active `TENANT_ADMIN`
principals (paged) and adds `ATSAPBX_CARRIER_ALERT_EMAILS` (installation
operators), then sends a plain-text email through the existing mailer.
Sending is asynchronous with a bounded context; a mail failure is logged,
never allowed to affect the probe or a call. No alert on recovery, and no
repeat while the state stays failed.

**Alternative rejected:** a webhook (LLD-05, not built). **Alternative
rejected:** a console-only banner (BR-08 says the operator is told; an
operator may not be watching). **Alternative rejected:** platform-only
recipients (the tenant's administrator is the person who can fix a
carrier).

### D5. Schema and retention

`trunk_health_samples` has `(tenant_id, trunk_id, source, healthy, reason,
at)` with RLS and a `(tenant_id, trunk_id, at DESC)` index. Each write
deletes samples older than one hour for that trunk: the call window is
60 s and recovery 5 min, so an hour is ample and the table stays bounded.
`carrier_trunks.healthy_since` holds the recovery start. The reason check
constraint gains `CALL_SETUP_FAILURES`.

## Risks / Trade-offs

- **[Risk] More checks (10 s) mean more SIP traffic.** One OPTIONS per
  trunk per 10 s is negligible; the prober already caps concurrency at 8.
- **[Risk] A call-driven Degraded is based on a small sample.** The
  minimum of 5 calls and the 20 % rate are T-6's; a single failed call
  never degrades.
- **[Risk] Mail delivery fails or SMTP is not configured.** The alert is
  logged at WARN and does not affect health or calls; the console still
  shows the state.
- **[Trade-off] Configuration failures bypass the 3-check threshold.** A
  host that cannot be a carrier is not a transient; Unhealthy at once is
  honest.
- **[Trade-off] Recovery takes 5 min even if the carrier was fine after
  one check.** That is T-6's anti-flap rule; Degraded still selects the
  trunk after healthy candidates.

## Migration Plan

Additive migration 0017. Existing trunks start with `healthy_since` null;
their next observation decides. No backfill is needed. Rollback drops the
samples table and the column; the prior health values remain.

## Open Questions

None that change the approach. Alert recipients can be tuned with
`ATSAPBX_CARRIER_ALERT_EMAILS` without a code change.
