# UAT — Click-to-call from the console (human pass with a real phone)

User Acceptance Testing for `console-call-session` (D-79), which completes
the click-to-call from `console-click-to-call` (D-78). The automated tiers
prove the ordering, the live session and every control. This pass is what
an administrator sees: the call is a visible session with a live state and
a one-click Cancel/Hang up, and it survives navigating around the console.

**Traceability:**
- BRD FBR-R1-01 (make and receive calls) and §16 (Phase A through the
  public API);
- PRD EPIC-03 AC-03.9, AC-03.10, AC-03.11 and §14 (live figures within 2 s);
- spec `openspec/specs/pbx-core/call-placement` and
  `openspec/specs/telephony-core/call-lifecycle`;
- `openspec/changes/console-call-session`.

## 1. Environment

| Item | Detail |
|---|---|
| Stack | The dev rig (`make tilt-up`, or the compose stack with the app rebuilt) and the console at **http://localhost:5173** |
| Login | `dev@example.test` / `e2e-fixture-operator-password` (`make dev-user`), tenant **Dev Acme** |
| Licence | The rig is licensed (`make dev-licence` once) |
| Phone | A real phone registered to an extension of Dev Acme: the owner's `baresip` 1001/1002, or an extension created in the console and registered by any softphone |
| Destination | Another extension of the tenant, or a number an outbound route reaches (the test trunks use `+44`) |

**Stop the owner's phones before automated rig/e2e runs that use UDP
5070/5071**, and restart them afterwards (HANDOFF §6).

## 2. Procedure

| # | Step | Expected |
|---|---|---|
| 1 | Open http://localhost:5173 and sign in as an administrator. Open **Call history** (`/calls`) | **New call** in the top bar and **Place a call** in the page header |
| 2 | Press **New call** | The dialer offers a searchable extension list (registration shown; unregistered disabled), a number field, a dial pad and recent numbers. It says "Your phone rings first. Answer it to be connected." |
| 3 | Choose your registered extension, enter another extension (e.g. `1002`) or a routed number, press **Call** | The dialog closes and a **call bar** appears: *Ringing your extension 1001 — answer your phone to continue. 1002 is not dialled yet.* Only your phone rings. **Cancel call** is available. |
| 4 | Answer your phone | The bar changes to *Calling 1002…*; the destination now rings. Answer it | the bar changes to *In call with 1002 · 00:12* with a running timer and **Hang up** |
| 5 | While connected, open **Extensions** or **Trunks & routes** | The call bar remains visible on the new page with the live state and **Hang up** |
| 6 | Press **Hang up** | The call ends immediately (no confirmation); the bar shows *Call ended — ended from the console* with **Dismiss**, and the history row reads **Terminated** |
| 7 | Repeat step 3 and do **not** answer your phone; wait for the placement timeout (default 30 s) | The bar shows *Call ended — 1001 did not answer*; *1002 was never dialled.* The licence channel is released. |
| 8 | In Call history, a Presenting or Active row has a direct **Hang up** button | One click ends the call; no menu and no confirmation |
| 9 | Sign in as a role that is not an administrator (an agent) and open Call history | No **New call**, **Place a call** or **Hang up**; the history is still readable |
| 10 | Reload the page while a call is still ringing | The bar re-attaches to the same call and still offers **Cancel call** |

## 3. Executed record

| Date | 2026-11-02 |
|---|---|
| Who | Claude (automated tiers; the owner's human pass is still to be recorded here) |
| Result | **PASS** at the automated tiers |

- **Phone UAT:** `make uat-placement` on real `baresip` phones and a carrier.
  The phone was rung in manual mode; the script asserted the carrier had not
  been dialled, accepted the phone, then saw the carrier dialled. Media flowed
  both ways (tx/rx > 0) and HangupCall released both phones.
- **Go e2e:** the full tier passed after `make rig-restore`; the caller-first
  order, the unanswered-caller timeout and the GetCall reason were covered.
- **Console rig integration:** a real internal call placed from the dialog
  showed *Ringing your extension …* with the destination not dialled, was
  cancelled from the bar, and the row ended Terminated.
- **Console browser:** the mocked session spec passed at 1280 px and 390 px
  for ringing → cancel, connected → hang up, the row Hang up, and an exposure
  scan of the bar. `make console-check` passed every console tier.
- **Human pass:** still to be recorded by the owner; the procedure above is
  the checklist.
