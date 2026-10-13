# Tasks — console-foundation

## 0. Scaffold milestone (reviewed before any screen is built)

- [x] 0.1 Initialise `console/` with exact D-68 dependencies, `mise.toml`, strict TypeScript, Vite, Vitest, Playwright and codegen; verify generated ConnectRPC output from the existing protos; create the `src/` skeleton, providers, route tree and CSS tokens; build and test the empty scaffold. *(done 2026-10-12 — see design §Dependency baseline and §Code generation; typecheck clean; 16 component tests; production build; browser smoke tests.)*

## 1. Foundation

- [ ] 1.1 Create the React + TypeScript application with generated ConnectRPC
  clients. Verify generated clients compile from the current protos with no
  hand-written RPC request/response types.
- [x] 1.2 Implement semantic tokens, app shell, responsive navigation, focus
  management, status badges, alerts, table primitives, confirmation dialog,
  and toast. Verify keyboard-only navigation and WCAG AA contrast. *(done 2026-10-12 — `components/`: StatusBadge, Alert (role and focus-on-mount for error summaries), PageHeader, EmptyState, DataTable (named, keyboard-scrollable region; no column hidden), Modal (focus trap, Escape, page made inert, focus restored; hand-built because jsdom has no `<dialog>`), ConfirmDialog (explicit verb, Cancel first when destructive, busy disables both), Toast (polite region, dismissible, pauses while read); shell with TopBar (Tenant · licence pill · account) and a modal drawer below 768 px; `LicenceStatusPill` maps the server's condition to words, icon and tone. **Owner decision:** Licence leaves the main navigation for the top-bar pill; HLD-19 and CONSOLE-DESIGN updated. Verified: 64 component tests including a contrast gate over the real token file (fault-injected: a low-contrast muted text fails it) and the focus trap (fault-injected: removing Tab handling fails the dialog and drawer tests); 9 Playwright tests at 1280 px and 390 px: Tab order with a visible focus ring, skip link, drawer keyboard use with the page inert, no sideways scroll. **Found by the browser tests:** the menu button showed on desktop (a later, equally specific `.icon-button` rule won); fixed.)*
- [x] 1.3 Implement an API client/feature adapter boundary. Verify no component
  imports raw transport details and no request targets a private endpoint. *(done 2026-10-12 — `api/publicApi.ts` derives the public procedure list from the generated descriptors; the transport's `publicOnly` interceptor refuses anything else before sending; `api/errors.ts` turns every failure into an `ApiError` with fixed copy by kind and keeps no server text; `features/licence/adapter.ts` is the first adapter (view model, query keys, `useLicenceStatus`). Enforced by `src/test/boundary.ts`: seven rules over the source tree, each with a broken case that must trip it. Verified: 99 component tests; fault-injected on the real tree (a `fetch` in the shell and a client in a shared component, each caught by its rule); Playwright visits every page and fails on any off-origin, probe or non-public request (fault-injected: a startup `fetch("/healthz")` fails it); a hand-built call to an undefined procedure is refused without reaching the network.)*

## 2. Session and first-run

- [x] 2.1 Implement DiscoverSignIn, SignIn, refresh-once, SignOut, password
  reset, password completion, and multi-Tenant selection. Verify credentials
  never enter browser persistence or URLs. *(done 2026-10-12 — `features/auth/`: in-memory `SessionStore`; `SessionController` (the auth adapter) restores from the cookie on load, signs in, chooses and switches Tenant through RefreshSession, refreshes once per expiry for all concurrent callers and across tabs (Web Locks), signs out even when the server is unreachable, and empties the query cache on every change of scope; the transport's `refreshOnce` repeats a refused call once. Screens: two-step sign-in (one neutral failure message, server Retry-After honoured, SSO explained rather than offered), Tenant choice and switch link in the top bar, no-access state, forgot password (same answer for every address), `/reset` and `/invite` pages that strip the fragment secret from the address. Route guard: signed-out → sign-in (returning afterwards), several memberships → choice, must-change → account page only. Verified: 124 component tests, including no storage/cookie/address residue after sign-in, Tenant choice and reset; fault-injected (unshared refresh, fragment left in the address, a token written to sessionStorage — each caught). 13 Playwright tests, 3 of them against the real API on the Tilt rig: sign-in → choice → reload restores the Tenant through the HttpOnly/SameSite=Strict cookie scoped to AuthService → switch → sign-out clears the cookie; a wrong password and an unknown email get identical answers; reset through the Mailpit email, link single-use, old password refused, new one signs in. Fault-injected: a token in localStorage fails the rig spec. **Found by the rig spec:** the server answers an unusable link with INVALID_ARGUMENT, not UNAUTHENTICATED as the mocks assumed (now handled and documented in HLD-19 §4); repeated runs tripped the server's source throttle, so the fixture now clears the throttle rows it touched. Dev reset links now open the console on :5173.)*
- [x] 2.2 Implement account password change and session-end feedback. Verify
  must-change credentials reach only the permitted actions. *(done 2026-10-12 — Account page: change password (current or temporary, new, confirm; checked before sending: required, 8–64 code points, different, matching) and sign out. `SessionController.changePassword` renews a token near expiry first, then renews the session after the change so a must-change token becomes a normal one; the transport never retries ChangePassword (a wrong current password is UNAUTHENTICATED too). Guard: a must-change session reaches only /account. Session end: the shell warns five minutes ahead with the time and *Stay signed in*; past the time it asks the server and, refused, returns to sign-in saying the session ended; a chosen sign-out is confirmed as such. Verified: 145 component tests; fault-injected (ChangePassword retried, the must-change gate removed, no renewal before change — each caught). 17 Playwright tests, 7 against the real API: admin `reset-password` temporary password → the server refuses ListExtensions with it (403) → the console shows only the account page → a wrong temporary password is shown at its field → the change opens the console and survives a reload; a change in one browser ends the other browser's session; sign-out is confirmed. **Found by the rig spec:** a refresh interrupted in flight ends the session (server reuse detection, D-62) — reproduced deterministically by a test marked expected-fail, recorded as a High open gap pending an owner decision on a grace window.)*
- [x] 2.3 Implement licence status and guarded activation. Verify every
  licence/capacity state has its own truthful UI; a tenant administrator cannot
  apply a key; no key appears in rendered output or error text. *(done 2026-10-12 — Licence page: the server's condition as a headline with its own explanation for each of Setup, Free Community, Valid, Capacity Warning, Capacity Reached, Entitlement Unverified, Degraded and unknown; facts (tier, entitlement state, capacity level, edition, channels in use of licensed, tenant and extension caps, expiry, last confirmation, a worded reason — an unknown reason code is never shown). Top-bar pill fed by the server on every page, refreshed every 30 s, "Licence unknown" when unreadable. Setup gate: extensions and trunks lead to the licence page with the reason; every other condition stays administrable; an unreadable status does not gate. Apply licence for a platform administrator only: a masked key (password field, autocomplete off) with a Show control that re-masks after 30 s, an explicit confirmation, status replaced from the server's answer and refetched, key cleared and the mutation reset so it is not kept in the query cache. **Design note:** a password field replaces the "masked multiline input" (CONSOLE-DESIGN updated): every browser masks it natively and a key is one line. Verified across every tier: 173 component tests (fault-injected: key kept in the mutation cache, gate removed, Apply shown to every role, raw reason shown, reveal never re-masked — each caught); e2e 13 (Setup gate, keyboard-only apply with the key masked, 390 px layout); integration 9 against the real API (real status values; a tenant administrator offered nothing and refused 403 by the server; a bad key refused with the licence untouched); UAT 1, HLD-18 A1 from the console onward: platform administrator signs in, applies the rig's real key, sees it confirmed, telephony unlocked. Rig left licensed (Valid, 16 channels), no fixture residue. New: the console's four test tiers as Playwright projects and make targets (`console-e2e`, `console-integration`, `console-uat`, `console-check`).)*

## 3. Overview and proof

- [x] 3.1 Implement the API-backed readiness checklist and compact licence
  summary. Verify no health metric or call status is fabricated. *(done 2026-10-13 — Overview: compact licence summary (the server's condition, channels in use of licensed); a setup checklist whose five steps each state their rule and read one public API fact for the session's Tenant:
  - licence in force (`GetLicenseStatus`, not Setup);
  - a user besides the first administrator (`ListPrincipals`, at most 2 read);
  - an extension (`ListExtensions`);
  - a trunk and a route (`ListTrunks` + `ListRoutes`);
  - a completed call (`ListCalls` state Terminated, any answered among the latest 50).

  A fact that cannot be read shows "Could not check", with "your role cannot see this" when refused, never Done. Recent calls (latest 5) show the server's Call state verbatim and an answered duration; trunk health shows the server's observed health and appears only when trunks exist. No CPU, memory, uptime or health score. Checklist links are named by purpose ("Go to Extensions"), and table captions differ from their section names, so landmarks and links are unambiguous.

  **Found and fixed:** `AppShell` focused the page heading on first load under React's development double effects, which the Tab-order test now catches.

  Verified across every tier:
  - 185 component tests, 12 of them for the overview; fault-injected (an unreadable fact counted as done, a call state relabelled "Answered"), each caught;
  - e2e 13;
  - integration 10, including the overview against the real API: a new tenant's real state, then an extension created through the public API flips its step to Done and is deleted through the API, with `pbx reconcile` clean afterwards;
  - UAT 1.

  Console coverage floors ratcheted to 93.8 / 85.1 / 96 / 94.3.)*
- [x] 3.2 Add browser E2E for A1, including Setup gate, successful licence
  application, session recovery, Tenant selection, and responsive navigation. *(done 2026-10-13 — `e2e/a1.spec.ts`: one continuous HLD-18 A1 journey against a stateful mock of the public API (the rig cannot be put in Setup: the app applies its licence at startup), at 1280, 390 and 360 px, navigating by the rail on desktop and the drawer on phones:
  - a neutral answer to a wrong password, then sign-in;
  - choosing between two tenants;
  - Setup shown by the pill and the checklist, and Extensions gated to the licence page;
  - a masked key applied after confirmation, with the gate lifting;
  - a reload restoring the session in the chosen tenant;
  - no storage, readable cookie or key left in the page, and no sideways scroll on phones.

  The real-API counterpart is `e2e/uat/a1-first-login.uat.spec.ts`.

  **Found and fixed:** at phone widths with two memberships, the top bar (tenant, Switch, licence pill) pushed the page 40–133 px sideways. Below 768 px the pill now drops its channel count (still in its accessible name), the tenant name shrinks with an ellipsis through a continuous flex chain, and below 420 px the brand gives way to the tenant name (still in the drawer). Checked by screenshot at 360 and 390 px. The overflow assertion failed at every width before the fix.

  Verified: `make console-check`, with 185 component tests, e2e 16, integration 10, UAT 1 and audit 0.)*
- [x] 3.3 Add an API-traffic assertion and browser-render scan proving that no
  credential or Asterisk/engine identifier is exposed. *(done 2026-10-13 — `e2e/support/exposure.ts` scans for:
  - engine endpoint ids (`e_`/`t_` + 32 hex);
  - `PJSIP/…`-style dial strings;
  - Asterisk channel unique ids;
  - bridge ids;
  - `ps_*` realtime tables;
  - `.conf` filenames;
  - Stasis contexts;
  - the session's credentials: password, licence key, refresh-cookie value and every access token sent.

  The rendered surface means visible text plus every attribute and form value. In traffic, a password may go only to SignIn, ChangePassword or CompletePasswordReset, a licence key only to ApplyLicenseKey, and none of them may come back in a response or appear in a URL. Access tokens may appear only in the Authorization header.

  **Design point recorded:** an extension's SIP username *is* its engine endpoint id (Asterisk matches a REGISTER by endpoint id; `pbx/domain/credential.go`), and a person configuring a phone must be given it. It is therefore allowed in exactly one place, the API's `authUsername` field, and nowhere on screen until the extension screens show it, labelled, where a phone is configured.

  `exposure.rig.spec.ts` (real API): a platform administrator, with an extension, a trunk and a route projected into Asterisk, walks every page and is refused a bogus licence key. All traffic and every page scan clean. The scan is shown not to be vacuous: every pattern fires on a sample, and the engine-backed responses did cross the wire.

  `exposure.spec.ts` (mocked, for CI): engine text in server error messages, a termination reason and fields the console does not show never reaches any page.

  Fault-injected, each caught:
  - the access token in a `data-` attribute (rig);
  - a real extension's engine id in the page title (rig);
  - a raw termination-reason column (mocked).

  Verified: `make console-check`, with 185 component tests, e2e 17, integration 12, UAT 1 and audit 0. No fixture residue, and `pbx reconcile` reports no divergence.

  Known and recorded elsewhere: `GetCall` still returns `Participant.endpoint_uri`, and the console does not call it (PROGRESS gap, `phase-a-hardening`).)*
- [ ] 3.4 Record coverage and any remaining evidence gap in `coverage.md`.
