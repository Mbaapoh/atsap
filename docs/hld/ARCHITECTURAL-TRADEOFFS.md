<!-- OpenSpec: TRD-HLD-13 -->
# AtsaPBX — Architectural Trade-Off Analysis & Technology Evaluation

**Standard Reference:** ISO/IEC/IEEE 42010 Architecture Description & SEI ATAM (Architecture Tradeoff Analysis Method)  
**Document Classification:** Authoritative Engineering Standard (Principal Solution Architecture)  
**Related Documents:** [TOOLSET.md](../TOOLSET.md), [DECISIONS.md](../DECISIONS.md) (D-28, D-30, D-31, D-33, D-34, D-35), [01-architecture.md](01-architecture.md), [11-decisions.md](11-decisions.md)

---

## 1. Executive Summary & Evaluation Framework

In high-concurrency, real-time telecommunications platforms, architectural choices cannot be made based on developer popularity or superficial framework ergonomics. Every dependency introduces a trade-off curve across **latency, memory allocation patterns, concurrency predictability, operational surface, and multi-tenant security**.

This document provides the definitive, industry-standard architectural trade-off analysis for AtsaPBX, evaluating:
1. **API Ingress & Contract Boundary:** ConnectRPC vs. Standard REST Frameworks (Gin, Fiber, Echo) vs. Traditional gRPC with Envoy.
2. **Database Persistence & Ingestion:** Native `pgx/v5` with Parameterized SQL vs. Heavy Object-Relational Mappers (GORM, Ent, SQLBoiler).
3. **Open-Source Software (FOSS) & Supply-Chain Governance:** Strictly Permissive FOSS vs. Copyleft (GPL/AGPL) and Proprietary SDKs.

### 1.1 Quality Attribute Utility Tree (SEI ATAM)

The following high-priority Quality Attribute Requirements (QARs) govern all evaluations:

```
AtsaPBX Quality Attribute Tree
├── Security & Isolation
│   ├── [QAR-01] Multi-Tenant Data Isolation (PostgreSQL 16 RLS session safety) [HIGH, HIGH]
│   └── [QAR-02] Supply Chain & License Compliance (Permissive FOSS, zero CVE tolerance) [HIGH, HIGH]
├── Performance & Throughput
│   ├── [QAR-03] Ultra-Low Setup Latency (Sub-2ms database routing lookups for PDD) [HIGH, HIGH]
│   ├── [QAR-04] High-Frequency Telemetry Ingestion (50,000+ usage/RTCP ticks/sec) [HIGH, HIGH]
│   └── [QAR-05] Concurrency Efficiency (Minimal GC pause time under 30+ CPS burst load) [HIGH, MEDIUM]
├── Operational Simplicity
│   ├── [QAR-06] Zero-Proxy Deployment Footprint (No Envoy sidecar in on-prem appliances) [HIGH, HIGH]
│   └── [QAR-07] Single Source of Truth for Contracts (Zero client-server schema drift) [HIGH, HIGH]
└── Maintainability & Modifiability
    ├── [QAR-08] Standard Library Conformance (Go net/http interoperability) [MEDIUM, HIGH]
    └── [QAR-09] Explicit, Explainable SQL (Sub-millisecond query plan predictability) [HIGH, HIGH]
```

---

## 2. Trade-Off Evaluation 1: ConnectRPC vs. Standard REST Frameworks

```
+---------------------------------------------------------------------------------------------------------+
|                                    ATSA PBX INGRESS ARCHITECTURE                                        |
+---------------------------------------------------------------------------------------------------------+
|                                                                                                         |
|   WebRTC Operator Portal / Admin Console                     External Services / Billing Workers        |
|               (HTTP/1.1 JSON)                                            (HTTP/2 gRPC)                  |
|                      │                                                         │                        |
|                      ▼                                                         ▼                        |
|   ┌─────────────────────────────────────────────────────────────────────────────────────────────────┐   |
|   │                       ConnectRPC Ingress Layer (connectrpc.com/connect)                         │   |
|   │     - Unified handler for standard JSON (POST/GET), gRPC-Web, and binary gRPC               │   |
|   │     - Single Source of Truth: Protocol Buffer Schemas (api/proto/atsap/v1/)                     │   |
|   │     - Native HTTP/2 Server-Streaming for Telephony Ticks (RTCP / Presence)                      │   |
|   │     - Zero External Reverse Proxies (Direct browser fetch() without Envoy)                      │   |
|   └────────────────────────────────────────────────┬────────────────────────────────────────────────┘   |
|                                                    │                                                    |
|                                                    ▼                                                    |
|                                  Standard Go net/http Middleware & Ports                                |
+---------------------------------------------------------------------------------------------------------+
```

### 2.1 Considered Options

1. **Option A (Chosen): ConnectRPC (`connectrpc.com/connect`)** — Schema-first RPC protocol built on standard Go `net/http`, supporting Protobuf binary, gRPC, gRPC-Web, and standard JSON over HTTP/1.1 and HTTP/2 simultaneously.
2. **Option B (Rejected): Standard REST Framework (Gin / Echo / Chi)** — Traditional REST routing with JSON serialization, custom routing contexts, and separate documentation (OpenAPI/Swagger).
3. **Option C (Rejected): Traditional gRPC (`google.golang.org/grpc`) + Envoy Sidecar** — Pure gRPC backend requiring an external Envoy proxy or `grpc-gateway` container to translate browser HTTP/1.1 and JSON requests.
4. **Option D (Rejected): Fast Non-Standard Framework (Fiber / Fasthttp)** — High-throughput REST engine utilizing `valyala/fasthttp` rather than the Go standard library.

### 2.2 In-Depth Sensitivity & Trade-Off Matrix

| Architectural Criterion | Option A: ConnectRPC | Option B: Standard REST (Gin/Echo) | Option C: gRPC + Envoy | Option D: Fiber (Fasthttp) |
|---|---|---|---|---|
| **Contract Authority & Drift Prevention (QAR-07)** | **Optimal.** Single source of truth via `.proto` files. Backward compatibility enforced by `buf breaking` in CI. Strongly-typed TypeScript client generated automatically. | **Poor.** Relies on manual OpenAPI YAML or code annotations. Inevitably drifts from implementation, causing runtime deserialization bugs. | **Optimal.** Protobuf contract single source of truth. | **Poor.** Severe schema drift; OpenAPI generation requires bulky reflection annotations. |
| **Operational Simplicity & Appliance Footprint (QAR-06)** | **Optimal.** Single Go binary serves browsers, cURL, webhooks, and gRPC clients without any external proxy process. | **Moderate.** Single binary for REST, but cannot serve high-performance binary gRPC without a second server. | **Severe Deficit.** Requires running, configuring, and monitoring an external Envoy container in every partner on-premise installation. | **Moderate.** Single binary for REST only. |
| **Real-Time Telemetry Streaming (QAR-05)** | **Optimal.** Native HTTP/2 server-streaming and bidirectional streaming with standard Go `context.Context` cancellation. Unified auth and rate limiting. | **Severe Deficit.** REST has no streaming semantics. Requires separate WebSocket (Gorilla) or SSE handlers with redundant auth and fragmented routing. | **Optimal.** Native HTTP/2 streaming, but requires Envoy translation for browser WebSockets/gRPC-Web. | **Poor.** Requires separate WebSocket upgrade paths with non-standard event dispatchers. |
| **Wire & Serialization Efficiency (QAR-04)** | **Optimal.** High-speed binary Protobuf (5–10x faster than JSON; 30–50% smaller payloads) with seamless JSON fallback. | **Poor.** Pure JSON reflection marshaling creates high CPU consumption during call setup spikes (30+ CPS). | **Optimal.** High-speed binary Protobuf. | **Moderate.** Optimized JSON parser (`sonic`), but still bounded by textual JSON payload bloat. |
| **Standard Library Interoperability (QAR-08)** | **Optimal.** 100% compliant with standard `net/http.Handler`. Works with standard Go middleware, OpenTelemetry, and Prometheus. | **Moderate.** Gin/Echo enforce custom contexts (`gin.Context`), preventing seamless standard middleware reuse. | **Poor.** Requires `grpc.Server` abstractions separate from standard `http.Handler`. | **Severe Hazard.** Breaks Go standard library `net/http`. Uses unsafe string-to-byte pointer conversions that risk memory corruption under high concurrency. |

### 2.3 Decision Outcome: ConnectRPC Selected

**Architectural Justification:**
AtsaPBX is a multi-tenant telecommunications platform deployed both as a cloud cluster and as partner on-premises appliances. Introducing an Envoy sidecar (Option C) violates **QAR-06** by adding an unacceptable operational burden for partner sysadmins. Conversely, traditional REST frameworks (Options B & D) violate **QAR-07** and **QAR-05** by introducing contract drift, lacking native streaming for 1-second voice quality metrics, and imposing JSON CPU overhead. 

ConnectRPC fulfills all requirements in a single Go process with zero external proxies, compile-time type safety, and standard library purity.

#### Trade-offs Accepted & Mitigations:
- *Non-REST URL Semantics:* Endpoints follow RPC semantics (`POST /atsap.v1.CallService/TransferCall`) rather than CRUD paths (`PATCH /calls/123/participants`).  
  *Mitigation:* Telephony is inherently command-driven. RPC method verbs accurately communicate business intent without the ambiguities of HTTP PATCH payloads.
- *Browser HTTP GET Caching:* Standard browser caching requires specific GET-enabled ConnectRPC annotations.  
  *Mitigation:* Read-only query endpoints (e.g. `GetCallDetails`, `ListTrunks`) are configured with Connect GET support for intermediate edge caching.

---

## 3. Trade-Off Evaluation 2: Native `pgx/v5` vs. Heavy ORMs

```
+---------------------------------------------------------------------------------------------------------+
|                                  DATABASE PERSISTENCE ARCHITECTURE                                      |
+---------------------------------------------------------------------------------------------------------+
|                                        Hexagonal Ports & Domain                                         |
|                                                    │                                                    |
|                                                    ▼                                                    |
|   ┌─────────────────────────────────────────────────────────────────────────────────────────────────┐   |
|   │                    Database Persistence Layer (github.com/jackc/pgx/v5)                         │   |
|   │     - Explicit Connection Lifecycle Hooks for PostgreSQL 16 RLS (SET LOCAL app.tenant_id)       │   |
|   │     - Binary Stream Ingestion (pgx.CopyFrom) for 50,000+ usage ticks/sec                        │   |
|   │     - Native Support for SKIP LOCKED (Transactional Outbox / Dialer Queues)                     │   |
|   │     - Deterministic Sub-Millisecond Execution (Zero N+1 query surprises)                        │   |
|   └────────────────────────────────────────────────┬────────────────────────────────────────────────┘   |
|                                                    │                                                    |
|                                                    ▼                                                    |
|                                      PostgreSQL 16 Multi-Tenant DB                                      |
+---------------------------------------------------------------------------------------------------------+
```

### 3.1 Considered Options

1. **Option A (Chosen): Native `github.com/jackc/pgx/v5` with `pgxpool`** — Direct PostgreSQL 16 driver utilizing the native binary wire protocol, explicit transaction scoping, connection acquisition hooks, and binary bulk ingestion (`pgx.CopyFrom`).
2. **Option B (Rejected): GORM (`gorm.io/gorm`)** — Heavy reflection-based Object-Relational Mapper offering automated struct-to-table migrations, hooks, and association preloading.
3. **Option C (Rejected): Ent (`entgo.io/ent`)** — Code-generated graph-based ORM from schema definitions, offering compile-time type safety at the expense of heavy framework code generation.
4. **Option D (Rejected): SQLBoiler** — Code-generated database-first ORM mapping existing database schemas to Go structs.

### 3.2 In-Depth Sensitivity & Trade-Off Matrix

| Architectural Criterion | Option A: Native `pgx/v5` | Option B: GORM | Option C: Ent | Option D: SQLBoiler |
|---|---|---|---|---|
| **Multi-Tenant RLS Session Safety (QAR-01)** | **Optimal.** Explicit `pgxpool` hooks (`BeforeAcquire`, `AfterRelease`) guarantee `SET LOCAL app.tenant_id` executes deterministically per transaction and is wiped on release. Zero leak risk. | **Catastrophic Hazard.** Opaque connection checkout. A single uncaught exception or hook misconfiguration returns a dirty connection to the pool, leaking tenant data across organizations. | **High Risk.** Complex hook chains wrap transactions. Validating that session-level variables are never reused requires auditing deeply nested framework code. | **Moderate Risk.** Supports transactions, but lacks explicit lifecycle interceptors specifically designed for PostgreSQL session variables. |
| **High-Frequency Ingestion (QAR-04)** | **Optimal.** `pgx.CopyFrom` binary streaming protocol inserts **50,000+ rows/sec** directly into PostgreSQL without query parsing overhead or GC allocations. | **Severe Bottleneck.** Reflection-driven batch `INSERT` statements generate severe memory allocations and GC pause spikes under 500 concurrent call ticks. | **Poor.** High CPU overhead constructing dynamic query graphs for per-second telemetry records. | **Moderate.** Struct-based batch inserts require SQL string construction and parsing. |
| **Low Setup Latency & Predictability (QAR-03, QAR-09)** | **Optimal.** Sub-millisecond deterministic execution. DBA runs `EXPLAIN (ANALYZE, BUFFERS)` directly on the exact SQL executed in production. Zero N+1 surprises. | **Severe Deficit.** Association preloading (`Preload`) frequently triggers unexpected N+1 database round-trips, ballooning call setup latency past the 2ms budget. | **Moderate.** Graph traversals generate complex multi-table joins that can degrade PostgreSQL query planner efficiency. | **Good.** Explicit queries, but schema changes force massive code re-generation across the codebase. |
| **Advanced Concurrency Patterns (QAR-05)** | **Optimal.** Native, unconstrained support for `SELECT ... FOR UPDATE SKIP LOCKED`, Common Table Expressions (CTEs), window functions, and monthly range partitions. | **Poor.** Does not cleanly support `SKIP LOCKED` or dynamic partition routing, forcing awkward raw SQL escapes that defeat the purpose of an ORM. | **Poor.** Requires writing bespoke schema modifiers and raw query overrides. | **Moderate.** Supports raw fragments, but awkward to compose with transactional workers. |

### 3.3 Decision Outcome: Native `pgx/v5` Selected

**Architectural Justification:**
In a multi-tenant telecommunications platform, cross-tenant data leakage is an existential compliance violation. GORM and other ORMs abstract away the physical database connection, making it impossible to mathematically prove that PostgreSQL session variables (`SET LOCAL app.tenant_id`) will never leak between pooled connections under race conditions. 

Furthermore, AtsaPBX must ingest per-second usage records (`usage_seconds`) and RTCP voice metrics for hundreds of concurrent calls. `pgx.CopyFrom` provides a **10x to 20x throughput advantage** over ORM reflection, ensuring the platform scales effortlessly without exhausting database memory or CPU.

#### Trade-offs Accepted & Mitigations:
- *Manual Struct Scanning:* Developers must scan query result sets into Go structs (using `rows.Scan` or `pgx.RowToStructByName`).  
  *Mitigation:* This overhead is intentional. It forces developers and AI agents to consider column indexes, data types, and memory allocations for every query.
- *Explicit Migration Management:* Schema evolution must be managed via explicit SQL migration scripts (`github.com/golang-migrate/migrate/v4`) rather than automatic ORM migrations.  
  *Mitigation:* Auto-migration is strictly forbidden in production enterprise systems. Controlled Expand/Contract SQL migrations are mandatory for zero-downtime upgrades (HLD Chapter 06, T-12).

---

## 4. Trade-Off Evaluation 3: Permissive FOSS & Supply-Chain Security

```
+---------------------------------------------------------------------------------------------------------+
|                                 FOSS & SUPPLY CHAIN SECURITY BOUNDARY                                   |
+---------------------------------------------------------------------------------------------------------+
|                                                                                                         |
|   Permissive Licenses Only              Continuous Security Gates              Zero Proprietary Locks   |
|   - MIT, Apache 2.0, BSD-3              - govulncheck in Jenkins CI            - Standard Go runtime    |
|   - Zero GPL / AGPL copyleft            - Trivy image vulnerability scan       - PostgreSQL 16 FOSS     |
|   - Zero partner distribution risk      - Locked .mise.toml toolchain          - NATS JetStream FOSS    |
|                                                                                                         |
+---------------------------------------------------------------------------------------------------------+
```

### 4.1 Legal & Commercial Licensing Analysis

| License Category | Examples | AtsaPBX Policy | Commercial Rationale |
|---|---|---|---|
| **Permissive FOSS** | MIT, Apache 2.0, BSD-2-Clause, BSD-3-Clause, ISC | **Mandatory** | Permits self-hosted enterprise deployment, on-premise hardware appliances, and commercial redistribution without requiring disclosure of proprietary business logic or partner customizations. |
| **Weak Copyleft** | LGPL, MPL 2.0 | **Prohibited** | Dynamic linking ambiguities in compiled Go static binaries risk triggering viral open-source disclosures. |
| **Strong Copyleft** | GPL v2, GPL v3, AGPL v3 | **Strictly Forbidden** | Poses immediate legal liability for enterprise partners. Any distribution of an appliance containing AGPL/GPL components could legally compel the disclosure of partner intellectual property. |
| **Proprietary / Dual-License** | Commercial SDKs, BSL / SSPL | **Strictly Forbidden** | Introduces subscription fees, artificial scalability restrictions, and vendor lock-in that compromise partner operational independence. |

### 4.2 Supply-Chain Defense & AI Agent Guardrails (DECISIONS D-28, D-30)

1. **Deterministic Pinning:** All external dependencies are pinned with cryptographic SHA-256 checksums in `go.mod` and `go.sum`. Unpinned dependencies (`@latest`) in Docker or CI builds are rejected.
2. **Automated Vulnerability Gating:**
   - **`govulncheck`:** Executes as a blocking step in the Jenkins CI pipeline. Any commit introducing a dependency with an unresolved Go vulnerability fails the build.
   - **Trivy Container Scanning:** Every production Docker image is scanned against the National Vulnerability Database (NVD); images with unmitigated High or Critical CVEs are blocked from deployment.
3. **No Unreviewed Agent Skills (D-30):** AI coding agents are strictly prohibited from downloading unvetted agent skill packages, third-party prompts, or external scaffolding scripts from online marketplaces. All agent capabilities must be authored internally in the codebase.

---

## 5. Architectural Decision Records (ADR) Summary

The trade-offs analyzed in this document are codified in the AtsaPBX Architectural Decision Log:

| ADR ID | Decision Title | Status | Primary Quality Attribute Driver |
|---|---|---|---|
| **ADR-004** | API-First Parity via ConnectRPC | **Accepted** | Contract Single Source of Truth (QAR-07), Zero Envoy Proxy (QAR-06) |
| **ADR-014** | Native `pgx/v5` Persistence over Heavy ORMs | **Accepted** | RLS Multi-Tenant Safety (QAR-01), High Ingestion Throughput (QAR-04) |
| **ADR-015** | Permissive FOSS & Supply Chain Governance | **Accepted** | License Compliance (QAR-02), Continuous CVE Auditing (QAR-02) |

---

## 6. Verification & Automated Compliance Gates

Automated gates enforce these architectural decisions across the development lifecycle:

1. **Dependency Boundary Linter:** CI runs `golangci-lint` with the `depguard` plugin to reject any import of forbidden packages (e.g. `gorm.io/*`, `github.com/gin-gonic/*`, `github.com/gofiber/*`, `github.com/sirupsen/logrus`).
2. **Protobuf Contract Verification:** `buf breaking --against .git#branch=main` runs on every pull request to ensure backwards compatibility of ConnectRPC services.
3. **Multi-Tenant RLS Test Harness:** Integration tests run concurrent simulated transactions across multiple tenants; test assertions verify that missing or mismatched `app.tenant_id` contexts fail closed immediately.
4. **Vulnerability Scanner:** `govulncheck ./...` runs on every pull request; exit code $\ne 0$ halts pipeline execution.
