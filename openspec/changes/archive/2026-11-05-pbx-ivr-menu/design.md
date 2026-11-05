# Design — IVR menu execution

## D1. Same flow revision, more fields

The menu is part of the immutable revision, not a second flow object.
Publish/rollback already move the active pointer; a live call keeps the
revision it started on. Migration 0025 adds `menu` (JSONB digit→extension
id), timeout and fallback extension ids.

## D2. ARI execution, no dialplan

Telephony answers the caller, starts `Playback`, stores the playback id
and a timeout session, and routes on `ChannelDtmfReceived`. It stops the
prompt and the timer on a digit. No dialplan is generated (D-47); ARI is
the call-control surface. The prompt is `sound:<name>` in this slice;
uploads land with the media/recording storage slice.

## D3. Menu semantics

- Digits 0–9 only, one destination each; ten branches maximum.
- No input for the timeout takes the no-input destination; an unmatched
  digit takes invalid-input; either may be absent, in which case the call
  ends with a stable reason rather than silence.
- The prompt is required when a menu exists.
- A closed/holiday flow never enters the menu; it uses the closed
  destination (D-89).
- The chosen destination is attributed to its extension/owner exactly like
  a directly routed call.

## D4. Failure and invariants

A prompt/playback failure ends the call with `ivr_prompt_failed`; it never
falls through to a random destination. An IVR call is not cut off by a
configuration change: the active revision was captured at entry. A missing
menu destination is refused before publish, not at call time.

## D5. Prompt media

The first slice accepts a built-in `sound:` reference only, so no media
upload/storage decision is smuggled in. The console says so. Uploaded
prompts use the same object-store adapter as recordings in LLD-09.

## Live proof

ARI answer/play/DTMF is exercised by unit tests with the media-gateway fake
and by the inbound decision integration test. A real handset pressing a
digit is the manual UAT (`docs/uat/ivr-menu.md`); the browser/desk
e2e suites do not yet drive RTP DTMF.
