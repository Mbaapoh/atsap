# Coverage — console-foundation

The console is an API consumer with no delta specs (`skip_specs`, HLD-19 §1). This file maps the design's testing commitments, and the console brief's security rules, to what proves them.

**Tiers** (`make console-check`, docs/TOOLSET.md §3a):
- **C**: component tests (Vitest, jsdom, MSW).
- **E**: browser e2e with the API mocked (Playwright, Chromium).
- **I**: browser against the real API on the Tilt rig (`*.rig.spec.ts`).
- **A**: UAT, the HLD-18 journey on the rig (`uat/*.uat.spec.ts`).

**Measured 2026-10-13:**
- C: 185 tests;
- E: 17;
- I: 12;
- A: 1;
- `npm audit --omit=dev`: 0 vulnerabilities.

Component coverage is 93.9% statements, 85.2% branches, 96.1% functions and 94.4% lines. It is gated by floors in `quality/coverage-floors.json` (D-71).

## Design testing commitments

### Component tests

| Commitment | Proven by |
|---|---|
| Route guards | **C** `routes.test.tsx` (every Phase A route; unknown address inside the shell; sign-in outside it); `session.test.tsx` (signed-out → sign-in and back, several memberships → Tenant choice, failed refresh → sign-in with reason); `account.test.tsx` (must-change reaches only /account, every route); `licence.test.tsx` "Setup gate" (extensions and trunks to /licence; Degraded, Unverified, Capacity Reached stay administrable; unreadable status does not gate) |
| Permission visibility | **C** `licence.test.tsx` (Apply licence offered to a platform administrator only; a server 403 shown plainly). **I** `licence.rig.spec.ts` (a tenant administrator is offered nothing and the server refuses ApplyLicenseKey with 403) |
| State badges | **C** `components.test.tsx` (StatusBadge, Alert roles); `LicenceStatusPill.test.tsx` (every condition has its own words and tone); `licence.test.tsx` (each of the seven conditions and unknown has its own headline and explanation; Degraded is never called Valid; unknown reason codes never shown) |
| Keyboard navigation | **C** `routes.test.tsx` (focus moves to the new page's heading and is announced). **E** `shell.spec.ts` (Tab order with visible focus ring, skip link), `scaffold.spec.ts`, `licence.spec.ts` (applying a key by keyboard alone) |
| Mobile drawer focus handling | **C** `components.test.tsx` (Modal: focus trap, Escape, page inert, focus restored; fault-injected). **E** `shell.spec.ts` "phone" (drawer from the keyboard, trap, inert page, Escape; choosing a page lands on its heading) |
| Error presentation | **C** `api/boundary.test.ts` (every failure becomes fixed console copy by kind; the server's text is never kept); `session.test.tsx` (neutral wrong-password message, Retry-After honoured, expired link); `licence.test.tsx` (unverified key explained at the field, without echo). **E** `exposure.spec.ts` (engine-laden server messages never reach the screen) |

### Browser e2e

| Commitment | Proven by |
|---|---|
| Password sign-in | **I** `auth.rig.spec.ts`; **A** `a1-first-login.uat.spec.ts`; **E** `a1.spec.ts` (×3 widths) |
| Multi-Tenant choice | **I** `auth.rig.spec.ts` (choose, reload keeps it, switch); **E** `a1.spec.ts` |
| Refresh / sign-out | **I** `auth.rig.spec.ts` (reload restores through the HttpOnly cookie; sign-out clears it; signed out stays signed out), `account.rig.spec.ts` (a password change ends the other browser's session); "a refresh interrupted in flight does not end the session" (identity-refresh-grace) |
| Password reset request | **I** `auth.rig.spec.ts` (Mailpit email → `/reset#…` → secret stripped from the address → new password; link single-use; old password refused) |
| Setup gate | **E** `a1.spec.ts`, `licence.spec.ts` (stateful mock: the rig cannot be in Setup, gap G2) |
| System-scope licence application | **A** `a1-first-login.uat.spec.ts` (the rig's real key); **E** `a1.spec.ts`, `licence.spec.ts` |
| Forbidden-action absence | **I** `licence.rig.spec.ts` (no Apply licence for a tenant administrator; the server refuses anyway); **C** `licence.test.tsx` |

### Network assertion

| Commitment | Proven by |
|---|---|
| Every request targets a generated public procedure; no private endpoint | **C** `api/boundary.test.ts` (procedure list derived from descriptors; a hand-built call to an undefined procedure is refused before sending); `test/boundary.test.ts` (seven source rules, each with a planted violation). **E** `boundary.spec.ts` (every page's requests stay on-origin and inside the public API; a planted `fetch("/healthz")` fails it) |

## Console brief security rules (§6)

| Rule | Proven by |
|---|---|
| 1–3 Tokens in memory only; refresh secret in the HttpOnly SameSite=Strict cookie; nothing in storage, URLs, logs | **C** `session.test.tsx` `noCredentialLeft` after sign-in, Tenant choice and reset (fault-injected: a token in sessionStorage). **I** `auth.rig.spec.ts` (cookie flags and path; storage empty; fault-injected: a token in localStorage) |
| 4 Refresh at most once after expiry | **C** `session.test.tsx` "refresh once after expiry" (shared by concurrent calls; never twice; failure returns to sign-in; fault-injected) and `account.test.tsx` (ChangePassword never retried) |
| 5 Tenant scope from the session; the server decides | **C** `overview.test.tsx` (every read names the session's Tenant). **I** `auth.rig.spec.ts` (switching Tenant) |
| 6 Hiding is not enforcement | **I** `licence.rig.spec.ts`, `account.rig.spec.ts` (the server refuses what the console hides) |
| 7 No secret in toast, URL, telemetry, error or later read | **C** `licence.test.tsx` (the key absent from page, query cache and mutation cache; fault-injected). **I** `exposure.rig.spec.ts` (password, licence key, refresh secret and access tokens scanned across all traffic and every rendered page) |
| 8 Extension secrets revealed once | Not yet built: extension screens are `console-admin-screens` |
| 9 Licence key masked with a temporary reveal | **C** `licence.test.tsx` (password field; Show re-masks after 30 s; fault-injected). **E** `a1.spec.ts`, `licence.spec.ts` |
| 10 No Asterisk identifiers or raw errors | **I** `exposure.rig.spec.ts` (engine-backed data on the wire; nothing on screen; fault-injected twice). **E** `exposure.spec.ts` (fault-injected). **C** `api/boundary.test.ts` |

## Also proven

- **Readiness checklist (3.1):** each step's rule, "Could not check" never Done, Call states verbatim, no invented metrics. Covered by **C** `overview.test.tsx` (fault-injected) and **I** `overview.rig.spec.ts`.
- **Session-end feedback (2.2):** the five-minute warning, ending past the time, and a confirmed sign-out. Covered by **C** `account.test.tsx`.
- **Contrast:** every token pair is checked against WCAG AA by **C** `contrast.test.ts` (fault-injected).
- **Layout:** no sideways scroll at 390 and 360 px with a long tenant name, checked by **E** `a1.spec.ts`, `shell.spec.ts` and `licence.spec.ts`.

## Gaps

| # | Gap | Severity | Note |
|---|---|---|---|
| G1 | No console linter: the approved ESLint set cannot parse TypeScript 7 | Medium | Oxlint with oxlint-tsgolint proposed (D-71, Open); `quality-and-supply-chain-gates` task 3.1 |
| G2 | The Setup gate is proven against a stateful mock only | Low | The rig cannot be in Setup (the app applies its licence token at startup). The gate is a pure read of the server's condition, and the server side is proven in `licensing-status-conditions` |
| G3 | Browsers: Chromium only | Medium | Firefox and WebKit are not run. The console uses standard DOM and CSS only (no `<dialog>`, no experimental APIs). Add the Playwright projects when a pilot names a browser |
| G4 | Assistive technology tested through roles, names, focus and contrast, not with a real screen reader | Medium | A manual NVDA/VoiceOver pass belongs to the pilot readiness checklist |
| G5 | An extension's SIP username is its engine endpoint id | Accepted (by design) | Allowed only in `authUsername` on the wire; the extension screens must show it labelled, where a phone is configured (`console-admin-screens`) |
| G6 | `GetCall` returns `Participant.endpoint_uri` | Medium | The console does not call `GetCall`. Tracked in PROGRESS for `phase-a-hardening` |
| G7 | "Complete a call" searches the latest 50 ended calls | Accepted | Can only under-report (To do), never claim Done falsely; the rule is stated on screen |
| G8 | The SSO branch of sign-in is proven against a mock only | Low | No provider is configured on the rig; Google sign-in is `identity-oidc-google` (D-67) |
| G9 | The bundle is one 499 kB chunk (150 kB gzip) | Low | Route-level code splitting when the admin screens land |
| G10 | The Jenkins console and quality stages have not run in Jenkins | Medium | They call the same make targets that pass locally; first pipeline run pending |
