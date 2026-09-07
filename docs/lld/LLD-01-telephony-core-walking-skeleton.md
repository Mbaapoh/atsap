# LLD-01 — Telephony Core Walking Skeleton

| | |
|---|---|
| **Bounded context** | `telephony-core` (Tier 0 — see [`04-bounded-contexts.md` §10](../hld/04-bounded-contexts.md#10-bounded-context-build--dependency-graph)) |
| **Status** | Implemented & archived — behavior lives in [`openspec/specs/telephony-core/call-lifecycle/spec.md`](../../openspec/specs/telephony-core/call-lifecycle/spec.md); change artifacts under `openspec/changes/archive/2026-09-07-telephony-core-originate-bridge-hangup/` |
| **Traces to** | BRD FBR-R1-01/§7.1; PRD EPIC-03 (calling only, no IVR yet), PRD §11.1 (Call lifecycle), PRD §11.1/T-5 (participant continuity); TRD Domain model; HLD [01-architecture.md](../hld/01-architecture.md), [03-domain-model.md](../hld/03-domain-model.md), [04-bounded-contexts.md §1](../hld/04-bounded-contexts.md); DECISIONS D-15–D-20, D-25, D-26, D-32 |
| **Why this is LLD-01** | D-25 (spike before spec), D-26 (prove the media path before building on assumptions about it). Every other bounded context — `pbx-core` for real routing, `dialer` for the predictive/power dialler, `compliance`, `reporting` — either calls into `telephony-core` or reacts to its events. Nothing else can be honestly specified until a real two-party call has gone `Initiated → Active → Terminated` through this code. |

## 1. Scope

**In scope (this LLD only):**
- `Call` and `CallParticipant` aggregates, persisted, RLS-protected.
- Asterisk Anti-Corruption Layer (ACL): correlation registry, Stasis event loop, bridge management, channel-to-participant mapping across the one transition this skeleton exercises (originate → answer → bridge → hangup — **not** a full attended transfer; that is verified separately once this skeleton is green, still within `telephony-core`, before `pbx-core` starts).
- Call lifecycle subset: `Initiated → Screening → Routing → Presenting → Active → Terminating → Terminated`. (`Degraded` is a real state in the domain type — see §3 — but nothing transitions into it yet; that needs RTCP-XR sampling, which is LLD-04/07's job.)
- Per-participant, per-second usage ticks (`usage_seconds`) from the moment a participant is `Connected` — this is D-24 extensibility seam #4, and it must exist from this first walking skeleton, not bolted on later.
- Outbox → NATS JetStream publishing of `event.call.*` / `event.participant.*`.
- One ConnectRPC method, `GetCall`, proving the "no channel ID ever leaves the ACL" invariant end-to-end.
- Relocating the existing `ari`/`ami` packages under the ACL boundary was
  completed by the codebase reorganisation that precedes this change (see
  §4 for the ACL design that consumes them).

**Explicitly out of scope (deferred to the LLD that owns it — see [`docs/lld/README.md`](README.md) index):**

| Deferred | Owning LLD | Why not now |
|---|---|---|
| Real extensions/trunks/LCR, IVR-as-data | LLD-03 (`pbx-core`) | `pbx-core` depends on `telephony-core` existing, not the reverse (dependency graph §10.1) |
| Real capacity/fingerprint licensing | LLD-02 (`licensing`) | Stubbed here (§5.3) behind the same port so zero `telephony-core` changes are needed when it lands |
| Real DNC/hours/spend compliance | LLD-02/04 (`compliance`) | Stubbed here (§5.3), pure-function port unchanged later |
| Real tenant/RBAC/audit | LLD-02 (`identity`) | A single hardcoded dev tenant is seeded (§6.2), replaced later |
| Recording, RTCP-XR quality, `Degraded` transitions | LLD-04/07 | Needs its own consent/storage/telemetry design, not required to prove the media path |
| WebRTC (DTLS-SRTP) ingress specifically | Follow-up inside `telephony-core`, after this lands | The ACL is transport-agnostic (any PJSIP endpoint becomes a Channel identically); proving the domain model against plain PJSIP first isolates WebRTC-specific risk (ICE/DTLS) from domain-model risk |
| Webhooks, AI pipeline, dialer | LLD-05, LLD-06 | Tier 1/2 in the dependency graph |

## 2. Go package layout

Per HLD [01-architecture.md §1.1](../hld/01-architecture.md), with the concrete change this LLD makes to what exists today:

```
api/internal/
├── shared/
│   ├── domain/          # NEW: TenantID, CallID, ParticipantID (typed UUID wrappers)
│   └── event/           # NEW: DomainEvent interface, envelope metadata
├── telephony/           # NEW bounded context root
│   ├── domain/          # NEW: Call, CallParticipant, state machine transition funcs (pure)
│   ├── ports/           # NEW: CallService, MediaGateway, CallStore, LicenseManager,
│   │                     #      ComplianceEngine, EventPublisher interfaces
│   ├── application/     # NEW: orchestrator implementing CallService; noop stub adapters
│   └── acl/
│       ├── ari/         # MOVED from api/internal/ari (unchanged import path fixed up)
│       └── ami/         # MOVED from api/internal/ami (kept, unused by this LLD — see §4.3)
├── postgres/            # NEW: pgxpool wiring, RLS session-var helper, outbox reader
├── nats/                # NEW: JetStream publisher
├── server/              # EXISTING — gains the ConnectRPC handler mount (§7)
├── config/              # EXISTING — gains Postgres/NATS/ACL config fields
└── logging/             # EXISTING — unchanged
```

**Why relocate `ari`/`ami` under `acl/`:** HLD 01-architecture.md §1.2 rule 3
states raw Asterisk concepts are "strictly forbidden outside
`api/internal/telephony/acl`." The packages now live under
`internal/telephony/acl/` (part of the codebase reorganisation that preceded
this change), which is what makes the Dependency Invariant Test
(HLD 01-architecture.md §6.1) enforceable — the rule only bites once the
packages it gates are under the path it checks. The client code itself
(`Client`, `StreamEvents`, `Originate`, etc.) does not need a rewrite — see
§4.

## 3. Domain types (`internal/telephony/domain`)

Pure Go, zero I/O, zero imports outside `internal/shared/domain`. This is the layer the IVR-pure-fold and compliance-pure-function pattern (D-21, D-22) also uses elsewhere — same shape, same testing story.

```go
type Call struct {
    ID           CallID
    TenantID     shared.TenantID
    Direction    Direction // Inbound | Outbound | Internal
    State        CallState
    SourceNumber string
    DestNumber   string
    StartedAt    time.Time
    AnsweredAt   *time.Time
    EndedAt      *time.Time
    Participants []*CallParticipant
}

type CallState string

const (
    CallInitiated   CallState = "Initiated"
    CallScreening   CallState = "Screening"
    CallRouting     CallState = "Routing"
    CallPresenting  CallState = "Presenting"
    CallActive      CallState = "Active"
    CallDegraded    CallState = "Degraded" // unreachable in this LLD — see §1
    CallTerminating CallState = "Terminating"
    CallTerminated  CallState = "Terminated"
)

type CallParticipant struct {
    ID              ParticipantID
    CallID          CallID
    TenantID        shared.TenantID
    Role            ParticipantRole // Caller | Agent | IVR | Queue | Supervisor | Specialist | AI
    EndpointURI     string
    State           ParticipantState // Invited | Ringing | Connected | OnHold | Transferring | Disconnected
    JoinedAt        time.Time
    AnsweredAt      *time.Time
    LeftAt          *time.Time
    BillableSeconds int
}
```

State transitions are pure functions returning `(newState, []DomainEvent, error)` — e.g. `TransitionCallToActive(call *Call) ([]DomainEvent, error)` enforces "at least 2 connected participants" as an aggregate invariant and refuses the transition otherwise, matching PRD §11.1's Active entry condition. This mirrors D-22's fold pattern without adopting it wholesale — no node-graph here, just guarded transitions, per D-31's "aggregates with invariants" (not "domain services by default").

`Degraded` and its RTCP-XR entry condition are represented in the enum (so LLD-07 doesn't need a migration to add a state value) but no code path produces the transition yet — this is a deliberate speculative-but-cheap seam, not scope creep, because it costs nothing beyond one enum value (contrast with D-24's actual four required seams, which this LLD implements in full).

## 4. Asterisk Anti-Corruption Layer (`internal/telephony/acl`)

### 4.1 Correlation registry

In-memory, per-process (see §9 for the crash-recovery limitation this implies — already an accepted platform limitation per `docs/DECISIONS.md` "Known and accepted limitations": *"Active calls on a lost media node end, unless call preservation is separately funded"*):

```go
type correlationRegistry struct {
    mu        sync.RWMutex
    byChannel map[string]correlation // Asterisk channel ID -> {CallID, ParticipantID}
}
```

Populated exactly per HLD [01-architecture.md §2.1](../hld/01-architecture.md) / D-19:
- **Outbound origination:** ACL calls `ari.Originate` with an explicit
  `channelId=atsa-part-<participant_id>-<uuid>` and a custom channel variable
  carrying the same IDs, so every channel we originate is self-describing to
  the ACL. (Being able to re-derive correlation from Asterisk's own channel
  var — `GET /channels/{id}/variable` — after an event-stream gap is
  deliberate design headroom, but no resync mechanism is built or exercised
  in this LLD; see §9.)
- **Inbound Stasis entry (`StasisStart`):** if the channel already carries `ATSA_PARTICIPANT_ID` (set by our own origination), reuse it; otherwise this is a fresh inbound leg — mint a new `CallID`/`ParticipantID` and set the variable immediately via ARI so any subsequent event referencing this channel is self-describing.

### 4.2 Required `ari` package additions

The existing `acl/ari.Client` (relocated from the original `api/internal/ari`)
already provides `Originate`, `AnswerChannel`, `HangupChannel`, `Play`,
`StreamEvents` — all reusable as-is. This LLD adds, following the same style
(plain `net/http`, no new dependency):

```go
// Originate gains explicit channel-ID and variable injection (D-19 correlation).
func (c *Client) Originate(ctx context.Context, req OriginateRequest) (string, error)

type OriginateRequest struct {
    Endpoint  string            // "PJSIP/1001"
    CallerID  string
    ChannelID string            // "atsa-part-<uuid>-<uuid>"
    Variables map[string]string // {"ATSA_PARTICIPANT_ID": "..."}
}

func (c *Client) CreateBridge(ctx context.Context, bridgeType string) (string, error)      // POST /bridges
func (c *Client) AddChannelToBridge(ctx context.Context, bridgeID, channelID string) error  // POST /bridges/{id}/addChannel
func (c *Client) DestroyBridge(ctx context.Context, bridgeID string) error                  // DELETE /bridges/{id}
func (c *Client) GetChannelVariable(ctx context.Context, channelID, name string) (string, error)
func (c *Client) SetChannelVariable(ctx context.Context, channelID, name, value string) error
```

`StartSnoop` (already on the HLD's `MediaGateway` port) is **not** implemented in this LLD — it returns `apperrors.ErrNotImplemented` until LLD-05 (`ai-pipeline`). The port method exists now so `telephony-core`'s port contract doesn't change shape later (avoids an interface-breaking edit down the line), but nothing calls it.

### 4.3 `ami` package

Not used by this LLD. Kept at its new path (`acl/ami`) unchanged, for a later LLD (queue/presence events in `pbx-core`, or CDR cross-checking in `reporting`) to pick up without another relocation. No AMI connection is opened by the walking skeleton.

### 4.4 Event handling loop

One goroutine per `ari.Client.StreamEvents`, dispatching on `Event.Type`:

| ARI event | ACL action | Domain effect |
|---|---|---|
| `StasisStart` | Register/confirm correlation; if this is the callee leg, `AnswerChannel` | `CallParticipant` → `Ringing` then `Connected` |
| `ChannelStateChange` (state=`Up`) | — | `CallParticipant.AnsweredAt` set |
| `ChannelDestroyed` / `ChannelHangupRequest` | Look up participant by channel ID | `CallParticipant` → `Disconnected`, `LeftAt` set; if last connected participant, `Call` → `Terminating` |
| (bridge fully empty) | `DestroyBridge` | `Call` → `Terminated`, `EndedAt` set, final outbox event |

Every domain effect above is written in **one PostgreSQL transaction** together with its outbox row (§6.3) — this is the transactional-outbox pattern from HLD 01-architecture.md §3.2, not optional for this LLD.

## 5. Ports (`internal/telephony/ports`)

Exactly the interfaces already declared in HLD [04-bounded-contexts.md §1](../hld/04-bounded-contexts.md) — this LLD does not change their shape, only implements them:

```go
type CallService interface {
    InitiateCall(ctx context.Context, cmd InitiateCallCommand) (CallID, error)
    AnswerParticipant(ctx context.Context, callID CallID, participantID ParticipantID) error
    HoldParticipant(ctx context.Context, callID CallID, participantID ParticipantID) error
    TransferParticipant(ctx context.Context, cmd TransferCommand) error // returns ErrNotImplemented in this LLD
    HangupCall(ctx context.Context, callID CallID, reason string) error
}

type MediaGateway interface { /* as in HLD 04-bounded-contexts.md §1 */ }
type CallStore interface   { /* as in HLD 04-bounded-contexts.md §1 */ }
```

### 5.1 `LicenseManager` — stub for this LLD

```go
type LicenseManager interface {
    ValidateCapacity(ctx context.Context, requestedChannels int) (CapacityVerdict, error)
}
```

`internal/telephony/application/noop_license.go`:
```go
// alwaysPermitLicense satisfies ports.LicenseManager until LLD-02 lands the
// real licensing bounded context behind the same port. Delete this file
// when that adapter is wired in — do not extend it with real logic here.
type alwaysPermitLicense struct{}

func (alwaysPermitLicense) ValidateCapacity(context.Context, int) (CapacityVerdict, error) {
    return CapacityVerdict{Permitted: true}, nil
}
```

### 5.2 `ComplianceEngine` — stub for this LLD

Same pattern, `noop_compliance.go`, always returns `Verdict{Permitted: true}`. The real port signature from HLD 04-bounded-contexts.md §3 is unchanged; only the adapter is a stub.

### 5.3 Why stub, not skip

The Screening state (PRD §11.1) calls both ports on every call, including in this LLD. Skipping the calls entirely (rather than calling a stub) would mean LLD-02's real adapters get wired into a code path that was never actually exercised — the walking skeleton is supposed to prove the *shape* of the system, including where licensing/compliance sit in the call flow, even before their real logic exists.

## 6. Data model

### 6.1 Migration scope (`api/migrations/0001_telephony_core.up.sql`, `golang-migrate`)

Exactly the subset of the full DDL in HLD [03-domain-model.md §5](../hld/03-domain-model.md) needed for this LLD — **not a new schema**, a subset of the already-agreed one:

- `tenants` (minimal — id, name, status)
- `calls`
- `call_participants`
- `channel_history`
- `usage_seconds`
- `outbox`
- RLS enabled + `tenant_isolation_*` policies on `calls`, `call_participants`, `usage_seconds`, `outbox`, exactly as specified in HLD 03-domain-model.md §5.

Deferred to later migrations (owned by their LLDs, per the dependency graph): `principals`, `carrier_trunks`, `carrier_routes`, `extensions`, `ivr_flows`, `flow_versions`, `recordings`, `audit_logs`.

### 6.2 Dev tenant seed

`api/migrations/0002_dev_tenant_seed.up.sql` inserts one fixed-UUID tenant row, gated behind a `ATSAPBX_SEED_DEV_TENANT=true` env var checked by the migration runner (never applied against a production config). Replaced by real tenant provisioning in LLD-02 — this migration is deleted, not left in place, once that lands.

### 6.3 Outbox worker

`internal/postgres` implements the poll loop from HLD 01-architecture.md §3.2 verbatim (`FOR UPDATE SKIP LOCKED`, batch 100, mark `published_at`). `internal/nats` publishes to subject `tenant.<tenant_id>.event.call.<action>` / `tenant.<tenant_id>.event.participant.<action>`, matching the subject convention already fixed in that section.

## 7. ConnectRPC surface (this LLD: one method)

`api/proto/atsapbx/v1/telephony.proto` (new — first use of `buf`/`connectrpc.com/connect`, already in `go.mod`/`TOOLSET.md`):

```protobuf
service TelephonyService {
  rpc GetCall(GetCallRequest) returns (GetCallResponse);
}
message GetCallResponse {
  string call_id = 1;
  string state = 2;
  repeated Participant participants = 3;
  // Deliberately no channel_id field anywhere in this schema.
}
```

`StreamCallEvents` (server-streaming, in the HLD's ingress design) is deferred — `GetCall` alone is enough to prove the "no Asterisk ID crosses the API boundary" gate.

## 8. Definition of done

Verbatim from HLD [`README.md` §5.3](../hld/README.md) (the Walking Skeleton Integration Test), plus this LLD's own additions marked ✦:

1. A PJSIP-to-PJSIP call between two statically-configured dev endpoints (`core/conf/pjsip.conf`, test fixtures — not the real `pbx-core` Extension feature) establishes an audio session with Asterisk.
2. Asterisk hands control to the Go Stasis application.
3. ACL creates a domain `Call` with stable `CallParticipant` entities.
4. ✦ `Screening` calls the stub `LicenseManager` and `ComplianceEngine` ports (not skipped).
5. ✦ Per-second `usage_seconds` rows are written with no duplicate `(participant_id, second_ts)` and no gap across a normal (non-transfer) call.
6. Outbox event publishes to NATS JetStream for `initiated`, `active`, and `terminated`.
7. `GetCall` (ConnectRPC) returns full call and participant state with zero Asterisk channel IDs anywhere in the response — verified by an automated schema scan (HLD 01-architecture.md §6.2), not just manual inspection.
8. ✦ `go-arch-lint` (or an AST test, per HLD 01-architecture.md §6.1) fails the build if any package outside `internal/telephony/acl` imports `acl/ari` or `acl/ami`.

## 9. Known limitations of this LLD (carried, not introduced)

- Correlation registry is in-memory: a process crash mid-call loses ACL state for calls in flight. This is the same "active calls on a lost media node end" limitation already accepted in `docs/DECISIONS.md`, not a new gap.
- A brief ARI WebSocket drop with the process alive is reconnected by the existing event-stream loop and the in-memory registry survives it, but events emitted during the gap are not replayed — an event-stream re-adoption/resync slice is tracked for later `telephony-core` work, not this LLD.
- No attended-transfer test yet (T-5's full scenario, HLD 03-domain-model.md §3) — this skeleton proves the simpler originate/answer/bridge/hangup path first, per D-26's incremental-risk framing; transfer continuity is the next thing built in `telephony-core` before `pbx-core` starts, still ahead of LLD-02.

## 10. OpenSpec handoff

This LLD was implemented as the OpenSpec change
`telephony-core-originate-bridge-hangup` (artifacts archived under
`openspec/changes/archive/2026-09-07-telephony-core-originate-bridge-hangup/`;
living spec at `openspec/specs/telephony-core/call-lifecycle/spec.md`).
`openspec/config.yaml`'s
`context:` surfaces `docs/hld/`, `docs/TRD.md`, and `docs/DECISIONS.md` to the
applying agent; this file is the design source. The codebase reorganisation
that preceded the change already relocated `ari`/`ami` under
`internal/telephony/acl` and created the skeleton package directories, so
tasks 5.1/5.2 are satisfied in the working tree before apply begins.
