<!-- OpenSpec: TRD-TOOLSET-01 -->
# AtsaPBX — Approved Implementation Toolset, FOSS Policy & Architectural Trade-offs

This document defines the **authoritative, frozen toolset, dependency baseline, and architectural trade-off analysis** for AtsaPBX. 
All human engineers and AI coding agents MUST adhere to this baseline. Agents are strictly forbidden from introducing unapproved third-party dependencies, alternate frameworks, or redundant libraries without explicit human architectural authorization (DECISIONS D-28, D-30).

---

## 1. FOSS Policy & Security Governance Baseline

To safeguard AtsaPBX against supply-chain attacks, license liabilities, and runtime instability, all software components must comply with the following governance rules:

1. **Permissive Open-Source Licensing Only:**
   - **Allowed:** Apache 2.0, MIT, BSD-2-Clause, BSD-3-Clause, ISC.
   - **Strictly Prohibited:** AGPL, GPL (v2/v3), LGPL, SSPL, or commercial/proprietary licenses. AtsaPBX is designed for flexible enterprise deployment (including self-hosted, on-premises, and white-label distributions); viral or copyleft licenses present severe legal liabilities for our partners.
2. **Latest Stable Releases & No Transitive Bloat:**
   - Always target the latest GA (Generally Available) stable release of any dependency. Experimental alphas, release candidates (RCs), or abandoned forks are forbidden.
   - Minimal dependency graph: Choose libraries with zero or minimal transitive dependencies to shrink the binary footprint and attack surface.
3. **Continuous Vulnerability Auditing:**
   - Automated `govulncheck` runs on every pull request and CI build to detect known CVEs in Go dependencies before code merges.
   - Container images are scanned via Trivy during CI packaging; builds with unmitigated High or Critical CVEs fail automatically.
4. **Standard Library First:**
   - If the Go standard library provides a capability (`net/http`, `log/slog`, `crypto/aes`, `crypto/ed25519`, `sync`, `context`), use it. Do not introduce third-party wrappers (e.g. no `logrus`, `zap`, `zerolog`).
5. **Strict Version Pinning:**
   - All Go modules must be pinned with exact semantic versions and cryptographic hashes in `go.mod` and `go.sum`.
   - All developer and CI CLI tooling must be pinned in `.mise.toml`.

---

## 2. Solution Architect Trade-off Analysis

A critical responsibility of the Solution Architect is making deliberate, documented technology choices that balance performance, developer ergonomics, operational simplicity, and long-term maintainability. 

Below is the formal architectural justification and trade-off evaluation for two fundamental platform decisions: **ConnectRPC vs. Standard REST Frameworks** and **Native `pgx/v5` vs. Heavy ORMs (GORM/Ent)**.

```
+---------------------------------------------------------------------------------------------------------+
|                                    ATSA PBX ARCHITECTURAL BOUNDARY                                      |
+---------------------------------------------------------------------------------------------------------+
|                                                                                                         |
|   WebRTC Portal / Third-Party PBX Admin                        High-Throughput Services / Workers       |
|              (HTTP/1.1 JSON)                                             (HTTP/2 gRPC)                  |
|                     │                                                          │                        |
|                     ▼                                                          ▼                        |
|   ┌─────────────────────────────────────────────────────────────────────────────────────────────────┐   |
|   │                       ConnectRPC Ingress Layer (connectrpc.com/connect)                         │   |
|   │     - Single unified Handler for gRPC, gRPC-Web, and JSON-over-HTTP                             │   |
|   │     - Single Source of Truth: Protocol Buffer Schemas (api/proto/)                              │   |
|   │     - Native HTTP/2 Streaming for Real-Time Telephony Ticks (RTCP / Presence)                   │   |
|   │     - Zero Reverse Proxies (No Envoy required for web browsers)                                 │   |
|   └────────────────────────────────────────────────┬────────────────────────────────────────────────┘   |
|                                                    │                                                    |
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
|                                                                                                         |
+---------------------------------------------------------------------------------------------------------+
```

---

### 2.1 Why ConnectRPC Instead of Standard REST Frameworks (Gin, Fiber, Echo, Chi)?

| Architectural Evaluation Criterion | Standard REST Frameworks (Gin, Fiber, Echo) | ConnectRPC (`connectrpc.com/connect`) | Solution Architect Verdict |
|---|---|---|---|
| **Contract Source of Truth & Drift** | **Manual / Fragile.** Schema defined via handwritten OpenAPI/Swagger YAML or struct annotations. Inevitably drifts from Go implementation, causing runtime parsing failures and customer integration bugs. | **Strictly Typed Protobuf.** Services, requests, responses, and field types are compiled from `.proto` files into Go code, TypeScript SDKs, and CLI tools. Breaking changes are caught at compile time by `buf lint` and `buf breaking`. | **ConnectRPC Wins.** Contact centers have dozens of complex entities (Calls, Participants, Trunks, Queues). Type safety across Go backend and TypeScript frontend eliminates an entire class of integration regressions. |
| **Ingress Protocol Flexibility & Zero Proxy** | **HTTP/1.1 JSON Only.** Serving gRPC to internal microservices or gRPC-Web to browsers requires running a separate gRPC server plus an external Envoy proxy or `grpc-gateway` process. | **Unified Dual-Protocol Engine.** A single Go HTTP handler serves: (1) standard JSON over HTTP/1.1 for browsers/cURL/webhooks, (2) gRPC-Web for frontend clients, and (3) standard binary gRPC over HTTP/2 for server-to-server calls. | **ConnectRPC Wins.** Eliminates the operational complexity, memory footprint, and latency penalty of deploying and maintaining Envoy sidecars in self-hosted enterprise deployments. |
| **Telephony Telemetry Streaming** | **Fragmented / Bolted-On.** REST lacks streaming semantics. Developers must bolt on ad-hoc WebSockets (e.g. Gorilla) or SSE libraries with separate authentication paths, different URL routes, and manual reconnection logic. | **First-Class HTTP/2 Streaming.** ConnectRPC provides built-in server-streaming and bidirectional streaming over HTTP/2 with native Go `context.Context` cancellation. Used directly for real-time RTCP-XR voice quality feeds, agent presence, and live channel events. | **ConnectRPC Wins.** Real-time telephony requires a unified streaming protocol with consistent interceptor chains (auth, rate-limiting, metrics) across unary and streaming calls. |
| **Serialization Performance & Throughput** | **JSON Overhead.** JSON reflection marshaling consumes heavy CPU and yields verbose payloads. High-throughput bursts (30+ CPS dialer launches) degrade under JSON serialization pressure. | **High-Efficiency Protobuf.** Protobuf binary serialization is 5–10x faster and produces 30–50% smaller payloads. ConnectRPC supports both Protobuf binary and JSON transparently on the wire. | **ConnectRPC Wins.** Minimizes CPU consumption during high-density concurrent calls and reduces network bandwidth on egress telemetry. |
| **Standard Library Compatibility** | **Framework Lock-In / Unsafe Hacks.** Gin and Echo require bespoke router contexts. Fiber uses `fasthttp`, which violates Go `net/http` standards, breaks standard middleware, and uses unsafe byte-string conversions that risk memory corruption. | **100% `net/http` Standard.** ConnectRPC handlers are plain standard `http.Handler` implementations. They integrate flawlessly with Go's standard library, standard middleware, OpenTelemetry, and Prometheus. | **ConnectRPC Wins.** Clean alignment with Go standard library avoids vendor lock-in and memory safety hazards. |

#### Architectural Trade-offs Accepted for ConnectRPC:
1. *Learning Curve:* Engineers familiar only with ad-hoc REST paths (e.g. `GET /api/v1/calls/:id`) must adopt Protobuf RPC style (`POST /atsap.v1.TelephonyService/GetCall`). 
   - *Mitigation:* ConnectRPC supports GET requests with query parameters for cacheable queries, and RPC method names are self-documenting.
2. *HTTP Verb Granularity:* ConnectRPC relies primarily on POST (and optional GET), departing from strict RESTful CRUD semantics (PUT/PATCH/DELETE).
   - *Mitigation:* RPC semantics represent domain intentions far better than REST verbs (e.g., `TransferCall`, `PauseRecording`, `MergeBridges` are clear RPC methods, whereas mapping them to REST `PATCH /calls/123/participants` requires ambiguous and complex payload flags).

---

### 2.2 Why Native `pgx/v5` Instead of Heavy ORMs (GORM, Ent, SQLBoiler)?

| Architectural Evaluation Criterion | Heavy ORMs (GORM, Ent) | Native `pgx/v5` with Parameterized SQL | Solution Architect Verdict |
|---|---|---|---|
| **Multi-Tenant Row-Level Security (RLS)** | **Severe Leak Risk.** PostgreSQL 16 multi-tenancy in AtsaPBX mandates `SET LOCAL app.tenant_id = '<uuid>'` scoped strictly to the current transaction. ORMs manage connection checkout and transactions opaquely; a hook misconfiguration or raw query fallback can return a "dirty" connection to the pool, leaking tenant data across organizations. | **Guaranteed Session Isolation.** `pgx/v5` provides explicit `pgxpool.Pool` lifecycle hooks (`BeforeAcquire`, `AfterRelease`) and clean `pgx.Tx` scoping. `SET LOCAL app.tenant_id` is executed deterministically at the start of every transaction, and connections are sanitized on release. | **`pgx/v5` Wins (Critical).** In multi-tenant telecommunications, cross-tenant data leakage is a catastrophic, company-ending compliance violation (GDPR, HIPAA, SOC 2). Opaque ORM connection layers cannot be trusted with RLS session state. |
| **High-Frequency Ingestion (`usage_seconds`)** | **Reflection & Query Bottlenecks.** ORMs generate individual or batched `INSERT INTO ... VALUES (...)` statements using runtime reflection over struct fields. Under 500 concurrent channels emitting 1-second usage ticks and RTCP telemetry, ORM reflection triggers massive garbage collector (GC) pauses and CPU spikes. | **Binary Stream Protocol (`pgx.CopyFrom`).** `pgx/v5` exposes PostgreSQL's native `COPY` protocol via `pgx.CopyFrom`. This streams binary rows directly into PostgreSQL tables, bypassing SQL parsing entirely and achieving **50,000+ inserts/second** at sub-millisecond latency and near-zero memory allocations. | **`pgx/v5` Wins.** Essential for meeting PRD/TRD performance targets (1-second billing tick aggregation, live call usage metering, and CDR recording). |
| **Advanced SQL Features (`SKIP LOCKED`, Partitions)** | **Awkward / Fallback to Raw.** Implementing the Transactional Outbox pattern and Dialer Queue requires `SELECT ... FOR UPDATE SKIP LOCKED` and dynamic monthly table partitioning. ORMs either lack native support or require awkward, semi-broken DSL escapes that negate the ORM's benefit. | **Native, First-Class Support.** Parameterized SQL gives the architect full access to the complete PostgreSQL 16 feature set: `FOR UPDATE SKIP LOCKED`, Common Table Expressions (CTEs), window functions, jsonb path operations, and partition routing. | **`pgx/v5` Wins.** Critical for dead-lock-free, zero-latency job dispatch in the transactional outbox worker and campaign dialer. |
| **Query Predictability & N+1 Prevention** | **Hidden N+1 Traps.** Eager loading (`Preload`) and lazy loading magic frequently hide sub-optimal query plans. A simple change to a domain model can silently trigger hundreds of database round-trips during a high-load call routing event. | **100% Deterministic Execution.** Every query executed in production is written explicitly in SQL. DBAs and architects can run `EXPLAIN (ANALYZE, BUFFERS)` directly on production SQL without guessing how an ORM might serialize relationships. | **`pgx/v5` Wins.** In telephony call setup, routing lookups must complete in under **2 milliseconds** to preserve low PDD (Post-Dial Delay). Sub-millisecond determinism cannot be compromised by ORM abstractions. |

#### Architectural Trade-offs Accepted for `pgx/v5`:
1. *Boilerplate Struct Scanning:* Developers must manually scan SQL result rows into Go structs (e.g., using `pgx.RowToStructByName` or explicit `rows.Scan`).
   - *Mitigation:* The slight increase in initial keystrokes is vastly outweighed by the elimination of runtime reflection overhead, absolute type safety, and zero hidden queries.
2. *Schema Migration Discipline:* Schema changes must be written in explicit, forward-compatible SQL migrations (`api/migrations/`) using `golang-migrate`, rather than auto-migrated by an ORM.
   - *Mitigation:* Auto-migration is dangerous in production multi-tenant systems. Explicit Expand/Contract migrations (HLD Chapter 06, T-12) are mandatory for zero-downtime upgrades.

---

## 3. Approved Go Libraries & Drivers (`api/go.mod`)

The following libraries represent the **complete, approved, and frozen dependency baseline** for AtsaPBX:

| Category | Approved Package | Permissive FOSS License | Purpose & Justification | Status |
|---|---|---|---|---|
| **Database Driver & Pool** | `github.com/jackc/pgx/v5` (`pgxpool`) | MIT | High-performance PostgreSQL 16 driver; binary wire protocol, native connection pool, connection lifecycle hooks for RLS (`app.tenant_id`), `pgx.CopyFrom`. | **Mandatory** |
| **Database Migrations** | `github.com/golang-migrate/migrate/v4` | Apache 2.0 | Deterministic schema migrations. Zero-downtime Expand/Contract forward compatibility. | **Mandatory** |
| **API & RPC Framework** | `connectrpc.com/connect` | Apache 2.0 | ConnectRPC implementation for Go. Direct browser HTTP/1.1 JSON, gRPC-Web, and HTTP/2 gRPC compatibility. | **Mandatory** |
| **Protobuf Runtime** | `google.golang.org/protobuf` | BSD-3-Clause | Protocol Buffer serialization, reflection, and code-generation runtime. | **Mandatory** |
| **Event Bus & Messaging** | `github.com/nats-io/nats.go` | Apache 2.0 | High-throughput NATS JetStream 2.10+ client for durable asynchronous domain event streaming. | **Mandatory** |
| **WebSocket Client** | `github.com/gorilla/websocket` | BSD-2-Clause | Low-latency WebSocket client for Asterisk REST Interface (ARI) Stasis event loop. | **Mandatory** |
| **Identifiers** | `github.com/google/uuid` | BSD-3-Clause | RFC 4122 UUID v4 generation for `tenant_id`, `call_id`, `participant_id`. | **Mandatory** |
| **Password Hashing** | `golang.org/x/crypto/argon2` | BSD-3-Clause | Argon2id password hashing for PBX extensions and user credentials (OWASP recommended). | **Mandatory** |
| **Metrics** | `github.com/prometheus/client_golang` | Apache 2.0 | Prometheus metrics instrumentation (`prometheus/promhttp`) for RTCP-XR and telephony counters. | **Mandatory** |
| **Distributed Tracing** | `go.opentelemetry.io/otel`<br>`go.opentelemetry.io/otel/trace` | Apache 2.0 | OpenTelemetry distributed tracing and W3C `traceparent` propagation across bounded contexts. | **Mandatory** |
| **Unit Testing Assertions** | `github.com/stretchr/testify` | MIT | Clean test assertions (`assert`, `require`) for unit and integration suites. | **Mandatory** |

---

## 4. Explicitly Forbidden Dependencies & Anti-Patterns

Any pull request, commit, or agent-generated code containing the following will be **immediately rejected** by automated linters and CI review gates:

| Forbidden Dependency / Pattern | Why It Is Forbidden | Approved Alternative |
|---|---|---|
| **GORM / Ent / SQLBoiler** | Bypasses PostgreSQL RLS session variables, encourages untracked database mutations, causes N+1 query performance degradations. | Raw parameterized SQL with `github.com/jackc/pgx/v5` and `pgxpool`. |
| **Gin / Fiber / Echo / Chi** | Introduces dual-stack API drift, inconsistent error serialization, non-standard HTTP hacks (`fasthttp`), and bypasses ConnectRPC type-safety. | `connectrpc.com/connect` handlers with standard `net/http.Handler`. |
| **Logrus / Zap / Zerolog** | Redundant third-party logging packages that fragment log output and introduce unnecessary dependencies. | Go standard library `log/slog` with custom JSON formatting and PII scrubbing. |
| **Third-Party Asterisk Clients** (e.g. `cyolo/ari`, `nadir/ari`) | Heavy external abstractions that obscure correlation tokens, bridge control, and channel swapping during attended transfers. | Internal lean adapter under `api/internal/telephony/acl` using `gorilla/websocket` and `net/http`. |
| **Global Mutable State** | Package-level global variables (`var db *pgxpool.Pool`) prevent test isolation, race-condition safety, and multi-tenant isolation. | Dependency injection via constructor functions (`NewService(deps...)`). |
| **Arbitrary Unpinned Tools** | Running `go install ...@latest` in CI or Docker builds causes non-reproducible environments and supply-chain vulnerabilities. | Tooling pinned in `.mise.toml`. |
| **Copyleft / AGPL / Proprietary Libs** | Legal liability for self-hosted enterprise deployments and commercial partner distribution. | Permissive FOSS libraries (MIT, Apache 2.0, BSD-3-Clause) only. |

---

## 5. Developer & CI Toolchain (`.mise.toml`)

All developer environments and CI pipelines execute tools managed strictly by `mise`:

| Tool | Version | Config File | Purpose |
|---|---|---|---|
| **Go** | `1.25` | `.mise.toml` | Application compilation, unit testing, benchmark execution |
| **golangci-lint** | `2.13.1` (pinned) | `.mise.toml`, `.golangci.yml` | Strict static analysis, deadcode detection, dependency boundary linting |
| **govulncheck** | `v1.7.0` (pinned via `GOVULNCHECK_VERSION` in `Jenkinsfile`, mirrored in `mise run vuln`) | CI Pipeline | Continuous scanning for known Go vulnerabilities and CVEs |
| **buf** | `1.40+` | `api/buf.yaml` | Protocol Buffer compilation, linting, and breaking change detection |
| **docker** | `24+` | Host / CI | Local dev stack (`docker compose`) and production image builds |
| **trivy** | `0.52.2` (pinned image in `Jenkinsfile`) | CI Pipeline | Container image security scanning for OS-level and binary CVEs |
| **SIPp** | `3.7+` | Test harness | SIP load testing, CPS burst benchmarks, and WebRTC simulation |

### CI provider abstraction

The CI contract is provider-agnostic: `mise run ci` (`docs → lint → vuln → test`)
plus image build, Trivy scan, test-report publishing, and (on `main`) image
push + deploy. `Jenkinsfile` is the current Jenkins implementation of that
contract — not the contract itself. Any CI provider (GitHub Actions, GitLab
CI, …) that implements the same stages in the same order satisfies it; no
document may require a specific provider, only the contract.

---

## 6. Dependency Addition Protocol

If an engineer or AI agent identifies a genuine need for a new external Go package:

1. **Verify Standard Library:** Confirm that Go's standard library (`net`, `crypto`, `os`, `encoding`, `sync`) cannot fulfill the requirement cleanly.
2. **License Compatibility Check:** The library must possess a permissive open-source license (**MIT, Apache 2.0, BSD-2/3-Clause**). AGPL, GPL, or proprietary licenses are strictly forbidden.
3. **Maintenance & Security Review:** Must have active maintenance, zero open high/critical CVEs, and minimal transitive dependencies.
4. **Approval Gate:** Record the proposal in an OpenSpec change artifact (`openspec/changes/<change-name>/proposal.md`) citing the architectural justification and trade-off analysis before merging to `main`.
