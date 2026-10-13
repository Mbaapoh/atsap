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
- [x] 2.4 Disable and enable a membership, and reset a password (generated in the browser, shown once through `OneTimeSecret`). Verify the generated password meets 8–64 code points and never reaches a toast, URL, storage or later read, and that the reset person must change it. *(done 2026-10-13: `useMembershipStatus` (SetPrincipalStatus; a membership only, never the person, D-61) and `useResetPassword` (sent once, the mutation reset and never cached). The `temporaryPassword()` generator draws 16 characters from a 56-character unambiguous alphabet with rejection sampling (about 93 bits). `DisableDialog` is a destructive confirmation that says the person's other tenants are unaffected. `ResetPasswordFlow` confirms, generates, sets, then shows the password once in `OneTimeSecret`; on refusal it is discarded and never shown. Enable needs no confirmation.

  Verified:
  - 7 component tests: generator (alphabet, length, bounds, uniqueness, rejection of biased bytes); confirmed disable and direct enable with exact requests; the shown password equals the sent one and is nowhere afterwards (page, mutation cache, storage); a refused reset shows no password and says nothing about other tenants;
  - integration against the real API: reset a second person through the console, read the temporary password from the panel, sign that person in (must change); disable them, after which they cannot get in; no residue;
  - component 225, e2e 18, secrets clean.)*

## 3. Extensions (HLD-18 A3)

- [x] 3.1 Extensions adapter and list: number, display name, device type and registration (Registered / Not registered, as observed), refreshed every 15 s. Verify each status renders its own badge, and that an unspecified status reads "Unknown", never "Not registered". *(done 2026-10-13: `features/extensions/adapter.ts` (`useExtensions(page)` pages ListExtensions for the session's Tenant and re-reads every 15 s; `ExtensionRow` keeps the SIP username only for the credential panel) and `ExtensionsPage` (number, name, device in words, observed registration badge, Pager, empty state, refusal copy).

  Verified:
  - 5 component tests: fields and Registered / Not registered / Unknown (never "Not registered" for an unreported status); no SIP username anywhere in the list markup; the periodic re-read updates the badge; empty; 403 as console copy;
  - integration against the real API: an API-created extension listed as Not registered with the device in words, and the page scans free of engine ids;
  - component 230, e2e 18.

  The secret scan flagged a synthetic SIP username in a test fixture. SIP usernames are non-secret engine ids by design, so `quality/gitleaks.toml` now allowlists exactly that shape (`e_` + 32 hex); a planted AWS key beside one is still caught. The 2.4 note is corrected to the measured component count, 225.)*
- [x] 3.2 Create an extension and show the credential panel: the labelled SIP username, the secret and the registrar address. Verify the secret is absent from the list, the query cache, the mutation cache and the URL after acknowledgement; that a duplicate number is refused at its field; and that the Setup gate still applies. *(done 2026-10-13: `useRegistrar` (GetSipRegistrar; null when not configured) and `useCreateExtension` (CreateExtension; the mutation reset and never cached; the list refetched). `AddExtensionDialog` checks the number (2–20 digits) and the name (up to 100 characters) with the server's bounds, tells "Extension 1001 already exists in this tenant" apart from "The licence allows no more extensions", and is shown to administrators only. `CredentialsPanel` (`OneTimeSecret`) shows the SIP server, port, transport, the labelled SIP username (`data-sip-username`) and the SIP password, with a warning when the registrar is not configured or cannot be read.

  **API-layer refinement:** `ApiError` gains kind `exists` (ALREADY_EXISTS), so a duplicate and a failed precondition are no longer one kind; Add user and the mapping test are updated.

  Verified:
  - 5 component tests: an exact request; the panel's fields and marking; the secret gone from page, mutation cache, query cache and URL after Done; not configured; duplicate versus licence cap; validation before sending; administrators only;
  - integration against the real API, run twice: create through the console, the panel shows localhost:5060/UDP and a real `e_` username, the panel scans clean, then Not registered in the list with neither secret nor username left on the page; no residue; `pbx reconcile` clean;
  - component 235, e2e 18, secrets clean (one made-up base32 password allowlisted by exact value).

  The Setup gate still applies (existing licence tests).)*
- [x] 3.3 Edit the display name and device type; rotate the secret (same panel); delete with a confirmation that states the phone stops working at once. Verify each mutation refetches and that rotation invalidates the old secret on the rig. *(done 2026-10-13: `useExtensionChanges` (UpdateExtension, DeleteExtension, and RegenerateSecret returned once with the mutation reset and never cached; each refetches). Three row actions for administrators:
  - Edit… (name and device; the number is identity and not editable);
  - Rotate SIP password… (a confirmation that the current password stops at once, then the same one-time credential panel);
  - Delete extension… (a destructive confirmation that says the phone stops working at once and its inbound numbers are removed).

  The delete copy was checked against the server first: `DeleteExtension` removes the extension's DIDs in the same transaction and audits each removal, so the copy states exactly that.

  Verified:
  - 4 component tests: exact update request with no number field; rotate confirmation and the new password shown once, then gone from the page and mutation cache; delete confirmation copy with focus on Cancel and an exact request; an already-deleted extension explained;
  - integration against the real API, run twice: edit, rotate (the engine credential `ps_auths.md5_cred` changes, read via a digest-only helper) and delete (the engine row is gone); no residue; `pbx reconcile` clean;
  - component 239, e2e 18, secrets clean.)*

## 4. Trunks, routes, inbound and emergency (HLD-18 A4)

- [x] 4.1 Trunks: list (name, host, port, channel limit, priority, observed health), create, edit and delete. The secret is write-only and requires a username. Verify the secret is never read back or rendered, and health is never a form input. *(done 2026-10-13: the `features/telephony` adapter (`useTrunks`; `useTrunkChanges` for create, update and delete, never caching the secret and refetching all telephony). `TrunkDialog` handles create and edit with the server's field rules: a bare host, port 1–65535 or empty for 5060, a channel limit of 1 or more, a priority of 0 or more, a username charset, and a secret only with a username. On edit, an empty secret is not sent, so the stored one is kept, as the API documents. `DeleteTrunkDialog` says routes go too. `TrunksSection` shows authentication in words and observed health (never an input). `TrunksRoutesPage` replaces the placeholder. The trunk-health copy moved from the overview into telephony (one source).

  **Found and fixed:** dialogs did not scroll, so the seven-field trunk form pushed its actions off-screen. `.modal` now scrolls within the viewport. A new 360×640 e2e test fails without the fix.

  Verified:
  - 5 component tests: list wording and health; validation (scheme or port in the host; a secret without a username) and the exact create request, with the secret in no page or cache; edit with an empty secret not sent; delete copy and request; no controls for a supervisor;
  - integration against the real API, run twice: add with a secret, edit with the engine credential fingerprint unchanged, delete with its route gone and the engine auth row gone; `pbx reconcile` clean;
  - component 244, e2e 19, secrets clean.)*
- [x] 4.2 Outbound routes: list (prefix, trunk name, cost, priority), create and delete, with an overlap warning that explains the selection order; "replace" is a confirmed create-then-delete. Verify there is no Edit route action, and that the warning appears without blocking the save. *(done 2026-10-13:
  - **Adapter:** `useRoutes` and `useRouteChanges` (create and delete; there is no update in the API).
  - **`routing.ts`:** the prefix rule (digits and X, optional +, up to the server's 50 characters), `overlaps` (X matches any digit), and cost parsing to micros, exact with bigint, up to 6 decimals.
  - **`RoutesSection`:** prefix, trunk by name (never an id), stored cost and priority. The row menu is only "Replace…" and "Delete route…"; there is no Edit. "Add route" is disabled with a hint until a trunk exists.
  - **`RouteDialog`:** shows a non-blocking overlap notice naming the overlapping routes and the real order. The order is read from `compareCandidates`: healthy trunks before degraded ones, then longest prefix, then priority, then stored cost. Writing the note found that the design omitted the health step; the proposal and design are corrected.
  - **Replace:** confirmed, and it creates the new route before deleting the old. If the delete fails, the dialog says the new route exists and the old one must be removed.

  **Found and fixed:**
  - Console branch and function coverage had fallen below the D-71 floors since section 3 (79.3% branches at 4.1). This was the first full `console-check` after those tasks.
  - The fix is `admin-dialogs.test.tsx`: every admin dialog refused with each error kind, carrying an engine-laden server message, and shown only as console copy (33 cases). Every dialog closes on Cancel and on Escape sending nothing (13). Trunk and route field limits are covered too.
  - The 15 copies of `instanceof ApiError ? … : new ApiError("unknown")` now use the existing `toApiError`.
  - Console floors ratcheted to 95.7 / 87.3 / 97.9 / 96.9.

  Verified:
  - component 328 (route list, no Edit; the exact create request; the overlap notice shown while the save goes through; replace order `create` then `delete r1`; a failed replace; delete; no trunks; load failure; supervisor);
  - integration 21/21, including the new rig test: two routes added through the UI (the overlap notice is seen), the API listing exact prefix, trunk, cost 15000 micros and priority, a replace onto a second carrier, then a delete;
  - e2e 19, UAT 1, secrets clean.)*
- [x] 4.3 Inbound numbers: assign a DID to an extension, list and remove. Verify assignments name the extension by number and display name, and that removal is confirmed. *(done 2026-10-13:
  - **Adapter:** `useInboundNumbers` and `useInboundChanges` (create, delete). The list is not paged by the API.
  - **`useExtensionDirectory`** (extensions adapter) reads every extension in pages of 200, the server's maximum, keeping number and name only. It sits under the extensions key, so extension changes refresh it.
  - **`InboundSection`:** number, and the extension it rings as "1001 · Reception", never an id or SIP username. "Assign number" is disabled with a hint until an extension exists.
  - **`AssignNumberDialog`:** follows the server's number rule (digits with an optional +, up to 32). A number already taken is explained at the field. On the server "+237…" and "237…" are the same number, and the rig proves it.
  - **`RemoveNumberDialog`:** confirmed, Cancel focused, naming the extension that stops ringing and saying the extension keeps working.

  Verified:
  - component 343 (naming, never ids; directory paging across 200; the exact create request; validation; taken number; confirmed removal; no extensions; supervisor; load failure; both dialogs added to the refusal and Cancel/Escape tables);
  - integration 22/22: a new rig test assigns with +, the API stores digits, the + less duplicate is refused, then removal;
  - e2e 19, UAT 1, secrets clean;
  - floors ratcheted.)*
- [x] 4.4 Emergency: the country profile and the effective emergency set, with built-in numbers marked, plus the create and delete controls the API supports. Verify built-in numbers cannot be deleted from the console, and that a tenant without a country is told so. *(done 2026-10-13:
  - **Adapter:** `useEmergencyProfile` (`GetEmergencyProfile`, which gives country, catalogue status and the effective set in one call) and `useEmergencyChanges` (set country, add, remove).
  - **`EmergencySection`:**
    - No country set: told that only 112 is recognised.
    - A country with no catalogue: asked for the tenant's own numbers.
    - Otherwise: "Country: CM. Its emergency numbers are built in."
    - Each number is marked "Built in" or "Added by this tenant". Built-in rows get **no** row actions, so the console never offers their removal.
    - The footer states what the server really does, read from `telephony/application/service.go` and `pbx/application/placement.go`: emergency calls skip the licence and calling-rule checks and may exceed a trunk's channel limit, but they still need a matching outbound route. An earlier draft claimed otherwise and was corrected before commit.
  - **Dialogs:** set or change the country (ISO alpha-2, upper-cased); add a number (an already-built-in number is explained at the field); remove, confirmed, saying the licence and calling rules apply again.
  - **Rig cleanup** now removes the inbound and emergency rows of each seeded tenant (no cascade on those tables).

  Verified:
  - component 363 (built-in rows have no actions; no-country and not-in-catalogue states; exact set, add and remove requests; built-in and missing refusals explained; supervisor sees no controls; refused read; three dialogs added to the refusal and Cancel/Escape tables);
  - integration 23/23: a new rig test sets CM through the UI, sees 112 and 117 built in with no actions, has 117 refused, adds and removes 1500, and confirms the server itself refuses `DeleteEmergencyNumber 117`;
  - e2e 19, UAT 1, secrets clean;
  - floors ratcheted.)*

## 5. Call history (HLD-18 A5)

- [x] 5.1 Paged history: time, direction, from, to, the actual Call state, duration and termination reason, with date-range, direction and state filters held in the URL and sent to `ListCalls` together. Verify the request carries all three filters, that states are verbatim (no Answered, Missed or Failed), that Back and reload keep the filters, and that there are no engine identifiers. *(done 2026-10-13:
  - **`features/calls`:**
    - `filters.ts`: the address is the source of truth (`from`, `to`, `direction`, `state`, `page`). Unrecognised values are dropped, never sent, and a non-calendar day such as `2026-13-45` is rejected (`Date` would have rolled it over). The days are whole local days, sent as the half-open `[from, to+1)` that `ListCalls` takes.
    - `CALL_STATES` are the eight PRD §11.1 names as the server's `CallState` spells them.
    - `adapter.ts`: `useCallHistory`, a read-only `ListCalls` query keyed by the filters.
    - `copy.ts`: direction and duration words, moved here from the overview, which re-exports them. `serverText` shows server-written text verbatim, but withholds it ("Not shown") if it ever carries engine vocabulary.
    - `CallsPage`: the filter form follows the address on Back, Forward and reload. An end date before the start date is refused and nothing is sent. "Clear filters" empties the address. The pager keeps the filters. Duration shows "—" when the call was never answered.
  - **Termination reasons** are shown exactly as stored. The server's set, read from `telephony/application/service.go` and `domain/transitions.go`, is all domain phrases ("normal clearing", "capacity verdict rejected the call", "call setup did not complete", …).
  - The scaffold `RoutePlaceholder` is gone, since every screen is now real; `NotFound` moved to its own file.
  - **Rig support:** `seedCalls` writes call rows as the telephony core stores them. Cleanup removes them. `signIn` moved from two spec files into `support/rig.ts`, because importing it from a spec also ran that spec's tests.

  Verified:
  - unit and component tests: filters, the guard, the exact request with all three filters, verbatim states only, an opened address applied, Back restoring the filters, a refused range, clear, paging with filters kept, engine text withheld, a refused read;
  - integration 24/24: a new rig test seeds four calls and, over the real API, checks the list, the four filters together in the request and the address, reload and Back, and no Answered, Missed or Failed;
  - the hostile mocked exposure scan now renders a real `/calls` with an engine-laden termination reason and stays clean;
  - e2e 19, UAT 1, secrets clean, floors ratcheted.)*

## 6. Proof

- [x] 6.1 Integration specs against the real API for A2, A3, A4 and A5, with cleanup through the API and `pbx reconcile` clean afterwards. Verify they pass three times in a row with no fixture residue. *(done 2026-10-13:
  - **Rig specs:** A2 (`users.rig.spec.ts`, 4), A3 (`extensions.rig.spec.ts`, 3), A4 (`telephony.rig.spec.ts`: trunks, routes, inbound, emergency) and A5 (`calls.rig.spec.ts`).
  - **Cleanup:** fixtures the API can remove are removed through it: routes, trunks, extensions, inbound numbers. What the API has no delete for is removed by `seedUser`'s cleanup with the tenant: seeded call records, and a tenant's emergency country. The cleanup now also covers calls, inbound and emergency rows, since those tables do not cascade.
  - **New `make console-soak`** (`scripts/console-soak.sh`) runs the integration tier `RUNS` times (default 3). It fails unless 22 tenant-data and engine tables (identity, sessions, PBX, `ps_*`, calls, usage) hold the same row counts afterwards, then runs `atsap-api pbx reconcile` in the app container.

  Verified: `make console-soak` — 24/24 three times in a row, "No residue: 22 tables unchanged after 3 runs", and reconcile "No divergence: the engine matches the platform".)*
- [ ] 6.2 UAT journeys A2–A5 on the rig, including a real phone registering with the console-issued credentials (A3) and a real call appearing in the history (A5). Verify they run green in `make console-uat`.
- [ ] 6.3 Extend the exposure scans (rig and mocked) to every new screen, including the credential panel's sanctioned SIP username. Verify by fault injection: a secret left in a list, and the SIP username outside its element, are each caught.
- [ ] 6.4 Run every console tier (`make console-check`), ratchet the coverage floors, and record coverage and gaps in `coverage.md`. Verify the floors do not fall.
