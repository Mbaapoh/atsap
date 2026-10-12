# Design — console-foundation

## Context

This is the Phase A console slice described in HLD-18 and HLD-19. It is an
API consumer, not a bounded context; therefore there is no new numbered LLD
(D-46/D-48). This document is the implementation-level design for this
feature-sized OpenSpec change.

## Design

### Application shape

Use React + TypeScript (D-36) with generated ConnectRPC TypeScript clients.
Use a feature directory boundary:

```text
src/
  app/       routes, providers, shell, route guards
  api/       generated clients and transport
  features/
    auth/    discovery, sign-in, recovery, Tenant selection, account
    licence/ status and activation
    overview/ setup checklist and API-backed summaries
  components/ accessible domain-neutral primitives
  styles/    semantic tokens and global accessibility styles
```

Visual components never create raw HTTP requests or hand-written wire types.
They call a feature adapter that wraps a generated client.

### Session and Tenant boundary

Keep the access token in memory. The refresh secret remains an `HttpOnly`,
`SameSite=Strict` cookie on the same site as the API. A refresh retry occurs
once only. Selecting a membership establishes the current Tenant for the
token/session; all tenant-scoped requests use that authenticated scope.

### Routes and guard

- `/sign-in`: discovery, sign-in, recovery and Tenant selection.
- `/licence`: status for any authenticated user; application only for a
  system-scope operator.
- `/`: API-backed setup overview.
- `/account`: change password and sign out.

When licence state is Setup, route guards redirect telephony configuration
routes to `/licence` with a clear explanation. Unverified, Capacity Warning,
Capacity Reached, and Degraded states remain administrable; their actual
restrictions are shown, never guessed by the browser.

### Error handling

Connect errors map to accessible, action-oriented messages. Validation errors
remain near their fields. Authentication failure, throttle, permission denied,
and tenant mismatch follow the server contract; no response reveals account or
other-Tenant existence.

### Security

No access/refresh token, password, licence key, or SIP secret reaches browser
persistence, URLs, logs, telemetry, toasts, or an error message. The licence
key input is masked by default with an explicit reveal action.

## Testing

- Component tests: route guards, permission visibility, state badges, keyboard
  navigation, mobile drawer focus handling, and error presentation.
- Browser E2E: password sign-in, multi-Tenant choice, refresh/sign-out,
  password reset request, Setup gate, system-scope licence application, and
  forbidden-action absence.
- Network assertion: every browser request targets a generated public
  ConnectRPC procedure; no private console endpoint exists.

### Dependency baseline (D-68), verified 2026-10-12

Resolved from the npm registry on 2026-10-12, installed with
`npm install --save-exact`, locked in `console/package-lock.json`; `npm
audit` reported 0 vulnerabilities at install.

| Package | Exact version | License |
|---|---|---|
| react, react-dom | 19.2.8 | MIT |
| react-router | 8.4.0 | MIT |
| @tanstack/react-query | 5.104.2 | MIT |
| @connectrpc/connect, @connectrpc/connect-web | 2.2.0 | Apache-2.0 |
| @bufbuild/protobuf | 2.16.0 | Apache-2.0 AND BSD-3-Clause |
| @bufbuild/protoc-gen-es | 2.16.0 | Apache-2.0 |
| typescript | 7.0.2 | Apache-2.0 |
| vite | 8.3.4 | MIT |
| @vitejs/plugin-react | 6.1.2 | MIT |
| vitest | 4.1.11 | MIT |
| jsdom | 29.1.1 | MIT |
| @testing-library/react | 16.3.3 | MIT |
| @testing-library/dom | 10.4.2 | MIT |
| @testing-library/user-event | 14.6.7 | MIT |
| @testing-library/jest-dom | 7.0.1 | MIT |
| @types/react, @types/react-dom | 19.2.18, 19.2.7 | MIT |
| msw | 3.0.3 | MIT |
| @playwright/test | 1.64.0 | Apache-2.0 |

Newer majors or minors exist and were deliberately not adopted: React
19.3.0 (released on the verification day), Vitest 5.0.4, jsdom 30.1.2.
Runtime: Node 24.21.0 and npm 11.20.0 through `console/mise.toml`.

### Code generation

One local plugin, no second generator: `protoc-gen-es` 2 emits each
service's descriptor (`GenService`) alongside its messages, and Connect 2
builds typed clients from those descriptors directly. `console/buf.gen.yaml`
generates from `../api` (the same `api/proto` module the server is built
from) into `src/api/gen/`, which is committed like the Go stubs.
`src/api/clients.test.ts` proves the descriptors drive real calls through
MSW.

### Serving (D-69)

One origin in every environment: the Vite dev server proxies
`/atsapbx.v1.*` to `localhost:8080`; production uses a reverse proxy
(`console/deploy/Caddyfile.example`, `console/DEPLOYMENT.md`) with SPA
history fallback and a strict CSP. The build has no inline script.

### Licence states (D-70)

The licence screen (task 2.3) needs the server to report Free Community,
Capacity Warning and Capacity Reached. That is a small licensing change,
proposed before task 2.3; the console never derives a state.

## Dependency-guard result

**PASS.** The change introduces only a frontend consumer. It adds no Go import,
database table, NATS subject, engine adapter, or backend runtime dependency,
and cannot create an edge between bounded contexts. Its npm packages are the
D-68 baseline. It complies with HLD-04
§10.1 and D-43 by consuming the existing public API only.
