<!-- OpenSpec: TRD-HLD-01 -->
# AtsaPBX High-Level Design

This directory defines the authoritative, implementation-facing High-Level Design (HLD) baseline for AtsaPBX. It translates the Business Requirements Document (`docs/BRD.md`), Product Requirements Document (`docs/PRD.md`), Technical Requirements Document (`docs/TRD.md`), and Architecture Decision Log (`docs/DECISIONS.md`) into a modular, production-grade technical specification that engineers and AI coding agents can implement incrementally without ambiguity or speculative design.

AtsaPBX is an enterprise-grade, multi-tenant contact center and PBX platform combining PBX functionality (3CX / VitalPBX equivalent) and outbound contact center capabilities (Vicidial equivalent) built on top of Asterisk 22.x LTS. It is designed to scale from a single-node MVP to a multi-node regional cluster handling 50,000 concurrent channels (D-08) without requiring architectural redesign.

---

## 1. Core Architecture Principles & Invariants

The design strictly implements the non-negotiable principles established across BRD, PRD, TRD, and DECISIONS:

1. **We are a software vendor, not an operator (D-01, BRD §2.1):** Partners deploy on their own infrastructure, bring their own SIP carriers (BYOT), supply their own AI provider credentials, and bill their own customers.
2. **Modular Monolith with Ports & Adapters (D-20, D-31, TRD §Architecture):** All bounded contexts live as Go modules within a single deployable binary. Inter-module communication uses explicit Go package interfaces or versioned asynchronous domain events over NATS JetStream, never shared private database tables.
3. **Asterisk as the Media Engine via Anti-Corruption Layer (D-15, D-16, TRD §Architecture):** We do not build telephony protocols from scratch. All dialplan logic is minimized to Stasis entry points (`core/conf/extensions.conf`); 100% of call logic, routing, and state machines live in Go application code. The Asterisk Anti-Corruption Layer (ACL) absorbs all ARI/AMI channel churn.
4. **Participant, Not Channel (D-17, D-18, D-32, TRD §Domain model):** `Call` is an aggregate of N `CallParticipant`s (never fixed caller/agent columns). A `Participant` is a persistent business identity that survives holds, transfers, conferences, and channel swaps. An Asterisk `Channel` is an ephemeral infrastructure resource. 1 Participant maps to 1..N Channels over time.
5. **API-First as a Commercial Strategy (D-24, BR-16, FBR-R1-11):** 100% of core signaling events, call state transitions, administrative controls, and telemetry are exposed through public ConnectRPC (gRPC + HTTP/JSON) APIs and webhooks. Our administrative console and CLI use the exact same public API.
6. **AI is Optional and Out-of-Band (D-05, D-06, D-23, BR-14, BR-17, INV-04, INV-05):** AI media pipelines subscribe to copied audio streams (audio snoop/fork) and are strictly out-of-band. AtsaPBX operates 100% natively without any AI engine configured. An AI failure never impairs call quality or disconnects a call.
7. **Compliance as Pure Functions (D-21, TRD §Domain model):** Do-Not-Call, calling hours, spend caps, and abandonment checks are pure, stateless deterministic functions: `Evaluate(Snapshot, RuleSet) -> Verdict`.
8. **IVR and Call Flows as Interpreted Data (D-22, TRD §Domain model):** Call flows are directed node graphs stored in PostgreSQL JSONB and executed via a pure fold function: `Fold(State, Event) -> (NextState, Commands)`.
9. **Strict Multi-Tenant Isolation (BR-01, BR-15, FBR-R1-04, AC-01.1):** Every database row, event, and cache entry carries `tenant_id`. PostgreSQL Row-Level Security (RLS) is enforced on all transactions. Secrets and encryption keys are isolated per tenant.
10. **Emergency Path Invariant (INV-01, BR-09, D-12):** Emergency calls connect in every licence, capacity, and suspension state. This path cannot be gated.

---

## 2. Technology Stack Baseline

| Layer / Concern | Technology Choice | Rationale & Requirements Trace |
|---|---|---|
| **Core Monolith** | Go 1.25+ | Memory safety, high concurrency, low latency, single static binary deployment [BRD §2.3; TRD §Tech stack] |
| **Media & SIP Engine** | Asterisk 22.x LTS (PJSIP, ARI, AMI) | Carrier-certified SIP/RTP engine, WebRTC gateway, DTLS-SRTP, audio snooping [BRD §2.3; DECISIONS D-15, D-16] |
| **API Framework** | ConnectRPC (HTTP/2, gRPC, Protobuf, JSON) | Type-safe RPCs, bidirectional streaming, web browser support without Envoy proxy [BRD FBR-R1-11; PRD EPIC-05] |
| **Primary Database** | PostgreSQL 16+ | ACID transactions, native Row-Level Security (RLS), JSONB flow storage, transactional outbox [BRD FBR-R1-04; PRD EPIC-01] |
| **Asynchronous Events** | NATS JetStream 2.10+ | Lightweight, durable, ordered pub/sub for domain events (`event.call.*`); strictly no SIP/RTP media [DECISIONS D-23; TRD §Architecture] |
| **Secrets & Encryption** | HashiCorp Vault / KMS | Tenant-scoped data encryption keys (DEK), envelope encryption (AES-256-GCM), zero hardcoded secrets [PRD T-10; DECISIONS D-05] |
| **Object Storage** | S3-Compatible (MinIO / Cloud Object Store) | Tamper-evident call recordings, voicemail storage, exported compliance archives [PRD T-10; BRD FBR-R1-09] |
| **Configuration & Deploy** | Docker Compose + Ansible | Idempotent single-node and multi-node partner infrastructure orchestration [PRD EPIC-09; DECISIONS D-01] |
| **Observability** | Go `slog`, Prometheus, OpenTelemetry, Alertmanager | 10-minute issue resolution gate, per-participant RTCP-XR quality scoring, W3C distributed tracing [PRD T-11; BRD FBR-R1-10] |

---

## 3. Document Navigation

The High-Level Design is partitioned into fifteen modular chapters:

1. [Architecture & Component Design](01-architecture.md) — Modular monolith structure, Hexagonal ports & adapters, Asterisk Anti-Corruption Layer (ACL), and context propagation.
2. [System Context & Interfaces](02-system-context.md) — C4 system context, container topology, network boundaries, protocols, and end-to-end scenario sequence diagrams.
3. [Domain Model & State Machines](03-domain-model.md) — Participant Aggregate pattern, Call & Participant lifecycle state machines, IVR flow-as-data engine, and relational PostgreSQL DDL.
4. [Bounded Contexts](04-bounded-contexts.md) — Detailed specifications for all 9 bounded contexts: interfaces, domain commands, published events, and dependency rules.
5. [Security & Isolation](05-security.md) — Tenant isolation via RLS, T-9 crypto-shredding deletion workflow, T-10 recording envelope encryption, and edge fraud prevention.
6. [Deployment & Infrastructure](06-deployment.md) — Container topology, networking, T-12 zero-downtime expand/contract upgrades, Ansible automation, and support diagnostic bundles.
7. [Observability & Telemetry](07-observability.md) — T-11 per-participant RTCP-XR quality measurement, Prometheus metrics catalog, structured `slog` logging, OpenTelemetry tracing, and alerting rules.
8. [Performance & Scalability](08-performance.md) — Concurrency baselines, transactional outbox worker design, database partitioning, caching, and load testing framework.
9. [Failure Model & Resilience](09-failure-model.md) — Exhaustive Failure Modes & Effects Analysis (FMEA), graceful degradation paths, carrier failover, and self-healing mechanisms.
10. [Non-Goals & Release Boundaries](10-non-goals.md) — Strict R1.0 scope boundaries vs R1.1, R2, R3 roadmap capabilities and architectural non-goals.
11. [Decisions & Technical Resolutions](11-decisions.md) — Definitive technical resolutions for PRD Questions T-1 through T-12 and the full Architectural Decision Record (ADR) index.
12. [Approved Toolset & Dependency Baseline](../TOOLSET.md) — Mandatory Go libraries, database drivers, developer CLI tools, and forbidden dependencies.
13. [Architectural Trade-Off Analysis & Technology Evaluation](ARCHITECTURAL-TRADEOFFS.md) — ISO/IEC/IEEE 42010 & SEI ATAM analysis: ConnectRPC vs REST, native pgx/v5 vs ORMs, and FOSS governance.
14. [Portal Architecture](12-portal-architecture.md) — React + TypeScript consumer of the ConnectRPC API (D-36): Admin UI, Agent UI with browser softphone, Supervisor Dashboard, and Partner Portal.
15. [Go Coding Standards](13-coding-standards.md) — Normative house subset for all Go code (D-40): SDD law, line-of-sight, error rules, context-first, concurrency safety, import grouping, machine gates, and the agent role prompt.

---

## 4. OpenSpec Traceability Matrix

Every requirement across `BRD.md`, `PRD.md`, and `TRD.md` is addressed in this HLD:

| BRD Requirement | PRD Epic / Reference | HLD Chapter | Architectural Solution |
|---|---|---|---|
| **FBR-R1-01** Browser Calling & Telephony | EPIC-03 | [01](01-architecture.md), [02](02-system-context.md), [03](03-domain-model.md) | WebRTC DTLS-SRTP via Asterisk PJSIP, ConnectRPC call control, Participant aggregate |
| **FBR-R1-02** Desk-Phone Provisioning | EPIC-03 | [02](02-system-context.md), [04](04-bounded-contexts.md) | HTTPS auto-provisioning service with MAC-based encrypted XML/cfg profiles |
| **FBR-R1-03** Carrier Neutrality & LCR | EPIC-02 | [03](03-domain-model.md), [04](04-bounded-contexts.md), [09](09-failure-model.md) | BYOT SIP trunking, cost-table routing engine, 5s automated carrier failover |
| **FBR-R1-04** Multi-Tenant Isolation | EPIC-01 | [01](01-architecture.md), [05](05-security.md) | Tenant context propagation, PostgreSQL Row-Level Security, NATS subject tenancy |
| **FBR-R1-05** Edge Security & Fraud Control | EPIC-08 | [05](05-security.md), [09](09-failure-model.md) | CPS token buckets, spend caps, country code blocking, automated anomaly shutdown |
| **FBR-R1-06** Visual IVR Builder | EPIC-03 | [03](03-domain-model.md), [04](04-bounded-contexts.md) | JSONB directed graph, pure fold execution, static cycle/dead-end validation |
| **FBR-R1-07** Open AI Engine Choice | EPIC-04 | [02](02-system-context.md), [04](04-bounded-contexts.md), [11](11-decisions.md) (T-4) | Out-of-band ARI audio snoop, gRPC/WebSocket provider adapters, zero call blocking |
| **FBR-R1-08** Access Control & Audit | EPIC-01 | [03](03-domain-model.md), [05](05-security.md) | Scoped RBAC, immutable append-only `audit_logs` table with actor and before/after state |
| **FBR-R1-09** Recording Governance | EPIC-07 | [05](05-security.md), [11](11-decisions.md) (T-10) | Consent gate before record, PCI-DSS pause/resume, AES-256-GCM envelope encryption |
| **FBR-R1-10** Tamper-Evident Records | EPIC-07 | [03](03-domain-model.md), [07](07-observability.md), [11](11-decisions.md) (T-11) | Immutable CDRs, per-second usage records, RTCP-XR per-participant MOS scoring |
| **FBR-R1-11** API-First Extensibility | EPIC-05 | [01](01-architecture.md), [04](04-bounded-contexts.md), [11](11-decisions.md) (T-7) | ConnectRPC endpoints for 100% of console actions, NATS JetStream event streaming, webhooks |
| **FBR-R1-12** Cryptographic Licensing | EPIC-06 | [04](04-bounded-contexts.md), [11](11-decisions.md) (T-1–T-3) | Ed25519 signed payload, weighted hardware fingerprint, atomic capacity counter, 7-day grace |
| **FBR-R1-13** Deployment & Lifecycle | EPIC-09 | [06](06-deployment.md), [11](11-decisions.md) (T-12) | Container compose topology, Ansible playbooks, expand/contract zero-downtime upgrades |
| **FBR-R2 / R3 Seams** Contact Center & OPI | EPIC-14–23 | [03](03-domain-model.md), [04](04-bounded-contexts.md) | Power dialler loop, pure compliance checks, per-second usage ledger, dispatch wave seams |

---

## 5. Verification & Acceptance Criteria

The architecture is verified through continuous automated gates:

1. **Static Marker & Link Check:** CI validates all fifteen `<!-- OpenSpec: TRD-HLD-xx -->` markers, cross-file document links, and ADR traceability citations.
2. **Architecture Dependency Linting:** Go import rules enforce strict package boundaries. Domain packages cannot import adapters, and no package outside `telephony/acl` may import Asterisk ARI/AMI packages or reference channel IDs.
3. **Walking Skeleton Integration Test (D-25):** End-to-end verification proving:
   - WebRTC ingress establishes an audio session with Asterisk.
   - Asterisk hands control to the Go Stasis application.
   - ACL creates a domain `Call` with stable `CallParticipant` entities.
   - An outbound carrier route is selected via LCR.
   - Per-second usage records are written with deduplication.
   - Outbox event publishes to NATS JetStream.
   - ConnectRPC API query returns full call and participant state without leaking Asterisk channel IDs.

