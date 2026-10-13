## 1. Calls dialled from a phone are recorded and presented truthfully

- [x] 1.1 Telephony records the router's decision.
  - `InboundDecision` gains `CallerNumber` and `CallerName`.
  - `CallArrived` applies a non-empty `CallerNumber` as the call's source.
  - `SaveCall`'s upsert also updates `direction` and `source_number`.

  Verify:
  - unit: an arriving call resolved INTERNAL with caller `1001` is saved INTERNAL with source `1001`, and an empty caller keeps the channel's number;
  - integration: an upserted call's direction and source change in the row;
  - `make test`, `make lint`.

  *(done 2026-10-13:
  - `InboundDecision` gains `CallerNumber` and `CallerName`;
  - `CallArrived` applies a non-empty caller number as the source;
  - the upsert also sets `direction` and `source_number`.

  Verified:
  - `TestCallArrived_RecordsTheRoutersDirectionAndCaller` (desk phone → INTERNAL from `1001`; a carrier keeps its number);
  - `TestCallStore_SaveCall_UpdatesRoutedDirectionAndSource` failed on the old upsert, showing the row kept `e_…`, and passes now, in both `GetCall` and the listing;
  - the Go unit, integration, e2e and lint tiers pass, and `make console-check` passes (387 / 24 / 24 / 3, audit 0, secrets clean).

  **Found while testing:**
  - The integration tier defaults to the rig's live `atsapbx` and migrates it down. It now runs against `atsapbx_test` (`DATABASE_URL`, `ENGINE_DATABASE_URL`), after the outbox tests were fixed to follow it (`feea947`).
  - Console branch coverage varied by one branch between runs, because of a race between two queries. A deterministic test fixes it (`6f2c3fd`).
  - `make test` needs `mise exec`: plain `make` finds a Go without `covdata`.)*
- [ ] 1.2 The inbound router attributes a desk-phone call to the dialling extension: its number and display name. A trunk call leaves both empty.

  Verify:
  - unit: a desk phone dialling another extension, and a route, each carry the dialling extension's number and name;
  - a trunk call carries neither.
- [ ] 1.3 The destination leg of an arriving call is originated with the caller ID `"Name" <number>`, or just the number when there is no name. The name is sanitized first: `"`, `<`, `>`, `\` and control characters are removed.

  Verify:
  - unit: the media gateway receives the expected caller ID;
  - a hostile display name cannot change the presented number;
  - API-placed calls are unchanged.
- [ ] 1.4 Real-phone UAT. `scripts/uat-inbound.sh` asserts:
  - the inbound call is INBOUND with the carrier's number as source;
  - the desk-phone call is OUTBOUND with the extension's number as source;
  - a new phone-to-phone step: a second extension's phone, on the carrier's port once the carrier is done, receives a call from the first. The call is recorded INTERNAL with source = the first extension's number, and the ringing phone logs the caller's name and number.

  Verify: `make uat-inbound` and `make uat-auto` pass on the rig.

## 2. No engine endpoint in `GetCall`

- [ ] 2.1 Stop filling `Participant.endpoint_uri`; mark it `deprecated = true` with a comment saying it is never filled.

  Verify:
  - `TestGetCall_NoChannelIDLeakage` also refuses `PJSIP/` and `e_…` forms;
  - `make proto` passes (`buf breaking`);
  - the console's regenerated clients typecheck, and `make console-check` passes.

## 3. No provisioning before a licence is applied (D-72)

- [ ] 3.1 Add `ErrNotLicensed` in pbx-core's and identity's ports, mapped to `FAILED_PRECONDITION` "no licence is applied".
  - The composition root's licence adapter returns it from `MaxExtensions`/`MaxTenants` while the entitlement is Setup.
  - A degraded installation keeps its caps.
  - `bootstrap` keeps `noTenantCap`.
  - Record D-72 in `docs/DECISIONS.md`.
  - The console's extension-cap refusal copy also covers "no licence applied".

  Verify:
  - unit for the adapter (Setup refuses; Valid and Degraded pass their caps);
  - unit for both RPC mappings;
  - integration: nothing is stored or projected on refusal;
  - the bootstrap path still creates the first tenant;
  - `make test`, `make lint`, `make console-check`.

## 4. A taken-over Stasis application is noticed (D-73)

- [ ] 4.1 The ARI client tracks its stream: connected, and replaced on an `ApplicationReplaced` event for its application. On replacement it logs ERROR, closes the connection and stops without reconnecting.

  Verify:
  - unit: event handling and the stream state;
  - integration against the rig's Asterisk with a throwaway application name: a second registration makes the first stream stop with the replaced error.
- [ ] 4.2 Add a `call-events` readiness checker, failing while the stream is disconnected or replaced. Record D-73 in `docs/DECISIONS.md`.

  Verify:
  - unit: the checker in each state;
  - e2e on the rig: `/readyz` lists `call-events` as passing;
  - Tilt shows the app ready after a rebuild.

## 5. No false error after a hang-up

- [ ] 5.1 `ParticipantLeft` for a call no longer in memory logs at DEBUG and returns nil.

  Verify:
  - unit;
  - on the rig, after a real call, no "participant-left handler failed" line in the app log.

## 6. Close-out

- [ ] 6.1 Update `docs/PROGRESS.md`:
  - Phase A step 4 done;
  - the known-gaps table: GetCall endpoint, pre-activation caps and the hang-up noise closed; the outbound caller ID towards carriers added as a new gap.

  Then:
  - run every tier: Go unit, integration and e2e, `make coverage-all`, the console tiers, both real-phone UATs, `make docs`, `make proto`;
  - ratchet the coverage floors.

  Verify: everything passes, and the measured coverage is recorded.
