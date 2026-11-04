# Design — outbound fraud guard

## D1. Configuration is a tenant policy

`tenant_outbound_policies` holds caps, blocked prefixes, anomaly settings,
business hours and the suspension state. A tenant with no row gets the
safe default: no spend cap, no blocked prefixes, and anomaly detection at
20 international calls in 10 minutes outside 08:00–18:00 UTC. Zero means
disabled for caps and threshold.

## D2. Spend is rated, not guessed

`carrier_routes.cost_per_minute` already stores the partner's cost. When
an answered trunk call ends, the usage observer computes
`seconds × cost_per_minute / 60` micros and adds it to
`tenant_spend_ledger` for the UTC day. Caps compare the day and month
totals before a new call. Calls in progress are never interrupted; the
cap applies to the next placement.

## D3. Anomaly detection is concurrent-safe

Every trunk-bound, non-emergency attempt runs under a per-tenant advisory
lock. In that transaction the service reads the policy, checks blocked
prefixes and caps, counts international attempts in the window, decides
whether this attempt crosses the threshold, records the attempt, and
suspends in the same row if so. Concurrent calls therefore cannot all see
an under-threshold count. Emergency never enters the guard.

## D4. Suspension is state, not a timer

An anomaly sets `suspended` with a reason and timestamp. New outbound
calls are refused; the administrator resumes explicitly from the console.
Calls in progress continue. This is the one-click reactivation HLD 05 §4.2
asks for.

## D5. Alerts never block the call path

The cap and anomaly alerts are sent asynchronously after the transaction,
with a 20-second bound, to the tenant's active administrators and
`ATSAPBX_CARRIER_ALERT_EMAILS`. A cap alert is sent at most once an hour
per tenant; a suspension alert once per transition.

## D6. Scope

This is the Phase A fraud guard, not the full `compliance` dialer context.
Calling hours, DNC lists and abandonment limits remain LLD-04 work for the
dialer. Emergency provenance remains the dial plan's (INV-01).
