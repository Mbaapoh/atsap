# UAT — Click-to-call from the console (human pass with a real phone)

User Acceptance Testing for OpenSpec change `console-click-to-call`
(design D1/D2). The automated tiers prove the ordering, the timeout and
the console actions. This pass is what a partner administrator sees: the
platform rings their own phone first, and only when they answer does the
number they chose ring.

**Traceability:**
- BRD FBR-R1-01 (make and receive calls) and BRD §16 (Phase A through the
  public API);
- PRD EPIC-03 (calls) and the `pbx-core/call-placement` scenario "The
  destination is never rung before the caller answers";
- spec `openspec/specs/pbx-core/call-placement`;
- `openspec/changes/console-click-to-call`.

## 1. Environment

| Item | Detail |
|---|---|
| Stack | The dev rig (`make tilt-up`, or the compose stack with the app rebuilt) and the console at **http://localhost:5173** |
| Login | `dev@example.test` / `e2e-fixture-operator-password` (`make dev-user`), tenant **Dev Acme** |
| Licence | The rig is licensed (`make dev-licence` once) |
| Phone | A real phone registered to an extension of Dev Acme: the owner's `baresip` 1001/1002, or an extension created in the console and registered by any softphone |
| Destination | Another extension of the tenant, or a number an outbound route reaches (the test trunks use `+44`) |

## 2. Procedure

| # | Step | Expected |
|---|---|---|
| 1 | Open http://localhost:5173 and sign in as an administrator. Open **Call history** (`/calls`) | The call list and a **Place a call** button in the page header |
| 2 | Press **Place a call** | The dialog offers every extension. An extension with no registered device is **disabled** and reads "No device registered" |
| 3 | Choose the extension your real phone is registered to. Enter another extension, e.g. `1002`, or a routed number such as `+442071234567`. Press **Call** | The toast says "Calling … — answer extension … to connect." **Only your phone rings.** The destination does not ring yet |
| 4 | Answer your phone | The destination now rings. Answer it (or let the carrier answer): the two parties are connected, and the call history row goes **Presenting** → **Active** |
| 5 | In the call's row, **Actions → Hang up… → Hang up** | The toast says "Call ended."; both parties are released and the row reads **Terminated** |
| 6 | (Timeout) Repeat step 3 and do **not** answer your phone | After the placement timeout (default 30 s, `ATSAPBX_PLACEMENT_CALLER_TIMEOUT`) the call ends with reason **caller_no_answer**; the destination never rings, and the licence channel is released |
| 7 | Sign in as a role that is not an administrator (an agent) and open Call history | No **Place a call** button and no **Hang up** row action; the history is still readable |

## 3. Executed record

| Date | 2026-11-02 |
|---|---|
| Who | Claude (automated tiers; a human pass with the owner's phone is still to be recorded here) |
| Result | **PASS** at the automated tiers |

- `make uat-placement` on real `baresip` phones and a carrier: the phone was
  rung in manual mode, the script asserted the carrier had **not** been
  dialled, accepted the phone, then saw the carrier dialled; media flowed
  both ways (tx/rx > 0) and `HangupCall` released both.
- The Go e2e tier: a caller that answers after 2 s rings the carrier only
  afterwards; an unanswered caller (30 s) terminates with `caller_no_answer`
  and the carrier receives no INVITE.
- The console rig spec: a real internal call placed from the dialog appears
  **Presenting** and is hung up from its row, ending **Terminated**.
- The console browser spec walks Place a call and Hang up at 1280 px and
  390 px against a stateful mock.
- Two live defects were found and fixed on the way: the caller's answer
  could race the placement timer's arming (leaving the destination
  unrung), and the engine's 20 s alert timeout preempted the 30 s placement
  timer (losing `caller_no_answer`). The UAT control-socket reader also no
  longer reads until quiet, which hung while an active call streamed
  events.
