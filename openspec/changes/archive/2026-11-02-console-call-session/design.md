## Context

See `proposal.md` — Why. The current console is `console-click-to-call`
(archived 2026-11-02): `PlaceCall` works, the caller rings first, and Call
history can place and hang up. What is missing is the *session* around the
call. The relevant current constraints:

- `useCallHistory` is a normal React Query read with no refresh; the list
  is invalidated once after a placement and then never updates. A row can
  therefore read `Presenting` after the call is `Active` or `Terminated`,
  which violates PRD AC-03.9 ("never shows a call as active when it is
  not").
- The placement dialog unmounts on success and the only feedback is a
  six-second toast. There is no persistent place to see the call, no
  hint that the caller's phone must be answered, and no cancel.
- Hang up exists only as a row action (`Actions → Hang up…`) with a
  confirmation. Before the caller answers, the same action is a *cancel*
  — the destination was never rung — but the UI does not say so.
- `GetCall` returns state, direction, numbers and participants only. It
  has no `started_at`, `answered_at`, `ended_at` or `termination_reason`;
  the reason is only on `ListCalls`. The live panel cannot explain an
  ended call without a second, filtered list read.
- HLD-19 §2 currently excludes "live call controls" from Phase A. That
  exclusion was written before click-to-call; status and hang-up are now
  in Phase A, while hold/transfer/park/conference stay Phase B.
- The console is an API consumer (D-43); no private endpoint, no engine
  identifier on screen (PRD principle 4). The access token stays in
  memory; only non-secret presentation state may be persisted.

## Goals / Non-Goals

**Goals:**

- A call, once placed, is a **visible session** until it ends: which
  extension is ringing, whether the destination is being dialled, whether
  it is connected, how long, and why it ended.
- **Hang up is always one click away** and is never hidden behind a menu.
  Before the caller answers, the same control reads **Cancel call**.
- The session **survives navigation** anywhere in the console (PRD
  AC-03.10) and, as far as the platform can tell, a reload.
- State changes appear within **2 s** (PRD §14) without a page reload.
- The New call flow looks and behaves like an industry dialer (3CX,
  VitalPBX): a global entry point, a searchable extension picker, a dial
  pad, recent numbers, one primary Call action, and explicit guidance.
- The design stays inside Phase A: the media is on the registered phone;
  the console observes and controls the call through the public API.

**Non-Goals:**

- Browser softphone/WebRTC (FBR-R1-01, item #8).
- Hold, mute, transfer, park, conference (PRD US-03.2; Phase B with the
  agent softphone).
- Multiple simultaneous click-to-call sessions in Phase A. One active
  session is a deliberate boundary; the shell says so and disables New
  call while one is live.
- Person-to-extension ownership / "call from my phone" (D-65, Phase B).
- Push/WebSocket call events. Polling is the Phase A mechanism; the
  realtime transport is revisited with the softphone (#8).
- Changing the ringing order, the 30 s timeout, or the carrier path (D-78).

## Experience

The wireframes below are the proposed shape, not a copy of any product. A
static mockup using the console's real colour tokens is at
`docs/design/call-session-mockup.html` (open it in a browser; it is a design
reference, not the running console).

### Desktop: the call session bar

The bar sits directly under the top bar and above the page. It is visible on
every signed-in route, so navigating to Extensions or Trunks does not lose
the call.

```
+----------------------------------------------------------------------------------+
| AtsaPBX        Tenant: Dev Acme v       Licence: Valid 16       [ New call ]   |
+----------------------------------------------------------------------------------+
| Overview        Call history                                                     |
| Users           +--------------------------------------------------------------+ |
| Extensions      | (phone)  Ringing your extension 1001                         | |
| Trunks & routes |          Answer your phone to continue.                      | |
| Call history    |          1002 is not dialled yet.               [Cancel call] | |
| Account         +--------------------------------------------------------------+ |
|                 | Started          | Direction | From | To   | State      | ... | |
|                 | 2 Nov 20:12      | Internal  | 1001 | 1002 | Presenting | ... | |
+----------------------------------------------------------------------------------+
```

Connected state — the same bar, a different sentence and a red action:

```
+----------------------------------------------------------------------------------+
| (phone)  In call with 1002                                                       |
|          00:12 · from extension 1001                              [ Hang up ]    |
+----------------------------------------------------------------------------------+
```

Ended state — explains itself, then can be dismissed:

```
+----------------------------------------------------------------------------------+
| (check)  Call ended — 1001 did not answer                                        |
|          1002 was never dialled.                                 [ Dismiss ]     |
+----------------------------------------------------------------------------------+
```

### Phone (≤768 px): the same session as a bottom sheet

```
+--------------------------------------+
| AtsaPBX                    [ menu ]  |
| Call history          [ New call ]   |
|                                      |
|  ... call rows or cards ...          |
|                                      |
+--------------------------------------+
| (phone) In call with 1002            |
| 00:12 · from extension 1001          |
| [            Hang up             ]   |
+--------------------------------------+
```

The sheet is above the safe area, 44 px targets, never wider than the
viewport. The page gets bottom padding while it is present, so the last row
is still reachable.

### The New call dialog (desktop)

```
+------------------------------------------------------------+
| New call                                              [ x ] |
|                                                            |
| From extension                                             |
| [ 1001 · Reception                          Registered  v ]|
|                                                            |
| Number to call                                             |
| [ +44 207 123 4567                                ] [ x ]  |
|                                                            |
|   1     2     3          Recent numbers                    |
|   4     5     6          +44 207 123 4567   2 min ago       |
|   7     8     9          1002               yesterday       |
|   *     0     #  [del]                                     |
|                                                            |
| Your phone rings first. Answer it to be connected.         |
|                                    [ Cancel ]  [  Call  ] |
+------------------------------------------------------------+
```

Below 768 px the same content is a full-screen sheet; the dial pad and the
primary Call button stay above the keyboard with `position: sticky`.

### Entry points

- **Top bar:** `New call`, always available to `call.place` holders, disabled
  ("A call is in progress") while a session is live.
- **Call history header:** the same action for discoverability.
- **Call history rows:** direct **Hang up** on Presenting/Active, sharing the
  bar's handler; no confirmation.
- **No private endpoint:** every action still goes through `PlaceCall`,
  `HangupCall` and `GetCall`.

## Decisions

### D1. A shell-level call session bar, not a dialog or a toast

The session lives in the application shell, above the page content,
rendered by a `CallSessionProvider` mounted inside the authenticated
shell. It is not a modal, so it never blocks navigation, and it is not a
toast, so it does not disappear while the call is still happening.

- Desktop: a compact bar under the top bar, full content width, 56–64 px
  high. It can coexist with the page.
- ≤768 px: a bottom sheet/card above the safe area, with the same
  information and 44 px touch targets. It must not cover the page heading
  when the page first loads; it sits at the bottom and the page content
  gets bottom padding while it is present.
- It remains visible on Call history, Extensions, Trunks, Overview — every
  route behind the shell. The sign-in/tenant-choice routes have no
  session.

**Alternative rejected:** keep the dialog open until the call ends. It
blocks the rest of the console and cannot satisfy AC-03.10. **Alternative
rejected:** a toast with actions. Toasts are transient and not focusable
enough for a primary control.

### D2. Poll `GetCall` while the call is in flight

After a successful PlaceCall the provider holds the call id and polls
`TelephonyService/GetCall` every **1 s** while the state is not
`Terminated`, then stops. On each transition it invalidates the Call
history queries so the list shows the same truth. The 1 s cadence leaves
margin under PRD §14 ("live figures update within 2 seconds") without a
new transport.

- Poll only when the browser tab is visible (`document.visibilityState`);
  resume on focus and poll once immediately. A hidden tab must not hammer
  the API.
- Back off to 2 s after 60 s in flight; the rollout timeout is 30 s by
  default, so this is mostly for future longer calls.
- Every poll failure is not a call failure: keep the last known state,
  mark it "Reconnecting…" after two consecutive failures, and keep the
  Hang up control enabled; a failed hang-up is shown in the bar.
- On `Terminated`, read the reason and end the session.

**Alternative rejected:** server-sent events/WebSocket now. It adds a new
transport, auth and reconnect surface; the public polling read already
exists and meets the latency requirement. Revisit with #8.

### D3. One active click-to-call session in Phase A

A second New call while one session is in flight is not started: the New
call button is disabled with "A call is in progress" and the shell points
at the bar. Multiple calls and call waiting are Phase B (softphone).

**Alternative rejected:** a stack of sessions. It is the right long-term
UI, but it needs hold/switch semantics that Phase A does not have.

### D4. Hang up is immediate; Cancel before answer

- While `Presenting` and the caller has not answered, the primary control
  is **Cancel call** (outline/danger). It ends the ringing leg; the
  destination was never rung.
- While `Active`, it is **Hang up** (solid danger).
- No confirmation dialog. Every VoIP client hangs up immediately; the
  confirmation added in `console-click-to-call` is removed. A mis-click is
  recoverable by placing the call again, and the alternative (a delayed
  hang-up) is worse in an emergency.
- The action is a real button with an explicit accessible name ("Hang up
  call with 1002", "Cancel call to 1002 before it is dialled") and
  keyboard focus order after the call details.
- While the request runs, the button shows a busy state and cannot be
  pressed twice.

### D5. Extend `GetCall` rather than polling `ListCalls`

`GetCallResponse` gains `started_at`, `answered_at` (when answered),
`ended_at` (when ended) and `termination_reason` (when ended). This is
additive and uses the existing resource; the console already has the call
id from PlaceCall. Polling `ListCalls` would need filters, could return
other calls, and is heavier; reading the whole history to find one call is
the wrong shape.

- Fields are always populated from the durable record, never synthesized.
- While in flight, `ended_at`/`termination_reason` are unset.
- No participant engine endpoint is added (PRD principle 4).

### D6. The New call dialog is a dialer, not a form

- Entry point: a **New call** primary action in the top bar (visible on
  every shell route to roles holding `call.place`) and the same action in
  the Call-history page header. One component, two placements.
- Dialog (modal on desktop, full-screen sheet below 768 px):
  - **From**: searchable extension picker. Each option shows number, name
    and a registration badge; unregistered extensions are disabled with
    "No device registered"; the last-used extension is remembered in
    memory, otherwise the first registered one. A "Manage extensions" link
    goes to Extensions.
  - **To**: large number field (`inputmode="tel"`) with a **dial pad**
    (1–9, *, 0, #, backspace, long-press 0 for +), a clear control, and
    **Recent numbers** from the tenant's call history (last 5 distinct
    destinations). Choosing a recent number fills the field.
  - One primary **Call** button (green), disabled until a registered
    extension is chosen and the number is valid (server rule: optional
    leading + then 1–32 digits). Secondary **Cancel**.
  - A short, explicit line: "Your phone rings first. Answer it to be
    connected." with the chosen extension number.
  - Inline validation and a failure alert in the dialog for immediate
    refusals (unroutable, no registered device, permission). A failure
    after the caller answered appears in the session bar, not the dialog.
  - Enter submits; Escape closes; focus starts in To; the first invalid
    field receives focus on failure.
- On success the dialog closes, focus moves to the session bar, and the
  bar announces "Ringing your extension 1001".

**Alternative considered:** keep the current select + text field. It is
functional but does not match the industry pattern the owner asked for
and gives no recents, no keypad and no registration scan.

### D7. Call history becomes live, but stays a history

- While a session is in flight, `useCallHistory` refetches at 1 s and
  shows a **Live** pill with the last update time. Outside a session it
  keeps the existing on-demand behaviour (no background polling).
- Presenting and Active rows get a direct **Hang up** button in the row
  (same handler as the bar), not only a menu item. The menu keeps the
  action for completeness, without the confirmation.
- Active rows tick their duration client-side from `answeredAt` between
  polls; the authoritative duration still comes from the server on
  termination.
- The row state and termination reason update when the call ends; an
  ended call is never shown as active (AC-03.9).

### D8. Reload recovery with a non-secret call id

The active call id is stored in `sessionStorage` under a console-local key
(no token, no secret, no engine identifier) so a reload can re-attach:
on mount the provider reads the id, calls `GetCall`, and shows the bar if
the call is still in flight or shows a short "Call ended" state if it has
ended. The key is cleared when the session ends. In-memory state alone
would lose the hang-up control on reload; persisting a token would violate
HLD-19 §4, but a call id is not a credential.

### D9. Phase A scope change: status and hang-up are Phase A; hold/transfer are not

HLD-19 §2 currently excludes "live call controls" from Phase A. This
change narrows that phrase: **call status and hang-up** are Phase A (they
are needed for click-to-call); **hold, mute, transfer, park and
conference** remain Phase B. The PRD and HLD updates are listed below and
require owner approval because they change the Phase A boundary.

### D10. Copy is behavior, not decoration

State copy is fixed and testable:

| State | Heading | Detail | Primary action |
|---|---|---|---|
| Starting | Starting call | Asking the platform to place it | Cancel |
| Ringing caller | Ringing your extension 1001 | Answer your phone to continue. 1002 is not dialled yet. | Cancel call |
| Calling destination | Calling 1002… | Connected to 1001. Waiting for 1002 to answer. | Hang up |
| Connected | In call with 1002 | 00:12 · from extension 1001 | Hang up |
| Ended (timeout) | Call ended — 1001 did not answer | 1002 was never dialled. | Dismiss |
| Ended (normal) | Call ended | 00:42 · normal clearing | Dismiss |
| Failed | Call could not be placed | No extension or outbound route matches that number. | Dismiss |

No raw server text, no engine vocabulary, no call id on screen.

### D11. PRD, HLD and TRD updates proposed (approval required)

- **PRD EPIC-03**: add AC-03.11 — "A placed call shows its live state
  (ringing the caller, calling the destination, connected with duration,
  or ended with reason) within 2 s, offers Hang up at every point in
  flight, survives navigation, and never shows an ended call as active."
  Extend the §14 responsiveness row to say "call state and duration
  update within 2 s". Add a user story for an administrator/agent:
  "As a person placing a call from the console, I always see whether my
  phone is ringing, whether the destination is ringing, and can end the
  call with one action."
- **HLD-18 flow A5**: A5 now includes placing, observing and ending a
  call from the console; the screen table says Call history plus the
  shell call session.
- **HLD-19**: §2 outcomes include the call session; §5 composition adds
  `CallSessionProvider` and the shell call bar; §6 truthful state adds
  the call-session states; §7 accessibility adds the live-region and
  touch-target rules; the Phase A exclusion list keeps hold/transfer/park
  but no longer excludes status/hang-up.
- **CONSOLE-DESIGN**: replace the current Call-history actions paragraph
  with the full session/dialer design (states, wireframes, copy,
  accessibility, mobile).
- **TRD/DECISIONS**: A new D-number, recorded on approval, captures the shell session, 1 s polling and the
  Phase A scope decision; a short TRD note points at it.
- **API.md**: the `GetCall` row gains the new fields.

## Risks / Trade-offs

- **[Risk] Polling every second per active call** → Only while visible and
  only for one Phase A session; 1 s is a small read and stops at
  termination. The number of console operators per tenant is small.
- **[Risk] A reload loses the in-memory session** → The call id is kept in
  `sessionStorage` (non-secret) and re-attached; if recovery fails the
  user can still see the call in history and end it there.
- **[Risk] The destination fails after the caller answered** (e.g. no
  device) → The session bar shows the recorded reason and a Dismiss
  action; the caller's leg is released by the platform.
- **[Risk] A stale poll shows "Active" after the call ended** → The
  provider always trusts the latest `Terminated` response and stops
  polling; the bar never renders Active from a cached value. AC-03.9 is
  covered by a component test with a state sequence.
- **[Trade-off] One active call** → Simpler and safe; multiple sessions
  arrive with the softphone and call waiting.
- **[Trade-off] Dial pad in Phase A** → Slightly more UI code, but it is
  the industry pattern the owner asked for and it makes the number entry
  usable on touch.

## Approval needed

The owner must approve these before implementation:

1. A global **New call** action and a shell-level **call session bar**
   (D1, D6).
2. **One active click-to-call session** in Phase A (D3).
3. **Immediate Hang up / Cancel**, no confirmation (D4).
4. A **dial pad and recent numbers** in Phase A (D6).
5. Extending **`GetCall`** with timestamps and termination reason (D5).
6. **1 s polling** rather than push events in Phase A (D2).
7. Reload recovery using a **non-secret call id in sessionStorage** (D8).
8. The **PRD/HLD-18/HLD-19/CONSOLE-DESIGN scope changes** in D11,
   including the narrowed meaning of "live call controls" in Phase A.

## Open Questions

None that change the approach. The verification cadence (1 s vs 2 s) can be
tuned during review without changing the specs or the task breakdown.
