## Why

`console-foundation` delivered sign-in, sessions, the licence screen and the Overview. The four remaining Phase A screens (Users, Extensions, Trunks & routes, Call history) are placeholders.

HLD-18 §7 defines Phase A as done only when a partner can walk flows A1 → A5 without leaving the console, without touching a configuration file, and without being told what Asterisk is. Today they can walk A1 only. Every API these screens need already exists, except one: the address a phone registers to, which the dependency change `pbx-sip-registrar-settings` adds.

## What Changes

- **Users (A2):** the tenant's people and their memberships.
  - List: name, email, status, roles.
  - Add a user with email, display name, role and an optional initial password. The copy says that an existing person keeps their own password, and never reveals whether the email has an account elsewhere.
  - Grant and revoke roles. Revoking the last tenant administrator is refused, and that refusal is shown in plain words.
  - Disable and enable a membership.
  - Reset a password: the console generates a temporary password and shows it once, and the person must change it at next sign-in.
  - No extension column, agent controls or queues (D-65).
- **Extensions (A3):**
  - List: number, display name, device type and registration status (Registered / Not registered, as the server observes them).
  - Create, edit display name and device type, and delete with confirmation.
  - **One-time credential panel** at creation and on secret rotation: the SIP username (labelled as such; it is also the engine endpoint id, D-71 coverage G5), the secret, and the registrar address from `pbx-sip-registrar-settings`. It has a copy control and must be acknowledged before leaving. The secret never appears in a list, form, URL, history, toast or later read.
- **Trunks & routes (A4):**
  - Trunks: create, edit and delete. The secret is write-only; supplying one requires a username. Health is shown as the platform observed it.
  - Outbound routes: create, list and delete, with no "Edit route" because the API has no `UpdateRoute`. Overlapping prefixes are allowed, with a warning that explains the real selection order (longest prefix, then priority, then cost).
  - **Inbound numbers:** assign a DID to an extension, list and remove assignments.
  - **Emergency:** the country profile and the effective emergency set, with built-in numbers marked; create and delete only where the API allows.
- **Call history (A5):**
  - A read-only, paged list: time, direction, from, to, the actual Call state, duration and termination reason.
  - Date-range, direction and state filters, all sent to `ListCalls` together.
  - No invented states such as "Answered", "Missed" or "Failed", and no engine identifiers.
- **Across all four screens:**
  - Mutation controls are hidden from roles that may not use them, while the server still decides every request.
  - Every mutation is confirmed in plain words, and the list is refetched afterwards; nothing is optimistic for destructive actions.
  - Failures show console copy.
  - Every screen is added to the exposure scans of console-foundation 3.3.

## Capabilities

### New Capabilities

None. The console is an API consumer with no spec domain (`skip_specs`, HLD-19 §1).

### Modified Capabilities

None in this change. The registrar address is a `pbx-core` public-API addition, specified in its own change, `pbx-sip-registrar-settings`, which must be applied before task 3.x (Extensions).

## Impact

- **Console:**
  - feature adapters and screens in `console/src/features/users`, `extensions`, `telephony` (trunks, routes, inbound, emergency) and `calls`;
  - routes and navigation wired to them;
  - component, e2e, integration (rig) and UAT tests for HLD-18 A2–A5;
  - the exposure scan extended to every new screen.
- **API:** none here. `pbx-sip-registrar-settings` adds the registrar address (backend, proto, docs).
- **Dependencies:** none new (D-68).
- **Docs:** CONSOLE-DESIGN where behaviour is clarified, HLD-19, and PROGRESS.
