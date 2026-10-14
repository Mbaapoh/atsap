## Context

`console-admin-screens` was walked on the dev rig on 2026-10-13. A call placed from one real baresip phone to another, and an accidental second app process, showed the defects below. Each was traced to its code:

| Defect | Cause |
|---|---|
| Phone-to-phone call recorded INBOUND | `CallArrived` saves the Call before routing with `Direction: Inbound`, then sets `call.Direction = decision.Direction`. But `CallStore.SaveCall`'s `ON CONFLICT` updates only state, answered_at, ended_at and termination_reason, so the routed direction never reaches the row. |
| Source is `e_…`, not `1001` | The source number is the channel's caller number. A baresip phone sends its SIP username, which is the engine endpoint id (`extension-management`: "the authentication identity is not the extension number"). The router knows the dialling extension but cannot say so: `InboundDecision` has no caller field. |
| Called phone sees "anonymous" | `originateParticipant` never sets `OriginateRequest.CallerID`. |
| Silent loss of call events | Asterisk sends `ApplicationReplaced` to the old event connection and keeps it open; this was confirmed against the rig's Asterisk 22.11 with a throwaway application. `EventLoop.Handle` ignores the event. `/readyz` checks only Postgres and NATS, so nothing reports it. |
| `GetCall` exposes `PJSIP/e_…` | `rpc.GetCall` copies `Participant.EndpointURI` into `endpoint_uri`. |
| Unbounded provisioning before activation | `SetupEntitlement` has `MaxExtensions = MaxTenants = 0`, and 0 means unlimited at both enforcement points. |
| ERROR after every hang-up | After an Active call terminates, it is removed from memory. The last leg's `ChannelDestroyed` then reaches `ParticipantLeft`, which returns `ErrCallNotFound`, and the event loop logs that at ERROR. |

## Goals / Non-Goals

**Goals:**
- Every defect above is fixed, with a test at the lowest tier that can show it.
- The phone-to-phone path is proven end to end with real phones.

**Non-Goals:**
- Caller ID towards a carrier on outbound calls, which needs a tenant presentation number. It is recorded as a gap.
- Person-aware participants (Phase B, `pbx-extension-assignment`).
- An extension column in call history (`console-admin-screens` G3).
- Recovering automatically from a takeover.

## Decisions

### D1. The router decides direction and caller; the store records both

`InboundDecision` gains two fields:
- `CallerNumber`: empty means keep the channel's caller number;
- `CallerName`: shown on the destination phone.

`CallArrived` applies them after routing. A desk-phone call fills both from the dialling extension. A trunk call leaves them empty, so the carrier's number stands. `SaveCall`'s upsert also updates `direction` and `source_number`. Both are provisional until routing decides them and fixed after it. No code path changes either one once the call is Screening.

*Alternative rejected:* saving the Call only after routing. The Initiated event and a refused call's Terminated record both need the row before routing.

### D2. The destination leg carries the caller ID

`originateParticipant` takes a caller ID. For an arriving call it is the call's source number, preceded by the caller name when there is one, in the engine's `"Name" <number>` form. API-placed calls are unchanged.

The display name is user-entered. Before the name is quoted, `"`, `<`, `>`, `\` and control characters are removed from it, so no name can change the number presented.

### D3. `GetCall` stops filling `endpoint_uri`

The field stays in the proto, marked `deprecated = true` with a comment saying it is never filled. Deleting it would fail `buf breaking` (FILE rules). `TestGetCall_NoChannelIDLeakage` gains the endpoint form. The console never read the field.

### D4. "No licence applied" travels through the cap ports (D-72)

Identity and pbx-core each gain an `ErrNotLicensed` sentinel in their ports. The composition root's licence adapter returns it from `MaxTenants`/`MaxExtensions` when the entitlement is `StateSetup`. A degraded installation is not affected: it keeps its caps (AC-06.13).

Each RPC layer maps `ErrNotLicensed` to `FAILED_PRECONDITION` with "no licence is applied". `atsap-api bootstrap` keeps `noTenantCap`, because the first tenant and its platform administrator must exist before anyone can apply a licence.

The contexts still never import licensing. The adapter in `cmd/` already depends on both sides.

### D5. A taken-over application fails readiness and is not taken back (D-73)

The ARI client tracks its stream:
- **connected:** set when the websocket is up, cleared when it drops;
- **replaced:** set when an `ApplicationReplaced` event names its application.

On replacement, `StreamEvents` logs ERROR, closes the connection and returns `ErrApplicationReplaced` without reconnecting. A `call-events` readiness checker reports either condition.

If the instance re-registered by itself, two live instances would take the application from each other in turn, and calls would be lost on both while both reported ready. One instance that reports not ready is the safer failure, and an orchestrator or operator restarts it.

The check also covers a dropped connection, which `/readyz` has never covered.

### D6. A leg leaving after its call ended is expected

When `ParticipantLeft` finds no in-memory call, it logs at DEBUG and returns nil. The Call was already finalized: the first leg to leave ended it, and the engine releases the other leg. This also covers legs from before a restart, which carry nothing to act on either.

## Risks / Trade-offs

- **Readiness fails briefly at start-up,** until the event stream connects, which takes milliseconds on the rig. Tilt and the test tiers wait on `/readyz`, so this delays them slightly and never makes them flaky.
- **No automatic recovery after a takeover.** It takes a restart. The error log and the failed readiness check are what make the takeover noticed; the D5 reasoning explains why.
- **Display names in caller ID** are sanitized, not escaped. A name containing quotes loses them on the phone screen only, never in the stored data.

## Migration Plan

- There is no schema change. The upsert writes columns that already exist.
- The proto change is additive (`deprecated` option) and wire-compatible.
- Rows recorded before this change keep their old direction and source.
