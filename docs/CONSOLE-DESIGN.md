# AtsaPBX Console Design

**Status:** Design input for `console-foundation` and `console-admin-screens`
**Updated:** 2026-10-11
**Applies to:** Phase A administration console

## Purpose and authority

This is the visual and interaction brief for the AtsaPBX administration
console. It gives the future React + TypeScript implementation a stable
product direction before components are written.

This document does not change a domain, API, permission, or Phase A scope
decision. If it conflicts with the following sources, they win:

1. `docs/DECISIONS.md`, especially D-36, D-43, D-46, D-65, and D-67.
2. `docs/hld/18-phase-a-user-flows.md`, which defines the Phase A flows and
   screens.
3. `docs/API.md`, which is the public API inventory.

The console is a public-API consumer, never a privileged back door. A screen
that needs a private endpoint is a signal to fix the API in its owning change,
not to bypass it.

## Product direction

The console is a calm, modern operational workspace for a PBX administrator.
It takes inspiration from two useful industry patterns without copying either
product:

- VitalPBX: a concise operational overview that makes current system state easy
  to scan.
- 3CX: direct, task-oriented configuration for users, connectivity, routing,
  and system administration.

AtsaPBX deliberately differs from the common “user equals extension” PBX
model. A **User** is a person who signs in. An **Extension** is a configured
phone line that a device registers to. They remain separate in Phase A;
ownership, agents, queues, and hot-desking are Phase B work (D-65).

The product promise is: a partner can install, licence, configure, and make
and receive real calls entirely in the console, without editing a file or
learning Asterisk terminology.

## Brand direction before a logo exists

There is no AtsaPBX logo yet. Phase A uses a temporary typographic identity
rather than an improvised icon system.

| Element | Direction |
| --- | --- |
| Product name | Render `AtsaPBX` as text in the application shell. Use sentence case, never an all-caps wordmark. |
| Temporary mark | Use a simple `A` monogram only for the collapsed navigation rail and favicon. It is a placeholder, not a logo to preserve. |
| Tone | Confident, practical, and human. Prefer “Trunk needs attention” to an unexplained protocol or engine error. |
| Primary colour | Deep leaf green for primary actions, selected navigation, and positive emphasis. |
| Neutral surfaces | Ink/navy navigation, warm off-white canvas, light panels, and cool-grey borders. |
| Semantic colours | Green = healthy/success; amber = attention/degraded; red = fault/destructive action; blue/grey = information. Colour never stands alone. |
| Typography | A highly legible sans-serif with tabular numerals for extension numbers, channel counts, dates, and call durations. |

Do not reproduce VitalPBX or 3CX logos, exact layouts, component styling,
copy, or iconography.

## Information architecture

The signed-in Tenant is always visible. The first release uses a left
navigation rail on desktop and a slide-out drawer on small screens.

```text
+--------------------+---------------------------------------------------+
| AtsaPBX            | Tenant: Acme Cameroon v       Licence: Valid 16  |
+--------------------+---------------------------------------------------+
| Overview           |                                                   |
| Users              |  Page title                         Primary action |
| Extensions         |                                                   |
| Trunks & routes    |  Content: summary, filters, table, or form       |
| Call history       |                                                   |
| ------------------ |                                                   |
| Account            |                                                   |
| Sign out           |                                                   |
+--------------------+---------------------------------------------------+
```

### Routes

| Route | Screen | Phase A intent |
| --- | --- | --- |
| `/sign-in` | Sign in | Discover the sign-in method, authenticate, select a Tenant where needed, and recover access. |
| `/licence` | Licence status and activation | Show entitlement to authenticated users; let only system-scope operators apply a licence key. |
| `/` | Overview | Help an administrator understand readiness and reach the next setup task. |
| `/users` | Users | List users, invite/provision, disable, reset passwords, grant and revoke roles. |
| `/extensions` | Extensions | List, create, edit, provision, rotate a secret, and delete extensions; show registration state. |
| `/trunks` | Connectivity and routing | Manage trunks, observed trunk health, outbound routes, inbound numbers, and emergency settings. |
| `/calls` | Call history | Paged and filtered Tenant call history, with New call and a live call session (Cancel/Hang up) for administrators. |
| `/account` | Account | Change password and sign out. |

`/licence` is the only usable destination after sign-in when the
installation is not activated. Other routes may remain visible for
orientation, but redirect there with an explanation rather than exposing
telephony configuration before activation.

### Navigation rules

- Primary navigation contains the five Phase A workflow screens: Overview,
  Users, Extensions, Trunks & routes, and Call history. Licence is reached
  from the top bar's status pill, not from the navigation (owner decision,
  2026-10-12). Account and sign out are utility actions, not workflow
  screens.
- Group connectivity and routing as one navigation item. Its page may use tabs:
  **Trunks**, **Outbound routes**, **Inbound numbers**, and **Emergency**.
- Do not add dormant menu items for queues, IVR, recordings, voicemail,
  agent state, live call control, or reporting.
- A platform/system-scope operator may see installation-management controls on
  the licence page. Tenant administration remains scoped to the selected
  Tenant.

## App shell and accessibility

### Desktop and small screens

- Use a persistent 240 px navigation rail, a 56–64 px top bar, and a content
  canvas with 24–32 px padding on desktop.
- Keep the primary page action in the page header. Secondary and destructive
  actions belong in a row menu or detail panel.
- The top bar shows current Tenant, licence state, and account menu, in that
  order. Licence state is a compact status pill, not a sales banner.
- On small screens, replace the rail with a menu button and drawer. Keep the
  page title and primary action visible.
- Preserve table data with horizontal scrolling before hiding columns. On very
  narrow screens, a table may become labelled row cards with the same fields
  and actions.
- Use full-screen sheets for create/edit forms below 768 px.

### Interaction baseline

- Meet WCAG 2.2 AA contrast and provide visible keyboard focus.
- Pair every colour-coded status with a text label and icon.
- Buttons use explicit verbs: **Apply licence**, **Invite user**, **Rotate
  secret**, **Delete route**.
- Destructive actions require a confirmation that names the affected resource.
- Preserve form values after recoverable request failures. Focus the error
  summary and first invalid field.
- Use local time with an explicit timezone in call history. Display durations
  consistently as `mm:ss` or `h:mm:ss`.

## Phase A screens

### Sign in

1. Ask for an email address.
2. Call `DiscoverSignIn` and render the allowed method without revealing
   whether the account exists.
3. Ask for password and call `SignIn`.
4. If memberships require a choice, show a Tenant picker before the console.
5. Offer **Forgot password?** using `RequestPasswordReset` and
   `CompletePasswordReset`.

Use one neutral failure message for an invalid password, unknown email, or
disabled account. Respect server-provided throttling and `Retry-After`.
When `GetSignInOptions` reports Google, show **Continue with Google**
before and after the email step (D-67). The browser leaves for Google and
returns to `/auth/google`; the server starts the session and sets the
normal cookie. An unlinked Google account gets the same neutral failure
as a wrong password.

### Licence status and activation

All authenticated users see the actual licence/capacity state, edition,
licensed channels, channels in use, Tenant and extension caps, expiry, last
confirmation, and a clear reason where applicable. The status pill shows
the server's `condition` exactly as `GetLicenseStatus` reports it:
**Setup**, **Free Community**, **Valid**, **Capacity Warning**, **Capacity
Reached**, **Entitlement Unverified**, or **Degraded**. The server picks
the most urgent when several apply (D-70, LLD-08 §6.1); the page also shows
the `tier`, `capacity` level and entitlement `state` beside it, and the
browser never computes any of them. **Suspended** is a tenant's status, not
a licence state, and a suspended tenant's users cannot sign in. The UI
never reduces these to an `activated: boolean` or labels a degraded
installation “Valid”. A system-scope operator also sees **Apply
licence**.

Use `GetLicenseStatus` to render this page and `ApplyLicenseKey` only
after clear confirmation. The key is secret: use a masked input (a
password field, which every browser masks; a licence key is one line)
with an explicit Show control that masks it again after 30 seconds;
never show it in a toast, URL, telemetry, or error. Refresh status in place
after success; do not promise a restart or interrupt a call.

### Overview

The overview is a setup and awareness surface, not a fabricated monitoring
dashboard. It shows only facts backed by public APIs:

- licence state and channels in use;
- setup checklist: licence active, first user, first extension, first trunk
  and route, first completed call;
- a compact recent-call list when `ListCalls` returns data;
- a compact trunk-health summary when `ListTrunks` returns data.

Do not display CPU, memory, arbitrary service health, live agent presence, or
an overall “health score” until a public API defines their meanings.

### Users

List display name, email, status, and role bindings. Row actions include
disable/enable where allowed, reset password, grant role, and revoke role. The
last tenant-administrator binding cannot be revoked; render that server
refusal in plain language.

The add-user flow asks for email, display name, role, and an optional initial
password. Explain that an existing person keeps their own password; do not
imply whether the email has an account elsewhere.

Do not show an Extension column, agent controls, queues, or per-person call
history in Phase A.

### Extensions

List extension number, display name, device type, outbound class, and
registration status. Each row shows its **Outbound** class; Add and Edit
offer Internal only, National (the default) and International. Emergency
numbers are never blocked by the class. National means numbers dialled
without a leading `+`; international is the `+` form (D-82).

Registration is device state, not a configuration toggle:

- **Registered:** a device has a current accepted registration.
- **Not registered:** the extension is configured and callable, but no device
  currently registers to it.

Creation displays a generated SIP secret exactly once in a secure confirmation
panel, with a copy control and acknowledgement before leaving. Secret rotation
uses the same one-time reveal pattern. Never put a secret in a list, edit
form, browser history, or notification.

No user ownership or assignment field appears until Phase B.

### Trunks and outbound routes

The **Trunks** section lists name, host, port, channel limit, priority, health
state, and status detail. If the API later returns an observation timestamp,
render it separately; never label a reason as “Last observed”. Health is
observed by the platform, never a form input. The create/edit form exposes
name, host, port, channel limit, priority, and optional digest username and
secret. Supplying a secret requires a username; the secret is write-only and
may be replaced but never read back.

The **Outbound routes** section lists prefix pattern, selected trunk, stored
cost, and priority. Overlapping prefixes are valid for least-cost routing.
Warn when an overlap exists and explain the real selection order: longest
prefix first, then priority, then stored cost. Do not block saving for overlap
alone. Phase A has Create/List/Delete route APIs, but no `UpdateRoute`;
therefore it offers no “Edit route” action. Replace a route through an explicit
delete-and-create workflow until an API change adds an update capability.

**Inbound numbers** assigns a carrier-provided DID to an extension using
`CreateInboundNumber`, lists assignments, and removes them with
`DeleteInboundNumber`. **Emergency** shows the effective emergency set and
country profile, then provides only the create/delete controls that the public
API supports. These controls are part of Phase A: a deployment cannot claim
to configure and receive real calls entirely in the console while hiding the
public inbound and emergency configuration APIs.

### Call history

A paged operational record plus the console's call actions (D-79). List time,
direction, from, to, extension number/name where available, actual Call state,
duration, and termination reason. Provide date-range, direction, and state
filters that map directly to `ListCalls`; all three filters must be sent to the
API and work together. Do not substitute invented states such as “Answered”,
“Missed”, or “Failed” for the Call lifecycle. Never display Asterisk endpoint
names, channel IDs, bridge IDs, or other engine identifiers. Do not label a
number as a user or agent: Phase A knows only Call Participants, numbers, and
extension names.

**A call is a session, not a toast.** The application shell carries a
persistent **call bar** on every signed-in route. It survives navigation and a
reload (the non-secret call id is kept in `sessionStorage`; never a token).
Its states are fixed copy:

| State | Heading | Detail | Primary action |
|---|---|---|---|
| Starting | Starting call | Asking the platform to place it | Cancel |
| Ringing caller | Ringing your extension 1001 | Answer your phone to continue. 1002 is not dialled yet. | Cancel call |
| Calling destination | Calling 1002… | Connected to 1001. Waiting for 1002 to answer. | Hang up |
| Connected | In call with 1002 | 00:12 · from extension 1001 | Hang up |
| Ended — timeout | Call ended — 1001 did not answer | 1002 was never dialled. | Dismiss |
| Ended — normal | Call ended — normal clearing | 00:42 · from extension 1001 | Dismiss |
| Failed | Call could not be placed | No extension or outbound route matches that number. | Dismiss |

Hang up is immediate: there is **no confirmation**. Before the caller answers
the action reads **Cancel call**, because no one else has been rung. The bar is
a named region with a polite live region; a new call moves focus to it. On
desktop it is a strip under the top bar; below 768 px it is a bottom sheet with
a full-width 44 px action.

**New call.** A global **New call** action in the top bar (and the Call-history
header) opens a dialer: a searchable **From** extension picker (number, name
and observed registration; unregistered options disabled), a large **To** field,
a dial pad, the last five distinct recent numbers from Call history, and one
primary **Call** action. It says, in words, “Your phone rings first. Answer it
to be connected.” The Call button is disabled until a registered extension is
chosen and the number matches the server rule (optional leading `+`, 1–32
digits). Immediate refusals are console words — no route, trunk at capacity, no
registered device, refused — never the server's text. While a call is live the
global action is disabled with “A call is in progress”.

**Live history.** While a call is in flight the list refetches every second and
shows a **Live · updated** pill; an Active row ticks its duration from the
answer time; Presenting and Active rows get a direct **Hang up** button, not
only a menu. The session bar remains the primary control. When the call ends
the row shows Terminated with the reason, and it is never shown as active after
that (PRD AC-03.9).

Agents and supervisors get these actions in Phase B, together with extension
ownership (D-65). Hold, mute, transfer, park and conference are Phase B; Phase A
has status and hang-up only.

Current API access is Tenant-wide for authenticated users. The UI must not
claim history is person- or agent-scoped; those views arrive with Phase B
reporting.

### Browser phone

A signed-in administrator holding `call.place` can open **Browser phone**
from the top bar (and `/phone`) and register a **WEBRTC** extension in the
browser. The form asks for the WEBRTC extension and its **SIP password**,
which is shown once when the extension is created or rotated; it is held
in memory only and cleared on reload, disconnect and Tenant change. The
installing operator supplies the signalling URL (`SIP_WEBRTC_URL`):
`ws://…` on the dev rig, `wss://…` in production.

The installer supplies the signalling URL (`SIP_WEBRTC_URL`) and the
STUN/TURN servers (`TURN_URLS`); the browser receives both through the
registrar. Host ICE still works on a normal NAT; TURN is required behind
a symmetric one (D-84).

The page states are: not configured / no WEBRTC extension / connect /
connecting / registered with dialler / incoming (Answer, Reject) /
calling / in call (Mute, Hang up) / ended (Dismiss) / connection failure.
The top-bar indicator names the state ("Browser phone", "Phone 1001",
"Incoming call", "Phone: in call") and always links back to the page. The
remote party is shown as the extension number and name when the directory
knows it, or "Unknown caller"; an engine endpoint id is never shown. Hold,
transfer, park and conference are Phase B. See D-81.

### Account

Allow the signed-in user to change their password, link a Google account
and sign out. Password change states that other sessions will end.
Linking asks for the current password, then leaves for Google; the
callback returns to `/account` (D-67). This page does not manage roles or
extensions in Phase A.

### Call flows

**Call flows** attaches a DID to a flow with an **open** and **closed**
destination, a UTC business window and a holiday calendar. Revisions are
published and rollbackable; the DID cannot also be a direct inbound
number. A revision may also carry a prompt, up to ten digit branches, a
timeout and no-input/invalid destinations (D-90); the prompt is a built-in
`sound:` reference until uploaded media lands.

### Reports

**Reports** is LLD-09's call-detail view: terminated calls only, filters
for date range, direction, extension, person and number, participant
roles with extension/person labels, duration and outcome, and **Export
CSV**. An agent sees only records where they are a participant;
administrators and supervisors see the tenant. Every export is audited.
The page never shows an engine endpoint (D-88).

### Outbound protection

**Trunks & routes** includes **Outbound protection**: daily and monthly
spend caps (micros, 0 unlimited), blocked destination prefixes, anomaly
threshold/window and UTC business hours, plus the suspension state and a
**Resume outbound calling** action. A suspension explains its reason;
calls in progress and emergency numbers are never affected. See D-85.

## React + TypeScript implementation guide

### Boundaries

- Generate TypeScript ConnectRPC clients from the same protobuf definitions as
  the server; do not hand-write request/response types.
- Keep RPC calls in feature-level API adapters, not visual components.
- The server remains authoritative for permissions, Tenant matching,
  validation, throttling, and licence state.
- Keep the access token in memory. Let the `HttpOnly` refresh cookie support
  silent refresh; never persist tokens in local or session storage, or a URL.
- On an expired access token, attempt the prescribed refresh flow once. On
  refresh failure, clear in-memory auth state and return to `/sign-in`.

### Suggested feature structure

```text
src/
  app/             route tree, providers, app shell
  features/
    auth/          sign-in, Tenant picker, session refresh, account
    licence/       status and activation
    overview/      setup checklist and small summaries
    users/         list, provision, role and status actions
    extensions/    list, forms, one-time secret reveal
    trunks/        trunk forms and observed health
    routes/        outbound route forms and overlap warning
    calls/         filters, paging, history table
  components/      reusable, domain-neutral UI primitives
  api/             generated clients and transport configuration
  styles/          design tokens and global accessibility styles
```

Feature components may depend on `components/` and generated `api/`
clients. Domain rules stay on the server: a UI overlap warning never replaces
route validation, and hiding an action never replaces authorization.

### State and feedback

- Use server-state caching with explicit invalidation after successful
  mutations. Refetch affected data rather than guessing final state locally.
- Do not optimistically apply a licence, rotate a secret, delete an extension,
  or revoke a role.
- Use inline field errors for validation and a page-level alert for request
  failures. Do not expose raw RPC, database, or Asterisk error text.
- Announce successful mutations with concise toasts and update the relevant
  table or status card in the same interaction.

## Design-system minimum

Define semantic tokens before page-specific CSS:

```text
color.canvas          color.surface          color.surface-muted
color.text            color.text-muted       color.border
color.action-primary  color.success          color.warning  color.danger
space.1 .. space.8    radius.sm / md         shadow.sm
font.body             font.mono-numeric      z.nav / z.modal / z.toast
```

Phase A reusable components:

- `AppShell`, `SideNav`, `TopBar`, `TenantSwitcher`,
  `LicenceStatusPill`;
- `PageHeader`, `PrimaryAction`, `EmptyState`, `StatusBadge`, `Alert`;
- `DataTable`, `FilterBar`, `Pagination`, `RowActions`;
- `FormField`, `SecretReveal`, `ConfirmDialog`, `Toast`.

Use status text that reflects the real domain state: **Registered**, **Not
registered**, **Healthy**, **Unhealthy**, **Valid**, or **Degraded**. Do not
collapse distinct domain states into generic green/red labels.

## Explicit Phase A exclusions

Do not mock, disable, or reserve navigation for:

- user-to-extension assignment;
- agents, queues, hot-desking, live presence, or supervisor wallboards;
- IVR, business-hours flows, voicemail, recordings, or quality analytics;
- CDR/reporting exports or person-scoped history;
- arbitrary server diagnostics and engine-facing data;
- MFA controls or Google sign-in configuration.

When a later phase supplies an API and a named consumer, add the screen in that
change and amend this document rather than pre-building inaccessible UI.

## Implementation acceptance checklist

The console is ready to demonstrate Phase A when:

- a partner completes A1 through A5 in the browser without a configuration
  file or Asterisk knowledge;
- every network request comes from a generated public API client;
- the UI never renders licence material, password material, SIP secrets after
  their one-time reveal, or engine identifiers;
- Tenant context is visible and never changes silently;
- User and Extension remain visibly separate concepts;
- all Phase A screens support keyboard navigation and small-screen layouts;
- browser end-to-end tests cover sign-in, licence activation, user/role
  management, extension registration visibility, trunk/route configuration,
  and read-only call history.

## Reference material

- `docs/hld/18-phase-a-user-flows.md` §§2–7 — authoritative workflow and
  screen scope.
- `docs/API.md` §1 — public RPC inventory.
- `docs/DECISIONS.md` D-36, D-43, D-46, D-65, D-67 — technology, API-first
  delivery, domain separation, and planned Google sign-in.
- [VitalPBX dashboard documentation](https://docs.vitalpbx.com/sonata-dialer/dashboard/)
  — operational overview inspiration.
- [3CX Administration Manual](https://www.3cx.com/docs/manual/) and
  [3CX SIP trunk configuration](https://www.3cx.com/docs/manual/sip-trunks/)
  — task-oriented administrative-flow inspiration only.
