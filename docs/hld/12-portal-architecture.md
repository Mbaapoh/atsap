<!-- OpenSpec: TRD-HLD-14 -->
## [HLD] Portal Architecture

The AtsaPBX portal is a **consumer** of the ConnectRPC API, just like partner systems.

### 1. Technology Stack (per D-36)

| Component | Technology | Version | Why |
|---|---|---|---|
| Framework | React | 18+ | Rich, stateful UI; ConnectRPC TypeScript support |
| Language | TypeScript | 5+ | Type safety; generated from Protobuf |
| API Client | ConnectRPC | Latest | Generated from Protobuf; type-safe |
| Styling | Tailwind CSS + CSS Modules | Latest | Component styling; utility-first |
| Build Tool | Vite | Latest | Fast builds; hot reload |
| State Management | React Context + React Query | Latest | Client state; server state |
| Routing | React Router | v6 | SPA routing |
| WebRTC | Native WebRTC | — | Browser softphone |

### 2. Architecture Diagram

```
  +----------------+   +----------------+   +----------------+   +----------------+
  |    Admin UI    |   |    Agent UI    |   |  Supervisor    |   |    Partner     |
  |                |   |  (+ softphone) |   |  Dashboard     |   |    Portal      |
  +-------+--------+   +-------+--------+   +-------+--------+   +-------+--------+
          |                    |                    |                    |
          |  ConnectRPC (JSON over HTTPS, port 443) + WSS presence/telemetry feeds
          |                    |                    |                    |
          +--------------------+--------------------+--------------------+
                                               |
                                               v
                                  +-------------------------+
                                  |   ConnectRPC API (D-33) |
                                  |   telephony / pbx-core  |
                                  |   identity / licensing  |
                                  +------------+------------+
                                               |
                          WSS signalling       |       ARI / AMI (ACL)
                  +-----------------+          |
                  v                 |          v
  +--------------------------+      |  +------------------+
  |  Agent browser softphone |      +-->|    Asterisk      |
  |  native WebRTC, DTLS-    |           |    22.x LTS      |
  |  SRTP media via Asterisk |           |    PJSIP / RTP   |
  +--------------------------+           +------------------+
```

The browser softphone path bypasses the API for media: signalling travels over
WSS and audio over DTLS-SRTP direct to Asterisk (per [02](02-system-context.md)
§4), while call control, presence, and telemetry stay on ConnectRPC. The portal
holds no telephony state of its own — every capability it shows is reachable
through the same endpoints partners use (D-24 seam 3: API-first, no privileged
path).

### 3. Portal Surfaces

| Surface | Audience | Primary capabilities | Traceability |
|---|---|---|---|
| Admin UI | Tenant administrators | Extensions, SIP trunks, IVR builder, routing, users/roles | PRD EPIC-03, EPIC-05 |
| Agent UI | Contact-centre agents | Softphone, queue membership, pause/resume recording, wrap-up | PRD EPIC-03 |
| Supervisor Dashboard | Supervisors | Live wallboards, presence, listen/whisper/barge, abandonment gauges | PRD EPIC-03, EPIC-15 |
| Partner Portal | Partners | Licence issue/renew, deal registration, release downloads, support tickets, certification | PRD EPIC-10 |

Admin, agent, and supervisor surfaces consume real-time feeds (presence, RTCP-XR
quality, queue depth) over ConnectRPC HTTP/2 server streaming, the same
mechanism defined for telemetry in [07](07-observability.md).

### 4. API Consumption & Authentication

- TypeScript clients are **generated from the Protobuf schema** (D-33); the
  portal never hand-writes request shapes. `buf breaking` guards catch contract
  drift before the portal builds.
- Sessions use short-lived tenant-scoped JWTs; every request carries tenant
  context enforced by PostgreSQL RLS downstream ([05](05-security.md)).
- Permissions follow the scoped RBAC model (FBR-R1-08): a partner sees only
  their own customers' licences (PRD AC-10.4); an agent sees only their own
  queues and recordings per consent policy (PRD §7.1).
- Recording pause/resume, consent announcement, and deletion flows exposed in
  the UI call the same policy endpoints as the API — enforcement stays
  server-side, never advised client-side (PRD INV-09).

### 5. Deployment

- The portal builds to a **static SPA** served from CDN or nginx; no
  server-side rendering, no session affinity.
- Public ingress remains TCP 443 ([06](06-deployment.md)): ConnectRPC API, web
  admin UI, and WSS WebRTC signalling share the port.
- The portal is versioned and released with the platform (expand/contract
  upgrades per [06](06-deployment.md) T-12) but deploys independently — a
  portal rollout never drops active calls.
- Frontend dependencies are pinned (`package-lock.json`); no unapproved
  libraries per TOOLSET.md.

### 6. Verification

1. **No-privileged-path test:** every portal action is replayed as raw
   ConnectRPC calls with the same JWT; identical results prove the portal adds
   no backdoor (D-24).
2. **Contract test:** generated TypeScript clients compile against the current
   Protobuf schema in CI; `buf breaking` failures block the portal build.
3. **Isolation test:** a partner session cannot list another partner's licences;
   an agent session cannot fetch out-of-scope recordings (AC-10.4 pattern).
4. **Softphone loop test:** agent answers a browser call, audio flows
   DTLS-SRTP to Asterisk, ACL creates the Participant, and the supervisor
   wallboard reflects the call within the presence SLA.

### 7. Requirement Traceability

| Requirement | Portal section | How it is addressed |
|---|---|---|
| BRD §2.4 (API-first as commercial strategy) | §§1–5 | Portal is a pure ConnectRPC consumer; reference implementation partners copy |
| PRD EPIC-05 (API telemetry & webhooks) | §§2, 4 | Generated TS clients, HTTP/2 streaming feeds, no hand-written contracts |
| PRD EPIC-10 (Partner Portal, US-10.1–10.6) | §3 Partner Portal | Licence issue/renew, deal registration, downloads, tickets, certification |
| PRD EPIC-03 (browser calling & telephony) | §3 Admin/Agent/Supervisor | Admin UI, agent softphone, supervisor wallboards over ConnectRPC + WSS |

*Decisions: D-36 (React + TypeScript portal), D-33 (ConnectRPC sole ingress),
D-24 (API-first, no privileged path).*
