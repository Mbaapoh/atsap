# Outbound fraud guard: spend caps, blocked prefixes and anomaly suspension

## Why

Outbound classes (D-82) confine an extension, but a tenant-wide
compromise or a fraud ring can still run many international calls. PRD
AC-08.1/08.2 require a spend cap that blocks new calls and an anomaly
rule that suspends outbound within five minutes; calls in progress and
emergency calls must never be cut off.

## What Changes

- A tenant outbound policy: daily/monthly spend caps, blocked prefixes,
  anomaly threshold and window, and business hours (UTC).
- Every trunk-bound attempt is recorded under a per-tenant lock; the
  anomaly rule suspends outbound when too many international attempts
  happen outside business hours, and emails the administrators.
- Answered calls are rated from the route's stored cost per minute and
  accrued in a daily ledger; reaching either cap blocks new calls and
  alerts, without touching calls in progress.
- Emergency calls bypass every one of these controls.
- Administrators see the state and configuration in **Trunks & routes →
  Outbound protection**, and can resume a suspension.

## Impact

- Migration 0021: policy, spend ledger and attempt log.
- PbxService RPCs `GetOutboundPolicy`, `UpdateOutboundPolicy`,
  `ResumeOutbound`; console section.
- Placement enforces the guard; a usage observer rates answered calls.
- Email alerts reuse the operator/administrator recipient logic.
