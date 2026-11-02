# Coverage — console-call-session

**Tiers:**
- **U**: Go unit/wire and console component tests.
- **I**: Go integration on `atsapbx_test` and the console rig integration spec.
- **E**: Go e2e and the console mocked browser specs.
- **A**: the three phone UATs (the API/phone flow is the same path the console calls).

## pbx-core/call-placement

### A call placed through the API can be ended through the API

| Scenario | Proven by |
|---|---|
| Hanging up ends the call | **U** `TestHangupCall`; **E** the console browser and Go e2e hang-up paths |
| Hanging up before the caller answers | **E** `calls.rig.spec.ts` (New call → Cancel while ringing; the carrier/destination is never dialled); **A** `uat-placement` asserts the carrier has not been dialled before the answer |
| Hanging up twice is harmless | **U** existing idempotent HangupCall test |
| Another tenant's call cannot be ended | **U** placement tenant-scoping test |

### A placed call reports live progress and its final reason

| Scenario | Proven by |
|---|---|
| The caller's extension is ringing | **U** `GetCall` in-flight wire test; **E** `session.test.tsx` "Starting/Ringing" and the mocked browser spec |
| The destination is dialled only after the caller answers | **E** `TestPlaceCall_RingsTheCallerBeforeTheDestination`; `session.test.tsx` "Calling 1002…" |
| A connected call reports its answer time | **U** `TestGetCall_Success`/`Terminated...`; **E** `session.test.tsx` connected duration |
| An ended call reports its reason | **U** `TestGetCall_TerminatedReportsTimesAndReason`; **E** `session.test.tsx` every ending reason |
| A caller who does not answer is reported as such | **U** `TestInitiateCall_OrderedCallerTimeout`; **E** `TestPlaceCall_UnansweredCallerNeverRingsTheDestination` and the console session test |

## telephony-core/call-lifecycle

### The public call query reports recorded timestamps and the outcome

| Scenario | Proven by |
|---|---|
| Querying a call in flight | **U** `TestGetCall_Success` (started set, end fields unset) |
| Querying an answered call | **U** the same plus the Active fixture; **E** the console session connected state |
| Querying a terminated call | **U** `TestGetCall_TerminatedReportsTimesAndReason` |
| The query leaks no engine identifier | **U** `TestGetCallResponse_NoChannelFieldsInSchema`, `TestGetCall_NoChannelIDInResponseContent`; console exposure scan |

**Console session (D-79):** component tests cover the provider, 1 s polling,
reload recovery, tenant change, corrupt storage, not-found, reconnect, every
bar state and reason, the dialer (search, registration gating, recents, dial
pad, validation, every refusal, no-device/no-extension), live history and the
direct row hang-up. Mocked browser specs run at 1280 and 390 px; the rig spec
places a real call, cancels it while the caller rings, and sees the row
Terminated. `make console-check` passes every console tier and the coverage
floors are ratcheted.

**Found and fixed on the way:**
- The Go e2e `TestG1_ProjectionIsLiveWithoutAnApplyStep` asserted `401` when
  a deleted extension's stored secret was re-registered. After a fresh
  Asterisk restart the distributor answers `404` (no endpoint) instead; both
  are refusals. The assertion now accepts either, and still proves the
  endpoint is gone. This is a test-robustness fix, not a product change.

## Gaps

| # | Gap | Severity | Note |
|---|---|---|---|
| G1 | The live state is polled every second, not pushed | Accepted | Meets PRD §14's 2 s bound; revisit with the browser softphone (#8) |
| G2 | One active click-to-call session in Phase A | Accepted | Multiple sessions/call waiting arrive with the softphone (D-79) |
| G3 | A reload recovers the session only from the same browser profile | Accepted | The call id is in `sessionStorage`; a different browser shows the call in history but not the bar |
| G4 | The destination's registration is not pre-checked | Low | The caller rings first; a missing destination ends the call after the caller answers (carried from `console-click-to-call` G2) |
| G5 | Hold/transfer/park/conference are not in this change | Accepted | Phase B (D-79) |
