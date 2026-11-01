## 1. Caller first

- [ ] 1.1 telephony-core: an ordered `InitiateCall` mode (the caller's leg first; the destination's on the caller's answer; a placement timeout ending the call with `caller_no_answer` and releasing capacity).

  Unit tests:
  - no destination origination before the caller answers;
  - the timeout ends the call and never originates the destination;
  - capacity is reserved once and released once on every path.

  Verify `make test` and `make lint`.
- [ ] 1.2 pbx-core placement uses the ordered mode, and the timeout is configuration (`ATSAPBX_PLACEMENT_CALLER_TIMEOUT`, default 30 s). Integration tests for placement. Verify the integration tier on `atsapbx_test`.
- [ ] 1.3 Live: Go e2e (the phone agent answers after a delay, and the carrier receives the INVITE only afterwards; an unanswered caller means the carrier never sees an INVITE), plus `uat-placement` updated to assert the order. Verify the full Go e2e tier and the three phone UATs after `make rig-restore`. The owner's phones on 5070/5071 must be stopped first, **with consent**.

## 2. Console

- [ ] 2.1 The Place-a-call dialog on Call history:
  - an extension select (unregistered disabled) and a number field;
  - the `PlaceCall` adapter and copy for every refusal;
  - visible only with `call.place`;
  - a Hang-up row action for Presenting and Active calls.

  Component tests, a mocked browser spec (including a 390 px width) and a rig spec placing a real call to an internal extension. Verify `make console-check` (if Node or Chrome workers crash under load, rerun with fewer workers and record it).

## 3. Docs and close

- [ ] 3.1 Update:
  - LLD-03 (placement order);
  - CONSOLE-DESIGN (Call history actions);
  - `docs/API.md` (the PlaceCall note: rings the caller first);
  - PROGRESS (G6 closed; pre-pilot item #7 done);
  - a UAT procedure in `docs/uat/` (click-to-call in the browser with a real phone);
  - `coverage.md`.

  Ratchet the floors. Verify `make docs` and `make quality`.
