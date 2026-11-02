# Coverage — console-click-to-call

**Tiers:**
- **U**: unit (`telephony/application` ordered originator, `pbx/application` placement defaults, `config`, console component tests).
- **I**: integration on `atsapbx_test` (`pbx/application` placement) and the console rig spec.
- **E**: e2e on the rig (Go placement tests) and the console browser specs.
- **A**: `make uat-placement` with real `baresip` phones.

## pbx-core/call-placement

### A call is placed from an extension to a dialled destination through the public API

| Scenario | Proven by |
|---|---|
| An outbound call is placed over a trunk | **U** `TestInitiateCall_OrderedWaitsForCaller`, `TestInitiateCall_OrderedConnects`; **I** `TestPlaceCall_OutboundOverATrunk` (asserts `Ordered` and the timeout); **E** `TestPlaceCall_RingsTheCallerBeforeTheDestination`; **A** `make uat-placement` |
| A call to another extension stays internal | **E** `TestPlaceCall_InternalCallConnectsWithoutATrunk`; **I** `TestPlaceCall_InternalCallUsesNoTrunk`; **A** the console rig spec places a real internal call |
| An unroutable destination is refused | **I** `TestPlaceCall_Refusals`; **E** `TestPlaceCall_RefusalsThroughTheWire`; console component test maps the refusal to console words |
| Another tenant's extension cannot place a call | **I** the placement suite's tenant scoping; **W** `TestPbxHandler` permission cases |
| An unauthorized caller cannot place a call | **W** `TestPbxHandler`; console component test (`AGENT` sees no action) |
| The destination is never rung before the caller answers | **U** `TestInitiateCall_OrderedWaitsForCaller` (one origination until the answer); **E** the delayed-answer test; **A** `uat-placement` checks the carrier log before accepting |
| A caller who does not answer rings no one | **U** `TestInitiateCall_OrderedCallerTimeout` (`caller_no_answer`, capacity released once, caller leg released); **E** `TestPlaceCall_UnansweredCallerNeverRingsTheDestination` (30 s, `ListCalls` reason, no carrier INVITE) |

**Console (task 2.1):** component tests cover the dialog, registration
gating, every refusal, validation, no-device/no-extension states, row
hang-up and failure; a stateful browser spec covers Place a call and Hang
up at 1280 and 390 px; the rig spec places a real call and ends it.
`make console-check` passes every console tier, and the coverage floors
were ratcheted to the measured values.

**Found and fixed on the way:**
- The placement timer was armed after the caller leg was originated, so an
  immediate answer could be treated as an ordinary (unordered) answer and
  the destination was never rung. The timer is now armed first; the
  duplicate-answer and wait tests cover it.
- The engine's 20 s alert timeout preempted the 30 s placement timer, so
  the reason was lost. The caller leg now gets `caller timeout + 5 s`;
  `TestInitiateCall_OrderedWaitsForCaller` asserts the wider engine window.
- The UAT `ctrl_rpc` reader read until quiet; an active call streamed
  events and hung it. It now returns the command's own response frame
  (`scripts/uat-placement.sh`, `scripts/uat-auto.sh`).
- The `DefaultPlacement` test was a unit file in `pbx/application`, which
  pulled that integration-only package (939 statements) into the unit
  coverage denominator and dropped the unit total from 79.2% to 65.3%.
  It moved behind the `integration` tag, restoring 79.2% (floor 79.1%).

## Gaps

| # | Gap | Severity | Note |
|---|---|---|---|
| G1 | The console cannot place a call "from my own phone": there is no person ↔ extension link in Phase A (D-65). An administrator chooses an extension explicitly | Accepted | Phase B: `pbx-extension-assignment`; agents and supervisors then get the action |
| G2 | The destination's registration is not pre-checked. If the caller answers and the destination's device is gone, the platform ends the call after origination fails, rather than refusing before the caller rings | Low | The caller-first behaviour still holds; a pre-check waits for extension ownership |
| G3 | A role check is shown by the console's own role catalogue (`canAdminister`), not by reading a permission set from the session. The catalogue mirrors identity's defaults | Accepted | Same arrangement as the rest of Phase A; the server enforces every request |
| G4 | The unanswered-caller e2e waits the full 30 s because the timeout is configuration, not a request field | Accepted | Keeps the setting out of the wire; the unit test uses a short timeout |
