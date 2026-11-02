## Why

The owner asked (2026-11-01) whether a call can be made from the portal. Today it cannot: `PlaceCall` exists in the API, but the console offers no way to use it, so calls start only from softphones and desk phones. Every comparable PBX (3CX, VitalPBX, FreePBX UCP) has **click-to-call**: the platform rings *your* phone, and when you answer it rings the number you chose.

Ours also has a known defect that makes click-to-call feel wrong. **Both legs ring at once** (call-placement G6), so the far end can answer before the person who clicked has picked up their own phone. The industry behaviour is "ring me first, then dial out".

This is item #7 of the pre-pilot refinements, which the owner scheduled for this phase.

## What Changes

- **Ring the caller first.** `PlaceCall` originates the caller's extension leg first, and the destination leg only when that leg answers.
  - If the caller does not answer within a timeout, the call ends and the destination never rings.
  - This closes call-placement G6.
- **Console click-to-call** (administrators, Phase A):
  - a **Place a call** action on *Call history*, where an administrator chooses the extension to call **from** (only extensions with a registered device can be chosen) and the number to call;
  - a toast with the outcome, and the new call at the top of the list;
  - a **Hang up** action on a call in progress (`HangupCall`).

  Placement refusals are shown in console words: unroutable, trunk at capacity, no registered device, refused.
- **Who may click-to-call:** `call.place` stays with tenant and platform administrators in Phase A. Agents and supervisors get it in Phase B, together with person-to-extension ownership (D-65, `pbx-extension-assignment`), so that "call from my phone" has a meaning. No role change now.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `pbx-core/call-placement`, *A call is placed from an extension to a dialled destination through the public API*: the destination rings only after the caller's extension answers, and is never rung if the caller does not answer.

## Impact

- **API (Go):** `internal/telephony/application`, where `InitiateCall` gains a "caller first" mode used by placement; `internal/pbx/application/placement.go`. No proto change.
- **Console:** `features/calls` gets a Place-a-call dialog (extension select plus number) and a Hang-up row action, with copy and tests.
- **Docs:** LLD-03 placement section, CONSOLE-DESIGN (Call history), PROGRESS (G6 closed; item #7 done), a UAT procedure.
- **Tests:**
  - unit: ordering, and the caller timeout;
  - integration: placement;
  - e2e: a real phone answers first and only then does the carrier ring; no answer means the destination never rings;
  - console: component, mocked browser and rig specs;
  - UAT: `uat-placement` updated to the new order.
- **Dependencies:** none.
