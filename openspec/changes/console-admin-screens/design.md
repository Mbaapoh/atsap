## Context

`console-foundation` established the patterns these screens reuse:
- one feature adapter per feature, the only code holding a generated client;
- `ApiError` with fixed copy;
- view models;
- the session's Tenant on every request;
- `ConfirmDialog`, `DataTable`, `StatusBadge`, `EmptyState` and `Toast`;
- the adapter-boundary scan;
- the exposure scans (`e2e/support/exposure.ts`);
- the four test tiers.

The public API has every procedure the screens need (`IdentityService` principals and roles, `PbxService` extensions, trunks, routes, inbound and emergency, and `TelephonyService.ListCalls`), except the SIP registrar address, which `pbx-sip-registrar-settings` adds. See proposal.md for scope.

API facts that shape the design:
- `ListPrincipals` returns principals with their role bindings and nothing else; there is no role catalogue RPC.
- `ResetPrincipalPassword` takes the temporary password from the caller and returns nothing.
- `CreateExtension` and `RegenerateSecret` return the secret once; `Extension.auth_username` is the engine endpoint id (`pbx/domain/credential.go`), and `registration` is observed state.
- Routes have no update. Lists page by `limit` and `offset` with no total.
- `CallSummary` carries no extension number or name.
- Termination reasons are domain words ("normal clearing").

## Goals / Non-Goals

**Goals:**
- A partner walks HLD-18 A2–A5 in the console alone, with each screen honest about what the server reports.
- No secret or engine identifier escapes its one sanctioned place.
- The same proof depth as console-foundation on every screen.

**Non-Goals:**
- **Phase B items:** people ↔ extension assignment, agent controls, queues, per-person call history (D-65).
- **API additions** beyond the registrar address. A missing capability is a recorded gap, never invented in the browser.
- **Call detail (`GetCall`).** Its participants carry `endpoint_uri` (PROGRESS gap, `phase-a-hardening`); the history list is enough for A5.
- **Bulk actions, import and export.**

## Decisions

### 1. The role catalogue is the Phase A set, named in the console

There is no `ListRoles`. The grant picker offers the Phase A tenant roles that identity's `DefaultPermissions` defines: Agent (`AGENT`), Supervisor (`SUPERVISOR`) and Tenant administrator (`TENANT_ADMIN`), all at tenant scope.

`PLATFORM_ADMIN` (system scope) appears in the picker only for a person who is a platform administrator, because the server requires system scope to grant it. A role the console does not know is shown by its server name, never hidden or renamed.

*Alternative considered: a `ListRoles` RPC.* Rejected for Phase A: the catalogue changes only with identity's code; recorded as gap G1 for when custom roles arrive.

### 2. Visibility follows the session's membership; the server decides

A `usePermissions()` helper reads the current membership's roles:
- `TENANT_ADMIN` and `PLATFORM_ADMIN` see mutation controls;
- other roles see read-only screens.

A 403 still renders as "Your role cannot do this" (console brief §6 rule 6). It reads the session only, with no extra API call.

### 3. One-time secret panel

A shared `OneTimeSecret` modal is used for:
- the extension secret at creation and rotation;
- the temporary password from a reset.

It shows the values with copy controls and a required acknowledgement ("I have saved this"). Until acknowledged:
- it cannot be dismissed by Escape or the backdrop;
- in-app navigation is blocked by React Router's `useBlocker`;
- a browser unload prompts.

The secret lives only in that modal's state. The mutation that produced it is reset and never cached, as with the licence key (task 2.3 pattern), and closing the modal discards the secret.

For extensions, the panel shows:
- the **SIP username**, labelled as such;
- the **secret**;
- the **registrar address** from `pbx-sip-registrar-settings`.

The SIP username is marked `data-sip-username`. The exposure scan allows the engine-id pattern inside that element only, and nowhere else.

### 4. Temporary passwords are generated in the browser

`ResetPrincipalPassword` needs the caller to supply the password. The console generates it from `crypto.getRandomValues`: 16 characters from an unambiguous alphabet, about 80 bits, meeting the server's 8–64 bound. It is sent once and shown once through the `OneTimeSecret` panel for the admin to pass on, and the person must change it at next sign-in (D-64).

*Alternative considered: the admin types one.* Rejected: people pick weak ones.

### 5. Lists page by offset; "Next" only when a page is full

The API returns no totals, so pages are 50 rows with Previous and Next. Next is enabled only when a full page came back. The console never shows a fabricated total or page count. Lists refetch after any mutation.

The extensions list also refetches every 15 s while open, because registration is live device state.

### 6. Route overlap: warn and explain, never block

When a new route's prefix overlaps an existing one, the form warns and explains the real selection order: longest prefix first, then priority, then stored cost. It does not block the save. The warning is descriptive text over the routes already listed; it is not a prediction of which trunk a call will use.

Changing a route is an explicit "replace" flow: create the new route, then delete the old one. It is offered as two confirmed steps, never as "Edit".

### 7. Call history filters live in the URL

The date range, direction and state live in the address (`?from=…&to=…&direction=…&state=…`), so Back, reload and shared links keep them. They are not credentials, so this is allowed (console brief §6 rule 3 concerns secrets).

All three are sent to `ListCalls` together. The state filter offers the PRD §11.1 lifecycle names exactly as the server uses them. The extension column is omitted because `CallSummary` has none (gap G3).

### 8. Destructive actions are confirmed and never optimistic

Delete extension, delete trunk, delete route, remove a DID, revoke a role, disable a membership and reset a password each use `ConfirmDialog`, with a title that names the resource:
- "Delete extension 1001?";
- "Revoke Tenant administrator from Ana?".

The UI changes only after the server confirms. Deleting an extension states that the phone stops working at once (HLD-18 A3, deprovisioning is immediate).

## Risks / Trade-offs

- **[Risk] The hard-coded role catalogue drifts from identity's table.** → A component test compares the catalogue with the roles the identity rig returns in `ListPrincipals` for seeded memberships. Unknown roles still render by name.
- **[Risk] `useBlocker` does not stop a browser close.** → `beforeunload` is set while a secret is unacknowledged. The secret cannot be retrieved later in any case (server guarantee); rotation is the recovery.
- **[Risk] Showing the SIP username contradicts "no engine identifiers".** → It is the one labelled exception, by Asterisk's design (D-71 coverage G5), scoped to one marked element in the credential panel and the extension's detail, and enforced by the scanner.
- **[Trade-off] No totals in lists.** → An honest "Next" in place of a fabricated count.

## Migration Plan

Not applicable: these are new screens replacing placeholders, with no data migration. `pbx-sip-registrar-settings` must land first for the Extensions credential panel. The other screens do not depend on it.

## Open Questions

- The UAT for A5 needs a real call in the console's tenant. Whether it reuses the rig's `sip-ua` sidecar extensions or the baresip helpers from `scripts/uat` is decided at apply time; either way the call goes through the public API (`PlaceCall`).
