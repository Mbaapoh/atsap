## 0. Dependency

- [x] 0.1 Propose and apply `pbx-sip-registrar-settings`: the installation's public SIP registrar address (host, port, transport) in the public API, specified in `pbx-core`, configured per installation, and documented. Verify it through its own tiers (unit, wire, e2e) and that the console's regenerated clients typecheck. *(done 2026-10-13: proposed, applied in five task commits, and archived as `2026-10-13-pbx-sip-registrar-settings` with its spec synced into `pbx-core/extension-management`. `PbxService.GetSipRegistrar` returns the rig's localhost:5060/udp; the console clients are regenerated and typecheck.)*

## 1. Shared foundations

- [x] 1.1 Add `usePermissions()` (the session's membership roles → may administer) and the Phase A role catalogue (Agent, Supervisor, Tenant administrator; Platform administrator only for a platform administrator). Verify with component tests for every role, and that an unknown role renders by its server name. *(done 2026-10-13: `features/auth/permissions.ts` holds `ROLE_CATALOGUE` (Agent, Supervisor, Tenant administrator at tenant scope; Platform administrator at system scope), `roleLabel` (an unknown role keeps its server name), and `permissionsFor`/`usePermissions` (canAdminister, isPlatformAdmin, grantableRoles), reading the session only. The licence page and tenant picker now use it (one source of truth). 12 component tests: every role and combination, the current membership only, unknown roles. Component 197; e2e 17.)*
- [x] 1.2 Add offset pagination (50 per page; Next enabled only on a full page; no totals) and a row-actions menu for `DataTable`. Verify with keyboard and screen-reader-name tests, and that no total or page count is ever shown. *(done 2026-10-13: `components/Pager` shows "Page N" with Previous/Next, enables Next only after a full page of 50, never shows a total, and is absent for a single page. `components/RowActions` is a WAI-ARIA menu button named after its row: Arrow Down opens it, arrows, Home and End move, disabled items are skipped, Escape returns focus, destructive actions are listed last, and an outside click closes it. 5 component tests; component suite 202. **Found:** the hidden " for …" text lost its space in the accessible name, so it now uses an explicit `aria-label` beginning with the visible word.)*
- [x] 1.3 Add the `OneTimeSecret` panel:
  - copy controls and a required acknowledgement;
  - not dismissible, and in-app navigation blocked, until acknowledged;
  - `beforeunload` while unacknowledged;
  - the secret held in panel state only;
  - `data-sip-username` marking the one sanctioned engine-id element. *(done 2026-10-13: `components/OneTimeSecret`:
  - copy controls per value;
  - an acknowledgement checkbox, with Done disabled until it is ticked;
  - Escape and the backdrop ignored;
  - a React Router `useBlocker` that holds any in-app navigation while the panel is open, explains why, and lets it continue after Done;
  - `beforeunload` while open;
  - values gone from the page after close;
  - one `data-sip-username` element.

  `renderedSurface` now replaces that element's text before scanning, reads the whole document including hidden text (stricter), and still scans attributes.

  Verified:
  - 4 component tests (copy, cannot dismiss, navigation held then continued, unload prompt);
  - 1 browser test of the scanner (allowed inside the marked element; caught in other text and in an attribute);
  - component 206, e2e 18, rig exposure spec 2 of 2, secrets clean.

  **Found while building:** an initial focus on the disabled Done button would have been lost, and an acknowledgement tick released a held navigation early; both were fixed before commit.)*

  Extend `e2e/support/exposure.ts` to allow the engine-id pattern only inside that element. Verify with component tests (cannot close, cannot navigate, discarded on close) and with the scanner's allow and deny cases.

## 2. Users (HLD-18 A2)

- [x] 2.1 Users adapter and list: name, email, membership status and roles, paged. Verify the request names the session's Tenant, that the list matches `ListPrincipals`, and that there is no extension column (D-65). *(done 2026-10-13: `features/users/adapter.ts` (`useUsers(page)`: `ListPrincipals` for the session's Tenant with limit 50 and offset paging, mapped to `UserRow`: name, email, membership status, role grants) and `UsersPage` (table, status badges, role labels with unknown roles shown by server name, Pager, an empty state, plain refusal copy).

  Verified:
  - 4 component tests: tenant and paging in the request; fields, roles and status; an unknown role; no extension column; Next only after a full page; empty; 403 shown as console copy;
  - integration `users.rig.spec.ts`: the seeded administrator listed with their real role and status;
  - component 210, e2e 18, secrets clean.)*
- [x] 2.2 Add user: email, display name, role and an optional initial password, with the copy that an existing person keeps their own password. Verify `ProvisionPrincipal` is called once, the list refetches, and the copy reveals nothing about whether the email exists elsewhere. *(done 2026-10-13: `useAddUser` calls `ProvisionPrincipal` once; the mutation is reset and never cached; the list is refetched. `AddUserDialog` collects email, name, role (grantable roles only) and an optional initial password, with the hint "Leave empty to invite a new person by email. Someone who already has an account keeps their own password." The same words and confirmation apply whether or not the email exists elsewhere. The "Add user" action is shown to administrators only.

  Verified:
  - 5 component tests: administrators only (fault-injected); an exact request sent once, the list refreshed, and the password in no cache or page; an empty password not sent, so the server invites; validation before sending and an existing member explained at the email field; the platform role offered only to a platform administrator;
  - integration: an administrator adds a supervisor with an initial password, and that person signs in with it in another browser; no residue;
  - component 215, e2e 18, secrets clean.)*
- [x] 2.3 Grant and revoke roles with confirmation. Verify that revoking the last tenant administrator shows the server's refusal in plain words, and that the platform-administrator role is offered only to a platform administrator. *(done 2026-10-13: `useRoleChanges` (GrantRole and RevokeRole with the grant's scope; the list refetched, nothing optimistic); `GrantRoleDialog` offers only grantable roles the person does not hold; `RevokeRoleDialog` is a destructive confirmation naming the role and the person, with focus on Cancel. The last-administrator refusal (FAILED_PRECONDITION) reads "X is this tenant's last administrator. Make someone else a tenant administrator first." Row actions for administrators only.

  Verified:
  - 4 component tests: only unheld roles, sent at their scope; a confirmed revoke with an exact request; the last-administrator refusal in plain words with no raw text and no success toast; no actions for a supervisor;
  - integration against the real API: grant Agent, revoke it, then the server refuses to remove the last administrator, the console explains it, and the role stays;
  - component 219, e2e 18, secrets clean.)*
- [ ] 2.4 Disable and enable a membership, and reset a password (generated in the browser, shown once through `OneTimeSecret`). Verify the generated password meets 8–64 code points and never reaches a toast, URL, storage or later read, and that the reset person must change it.

## 3. Extensions (HLD-18 A3)

- [ ] 3.1 Extensions adapter and list: number, display name, device type and registration (Registered / Not registered, as observed), refreshed every 15 s. Verify each status renders its own badge, and that an unspecified status reads "Unknown", never "Not registered".
- [ ] 3.2 Create an extension and show the credential panel: the labelled SIP username, the secret and the registrar address. Verify the secret is absent from the list, the query cache, the mutation cache and the URL after acknowledgement; that a duplicate number is refused at its field; and that the Setup gate still applies.
- [ ] 3.3 Edit the display name and device type; rotate the secret (same panel); delete with a confirmation that states the phone stops working at once. Verify each mutation refetches and that rotation invalidates the old secret on the rig.

## 4. Trunks, routes, inbound and emergency (HLD-18 A4)

- [ ] 4.1 Trunks: list (name, host, port, channel limit, priority, observed health), create, edit and delete. The secret is write-only and requires a username. Verify the secret is never read back or rendered, and health is never a form input.
- [ ] 4.2 Outbound routes: list (prefix, trunk name, cost, priority), create and delete, with an overlap warning that explains the selection order; "replace" is a confirmed create-then-delete. Verify there is no Edit route action, and that the warning appears without blocking the save.
- [ ] 4.3 Inbound numbers: assign a DID to an extension, list and remove. Verify assignments name the extension by number and display name, and that removal is confirmed.
- [ ] 4.4 Emergency: the country profile and the effective emergency set, with built-in numbers marked, plus the create and delete controls the API supports. Verify built-in numbers cannot be deleted from the console, and that a tenant without a country is told so.

## 5. Call history (HLD-18 A5)

- [ ] 5.1 Paged history: time, direction, from, to, the actual Call state, duration and termination reason, with date-range, direction and state filters held in the URL and sent to `ListCalls` together. Verify the request carries all three filters, that states are verbatim (no Answered, Missed or Failed), that Back and reload keep the filters, and that there are no engine identifiers.

## 6. Proof

- [ ] 6.1 Integration specs against the real API for A2, A3, A4 and A5, with cleanup through the API and `pbx reconcile` clean afterwards. Verify they pass three times in a row with no fixture residue.
- [ ] 6.2 UAT journeys A2–A5 on the rig, including a real phone registering with the console-issued credentials (A3) and a real call appearing in the history (A5). Verify they run green in `make console-uat`.
- [ ] 6.3 Extend the exposure scans (rig and mocked) to every new screen, including the credential panel's sanctioned SIP username. Verify by fault injection: a secret left in a list, and the SIP username outside its element, are each caught.
- [ ] 6.4 Run every console tier (`make console-check`), ratchet the coverage floors, and record coverage and gaps in `coverage.md`. Verify the floors do not fall.
