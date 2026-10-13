# Coverage — console-admin-screens

The console is an API consumer with no delta specs (`skip_specs`, HLD-19 §1). This file maps the design's decisions and risks, the HLD-18 flows A2–A5, and the console brief's security rules to what proves them. The one API addition, the SIP registrar address, was its own change, `pbx-sip-registrar-settings` (archived, with its own coverage).

**Tiers** (`make console-check`, docs/TOOLSET.md §3a):
- **C**: component and unit tests (Vitest, jsdom, MSW).
- **E**: browser e2e with the API mocked (Playwright, Chromium).
- **I**: browser against the real API on the Tilt rig (`*.rig.spec.ts`).
- **A**: UAT, the HLD-18 journeys on the rig (`uat/*.uat.spec.ts`), with real baresip phones for A3–A5.
- **S**: `make console-soak`, the I tier three times in a row with a residue and engine-divergence check.

**Measured 2026-10-13** (all in one green `make console-check`):
- C: 386 tests;
- E: 22;
- I: 24;
- A: 3;
- S: 24/24 ×3, 22 tables unchanged, `pbx reconcile` "No divergence";
- `npm audit --omit=dev`: 0 vulnerabilities;
- gitleaks: no leaks.

Component coverage is 96.3% statements, 88.1% branches, 98.3% functions and 97.5% lines. It is gated by the floors in `quality/coverage-floors.json` (D-71), ratcheted after every task in this change.

| Area | Statements | Branches | Functions | Lines |
|---|---|---|---|---|
| `features/users` | 97.7 | 87.9 | 100 | 99.6 |
| `features/extensions` | 96.6 | 82.2 | 98.6 | 98.6 |
| `features/telephony` (trunks, routes, inbound, emergency) | 98.9 | 92.2 | 100 | 100 |
| `features/calls` | 100 | 90.0 | 100 | 100 |
| `components` (Pager, RowActions, OneTimeSecret, …) | 94.4 | 89.7 | 95.5 | 95.9 |

**Floors fell before this change's section 4.** Branch and function coverage had dropped below the D-71 floors during sections 2–3 (79.3% branches at task 4.1), because no task ran the full `console-check` in that stretch. Task 4.2 found it and restored coverage with `admin-dialogs.test.tsx`: every admin dialog refused with every error kind, and Cancel and Escape sending nothing. Every later task ratcheted. The tasks note it; nothing was lowered.

## HLD-18 flows

| Flow | Proven by |
|---|---|
| A2 Users and roles | **A** `a2-users.uat.spec.ts` (add a supervisor who signs in read-only, with the server refusing them too; promote; disable, after which they cannot sign in). **I** `users.rig.spec.ts` (4). **C** `users.test.tsx`, `temporaryPassword.test.ts` |
| A3 Extensions | **A** `a3-a5-phone-and-call.uat.spec.ts` (created in the console; a real phone configured only from the credential panel; shown Registered). **I** `extensions.rig.spec.ts` (3: observed registration; credentials once with the registrar; edit, rotate with the engine credential changed, delete with the engine forgetting it). **C** `extensions.test.tsx` |
| A4 Trunks, routes, inbound, emergency | **A** same UAT (trunk, route, inbound number and emergency country, all in the console). **I** `telephony.rig.spec.ts` (4: trunk with the secret kept on edit; routes with overlap, replace and delete; inbound with + normalisation; emergency with built-ins). **C** `telephony.test.tsx`, `routing.test.ts` |
| A5 Call history | **A** same UAT (a real call through the carrier, shown Terminated with a real duration and the server's reason). **I** `calls.rig.spec.ts` (filters together, address, reload, Back, verbatim states). **C** `calls.test.tsx`, `filters.test.ts` |

## Design decisions and risks

| Decision or risk | Proven by |
|---|---|
| D1 Phase A role catalogue in the console | **C** `permissions.test.tsx`: every role and combination; an unknown role keeps its server name; **the catalogue equals the keys of identity's `DefaultPermissions`**, a contract test that fails on drift in either direction (the design's mitigation, added in 6.4) |
| D2 Visibility follows the membership; the server decides | **C** a supervisor sees no controls on every admin screen. **A** `a2-users.uat.spec.ts` (403 from `ProvisionPrincipal` for the hidden action). **I** `users.rig.spec.ts` (last-administrator refusal) |
| D3 One-time secret panel; SIP username only in its marked element | **C** `OneTimeSecret.test.tsx` (acknowledgement, blocked navigation, beforeunload, nothing cached), `extensions.test.tsx`, `users.test.tsx`. **I** `exposure.rig.spec.ts` (panel scanned; SIP password absent from every page, request and response afterwards except `CreateExtension`). **E** `exposure.spec.ts` (fault injection: the marker removed, so the username sits outside its element; caught) |
| D4 Browser-generated temporary passwords | **C** `temporaryPassword.test.ts` (alphabet, length, rejection sampling); `users.test.tsx` (shown once, exactly as sent) |
| D5 Offset paging, "Next" only after a full page | **C** `users.test.tsx`, `calls.test.tsx` (with filters kept), `admin-primitives.test.tsx` (Pager) |
| D6 Route overlap warned and explained, never blocked; replace = create then delete | **C** `telephony.test.tsx` (the notice shown while the save goes through; replace order `create`, `delete r1`; a failed delete explained; no Edit). **I** `telephony.rig.spec.ts`. The copy states the server's real order, read from `compareCandidates`: health first (corrected in 4.2) |
| D7 Call-history filters in the URL | **C** `calls.test.tsx`, `filters.test.ts` (round trip; unknown and non-calendar values dropped). **I** `calls.rig.spec.ts` (reload and Back on the real API) |
| D8 Destructive actions confirmed, never optimistic | **C** every delete, revoke, disable, reset and remove test opens a confirmation titled with the resource; the extension-delete, revoke and number-removal tests also check that Cancel has focus and nothing was sent before confirming; `admin-dialogs.test.tsx` (Cancel and Escape send nothing, 18 dialogs; every refusal leaves the dialog open) |
| Risk: `useBlocker` does not stop a browser close | **C** `OneTimeSecret.test.tsx` (beforeunload set while open) |

## Console brief security rules (§6)

| Rule | Proven by |
|---|---|
| 6 Hiding is not enforcement | **A** `a2-users.uat.spec.ts` (403); **I** `users.rig.spec.ts`, `exposure.rig.spec.ts` |
| 7 No secret in a toast, URL, error, list or later read | **C** trunk secret absent from the page and the mutation cache; temporary password and SIP password gone after Done. **I** `exposure.rig.spec.ts` (new write-only class: a trunk secret only in `CreateTrunk`'s request). **E** `exposure.spec.ts` (fault injection: the one-time password in a list cell, then in an attribute; both caught) |
| 8 Extension secrets revealed exactly once, behind an acknowledgement | **C** `extensions.test.tsx` (create and rotate); **I** `extensions.rig.spec.ts`; **A** the UAT phone registers with it |
| 10 No engine identifiers, and no raw RPC, database or Asterisk errors | **C** `admin-dialogs.test.tsx` (every refusal carries an engine-laden server message and shows only console copy); `calls/copy.ts` `serverText` withholds engine vocabulary. **E** `exposure.spec.ts` (hostile reads on every screen, including inbound and emergency, and an engine-laden termination reason on a real `/calls`). **I** `exposure.rig.spec.ts` |
| No "Edit route" | **C** `telephony.test.tsx` (the menu is exactly Replace… and Delete route…) |
| Never invent Call states | **C** `filters.test.ts` (the eight PRD §11.1 names, Answered, Missed and Failed dropped from the address), `calls.test.tsx`. **I** `calls.rig.spec.ts`; **A** UAT (no Answered, Missed or Failed on screen) |

## Found while proving

- **Scanner blind spot (6.3).** `renderedSurface` used `textContent`, which runs adjacent elements together, so an engine id between two words escaped the word-boundary patterns. The fault injection exposed it. Each text node is now scanned on its own, with a self-test.
- **Route-order copy (4.2).** The first draft omitted the server's health step.
- **Emergency copy (4.4).** A draft claimed emergency calls need no route. Both are corrected against the Go source.
- **Non-calendar dates (5.1).** `2026-13-45` passed the date pattern because `Date` rolls it over into a later month. Days are now checked part by part.
- **Rig cleanup (4.4, 5.1, 6.2).** The inbound, emergency, call and placed-leg tables do not cascade from tenants. `seedUser`'s cleanup now removes them. Without that, a residue would have made the cleanup fail, and the soak run would have caught it.

## Gaps

| Gap | Why | Where it goes |
|---|---|---|
| G1 No `ListRoles` RPC | Phase A roles are fixed in identity's code; the contract test above stops silent drift | When custom roles arrive |
| G2 The rig cannot be put in Setup | It is licensed; the Setup gate is proven with a stateful mock (**E**) | Unchanged from console-foundation |
| G3 `CallSummary` carries no extension | The history omits the extension column rather than guess it | An API change, `phase-a-hardening` |
| G4 No call detail (`GetCall`) | Its participants carry `endpoint_uri`, an engine identifier | `phase-a-hardening` |
| G5 The SIP username is the engine endpoint id | By Asterisk's design; shown in one marked element only, enforced by the scanner | D-71 coverage G5 |
| G6 Inbound calls are not placed in the console UAT | A real inbound call is proven by `scripts/uat-inbound.sh` at the API level; the console proves the assignment | Unchanged |
| G7 Call records in `calls.rig.spec.ts` are seeded | The API has no call-creation path except a real call; the UAT proves a real one end to end | By design |
