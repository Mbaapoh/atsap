<!-- OpenSpec: TRD-HLD-19 -->
# Console UI Architecture

**Status:** Phase A implementation design
**Scope:** The administration console slice defined by D-46 and HLD-18
**Technology:** React + TypeScript (D-36)

## 1. Purpose and boundary

This HLD defines how the AtsaPBX administration console consumes public
capabilities. It is not a new bounded context and does not own domain state.
The console is a named consumer of `identity`, `licensing`, `pbx-core`, and
`telephony-core` APIs. D-43 and PRD AC-05.1/AC-10.7 prohibit a private console
endpoint.

The agent workspace is a separate product (PRD principle 2). This HLD covers
only administration and partner-facing configuration flows.

## 2. Phase A outcomes

The console enables the five HLD-18 journeys:

| Flow | Console responsibility | Public API owner |
| --- | --- | --- |
| A1 | Sign in, choose Tenant, see/apply a licence, change/recover password | identity, licensing |
| A2 | Provision/list users; manage status and role bindings | identity |
| A3 | Configure extensions; show observed registration state; reveal secrets once | pbx-core |
| A4 | Configure trunks, outbound routes, inbound numbers, and emergency settings; show observed health | pbx-core |
| A5 | Show read-only, filtered and paged Call history | telephony-core |

Phase A excludes User-to-Extension assignment, agents, queues, IVR, voicemail,
recording, reporting, live call controls, and partner commerce. Those receive
their own console slice alongside the Phase B/C capabilities that own them.

## 3. Logical architecture

```text
+---------------------------- Browser -----------------------------+
| routes -> feature views -> feature adapters -> generated clients |
|          |                 |                                    |
|          +-- app shell ---+-- in-memory access-token state       |
+---------------------------|--------------------------------------+
                            | same-site HTTPS only
+---------------------------v--------------------------------------+
| Public ConnectRPC API                                         |
| AuthService | IdentityService | LicensingService | PbxService  |
| TelephonyService                                               |
+---------------------------------------------------------------+
```

Generated protobuf clients are the only network boundary used by UI features.
Feature views do not construct URLs, parse wire JSON, or hand-write response
types. Between them sits one feature adapter per feature
(`features/<name>/adapter.ts`): the only code that holds a generated client.
It maps wire messages to view models, owns the feature's query keys, and
turns every failure into an `ApiError` carrying fixed console copy by kind
(unauthenticated, forbidden, not found, conflict, invalid, throttled,
unavailable, unknown) — never the server's message, so no internal name or
raw error reaches the page. The transport refuses, before sending, any
request that is not a procedure of a public service on the console's own
origin; the procedure list is derived from the generated descriptors.
These rules are enforced, not conventional: a source scan fails the build
when a component imports the transport or clients, opens a request, or
hand-writes a procedure path, and a browser test fails when any page
requests something off-origin or outside the public API. Browser state is presentation state; all validation, authorization,
Tenant isolation, mutation auditing, screening, and configuration projection
remain server responsibilities.

## 4. Security and session model

- The access token lives only in memory.
- The refresh secret stays in the `HttpOnly`, `SameSite=Strict` refresh cookie;
  neither credential is written to local storage, session storage, URLs, logs,
  or analytics.
- A single refresh attempt may follow an expired access token. Failure clears
  client auth state and returns the user to sign-in.
- The selected Tenant comes from authenticated memberships. It is displayed
  persistently, and every tenant-scoped request carries the selected Tenant ID.
  A tenant mismatch is a server refusal, never a client-side switch.
- Controls are hidden when the current permission set does not allow the
  action, but the server remains the enforcement point (PRD AC-10.8).
- Licence keys, password values, and SIP/trunk secrets never appear in a
  toast, URL, telemetry, error, list, or later read. Extension secrets have a
  deliberate one-time reveal. A licence key uses a masked input with an
  explicit temporary reveal control.
- No UI string, error, table, or export exposes Asterisk configuration,
  endpoint names, channel identifiers, bridge identifiers, or filenames
  (PRD principle 4; AC-10.6).

## 5. UI composition and state

```text
AppShell
├── Authentication boundary
│   ├── Sign-in / recovery / Tenant selection
│   └── Session refresh and sign-out
├── Navigation boundary
│   ├── Overview
│   ├── Users
│   ├── Extensions
│   ├── Connectivity and routing
│   ├── Call history
│   └── Account (utility link)
├── Top bar: Tenant · Licence status pill (links to /licence) · account
└── Feature boundaries
    ├── identity      users, roles, account
    ├── licensing     status, activation
    ├── pbx-core      extensions, trunks, routes, DIDs, emergency
    └── telephony     Call history
```

Feature-local server-state queries are cached and invalidated after a successful
mutation. The UI does not optimistically apply a licence, rotate a secret,
delete a resource, revoke a role, or change an emergency number. It refetches
the authoritative resource after success.

## 6. Truthful state presentation

| Domain state | UI rule |
| --- | --- |
| Licence/capacity | Show the server's `condition` as the status pill, exactly as reported (D-70): Setup, Free Community, Valid, Capacity Warning, Capacity Reached, Entitlement Unverified or Degraded. The detail view also shows the `tier`, the `capacity` level and the entitlement `state`, so a headline that wins by precedence never hides another fact. Setup gates call configuration; Unverified and Degraded explain their distinct impact; Capacity Warning and Reached are not conflated. Suspended is a tenant status, not a licence state. |
| Extension registration | `Registered` or `Not registered` is observed device state, never an editable toggle. |
| Trunk health | `Healthy`, `Degraded`, and `Unhealthy` are observed state, never an input. A reason is a status detail, not a timestamp. |
| Call lifecycle | Render API Call states and termination reason. Derived labels are supplementary and never replace lifecycle state. |
| Pending configuration | A successful response is shown as live only when the API reports it live. Otherwise render the real pending state (PRD AC-10.9). |

## 7. Accessibility and responsive requirements

- Meet WCAG 2.2 AA contrast; colour is accompanied by text and icon.
- All functions are keyboard reachable with visible focus and semantic labels.
- Tables preserve all information through horizontal scrolling, then labelled
  compact cards on narrow screens.
- The 240 px desktop navigation rail becomes a focus-managed modal drawer at
  small widths. Create/edit forms become full-screen sheets below 768 px.
- Request failures use an accessible summary and field-level errors. Focus
  moves to the summary/first invalid field; entered data remains after a
  recoverable failure.

## 8. Phase A acceptance evidence

Browser E2E proves A1–A5 through the running public API. At minimum it proves:

1. session lifecycle, Tenant selection, licence-state rendering, and
   system-scope-only key application;
2. User provisioning, role grant/revoke, last-administrator refusal, and
   status change;
3. extension create/edit/delete, registration presentation, and one-time
   secret flow;
4. trunk create/update/delete, observed health, long-prefix route explanation,
   route create/delete, inbound number assignment, and emergency settings;
5. Call-history pagination and combined date/direction/state filters;
6. a wire/render scan proving no credential or engine identifier reaches the
   browser.

## 9. Traceability

- PRD principles 1–5; EPIC-01, EPIC-03, EPIC-05, EPIC-06, EPIC-10;
  AC-05.1, AC-10.5–AC-10.9.
- D-36 (React + TypeScript), D-43 (public APIs), D-46 (per-phase console),
  D-65 (User/Extension separation), D-67 (Google sign-in later).
- HLD-18 flows A1–A5; `docs/API.md` current surface.
