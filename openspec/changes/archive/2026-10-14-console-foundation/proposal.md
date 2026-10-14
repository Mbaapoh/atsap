# Change: Console foundation

## Why

All Phase A backend capabilities now exist, but a partner cannot yet complete
HLD-18 flows A1–A5 in the browser. PRD EPIC-10 requires the console to use the
same public APIs as partners and to hide all Asterisk implementation detail.

## What changes

- Create the React + TypeScript console foundation: generated ConnectRPC
  clients, same-site session handling, Tenant selection, route guard, app
  shell, responsive navigation, semantic design tokens, and accessible shared
  components.
- Deliver Sign in, password recovery/completion, Account, Licence status and
  activation, and Overview. Licence presentation preserves the API/PRD state;
  Setup is the only no-call-path state.
- Establish testable feature boundaries for subsequent Users, Extensions,
  Connectivity & routing, and Call history screens.

## Non-goals

- User and Extension administration, trunks/routes/DIDs/emergency forms, and
  Call history screens; these follow in `console-admin-screens`.
- New backend RPCs, database schema, domain rules, Go dependencies, or changes
  to any bounded-context package. (The console's npm packages are the D-68
  baseline.) The licence states the console needs from the server (D-70) come
  from a separate licensing change.
- Google sign-in, MFA, queues, IVR, agents, recording, reporting, and partner
  commerce.

## Impact

- New frontend application only, consuming existing public Auth, Licensing,
  Identity, PBX, and Telephony APIs.
- Browser E2E adds evidence that the console has no private back door.
- No dependency-graph edge or context ownership changes.
