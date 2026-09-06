---
name: c4-diagrams
description: Draw C4 architecture diagrams (Context, Container, Component, Deployment) for AtsaPBX. Use when a change needs an architecture sketch, or when asked to diagram system structure.
---

Draw C4 diagrams grounded in the HLD — every box traces to a documented
element. Never invent components, containers, or dependencies the HLD does
not contain; a proposed new box is marked `PROPOSED` and must land in a
design doc, not presented as existing.

**Lens:** the solution architect visualizing structure — every box defended,
every arrow priced.

## Level → source mapping

- **L1 System Context** ← `docs/hld/02-system-context.md` (C4 context,
  external actors: partners, carriers, AI providers, browsers, desk phones).
- **L2 Container** ← `02-system-context.md` container topology +
  `docs/hld/01-architecture.md` (Go services, Asterisk media engine,
  Postgres, NATS, portal SPA) + `docs/hld/06-deployment.md` (ports,
  networks: 443/TCP API+WSS, 5061 SIP/TLS, UDP 10000–20000 RTP).
- **L3 Component** ← `docs/hld/04-bounded-contexts.md` (the 9 bounded
  contexts, port contracts, allowed-dependency matrix) + TRD ports &
  adapters + the single Asterisk anti-corruption layer (D-15/D-16/D-20).
  Aggregates and ports only — no per-entity repositories, no event store,
  no speculative services (D-31; D-08: nothing over-built for year-three
  scale).
- **Deployment** ← `docs/hld/06-deployment.md` (compose topology, Ansible,
  expand/contract upgrades) + `docs/hld/12-portal-architecture.md` §5
  (static SPA via CDN/nginx). Backing services (Postgres, NATS, Asterisk,
  carriers) are drawn as attached resources, never embedded boxes
  (12-factor IV); processes are stateless with no session affinity and
  restart within their hld/09 budget (12-factor VI/IX).

## Method (industry C4, project-grounded)

1. Fix the level first — one diagram, one level. Drilling down means a
   second diagram, not a cluttered one.
2. Label every relationship with protocol + direction (`ConnectRPC/HTTP2`,
   `WSS signalling`, `DTLS-SRTP media`, `ARI WebSocket`, `NATS JetStream`,
   `SQL via pgx`) — notation from `02-system-context.md` §4 and the
   `12-portal-architecture.md` §2 diagram.
3. Respect the dependency arrows: `04-bounded-contexts.md` §10 build graph
   is the law (telephony-core first, dialer last). A diagram that implies a
   forbidden import is wrong no matter how pretty.
4. Keep the domain/infrastructure split visible: `Call`/`Participant`
   aggregates above the anti-corruption layer; Asterisk channels, bridges,
   PJSIP below it. Participant ≠ Channel on every diagram that shows both.
5. Notation follows the target doc: mermaid where the HLD uses mermaid
   (TRD lineage graphs), plain ASCII in chat. Match surrounding style.

## Constraints

- Ubiquitous language on labels (see glossary skill): no `Leg`, no fixed
  caller/agent boxes (D-18/D-32).
- Security boundaries from `05-security.md` (tenant RLS, edge ports, NATS on
  private net) appear wherever the diagram crosses them.
- Failure behaviour (hld/09) is annotated, not hidden: crashed media node →
  calls end; carrier failover → 5s.
- A diagram that needs a structural choice nobody has taken (new container,
  new trust boundary, new protocol) marks it `PROPOSED` and routes the
  decision through the architectural-decision-records skill — boxes don't
  decide.
