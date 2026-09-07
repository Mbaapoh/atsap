<!-- OpenSpec: TRD-HLD-02 -->
# Architecture & Component Design

## 1. Modular Monolith Architecture (D-20, D-31)

AtsaPBX is designed as a single deployable Go binary structured into distinct bounded contexts. This architecture provides the operational simplicity, transactional consistency, and low latency of a monolith, while strictly maintaining domain boundaries that allow individual contexts to be extracted into standalone microservices in the future if justified by measured load (D-20).

```text
+---------------------------------------------------------------------------------------------------------+
|                                           AtsaPBX API / Ingress Layer                                  |
|                                (ConnectRPC / HTTP/2 / JSON Gateway / WebSockets)                        |
+---------------------------------------------------------------------------------------------------------+
                                                     │
                                                     ▼
+---------------------------------------------------------------------------------------------------------+
|                                    Application & Orchestration Layer                                    |
|   (Auth & Tenant Middleware, Command Handlers, Query Handlers, Event Handlers, Outbox Coordinator)     |
+---------------------------------------------------------------------------------------------------------+
                                                     │
                                                     ▼
+---------------------------------------------------------------------------------------------------------+
|                                         Core Domain Bounded Contexts                                    |
|  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐  ┌───────────┐  |
|  │  telephony-core  │  │     pbx-core     │  │    compliance    │  │    licensing     │  │ reporting │  |
|  │ (Call/Participant│  │ (Extensions, IVR,│  │ (Pure functions, │  │ (Capacity, grace,│  │ (CDR, MOS,│  |
|  │   Aggregates)    │  │  LCR, Queues)    │  │   spend, DNC)    │  │   fingerprint)   │  │   ledger) │  |
|  └──────────────────┘  └──────────────────┘  └──────────────────┘  └──────────────────┘  └───────────┘  |
|  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐                 |
|  │     dialer       │  │   ai-pipeline    │  │     identity     │  │ webhook-delivery │                 |
|  │ (Campaigns, list,│  │ (Audio snoop,    │  │  (Tenant, RBAC,  │  │ (Signed at-least-│                 |
|  │  pacing, waves)  │  │  provider gRPC)  │  │   API keys)      │  │   once retries)  │                 |
|  └──────────────────┘  └──────────────────┘  └──────────────────┘  └──────────────────┘                 |
+---------------------------------------------------------------------------------------------------------+
                                                     │ (Interfaces / Ports)
                                                     ▼
+---------------------------------------------------------------------------------------------------------+
|                                      Infrastructure Adapters Layer                                      |
|  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐  ┌───────────┐  |
|  │   Asterisk ACL   │  │  PostgreSQL 16   │  │  NATS JetStream  │  │  HashiCorp Vault │  │  MinIO/S3 │  |
|  │ (ARI REST & WS,  │  │ (pgx, RLS, DDL,  │  │ (Event publisher,│  │ (KMS, wrapped   │  │ (Encrypted│  |
|  │   AMI adapter)   │  │  JSONB, Outbox)  │  │  durable queues) │  │   tenant keys)   │  │  records) │  |
|  └──────────────────┘  └──────────────────┘  └──────────────────┘  └──────────────────┘  └───────────┘  |
+---------------------------------------------------------------------------------------------------------+
```

### 1.1 Go Package Layout

The repository follows a clean Hexagonal / Ports & Adapters directory structure under `api/`:

```text
api/
├── cmd/
│   └── atsapbx/              # Application entrypoint (main.go)
├── internal/
│   ├── shared/               # Shared kernel across bounded contexts
│   │   ├── domain/           # Ubiquitous types: TenantID, CallID, ParticipantID
│   │   ├── event/            # Base Event interface and metadata
│   │   ├── apperrors/        # Standard domain errors and error codes
│   │   └── context/          # Tenant, security, and correlation context propagation
│   ├── telephony/            # bounded context: telephony-core
│   │   ├── domain/           # Call, CallParticipant aggregates, invariants
│   │   ├── application/      # Call service, command/query handlers
│   │   ├── ports/            # MediaGateway, CallStore, QualityReader interfaces
│   │   └── acl/              # Asterisk Anti-Corruption Layer (ARI/AMI adapters)
│   ├── pbx/                  # bounded context: pbx-core
│   │   ├── domain/           # Extension, Trunk, LCR route, IVR flow graph
│   │   ├── application/      # Routing engine, flow interpreter, queue manager
│   │   └── ports/            # ExtensionStore, TrunkStore, FlowStore interfaces
│   ├── compliance/           # bounded context: compliance
│   │   └── domain/           # Pure policy functions (DNC, hours, spend caps)
│   ├── licensing/            # bounded context: licensing
│   │   ├── domain/           # Licence token, hardware fingerprint, capacity
│   │   └── application/      # Daily check worker, atomic capacity monitor
│   ├── reporting/            # bounded context: reporting
│   │   ├── domain/           # Immutable CDR, UsageSecond, QualityRecord
│   │   └── application/      # CDR aggregator, export generator
│   ├── aipipeline/           # bounded context: ai-pipeline
│   │   ├── domain/           # Audio stream job, translation session
│   │   └── adapters/         # gRPC/WebSocket client for Whisper/Ollama/OpenAI
│   ├── identity/             # bounded context: identity
│   │   ├── domain/           # Tenant, Principal, Role, API Key
│   │   └── application/      # JWT issuer, RBAC authorizer, audit logger
│   ├── webhook/              # bounded context: webhook-delivery
│   │   ├── domain/           # Subscription, DeliveryAttempt, DLQ
│   │   └── application/      # Retrying sender with HMAC-SHA256 signature
│   ├── dialer/               # bounded context: dialer (R2/R3 ready)
│   │   └── domain/           # Campaign, DialList, Pacing, WaveDispatch
│   ├── server/               # Ingress HTTP/2 ConnectRPC handlers & routing
│   ├── postgres/             # Database connection pool, migrations, outbox reader
│   └── nats/                 # NATS JetStream publisher and consumer connections
```

### 1.2 Architectural Invariant & Dependency Rules

1. **Strict Dependency Hierarchy:** `domain` depends on nothing. `application` depends only on `domain` and `ports`. `adapters` implement `ports` and depend on external drivers.
2. **Context Isolation:** A bounded context MUST NOT directly import another bounded context's internal packages or query another context's database tables. Cross-context interactions must pass through published application interfaces or NATS domain events.
3. **No Raw Telephony Exposure:** Raw Asterisk concepts (`Channel`, `Bridge`, `StasisApp`, `Uniqueid`) are strictly forbidden outside `api/internal/telephony/acl`. The rest of the system operates exclusively on `Call` and `Participant` abstractions (D-17, D-18, D-32).

---

## 2. Asterisk Anti-Corruption Layer (ACL) (D-15–D-19)

Asterisk 22.x LTS provides the battle-tested media engine (SIP, RTP, WebRTC DTLS-SRTP, CODEC negotiation, and hardware timing). However, Asterisk's internal channel model is volatile: attended transfers destroy and recreate channels, bridges mutate, and channel IDs are tied to transient socket lifecycles.

The **Asterisk Anti-Corruption Layer (ACL)** sits between the Asterisk ARI/AMI interfaces and the AtsaPBX domain, completely absorbing infrastructure complexity.

```text
       +---------------------------------------------------------------+
       |                      telephony-core Domain                    |
       |  Call Aggregate                                               |
       |    ├── CallID (UUID)                                          |
       |    └── CallParticipants: []CallParticipant                   |
       |           ├── ParticipantID (UUID)                            |
       |           ├── Role (Caller, Agent, Supervisor, IVR, etc.)     |
       |           └── Lifecycle State (Connected, OnHold, etc.)       |
       +---------------------------------------------------------------+
                                      ▲
                                      │ Domain Commands & Events
                                      ▼
       +---------------------------------------------------------------+
       |                 Asterisk Anti-Corruption Layer                |
       |  ┌─────────────────────────────────────────────────────────┐  |
       |  │ In-Memory Correlation Registry                           │  |
       |  │  - Token <-> ParticipantID <-> CallID                    │  |
       |  │  - ChannelHistory: [ChannelID_1, ChannelID_2, ...]       │  |
       |  │  - Active Bridges Map                                    │  |
       |  └─────────────────────────────────────────────────────────┘  |
       |  ┌─────────────────────────────────────────────────────────┐  |
       |  │ Stasis Application Controller ("atsapbx")               │  |
       |  │  - Originates channels with custom AtsaPBX tokens        │  |
       |  │  - Ingests ARI WebSocket events                         │  |
       |  │  - Maps channel swaps/transfers into Participant updates│  |
       |  │  - Dispatches media actions (Bridge, Hold, Snoop, Play) │  |
       |  └─────────────────────────────────────────────────────────┘  |
       +---------------------------------------------------------------+
                          │ ARI REST / WS        │ AMI TCP
                          ▼                      ▼
       +---------------------------------------------------------------+
       |                      Asterisk 22.x LTS                        |
       |  PJSIP Channel 1 ──┐                                          |
       |  PJSIP Channel 2 ──┼──> Asterisk Bridge 1 ──> RTP Media       |
       |  Snoop Channel   ──┘                                          |
       +---------------------------------------------------------------+
```

### 2.1 Correlation Strategy (D-19, PRD T-5)

Under high call concurrency (e.g., 30+ CPS), correlating events by caller ID and timestamp leads to race conditions. The ACL enforces deterministic correlation:
1. **Outbound Origination:** AtsaPBX injects a custom channel variable and channel ID during ARI origination: `endpoint=PJSIP/carrier/...`, `channelId=atsa-part-<participant_id>-<uuid>`.
2. **Inbound Ingress:** When a call enters Stasis from the minimal dialplan, Asterisk passes the channel to Stasis. The ACL inspects SIP headers (`X-Atsa-Correlation-ID` if internal, or generates a fresh `CallID` and `ParticipantID`), registers the initial channel in the `ChannelHistory` registry, and sets channel variables in Asterisk.
3. **Channel Replacement & Attended Transfer Handover:** When Asterisk issues `ChannelDestroyed` followed by `ChannelCreated` or `BridgeMerged` during a transfer, the ACL identifies the handover via the bridge token, links the new channel ID to the existing `participant_id` in `ChannelHistory`, and preserves the participant's domain identity and billing timer unbroken.

### 2.2 Engine Capability Checklist (D-41)

`MediaGateway` is written in domain capabilities so a future engine can
implement it without reshaping (normative contract on the interface
itself; executable proof in `api/internal/telephony/mediatest`). An
engine qualifies by meeting every row — a miss is a pre-decided degraded
path, never orchestrator branching:

| Primitive | Engine must provide | If it cannot |
|---|---|---|
| Originate with caller-supplied ID + variables | Token in, same token on every later event (D-19) | Disqualified — correlation by guess is forbidden, no fallback exists |
| Answer / hangup events | Answered + destroyed/hangup signals mappable to the fixed sink vocabulary | Disqualified — the orchestrator cannot track what it cannot see |
| Bridge / mix N legs | Mixer with add/remove semantics | Out of scope for that engine (no multi-party on it) |
| Hold / playback / record | Media operations per leg | Feature-gated off for that engine |
| Audio tap for AI | Non-blocking cloned-audio feed | AI pipeline disabled for that engine (INV-04) |
| Transfer with identity continuity | Channel-swap events preserving participant (T-5 pattern) | Transfers rejected on that engine, never half-migrated |
| Per-second usage + CDR-grade detail | Derivable from answer/hangup/transfer events | Disqualified — an engine that cannot bill cannot serve |
| TLS/SRTP media | Encrypted signaling and media | Disqualified (BR-02) |

---

## 3. Ingress, Protocols & Outbox Pattern

### 3.1 ConnectRPC API Layer (BR-16, FBR-R1-11)

AtsaPBX exposes its public and private APIs via ConnectRPC (gRPC-compatible HTTP/2 with transparent JSON mapping):
- **Typed Protobuf Contracts:** High performance, binary serialization, strict backward compatibility.
- **Direct Browser Support:** Web browsers communicate directly with ConnectRPC over HTTP/1.1 or HTTP/2 without requiring a gRPC-Web proxy (such as Envoy).
- **Server-Streaming Telemetry:** Live call states, agent presence, and per-participant quality streams use HTTP/2 server streaming (`rpc StreamCallEvents(StreamCallRequest) returns (stream CallEvent)`).
- **Idempotent Commands:** All mutating endpoints require an `Idempotency-Key` header stored in PostgreSQL to guarantee exactly-once command execution.
- **Architectural Trade-Off Analysis:** See [TOOLSET.md §2.1](../TOOLSET.md#21-why-connectrpc-instead-of-standard-rest-frameworks-gin-fiber-echo-chi) for the detailed Solution Architect justification contrasting ConnectRPC with standard REST frameworks (Gin, Fiber, Echo).

### 3.2 Transactional Outbox Pattern

To prevent dual-write bugs between PostgreSQL and NATS JetStream, AtsaPBX implements the **Transactional Outbox Pattern**:
1. Within a single PostgreSQL ACID transaction, domain state changes and outbox event records are committed together:
   ```sql
   INSERT INTO calls (...) VALUES (...);
   INSERT INTO participants (...) VALUES (...);
   INSERT INTO outbox (id, tenant_id, event_type, aggregate_id, payload)
   VALUES (gen_random_uuid(), $1, 'event.call.started', $2, $3);
   ```
2. A background Go Outbox Worker polls unpublished rows using high-throughput skip-locked queries:
   ```sql
   SELECT id, tenant_id, event_type, payload
   FROM outbox
   WHERE published_at IS NULL
   ORDER BY id
   FOR UPDATE SKIP LOCKED
   LIMIT 100;
   ```
3. Upon receiving an acknowledgement from NATS JetStream, the worker marks `published_at = NOW()`.
4. Domain events on NATS JetStream are published under tenant-partitioned subjects: `tenant.<tenant_id>.event.call.<action>`.
5. **Persistence Engine Justification:** See [TOOLSET.md §2.2](../TOOLSET.md#22-why-native-pgxv5-instead-of-heavy-orms-gorm-ent-sqlboiler) for the Solution Architect rationale regarding native `pgx/v5` with connection lifecycle hooks for RLS and `SKIP LOCKED` over heavy ORMs.

---

## 4. Tenant Context Propagation (BR-01, FBR-R1-04)

Tenant isolation is enforced continuously from HTTP ingress to storage:

```text
[Incoming ConnectRPC Request]
        │
        ▼ (Extract JWT / API Key)
[Tenant Auth Middleware]
        │ 1. Validate signature, issuer, expiry
        │ 2. Extract tenant_id, principal_id, role, residency
        │ 3. Inject immutable TenantContext into Go context.Context
        ▼
[Application Command Handler]
        │ Passes ctx to repository
        ▼
[Postgres Connection Pool (pgx)]
        │ 1. Check out connection from pool
        │ 2. Execute: SET LOCAL app.tenant_id = '<tenant_id>';
        │ 3. Execute domain query (RLS policy automatically filters by app.tenant_id)
        │ 4. Return connection to pool (resets session state)
        ▼
[PostgreSQL Table with RLS ENABLED]
```

If a database query is executed without `app.tenant_id` set, PostgreSQL Row-Level Security evaluates `app.tenant_id` to `NULL` and returns zero rows, failing closed safely.

> **One documented exception:** cross-tenant platform workers that must read
> every tenant's rows — the transactional outbox publisher (§3.2) — connect
> with a narrowly-scoped `BYPASSRLS` role (granted only the tables they need,
> e.g. `outbox`). This is platform infrastructure, never a tenant-facing
> interface (see HLD `03-domain-model.md` §5). Everything tenant-facing keeps
> the fail-closed behaviour above.

---

## 5. Architectural Seams for R2 / R3 (D-24, TRD §Extensibility seams)

Four non-negotiable seams are built into the architecture from commit one:

1. **`tenant_id` Everywhere:** Present in every database schema, event header, log record, and cache key.
2. **Call as N Participants:** Data structures and state machines model an arbitrary collection of participants. Supervisor barge-in (R2) and multi-party specialist translation (R3) require zero schema changes.
3. **API-First Parity:** No private backdoor interfaces exist. Any action executable in the partner portal or admin console is executed via public ConnectRPC methods.
4. **Per-Participant, Per-Second Usage Records (`usage_seconds`):** Emitted continuously during every active call. R3 margin ledger and per-second rating consume this immutable stream without requiring engine refactoring.

---

## 6. Architecture Verification Gates

1. **Dependency Invariant Test:** Go static analysis tool (`go-arch-lint` or AST tests) fails CI if any non-telephony package imports `internal/telephony/acl` or Asterisk client libraries.
2. **Channel ID Leakage Test:** JSON schemas for all public ConnectRPC messages and webhook events are scanned via automated tests; any presence of `channel_id`, `channel`, or Asterisk unique ID fails the build.
3. **Tenant Context Leakage Test:** Unit test attempts to execute a repository query with an empty context; asserts that the query fails closed with `ErrTenantContextMissing`.


