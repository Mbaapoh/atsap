## Why

Trying the finished Phase A console on the dev rig (2026-10-13) showed that the platform under it still has gaps that a pilot would hit:

- **Call history records phone-to-phone calls wrongly.** A call from one extension to another is recorded as INBOUND rather than INTERNAL. Its source is the caller's engine endpoint id (`e_…`), not `1001`, so the console can only show "Not shown". The phone being called sees the caller as anonymous.
- **Call events can be lost silently.** A second app process registered the same Stasis application, Asterisk handed the call events to it, and the running app kept reporting itself ready. Calls then stuck at Presenting, with no log and no failed readiness check, until a restart.
- **`GetCall` still returns engine endpoint names** (`PJSIP/e_…`) in its participants.
- **Provisioning is unbounded before a licence is applied.** Unlimited is spelled 0, and an installation that was never activated also reads 0.
- **Every hang-up logs a false ERROR** ("participant-left handler failed: call not found").

These are the items `docs/PROGRESS.md` lists as Phase A step 4. Each is small, and together they finish Phase A.

## What Changes

- **Phone-to-phone calls:**
  - A call that arrives from a registered phone is recorded with the direction the router decided: INTERNAL to another extension, OUTBOUND through a trunk.
  - Its source number is the dialling extension's number, never the engine endpoint.
  - The phone being called is presented with a caller ID: the extension's number and display name for a call from an extension, or the carrier's caller number for a carrier call.
- **Call-event takeover:**
  - Readiness gains a call-events check. `/readyz` fails while the app's event stream from the engine is down, or after another process has taken over the app's Stasis application.
  - A takeover is logged at ERROR. The app does not take the application back: two instances fighting over it would be worse than one instance that says it is not ready.
- **`GetCall`:** participants no longer carry an engine endpoint. `Participant.endpoint_uri` stays in the proto, deprecated and never filled, because removing it would fail the breaking-change gate.
- **Licence gate:** an installation with no licence applied refuses to create an extension or provision a tenant, with `FAILED_PRECONDITION` and a reason saying no licence is applied. Bootstrapping the first tenant from the command line is unchanged, because it must happen before a licence can be applied.
- **Hang-up noise:** a call leg that hangs up after its call has already ended is expected and no longer logged as an error.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `telephony-core/call-lifecycle`:
  - an arriving call records the router's direction and caller number, and presents a caller ID to the destination;
  - no engine endpoint appears in `GetCall`;
  - readiness reflects whether call events are being received.
- `pbx-core/inbound-routing`: a call dialled on a desk phone is attributed to the dialling extension.
- `pbx-core/extension-management`: an installation with no licence applied refuses extension creation.
- `identity/tenant-provisioning`: an installation with no licence applied refuses tenant provisioning, except the command-line bootstrap.

## Impact

- **API (Go):**
  - `internal/telephony/application` (CallArrived, ParticipantLeft, origination caller ID);
  - `internal/telephony/postgres` (the call upsert);
  - `internal/telephony/rpc` (GetCall);
  - `internal/telephony/ports` (InboundDecision);
  - `internal/telephony/acl/ari` (the ApplicationReplaced event and stream state);
  - `internal/pbx/application` (the inbound router);
  - `internal/pbx` and `internal/identity` (a not-licensed refusal);
  - `cmd/atsap-api` (the licence adapter and the readiness checker).
- **Proto:** `Participant.endpoint_uri` is marked deprecated. This is wire-compatible and passes `buf breaking`.
- **Console:** no new screen. Call history shows real source numbers and directions for phone-dialled calls. The extension-cap refusal copy also covers "no licence applied".
- **Tests:**
  - unit tests for each change;
  - integration tests (the call store, and the ARI stream against the rig's Asterisk with a throwaway application name);
  - e2e tests on the rig;
  - UAT with real baresip phones, extended to a phone dialling another extension.
- **Decisions:** D-72 (no provisioning before a licence is applied) and D-73 (a taken-over Stasis application fails readiness and is not taken back).
- **Dependencies:** none.
