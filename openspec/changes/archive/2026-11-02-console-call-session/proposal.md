## Why

The first click-to-call implementation works at the API but not as a
product. After **Place a call** the dialog disappears and a six-second
toast is the only feedback; Call history is never refreshed, so a call
that is ringing, connected or ended keeps showing `Presenting` until the
page is reloaded. The only Hang up is buried in the row's overflow menu,
and nothing survives navigating to another screen. The owner's three test
calls on 2026-11-02 all ended `caller_no_answer` after 30 s, exactly as
designed, but the console never said "your phone is still ringing", never
offered a cancel, and never showed why the call ended.

3CX and VitalPBX both treat a call as a **session**: a persistent call
panel with a live state, a running timer and a one-click Hang up, and a
dialer that is available from anywhere in the product. This change makes
the AtsaPBX console do the same, within Phase A's scope (a real phone
does the media; there is no browser softphone yet).

## What Changes

- **A persistent call session bar** in the application shell, visible on
  every screen and surviving navigation, with state-specific copy and a
  one-click **Hang up** / **Cancel call**.
- **Honest live state**: the session polls the call while it is in flight
  and changes within 2 s (PRD §14). Ringing the caller, calling the
  destination, connected with a running timer, ended with the reason, and
  failure are all distinct, visible states.
- **A redesigned New call experience**: a global **New call** action
  available from any page, a searchable extension picker with observed
  registration, a dial pad, recent numbers, and a single primary Call
  action. The dialog explains "your phone rings first".
- **Live Call history**: while a call is in flight the list refreshes, a
  Live indicator is shown, the row's state and duration update, and
  Presenting/Active rows get a direct Hang up button (not only a menu).
- **Hang up is immediate**, with no confirmation, as in every VoIP
  client. Before the caller answers the action is labelled **Cancel call**
  because no one else has been rung yet.
- **The call query becomes complete**: `GetCall` carries the started,
  answered and ended timestamps and the recorded termination reason, so
  the console can say "1001 did not answer" or show a live duration
  without reading the whole history.
- **Docs and scope**: PRD EPIC-03 and §14, HLD-18 flow A5, HLD-19 §§2/5/6/7
  and CONSOLE-DESIGN gain the Phase A call-session behaviour; the next D-number records
  the decision. Hold, transfer, park and conference remain Phase B.

**This is a proposal. No implementation starts until the owner approves
the decisions in `design.md` ("Approval needed").**

## Capabilities

### New Capabilities

None. The session panel is console behaviour, documented in PRD,
HLD-19 and CONSOLE-DESIGN; the public behaviour it needs is expressed as
changes to the two existing capabilities below.

### Modified Capabilities

- `pbx-core/call-placement`: a placed call is observable and can be ended
  at any point in flight, including while the caller's extension is still
  ringing; the timeout reason and the final state are queryable; an ended
  call is never reported as active.
- `telephony-core/call-lifecycle`: the public call query reports the
  call's recorded timestamps and termination reason, with no engine
  identifier.

## Impact

- **API (Go, protobuf):** `GetCallResponse` gains `started_at`,
  `answered_at`, `ended_at` and `termination_reason` (additive; generated
  TypeScript clients regenerated). No new RPC.
- **Console (React/TypeScript):** a `CallSessionProvider` and shell-level
  call bar; a redesigned New call dialog; live Call-history refresh and
  row Hang up; new copy and states; component, mocked-browser, rig and UAT
  coverage.
- **Docs:** PRD EPIC-03 acceptance criteria and §14; HLD-18 A5 and the
  Phase A scope table; HLD-19 Phase A outcomes/state presentation/
  accessibility; CONSOLE-DESIGN; `docs/API.md`; DECISIONS (a new D-number on approval); a UAT
  procedure; PROGRESS/HANDOFF on archive.
- **Dependencies:** none. Polling uses the existing ConnectRPC client.
- **Out of scope:** browser softphone/WebRTC (#8), hold, transfer, park,
  conference, multiple simultaneous click-to-call sessions, and agent
  "call from my phone" (D-65, Phase B).
