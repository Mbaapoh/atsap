# Tasks — console-foundation

## 0. Scaffold milestone (reviewed before any screen is built)

- [x] 0.1 Initialise `console/` with exact D-68 dependencies, `mise.toml`, strict TypeScript, Vite, Vitest, Playwright and codegen; verify generated ConnectRPC output from the existing protos; create the `src/` skeleton, providers, route tree and CSS tokens; build and test the empty scaffold. *(done 2026-10-12 — see design §Dependency baseline and §Code generation; typecheck clean; 16 component tests; production build; browser smoke tests.)*

## 1. Foundation

- [ ] 1.1 Create the React + TypeScript application with generated ConnectRPC
  clients. Verify generated clients compile from the current protos with no
  hand-written RPC request/response types.
- [ ] 1.2 Implement semantic tokens, app shell, responsive navigation, focus
  management, status badges, alerts, table primitives, confirmation dialog,
  and toast. Verify keyboard-only navigation and WCAG AA contrast.
- [ ] 1.3 Implement an API client/feature adapter boundary. Verify no component
  imports raw transport details and no request targets a private endpoint.

## 2. Session and first-run

- [ ] 2.1 Implement DiscoverSignIn, SignIn, refresh-once, SignOut, password
  reset, password completion, and multi-Tenant selection. Verify credentials
  never enter browser persistence or URLs.
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
