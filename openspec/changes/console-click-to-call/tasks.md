## 1. Caller first

- [x] 1.1 telephony-core: an ordered `InitiateCall` mode (the caller's leg first; the destination's on the caller's answer; a placement timeout ending the call with `caller_no_answer` and releasing capacity).

  Unit tests:
  - no destination origination before the caller answers;
  - the timeout ends the call and never originates the destination;
  - capacity is reserved once and released once on every path.

  Verify `make test` and `make lint`.

  *(done 2026-11-02: `InitiateCallCommand.Ordered` plus `CallerTimeout`; `orderedDestination` originates the destination from the caller's `ParticipantAnswered`, and `callerTimedOut` finalizes with `caller_no_answer` and releases the caller's leg and the licence channel. Unit tests: waits for the caller, duplicate answer does not re-originate, full connect, timeout never rings the destination, answer cancels the timer. `make lint` 0. `make test` (`-race -cover`) passes in a `golang:1.26-bookworm` container because the host's `/usr/lib/gcc/.../libgcc.a` and `liblto_plugin.so` return EIO; the host's `-race` link fails for every package, including an empty program.)*
- [x] 1.2 pbx-core placement uses the ordered mode, and the timeout is configuration (`ATSAPBX_PLACEMENT_CALLER_TIMEOUT`, default 30 s). Integration tests for placement. Verify the integration tier on `atsapbx_test`.

  *(done 2026-11-02: `Placement.CallerTimeout`, filled from the new config field, is passed as `Ordered: true` on every placement. Config unit tests cover the default and invalid values; `DefaultPlacement` unit test covers the 30 s fallback; placement integration asserts `Ordered` and the timeout. Integration tier on `atsapbx_test` passes (`internal/pbx/application`, `internal/telephony/...`). `make lint` 0.)*
- [x] 1.3 Live: Go e2e (the phone agent answers after a delay, and the carrier receives the INVITE only afterwards; an unanswered caller means the carrier never sees an INVITE), plus `uat-placement` updated to assert the order. Verify the full Go e2e tier and the three phone UATs after `make rig-restore`. The owner's phones on 5070/5071 must be stopped first, **with consent**.

  *(done 2026-11-02: two new e2e tests — a caller that answers after 2 s and only then rings the carrier, and a caller that never answers (30 s) ending with `caller_no_answer` while the carrier receives no INVITE. Full Go e2e tier green after `make rig-restore`. `uat-placement` now rings the phone in manual mode, checks the carrier has not been dialled, accepts via ctrl_tcp, and checks the carrier was dialled afterwards; `uat-auto` and `uat-inbound` pass unchanged. Two defects found live and fixed: (1) the caller's answer could arrive before the placement timer was armed, so the destination was never rung — the timer is now armed before origination; (2) the engine's 20 s alert timeout preempted the 30 s placement timer and the reason was lost — the caller leg now gets `caller timeout + 5 s` so the platform owns the timeout. Also fixed the UAT ctrl_tcp reader, which read until quiet and hung while an active call streamed events; it now returns the command's own response frame. Lint 0; unit race+cover green in the Go container.)*

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
