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
- [ ] 2.2 Implement account password change and session-end feedback. Verify
  must-change credentials reach only the permitted actions.
- [ ] 2.3 Implement licence status and guarded activation. Verify every
  licence/capacity state has its own truthful UI; a tenant administrator cannot
  apply a key; no key appears in rendered output or error text.

## 3. Overview and proof

- [ ] 3.1 Implement the API-backed readiness checklist and compact licence
  summary. Verify no health metric or call status is fabricated.
- [ ] 3.2 Add browser E2E for A1, including Setup gate, successful licence
  application, session recovery, Tenant selection, and responsive navigation.
- [ ] 3.3 Add an API-traffic assertion and browser-render scan proving that no
  credential or Asterisk/engine identifier is exposed.
- [ ] 3.4 Record coverage and any remaining evidence gap in `coverage.md`.
