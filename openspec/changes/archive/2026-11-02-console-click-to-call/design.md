## Context

- **Today:** `PlaceCall` (`pbx/application/placement.go`) resolves the dial plan, then telephony-core's `InitiateCall` originates **both** legs in parallel; they are bridged when both answer (call-placement G6, "a telephony-core change"). The caller's leg is an extension endpoint; the destination leg is an extension or a trunk.
- **Permissions:** `call.place` and `call.hangup` are held by `TENANT_ADMIN` and `PLATFORM_ADMIN` (wildcard) only (`identity/application/service.go` `DefaultPermissions`).
- **Console:** *Call history* (`features/calls`) is read-only. The extension list already shows registration status.

## Goals / Non-Goals

**Goals:**
- Ring the caller first, then the destination only after the caller answers.
- An administrator can place and hang up a call from the console.

**Non-Goals:**
- **Agents and supervisors placing calls from "their" phone:** needs extension ownership (D-65, Phase B).
- **WebRTC browser calling:** item #8 of the pre-pilot refinements, its own change in this phase.
- **Call transfer, hold and conference.**

## Decisions

### D1. Caller-first origination in telephony-core

`InitiateCall` gains an ordered mode used by placement:
1. Originate the caller's leg first; it rings the extension.
2. On that participant's answer (`ParticipantAnswered` for the caller), originate the destination leg.
3. Bridge when the destination answers, exactly as today.

A placement timeout (default 30 s, configuration) ends a call whose caller has not answered, with the reason `caller_no_answer`. Capacity is reserved at placement, as today, and released on every end, including the timeout.

**Alternative rejected:** a Local channel or dialplan `Dial()` doing the sequencing in Asterisk. It would move call control out of the platform (D-47: the engine is configured, not programmed) and lose per-leg participant tracking.

### D2. Console

- **Place a call dialog on Call history:**
  - *From extension*: a select listing the tenant's extensions; unregistered ones are disabled, with "No device registered".
  - *Number*: the digits as dialled.
  - *Call*: sends `PlaceCall`; a toast says "Calling … — answer extension 1001 to connect".
- **Errors:** placement refusals map to console copy by gRPC code and the fixed server messages (`mapCallErr`), never raw text.
- **Hang up:** a row action on calls whose state is Presenting or Active (`HangupCall`).
- **Visibility:** the action is shown only to roles holding `call.place` (the console's role catalogue mirrors identity's).

## Risks / Trade-offs

- **[Risk] A registered device that never answers** (auto-answer off, or nobody there) holds a licence channel until the timeout. → The timeout is short and configurable, and capacity is released on the timeout end.
- **[Trade-off]** The far end now waits longer to ring, by the time it takes the caller to answer. That is the industry's expected behaviour for click-to-call.

## Open Questions

- The default caller timeout: 30 s (the usual PBX click-to-call value). Configurable, so this can be settled in review without changing the design.
