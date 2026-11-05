# IVR menu execution

## Why

D-89 delivered time-based flow routing. A real auto-attendant needs a
prompt and DTMF. This change adds the menu on the same immutable flow
revisions and the same DID routing, without generating dialplan.

## What Changes

- A flow revision gains a prompt, up to ten digit branches, a timeout and
  no-input/invalid-input destinations.
- A published menu turns the open branch into an ARI-executed IVR:
  answer, play the prompt, collect one DTMF digit, then route to the
  chosen extension. Closed/holiday still uses the closed destination.
- Prompt and menu digits are validated before publish; destinations must
  exist.
- The console Call flows editor gains the prompt, menu, timeout and
  fallback destinations.
- Prompt uploads are not in this slice; the prompt is a built-in Asterisk
  `sound:` reference.

## Impact

- Migration 0025 menu columns on immutable flow versions.
- Flow proto/Go/TS/OpenAPI fields; no new RPC endpoints.
- Telephony ARI: answer, playback id/stop, DTMF event, IVR state timer.
- PBX inbound decision carries an IVR menu to telephony.
