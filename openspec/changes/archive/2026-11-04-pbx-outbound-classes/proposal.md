# Outbound destination classes

## Why

A compromised extension dialling premium international numbers is the
classic PBX loss. LLD-03 §10.2 makes the first control configuration,
not detection: each extension holds an outbound class, and routing
refuses a destination the class does not cover.

## What Changes

- An extension has an outbound class: INTERNAL, NATIONAL or
  INTERNATIONAL. A new extension is NATIONAL.
- A trunk-bound destination is classified from its dialled form: a
  leading `+` is INTERNATIONAL, otherwise NATIONAL.
- Call placement refuses a trunk destination outside the calling
  extension's class, before anything is set up. Emergency destinations
  are exempt (AC-08.3), and internal calls are always allowed.
- The console shows the class and lets an administrator choose it when
  adding or editing an extension.

## Impact

- New `extensions.outbound_class` column (migration 0019).
- `Extension` proto and `CreateExtension`/`UpdateExtension` gain a field;
  generated Go, TypeScript and OpenAPI follow.
- `pbx/application.PlaceCall` gains the class check.
- No engine projection change: the engine does not enforce call classes.

## Out of scope

- Spend caps and anomaly detection (AC-08.2) belong to `compliance`.
- Prefix allow/deny lists beyond the three classes.
