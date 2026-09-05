<!-- OpenSpec: TRD-HLD-05 -->
# Bounded Contexts

All bounded contexts live as Go modules within the single AtsaPBX monolithic binary (`api/internal/...`). Cross-context communication is strictly governed: synchronous requests use Go public package interfaces (Inbound Ports); asynchronous notifications use versioned domain events over NATS JetStream (`event.call.*`, `event.telemetry.*`). Direct cross-context database access is strictly prohibited (D-20, D-31).

```text
+----------------------------------------------------------------------------------------------------+
|                                    AtsaPBX Modular Monolith                                        |
|                                                                                                    |
|   ┌────────────────────┐   publishes events    ┌───────────────────┐    validates verdicts         |
|   │   telephony-core   ├──────────────────────>│     reporting     │<───────────────────────────┐  |
|   └────────┬───────────┘                       └───────────────────┘                            │  |
|            │                                                                                    │  |
|            │ invokes ports                                                                      │  |
|            ▼                                                                                    │  |
|   ┌────────────────────┐   probes health       ┌───────────────────┐    pure function checks    │  |
|   │      pbx-core      ├──────────────────────>│    compliance     │<───────────────────────────┼──┐
|   └────────┬───────────┘                       └───────────────────┘                            │  │
|            │                                                                                    │  │
|            │ checks capacity                                                                    │  │
|            ▼                                                                                    │  │
|   ┌────────────────────┐   snoops audio copy   ┌───────────────────┐    delivers signed events  │  │
|   │     licensing      │                       │    ai-pipeline    │                            │  │
|   └────────────────────┘                       └───────────────────┘                            │  │
|                                                                                                 │  │
|   ┌────────────────────┐   authenticates       ┌───────────────────┐    delivers webhooks       │  │
|   │      identity      │                       │ webhook-delivery  │<───────────────────────────┘  │
|   └────────────────────┘                       └───────────────────┘                               │
|                                                                                                    │
|   ┌────────────────────┐   (R2/R3 Dialler)                                                         │
|   │       dialer       ├───────────────────────────────────────────────────────────────────────────┘
|   └────────────────────┘
+----------------------------------------------------------------------------------------------------+
```

---

## 1. `telephony-core`

- **Purpose:** Owns the `Call` and `CallParticipant` aggregate lifecycles, Stasis application orchestration, bridge lifecycle, and Asterisk Anti-Corruption Layer (ACL).
- **Aggregates:** `Call`, `CallParticipant`, `ChannelHistory`.
- **Inbound Ports (Go interfaces):**
  ```go
  type CallService interface {
      InitiateCall(ctx context.Context, cmd InitiateCallCommand) (CallID, error)
      AnswerParticipant(ctx context.Context, callID CallID, participantID ParticipantID) error
      HoldParticipant(ctx context.Context, callID CallID, participantID ParticipantID) error
      TransferParticipant(ctx context.Context, cmd TransferCommand) error
      HangupCall(ctx context.Context, callID CallID, reason string) error
  }
  ```
- **Outbound Ports (Go interfaces):**
  ```go
  type MediaGateway interface {
      Originate(ctx context.Context, req OriginateRequest) (ChannelRef, error)
      CreateBridge(ctx context.Context, bridgeType string) (BridgeID, error)
      AddChannelToBridge(ctx context.Context, bridgeID BridgeID, channelRef ChannelRef) error
      StartPlayback(ctx context.Context, channelRef ChannelRef, mediaURI string) error
      StartSnoop(ctx context.Context, channelRef ChannelRef, snoopReq SnoopRequest) (SnoopRef, error)
      DestroyChannel(ctx context.Context, channelRef ChannelRef) error
  }
  type CallStore interface {
      SaveCall(ctx context.Context, call *domain.Call) error
      GetCall(ctx context.Context, id CallID) (*domain.Call, error)
      AddChannelHistory(ctx context.Context, history *domain.ChannelHistory) error
  }
  ```
- **Published Domain Events:**
  - `event.call.initiated`: Call requested.
  - `event.call.active`: Two or more participants bridged.
  - `event.call.degraded`: Quality threshold crossed.
  - `event.call.terminated`: Call completed, billable seconds frozen.
  - `event.participant.joined`: New party entered bridge.
  - `event.participant.left`: Party departed bridge.
- **Forbidden Dependencies:** Zero imports of Web UI packages, partner billing logic, AI model clients, or Asterisk channel IDs outside `internal/telephony/acl`.
- **R2 / R3 Seams:** Participant collection models arbitrary parties (supervisor listen/barge in R2; simultaneous broadcast dispatch waves in R3).

---

## 2. `pbx-core`

- **Purpose:** Manages PBX configuration, extension registrations, BYOT SIP carrier trunks, Least-Cost Routing (LCR), visual IVR flow execution, and ACD call queues.
- **Aggregates:** `Extension`, `Trunk`, `CarrierRoute`, `IVRFlow`, `Queue`.
- **Inbound Ports (Go interfaces):**
  ```go
  type RoutePlanner interface {
      SelectOutboundRoute(ctx context.Context, tenantID TenantID, destination string) ([]TrunkRoute, error)
      ReportRouteHealth(ctx context.Context, trunkID TrunkID, statusCode int, latency time.Duration) error
  }
  type FlowInterpreter interface {
      ExecuteStep(ctx context.Context, state *FlowState, evt TelephonyEvent) (*FlowStepResult, error)
      ValidateFlowGraph(ctx context.Context, graphJSON []byte) error
  }
  ```
- **Outbound Ports (Go interfaces):** `TrunkStore`, `ExtensionStore`, `FlowStore`.
- **Published Domain Events:**
  - `event.pbx.route.degraded`: Carrier route failed health checks or setup threshold.
  - `event.pbx.flow.published`: New flow version deployed.
  - `event.pbx.extension.status_changed`: Endpoint presence updated.
- **Forbidden Dependencies:** Direct Asterisk channel creation; billing/rating calculations; dialer pacing.

---

## 3. `compliance` (D-21)

- **Purpose:** Implements regulatory and corporate compliance checks as **pure deterministic functions** (zero database I/O, zero network calls, zero state).
- **Function Contracts (Pure Go functions):**
  ```go
  type ComplianceEngine interface {
      EvaluateCallingHours(localTime time.Time, ruleset JurisdictionRules) Verdict
      EvaluateDoNotCall(destNumber string, dncSet DNCSet) Verdict
      EvaluateSpendCap(currentSpendMicros, capMicros int64) Verdict
      EvaluateAbandonmentLimit(abandonedCalls, answeredCalls int, thresholdPct float64) Verdict
  }
  ```
- **Inputs & Outputs:** Caller passes immutable snapshot of policy and call parameters; function returns `Verdict{Permitted: bool, Reason: string, RuleID: string}`.
- **Invariants:** Mandatory for both automated dialer and manual agent dials (BR-10). Completely testable with property-based unit testing.
- **R2 / R3 Seams:** Enforces FTC/FCC calling hours and abandonment rate caps in R2; verifies department spend caps and data residency rules in R3.

---

## 4. `licensing` (D-11, D-12, D-13, D-14, BR-05, EPIC-06)

- **Purpose:** Enforces commercial entitlement, cryptographically validates signed license keys, computes weighted hardware fingerprints, monitors atomic concurrent channel capacity, and executes 7-day offline grace degradation.
- **Aggregates:** `LicenseToken`, `HardwareFingerprint`, `CapacityCounter`, `GraceTracker`.
- **Inbound Ports (Go interfaces):**
  ```go
  type LicenseManager interface {
      ValidateCapacity(ctx context.Context, requestedChannels int) (CapacityVerdict, error)
      ReleaseCapacity(ctx context.Context, channels int) error
      ApplyLicenseKey(ctx context.Context, signedPayload []byte) error
      VerifyDailyEntitlement(ctx context.Context) (EntitlementStatus, error)
  }
  ```
- **Capacity Enforcement Invariants:**
  - Concurrent channel counter is atomic across threads.
  - Exceeding entitlement triggers the 10% burst allowance (up to 15 minutes, PRD T-3).
  - If capacity is exhausted, new calls are rejected with SIP `503 Service Unavailable` with `Retry-After: 30` (BR-05, D-11). **Never send 486 Busy.**
  - **Live calls in progress are NEVER terminated (INV-03).**
  - **Emergency calls ALWAYS connect regardless of license state (INV-01, D-12).**
- **Hardware Fingerprint Matching (D-13):** Weighted scoring across 5 machine attributes (CPU, motherboard, root disk UUID, MAC, system hostname). At least 3 of 5 must match. Tolerates VM migrations and cloud rebuilds.

---

## 5. `reporting` (BR-07, FBR-R1-10, EPIC-07)

- **Purpose:** Generates immutable Call Detail Records (CDRs), records per-participant per-second usage ledger entries (`usage_seconds`), aggregates RTCP-XR quality scores, and exports compliance data.
- **Aggregates:** `CDRRecord`, `UsageSecond`, `QualityAggregate`.
- **Inbound Ports (Go interfaces):**
  ```go
  type UsageRecorder interface {
      RecordUsageSecond(ctx context.Context, tick UsageTick) error
      RecordRTCPQualitySample(ctx context.Context, sample QualitySample) error
      FinalizeCDR(ctx context.Context, callID CallID) (*CDRReport, error)
  }
  type ReportExporter interface {
      ExportTenantBilling(ctx context.Context, query BillingQuery) (io.Reader, error)
  }
  ```
- **Invariants:**
  - Usage records are deduplicated by `(participant_id, second_timestamp)`.
  - All records are strictly append-only. Corrections are written as separate, attributable adjustment rows (BR-07).
  - Records are derived exclusively from `Participant` identity, never raw Asterisk channels.

---

## 6. `ai-pipeline` (D-05, D-06, D-23, BR-14, BR-17, EPIC-04)

- **Purpose:** Manages optional, out-of-band audio streaming to partner-selected AI providers for speech-to-text, live agent assist (R2), and specialist translation overlays (R3).
- **Aggregates:** `AISession`, `AudioStreamJob`, `TranscriptSegment`.
- **Inbound Ports (Go interfaces):**
  ```go
  type AIStreamOrchestrator interface {
      StartStream(ctx context.Context, req StreamRequest) (SessionID, error)
      IngestAudioFrame(sessionID SessionID, pcmFrame []byte) error
      StopStream(ctx context.Context, sessionID SessionID) error
  }
  ```
- **Outbound Ports (Go interfaces):**
  ```go
  type AIProviderClient interface {
      StreamAudioBidirectional(ctx context.Context) (AudioSender, ResultReceiver, error)
  }
  ```
- **Invariants:**
  - **Strictly Out-of-Band:** Operates exclusively on Asterisk Snoop channels (`ARI Snoop`).
  - **Zero Call Path Blocking:** If an AI provider slows down or disconnects, audio frames are discarded via tail-drop buffers. Call quality and call continuity are 100% unaffected (INV-04).
  - **Zero AI Dependency:** AtsaPBX operates fully without AI (INV-05). AI features are enabled per-tenant.

---

## 7. `identity` (BR-01, BR-15, FBR-R1-04, EPIC-01)

- **Purpose:** Manages tenants, user accounts, authentication tokens, API keys, role-based access control (RBAC), and append-only audit logs.
- **Aggregates:** `Tenant`, `Principal`, `RoleBinding`, `ApiKey`, `AuditEntry`.
- **Inbound Ports (Go interfaces):**
  ```go
  type IdentityService interface {
      AuthenticateUser(ctx context.Context, username, password string) (*AuthToken, error)
      ValidateToken(ctx context.Context, tokenString string) (*TenantContext, error)
      AuthorizeAction(ctx context.Context, principal PrincipalID, action string, resource string) error
      RecordAudit(ctx context.Context, entry AuditEntry) error
  }
  ```
- **Invariants:**
  - Every administrative action writes an immutable row to `audit_logs` containing `actor_id`, timestamp, before state, and after state (AC-01.3).
  - Passwords hashed with Argon2id; API keys stored as SHA-256 hashes.
  - Staff have no backdoor access to partner tenant data.

---

## 8. `webhook-delivery` (INV-06, AC-05.3, PRD §13.3)

- **Purpose:** Provides durable, at-least-once signed delivery of domain events to partner HTTPS webhook endpoints.
- **Aggregates:** `WebhookSubscription`, `DeliveryAttempt`, `DeadLetterEntry`.
- **Inbound Ports (Go interfaces):**
  ```go
  type WebhookDispatcher interface {
      QueueEvent(ctx context.Context, tenantID TenantID, event DomainEvent) error
      ProcessRetryQueue(ctx context.Context) error
      ReplayDeadLetter(ctx context.Context, deliveryID UUID) error
  }
  ```
- **Delivery Policy (PRD §13.3):**
  - Signatures: HMAC-SHA256 signature in `X-AtsaPBX-Signature` header using tenant signing key.
  - Exponential Backoff Retry Schedule: 1m, 5m, 15m, 1h, 6h, 24h (up to 6 attempts across 24 hours).
  - Circuit Breaker: Marked `Unhealthy` after 3 consecutive delivery failures; marked `Disabled` after 24 hours of sustained failure.
  - Calls are NEVER blocked by webhook delivery (INV-06).

---

## 9. `dialer` (R2 Contact Center & R3 Dispatch Seams)

- **Purpose:** Manages outbound contact center campaigns, dial lists, pacing algorithms, and sub-15-second simultaneous specialist dispatch waves.
- **Aggregates:** `Campaign`, `DialList`, `DialAttempt`, `DispatchWave`.
- **Implementation Phasing (D-27):**
  - R1.0: Domain seams established; manual click-to-dial only.
  - R2: Power dialling loop implemented first to prove telephony reliability; predictive pacing algorithm added second (D-27).
  - R3: `DispatchWave` abstraction triggers simultaneous broadcast calls to candidate specialists (first-accept-wins, D-10).
- **Invariants:** Every outbound attempt must receive a clearance verdict from `compliance` before Asterisk origination (BR-10).

---

## 10. Bounded Context Build & Dependency Graph

This is the single authoritative build-order reference. Its purpose is to
stop scope creep: a change proposed against a Tier-*N* context that
requires functionality from a higher tier is out of order and should be
split or deferred, not absorbed into the current change.

**Dependency order is not build order.** Architecturally, `identity` (tenancy)
is the correct first *dependency* — every table and event carries
`tenant_id` from commit one (D-24). But it is deliberately not the first
*build*: D-26 requires the media path to be proven before anything is built
on assumptions about it, and D-25 requires a walking-skeleton spike before
any formal specification. The build order below is risk-first; the
dependency graph is dependency-first. Where they conflict, Tier 0's first
module (`telephony-core`) is built against **stub adapters** for the ports
it needs from `identity`/`licensing`/`compliance`, satisfying the
dependency direction (still calls the port, never bypasses it) without
waiting for those contexts' real implementations.

```text
                    ┌───────────────┐  ┌───────────────┐  ┌───────────────┐  ┌───────────────┐
        TIER 0      │ telephony-core│  │   identity    │  │   licensing   │  │  compliance   │
     (zero bounded-  │  (Call/       │  │  (Tenant,     │  │  (Capacity,   │  │  (pure DNC/   │
      context deps)  │  Participant, │  │   RBAC, audit)│  │   fingerprint,│  │   hours/spend │
                      │  ACL)         │  │               │  │   grace)      │  │   functions)  │
                      └───────┬───────┘  └───────┬───────┘  └───────┬───────┘  └───────┬───────┘
                              │ ports called by  │ ports called by  │ ports called by  │
                              │ (Screening state) │ (auth middleware)│ (Screening state)│
                              ▼                  ▼                  ▼                  ▼
     ┌────────────────────────────────────────────────────────────────────────────────────────┐
     │  TIER 1 — depend on telephony-core's Call/Participant + identity's tenant context        │
     │  ┌─────────────┐   ┌─────────────┐   ┌──────────────────┐   ┌───────────────┐            │
     │  │  pbx-core   │   │  reporting  │   │ webhook-delivery │   │  ai-pipeline  │            │
     │  │ (routes real│   │ (consumes   │   │ (delivers events │   │ (snoops       │            │
     │  │  calls;     │   │  call/      │   │  from any        │   │  Participant  │            │
     │  │  needs a    │   │  participant│   │  context, needs  │   │  audio; needs │            │
     │  │  Call to    │   │  events,    │   │  identity signing│   │  telephony-   │            │
     │  │  route)     │   │  async)     │   │  keys)           │   │  core)        │            │
     │  └─────────────┘   └─────────────┘   └──────────────────┘   └───────────────┘            │
     └────────────────────────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
     ┌────────────────────────────────────────────────────────────────────────────────────────┐
     │  TIER 2 — R2, additionally depends on compliance + pbx-core + reporting                  │
     │  ┌──────────────────────────────────────────────────────────────────────────────────┐   │
     │  │  dialer  (power-dial loop first, predictive pacing second — D-27)                 │   │
     │  │  Needs: telephony-core (originate), compliance (BR-10 clearance),                 │   │
     │  │         pbx-core (queues/routing), reporting (usage)                              │   │
     │  └──────────────────────────────────────────────────────────────────────────────────┘   │
     └────────────────────────────────────────────────────────────────────────────────────────┘
```

### 10.1 Allowed-dependency matrix

| Context | May depend on (Go interface call or NATS subscribe) | Must NOT depend on |
|---|---|---|
| `telephony-core` | `identity` (tenant context, passive), `licensing` (capacity port), `compliance` (verdict port) — all via stub in LLD-01 | `pbx-core`, `reporting`, `dialer`, `ai-pipeline`, `webhook-delivery` |
| `identity` | nothing | every other context |
| `licensing` | nothing | every other context |
| `compliance` | nothing (pure functions — no I/O, so no dependency is even possible) | every other context |
| `pbx-core` | `telephony-core` (Call/Participant), `identity` | `dialer`, `ai-pipeline`, `reporting` |
| `reporting` | `telephony-core` events (async, NATS) | any synchronous call into another context |
| `webhook-delivery` | `identity` (signing keys), any context's published events (async) | synchronous calls into any context |
| `ai-pipeline` | `telephony-core` (Participant + Snoop) | `dialer`, `pbx-core`, billing/reporting internals |
| `dialer` (R2) | `telephony-core`, `compliance`, `pbx-core`, `reporting` | nothing further (it is the top of the graph) |

A pull request or OpenSpec change that adds an import violating this table
fails the Dependency Invariant Test (§6 of [01-architecture.md](01-architecture.md)).

### 10.2 LLD build sequence derived from this graph

1. **LLD-01 — `telephony-core` walking skeleton** (Tier 0, built first per D-25/D-26): Call/Participant lifecycle + Asterisk ACL, against stub `LicenseManager` and `ComplianceEngine` adapters.
2. **LLD-02 — `identity` + `licensing` real adapters**: replace the LLD-01 stubs behind the same ports (D-20 seam pays off here — zero changes to `telephony-core`).
3. **LLD-03 — `pbx-core`**: extensions, trunks, LCR, IVR-as-data, queues — now that real calls exist to route.
4. **LLD-04 — `compliance` + `reporting`**: real DNC/hours/spend functions; CDR/usage export.
5. **LLD-05 — `webhook-delivery` + `ai-pipeline`**: round out R1.0 API-first/optional-AI commitments.
6. **LLD-06 — `dialer` (R2)**: power-dial loop first, predictive pacing second (D-27), only after 1–4 exist.

This sequence is the scope-creep guard the graph exists for: an LLD is not
started until every context in its "must depend on" column already has at
least a stub satisfying its port contract.

