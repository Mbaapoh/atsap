# LLD-01 — Telephony Core Walking Skeleton

| | |
|---|---|
| **Bounded context** | `telephony-core` (Tier 0 — see [`04-bounded-contexts.md` §10](../hld/04-bounded-contexts.md#10-bounded-context-build--dependency-graph)) |
| **Status** | §1–§10: implemented & archived. §11–§24 (2026-11-07): call-control design to the Flow Definition of Ready, not built. Archived skeleton — behavior lives in [`openspec/specs/telephony-core/call-lifecycle/spec.md`](../../openspec/specs/telephony-core/call-lifecycle/spec.md); change artifacts under `openspec/changes/archive/2026-09-07-telephony-core-originate-bridge-hangup/` |
| **Traces to** | BRD FBR-R1-01/§7.1; PRD EPIC-03 (calling only, no IVR yet), PRD §11.1 (Call lifecycle), PRD §11.1/T-5 (participant continuity); TRD Domain model; HLD [01-architecture.md](../hld/01-architecture.md), [03-domain-model.md](../hld/03-domain-model.md), [04-bounded-contexts.md §1](../hld/04-bounded-contexts.md); DECISIONS D-15–D-20, D-25, D-26, D-32 |
| **Why this is LLD-01** | D-25 (spike before spec), D-26 (prove the media path before building on assumptions about it). Every other bounded context — `pbx-core` for real routing, `dialer` for the predictive/power dialler, `compliance`, `reporting` — either calls into `telephony-core` or reacts to its events. Nothing else can be honestly specified until a real two-party call has gone `Initiated → Active → Terminated` through this code. |

## 1. Scope

**In scope (this LLD only):**
- `Call` and `CallParticipant` aggregates, persisted, RLS-protected.
- Asterisk Anti-Corruption Layer (ACL): correlation registry, Stasis event loop, bridge management, channel-to-participant mapping across the one transition this skeleton exercises (originate → answer → bridge → hangup — **not** a full attended transfer; that is verified separately once this skeleton is green, still within `telephony-core`, before `pbx-core` starts).
- Call lifecycle subset: `Initiated → Screening → Routing → Presenting → Active → Terminating → Terminated`. (`Degraded` is a real state in the domain type — see §3 — but nothing transitions into it yet; that needs RTCP-XR sampling, which is LLD-09's job.)
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
| Recording, RTCP-XR quality, `Degraded` transitions | LLD-09 | Needs its own consent/storage/telemetry design, not required to prove the media path |
| WebRTC (DTLS-SRTP) ingress specifically | Follow-up inside `telephony-core`, after this lands | The ACL is transport-agnostic (any PJSIP endpoint becomes a Channel identically); proving the domain model against plain PJSIP first isolates WebRTC-specific risk (ICE/DTLS) from domain-model risk |
| Webhooks, AI pipeline, dialer | LLD-05, LLD-10, LLD-06 | Tier 1/2 in the dependency graph |

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
    State           ParticipantState // Invited | Ringing | Connected | OnHold | Parked | Transferring | Disconnected (canonical set: §11.2)
    JoinedAt        time.Time
    AnsweredAt      *time.Time
    LeftAt          *time.Time
    BillableSeconds int
}
```

State transitions are pure functions returning `(newState, []DomainEvent, error)` — e.g. `TransitionCallToActive(call *Call) ([]DomainEvent, error)` enforces "at least 2 connected participants" as an aggregate invariant and refuses the transition otherwise, matching PRD §11.1's Active entry condition. This mirrors D-22's fold pattern without adopting it wholesale — no node-graph here, just guarded transitions, per D-31's "aggregates with invariants" (not "domain services by default").

`Degraded` and its RTCP-XR entry condition are represented in the enum (so LLD-09 does not need a migration to add a state value) but no code path produces the transition yet — this is a deliberate speculative-but-cheap seam, not scope creep, because it costs nothing beyond one enum value (contrast with D-24's actual four required seams, which this LLD implements in full).

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

`StartSnoop` (already on the HLD's `MediaGateway` port) is **not** implemented in this LLD — it returns `apperrors.ErrNotImplemented` until LLD-10 (`ai-pipeline`). The port method exists now so `telephony-core`'s port contract doesn't change shape later (avoids an interface-breaking edit down the line), but nothing calls it.

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

## 11. Call control: shared model (revision 2026-11-07)

Sections 11–24 refine [HLD-21 §1–§2](../hld/21-media-and-call-control-flows.md)
into the twelve-item Flow Definition of Ready ([README.md](README.md), D-98)
for the OpenSpec change `telephony-call-control`. Sections 11, 22 and 23 hold
what every flow shares; each flow section (12–20) states its own items
and points at the shared tables instead of repeating them.

**Test tier tags** (TESTING.md §8): **U** unit (pure, hermetic) · **I**
integration (Postgres `atsapbx_test`) · **W** wire (ConnectRPC handler
contract) · **E** e2e (rig: Asterisk 22.11.0 + Postgres + NATS) · **C**
console component · **B** browser (Playwright) · **A** acceptance/UAT
against a PRD criterion.

### 11.1 Ownership split

| Concern | Owner | How it crosses |
|---|---|---|
| Bridges, channels, MOH, snoop, device-state writes, Participant states, operation rows (`call_operations`, including the park's history), the park timer and every other timer that moves media | `telephony-core` (this LLD) | — |
| Who may do what, destination resolution (D-91) into a `RingPlan`/`Program`, outbound class and fraud screening of an external target, feature codes, recall and fallback chains, park lots **and slot allocation (`parked_calls`)**, pickup groups and candidate selection, rooms and PIN checks, MOH classes, monitoring policy and its audit, presence and lamp values | `pbx-core` ([LLD-03 §3.5–§3.15](LLD-03-pbx-core.md)) | Console RPCs live on `PbxService` (the `PlaceCall` precedent): `pbx-core` authorises, resolves, then calls the telephony inbound ports `CallControl` and `ConferenceControl` (§11.5). Phone-originated actions and every recall/fallback enter telephony first and ask `pbx-core` through `InboundRouter.ResolveInbound` / `InboundRouter.Continue`, the LLD-03 §4.2 pattern. Telephony reports facts back in process through `CallEventSink` |
| Capacity | `licensing` via `LicenseManager` | Reservation keys per §11.8 |
| Recording start/stop/pause | LLD-09 | Recording is on the call's main bridge `M`, which every flow keeps (§11.4), so call control never restarts it |

### 11.2 Canonical Participant states (resolves the audit finding)

The code's names are kept (`api/internal/telephony/domain/types.go`); two
are added. HLD-03, HLD-16 and HLD-21 now use exactly this set.

| State | Media position | Billable (usage ticks) | Counts as "live" for the end rule | Code today |
|---|---|---|---|---|
| `Invited` | Leg being originated or resolved; no alerting yet | no | no (yes if it is a transfer/consult/recall target) | exists |
| `Ringing` | Endpoint alerting (SIP 180/183). Was `Alerting` in HLD-21 and LLD-03 | no | as `Invited` | exists |
| `Connected` | In a conversation bridge (`M`, consult `K`, room), or answered by an in-platform application (IVR, voicemail) | yes | yes | exists |
| `OnHold` | Out of the conversation, hearing MOH: held by a party, by its own phone's peer, as the caller during a consult, or waiting in a queue (`hold_origin=queue`, §23.1) | yes | yes | exists |
| `Parked` | Alone in the call's main bridge `M` with bridge MOH, owning a slot (`pbx-core`'s `parked_calls` row); no other party | yes | yes | **new** |
| `Transferring` | The **transferee** while a blind-transfer target, recall or fallback rings; hears ringback or MOH | yes | yes | exists (meaning fixed) |
| `Disconnected` | Terminal. `left_reason` says why. Was `Left` in HLD-03/21 | stops | no | exists |

**Rules.**
- `Transferring` belongs to the **transferee** (A), never the transferor.
  The transferor (B) goes `Connected → Disconnected(transferred)`. The
  target (C) is a **new** Participant: a different person is a different
  participation (PRD T-5, D-17). HLD-16's "Transferring → Connected, same
  participant" is A's path. HLD-03 §3's example that reuses Agent A's
  Participant for Agent B is superseded by this rule.
- **No `Consulting` or `PickedUp` participant state.** A consult is an
  operation (§11.3): during it the caller is `OnHold` and the transferor
  and target are `Connected` in `K`. A picked-up leg is `Disconnected`
  with `left_reason=picked_up`, and the picker is a new `Connected`
  Participant.
- **Mute is a flag** (`muted`), not a state (unchanged).
- **Monitoring supervisors** (listen/whisper) are `Connected` Participants
  with role `SUPERVISOR` and `audibility` `listen|whisper`; they never count
  as live. A barging supervisor (`audibility=full`) counts.
- **Renames are documentation only.** No code or row ever stored `Left`
  or `Alerting`, so no data migration is needed. Migration 0030 adds a
  `CHECK` constraint on the final set. LLD-03's `Alerting`/`Left` (§3.5.1
  `CallEventSink`, §3.8, §3.9) read as `Ringing`/`Disconnected`, and its
  `ErrNotAlerting` is telephony's `ErrNotRinging`.

**Transition table** (pure: `domain.NextParticipantState(from, ev) (to, error)`).
Unlisted pairs return `ErrInvalidTransition`. Any non-terminal state also
goes to `Disconnected` on `Left(reason)`.

| From | Event | Guard | To | Side effect |
|---|---|---|---|---|
| — | `Created` | call not Terminated | Invited | — |
| Invited | `Alerted` | — | Ringing | `ringing_since` set |
| Invited, Ringing | `Answered` | bridged or app-answered | Connected | `answered_at`; ticks start; `participant.joined` |
| Invited, Ringing | `Failed(busy/no_answer/rejected/unreachable/cancelled)` | — | Disconnected | `left_reason`; no ticks |
| Ringing | `PickedUp` | a picker claimed this leg (§17) | Disconnected | `left_reason=picked_up` |
| Ringing | `AnsweredElsewhere` | sibling leg answered (ring group, pickup) | Disconnected | `left_reason=answered_elsewhere` |
| Connected | `Held(origin)` | not already held | OnHold | `hold_origin`; `call.held` |
| OnHold | `Resumed` | `hold_origin` ∈ {api, phone, consult}: the hold's operation is open | Connected | `call.resumed` |
| OnHold | `QueueConnected` | `hold_origin=queue`: no hold op exists; `QueueMedia.Connect` for this caller's entry (§23.1) | Connected | `call.resumed{hold_origin: queue}` |
| OnHold | `QueueLeft` | `hold_origin=queue`: `QueueMedia.Leave` (§23.1) | Transferring | as `BlindTransferStarted` |
| Connected, OnHold | `Parked(slot)` | slot reserved | Parked | `call.parked` |
| Parked | `Retrieved` | `pbx-core`'s park row won (§16) | Connected | `call.unparked` |
| Parked | `ComebackStarted` | `Continue{park_timeout}` returned a ring plan (§16) | Transferring | `call.park_recalling` |
| Parked | `ComebackToApplication` | `Continue{park_timeout}` returned voicemail/IVR (§16) | Connected | counterpart is the application; `call.park_fallback` |
| Connected, OnHold | `BlindTransferStarted` | subject of the operation | Transferring | `call.transfer_started` |
| Transferring | `TargetAnswered` | — | Connected | `call.transferred` |
| Transferring | `FallbackToApplication` | fallback is voicemail or IVR | Connected | counterpart is the application |
| Transferring | `FallbackToQueue` | fallback is a queue | OnHold | `hold_origin=queue` (§23.1) |
| OnHold | `ConsultCompleted` | subject of an attended op | Connected | `call.transferred` |
| Connected | `Rehomed(newCall)` | §11.6 | Disconnected | `left_reason=rehomed`; a new Participant on the new call continues the same channel |
| any non-terminal | `Left(reason)` | — | Disconnected | `left_at`; ticks stop; `participant.left` |

**`left_reason` — closed list** (CHECK `call_participants_left_reason_ck`
in 0030; Go `domain.LeftReason` with an exhaustive switch): `normal`,
`api_hangup`, `transferred`, `transfer_failed`, `parked`, `picked_up`,
`pickup_failed`, `answered_elsewhere`, `cancelled`, `busy`, `no_answer`,
`rejected`, `unreachable`, `rehomed`, `kicked`, `pin_failed`,
`no_moderator`, `room_ended`, `hold_abandoned`, `monitor_stopped`,
`voicemail_dropped`, `leg_lost`, `engine_lost`, `app_restart`.

**`Continue` reasons — closed list** (Go `ports.ContinueReason`; recorded
in `call_operations.reason`, whose CHECK includes them): `plan_terminal`
(a plan's `Destination` terminal), `hold_orphaned` (§12, and §14 when the
transferor leaves without drop-completes), `transfer_no_answer` (§13),
`transfer_recall` (§13: `pbx-core` returns the transferor's recall plan
with this reason echoed), `park_timeout` (§16), `pickup_failed` (§17),
`queue_overflow` (§23.1).

**Call end rule (replaces "fewer than two Connected").** Pure:
`(c *Call) EndDecision() (end bool, reason string)`. The code's current
rule (`connectedCount() < 2`, `transitions.go`) would end a call the
moment a party is held or parked; it is replaced by:

| Call kind | Ends when |
|---|---|
| `standard` | live parties (table above) ≤ 1 **and** no open operation owns a pending leg (transfer target, recall, consult) **and** the one live party, if any, is not `OnHold`/`Parked`/`Transferring` and not `Connected` to an in-platform application (IVR, voicemail) — those are waiting for someone (§12, §16, §23.1) |
| `room` (§18) | zero live parties, or the last moderator leaves and the room says so |
| any, emergency (INV-01) | never by a call-control timer or fallback; only by a party hanging up |

### 11.3 The call-control operation (new aggregate)

Every multi-step action is one `CallOperation` row. It says what was in
flight when something fails (§22), and its partial unique indexes make
"already held" and "two transfers at once" impossible.

| Kind | Open states | Terminal outcomes (`state` = `outcome` once `ended_at` is set) | Subject / target |
|---|---|---|---|
| `hold` | `Held`, `Recalling` | `Resumed`, `FellBack`, `Abandoned`, `Ended` (subject left), `Superseded`, `Lost` | held party / recall leg |
| `blind_transfer` | `TargetRinging`, `Recalling` | `Completed`, `Recalled`, `FellBack`, `Failed`, `TransfereeLeft`, `Lost` | transferee / answering leg |
| `attended_transfer` | `ConsultRinging`, `Consulting` (flag `active_side` = `caller`/`target`) | `Completed`, `Cancelled`, `ConvertedToBlind`, `TransfereeLeft`, `Failed`, `Lost` | transferee / consult target |
| `park` | `Parked`, `Recalling` | `Retrieved`, `FellBack`, `Abandoned`, `Lost` | parked party / retriever. This row is the park's history; `pbx-core`'s `parked_calls` row is the slot claim and is deleted at the end (LLD-03 §3.10) |
| `pickup` | `Claimed` | `Completed`, `CallerLeft`, `Lost` | claimed ringing leg / picker |
| `adhoc_add` | `TargetRinging` | `Completed`, `NoAnswer`, `Cancelled`, `Lost` | the added party / — (initiator in `initiator_participant_id`) |

- **Monitoring writes no operation row:** `monitor_sessions` (§20) is its
  record and carries its own partial unique indexes.
- `Lost` is the outcome of every kind when the engine or app is lost
  (§22); `Failed` exists only for transfers, where a plan can end with
  nobody answering.
- Migration 0031 enforces the per-kind sets with CHECKs (§11.9). Each
  kind's full state × event table is in its flow section. Terminal
  outcomes are immutable (INV-08).

### 11.4 Bridge topology and ARI primitives

| Bridge | ARI | Id (ACL-internal) | Lifetime |
|---|---|---|---|
| `M` main | `POST /bridges?type=mixing&bridgeId=atsa-m-<call>&name=…` | `atsa-m-<call_id>` | Life of the Call (today's code) |
| `K` consult | `type=mixing` | `atsa-k-<operation_id>` | Life of an attended operation |
| `S` snoop | `type=mixing` | `atsa-s-<monitor_session_id>` | Life of a monitor session |
| `Q` queue wait | `type=holding` + bridge MOH | `atsa-q-<queue_entry_id>` | While the caller waits in a queue (§23.1, LLD-03 §3.3.1) |

- **Hold has no bridge.** Hold = `POST /bridges/{M}/removeChannel` then
  `POST /channels/{id}/moh?mohClass=<class>`; resume = `DELETE
  /channels/{id}/moh` then `POST /bridges/{M}/addChannel`. A channel in
  Stasis outside a bridge keeps its media. This avoids a bridge per hold.
- **Park has no bridge either (D-99).** The parked party stays alone in
  `M` with `POST /bridges/{M}/moh`; retrieve stops the bridge MOH and adds
  the retriever to `M`. Reconcile finds a park by its open `park` row and
  `M` (§22).
- **`M` is never replaced.** Every flow adds and removes channels on `M`,
  so a bridge recording (LLD-09) is continuous through hold, transfer,
  park and pickup. Asterisk changes a mixing bridge's technology (native
  RTP for two, softmix for three or more) by itself, so an ad-hoc
  conference needs no bridge "upgrade".
- **Ids derived from platform ids stay inside the ACL** (D-19, rule 3 of
  HLD-01 §1.2). Nothing above the ACL sees a bridge or channel id.
- **No new event subscription.** Channel and bridge events for the app's
  own channels and bridges arrive without `subscribeAll`, which stays off
  while asterisk/asterisk#2170 is open (ASTERISK-INTEGRATION.md).
  Device-state **writes** need no subscription.

ARI resources this LLD adds to `acl/ari.Client`:

| Purpose | ARI | Used by |
|---|---|---|
| Remove from bridge | `POST /bridges/{id}/removeChannel?channel=` | all |
| Named bridge | `POST /bridges?type=&bridgeId=&name=` | all |
| Channel MOH | `POST` / `DELETE /channels/{id}/moh?mohClass=` | hold, transfer ringback-as-MOH, consult |
| Bridge MOH | `POST` / `DELETE /bridges/{id}/moh?mohClass=` | park, room waiting for a moderator |
| Ringback | `POST` / `DELETE /channels/{id}/ring` | transferee while the target rings |
| Bridge playback | `POST /bridges/{id}/play?media=` | room tones, announcements |
| Mute | `POST` / `DELETE /channels/{id}/mute?direction=in` | room mute |
| Snoop | `POST /channels/{id}/snoop?spy=&whisper=&app=&snoopId=` | §20 |
| Hang up with cause | `DELETE /channels/{id}?reason_code=26` (answered elsewhere) or `reason=` | pickup, ring-group siblings |
| Transfer progress | `POST /channels/{id}/transfer_progress?states=` | §15 (SPIKE-CC-1) |
| Device state | `PUT` / `DELETE /deviceStates/Stasis:<name>?deviceState=` | §21 |
| MWI | `PUT` / `DELETE /mailboxes/{name}?newMessages=&oldMessages=` | `MailboxPublisher` (LLD-03 §3.4) |
| Live inventory | `GET /channels`, `GET /bridges`, `GET /channels/{id}/variable` | §22 |

Events the event loop adds (translated inside `acl`, D-41 clause 2):
`ChannelStateChange` (Ringing), `Dial` (`dialstatus`), `ChannelHold`,
`ChannelUnhold`, `ChannelEnteredBridge`, `ChannelLeftBridge`,
`ChannelTransfer` (SPIKE-CC-1), `BridgeBlindTransfer`,
`BridgeAttendedTransfer`, `PlaybackFinished`, `StasisStart` for snoop
channels (`args=["snoop","<session>"]`).

### 11.5 Ports (Go): the contract LLD-03 §3.5.1 relies on

LLD-03 §3.5.1 lists what `pbx-core` needs; this section owns the final
names. Every name there is adopted; the two refinements (`Stasis:` device
prefix, `Ringing`/`Disconnected` event names) are noted where they occur.

| Port (telephony/ports) | Direction | This LLD |
|---|---|---|
| `InboundRouter.ResolveInbound` (exists) | telephony → pbx | `InboundDecision` gains `Plan *RingPlan`, `Program *Program`, `HoldClass string`, `Routing []byte`, `Feature *FeatureDecision` (`Pickup{CallID, ParticipantID}`, `Park{LotID, Slot, ExpiresAt, MOHClass}`, `Retrieve{CallID, ParticipantID}`, `Conference{RoomJoin}`), `RecallOf *ParticipantID` (the **recall marker**: set when the plan rings back the holder, transferor or parker named, so telephony moves the op to `Recalling`), and `RefusalBusy` (486, ARI reason `busy`). Each `RingLeg` carries `Pickable bool` (§17). `InboundRequest` gains `Purpose` (`arrival` \| `transfer` \| `consult`), `CallID` of the call being transferred, and the transferor's extension, so a REFER target or an API transfer target is resolved exactly like a dialled number (§13, §15) |
| `InboundRouter.Continue` (new) | telephony → pbx | `ContinueRequest{TenantID, CallID, ParticipantID, Destination, Routing, Reason ContinueReason, RecallOf *ParticipantID}` → `InboundDecision`. `Reason` is the closed list of §11.2 (`plan_terminal`, `hold_orphaned`, `transfer_no_answer`, `transfer_recall`, `park_timeout`, `pickup_failed`, `queue_overflow`); an unknown value is a programming error (`ErrUnknownReason`). `RecallOf` names the party to ring back (holder, transferor, parker). `pbx-core` owns every recall/fallback chain and its timers' values |
| Plan runner, Program runner | pbx → telephony (decision data) | Executed by `application`; first answer wins, siblings hung up `answered_elsewhere` (§11.4 cause 26). A transferee runs a plan exactly as an arriving caller does |
| `CallControl` (new inbound port) | pbx → telephony | Signatures below. `Park` / `Retrieve` / `Pickup` are LLD-03's names |
| `ConferenceControl.Join/Mute/Remove/Lock` | pbx → telephony | §18 |
| `DeviceStatePublisher.Set(name, state)` | pbx → telephony | §21. The ACL writes `Stasis:<name>`: ARI controls only `Stasis:` devices, so LLD-03's `Custom:` reads `Stasis:` (verified in SPIKE-P1) |
| `MailboxPublisher.Set(name, new, old)` | pbx → telephony | `PUT /mailboxes/{name}`; telephony adds nothing else |
| `CallEventSink` (pbx implements) | telephony → pbx, in process | Kinds `ParticipantRinging` (with `Pickable`, `ringing_since`), `ParticipantAnswered`, `ParticipantHeld`, `ParticipantResumed`, `ParticipantParked`, `ParticipantTransferring`, `ParticipantLeft` (with `left_reason`), `MonitorStarted`, `MonitorStopped` (session id, reason; §20), `RegistrationChanged`. Non-blocking, buffer 1024; on overflow telephony raises `Resync` and `pbx-core` reads `CallControl.LiveParticipants` |
| `MediaStore.Store` | telephony → shared | LLD-09; unchanged here |

```go
// telephony/ports — inbound, implemented by application, called by pbx-core.
// CallService.HoldParticipant delegates to Hold; TransferParticipant
// (TransferCommand{NewDestEndpointURI}) is replaced by BlindTransfer.
type CallControl interface {
    Hold(ctx context.Context, cmd HoldCommand) (OperationID, error)
    Resume(ctx context.Context, cmd OperationCommand) error
    BlindTransfer(ctx context.Context, cmd TransferCommand) (OperationID, error)
    StartConsult(ctx context.Context, cmd TransferCommand) (OperationID, error)
    SwapConsult(ctx context.Context, cmd OperationCommand) error
    CompleteTransfer(ctx context.Context, cmd OperationCommand) error
    CancelConsult(ctx context.Context, cmd OperationCommand) error
    AddParticipant(ctx context.Context, cmd TransferCommand) (OperationID, error) // ad hoc third party
    Park(ctx context.Context, cmd ParkCommand) (OperationID, error)
    Retrieve(ctx context.Context, cmd RetrieveCommand) error
    Pickup(ctx context.Context, cmd PickupCommand) error // ErrNotRinging when the leg stopped ringing
    StartMonitor(ctx context.Context, cmd MonitorCommand) (MonitorSessionID, error)
    ChangeMonitorMode(ctx context.Context, id MonitorSessionID, mode MonitorMode) error
    StopMonitor(ctx context.Context, id MonitorSessionID) error
    LiveParticipants(ctx context.Context, t shareddomain.TenantID) ([]ParticipantView, error) // presence resync
}

type ConferenceControl interface {
    Join(ctx context.Context, cmd JoinCommand) error // ErrFull, ErrLocked
    Mute(ctx context.Context, room RoomID, p shareddomain.ParticipantID, on bool) error
    Remove(ctx context.Context, room RoomID, p shareddomain.ParticipantID) error
    Lock(ctx context.Context, room RoomID, on bool) error
}

type Actor struct { // who asked; authorised by pbx-core, recorded here
    PrincipalID   *shareddomain.PrincipalID
    ParticipantID *shareddomain.ParticipantID // the actor's own leg on the call, if any
    Origin        Origin // api | phone_refer | phone_native | phone_hold | feature_code | timer
}
type HoldCommand      struct { TenantID shareddomain.TenantID; CallID shareddomain.CallID; Subject shareddomain.ParticipantID; MOHClass string; Actor Actor }
type OperationCommand struct { TenantID shareddomain.TenantID; OperationID OperationID; Actor Actor }
type TransferCommand  struct {
    TenantID shareddomain.TenantID; CallID shareddomain.CallID
    Subject  shareddomain.ParticipantID // the transferee (A)
    Actor    Actor                      // normally B
    Target   InboundDecision            // from ResolveInbound(Purpose=transfer|consult); never a raw dial string
}
type ParkCommand     struct { TenantID shareddomain.TenantID; CallID shareddomain.CallID; Subject shareddomain.ParticipantID; LotID LotID; Slot int; MOHClass string; ExpiresAt time.Time; Actor Actor }
type RetrieveCommand struct { TenantID shareddomain.TenantID; CallID shareddomain.CallID; Parked shareddomain.ParticipantID; Retriever Leg }
type PickupCommand   struct { TenantID shareddomain.TenantID; CallID shareddomain.CallID; Ringing shareddomain.ParticipantID; Picker Leg }
type JoinCommand     struct { TenantID shareddomain.TenantID; Room RoomID; Entrant Leg; Moderator, Muted, WaitForModerator, EndWithLastModerator, EntryTone bool; WaitTimeout time.Duration; Limit int; MOHClass string }
// Leg names an arriving channel by its provisional Call and Participant (§11.6), or
// asks telephony to originate one (console retrieve/pickup, click-to-call D-78).
type Leg struct { CallID *shareddomain.CallID; ParticipantID *shareddomain.ParticipantID; Originate *OriginateTarget }
```

Supporting types (`RingPlan`, `Program`, `Terminal`, `Destination`) are
`pbx-core`'s pure data (LLD-03 §3.5.2), declared in `telephony/ports` so
telephony never imports `pbx`. The static `[stasis-in]` context gains
one hand-written `_*X.` pattern so `*` codes reach Stasis; nothing is
generated (D-47).

**Engine-facing additions** (`MediaGateway`, ACL adapter): `CreateNamedBridge`,
`RemoveChannelFromBridge`, `StartMOH`/`StopMOH`, `StartBridgeMOH`/`StopBridgeMOH`,
`StartRinging`/`StopRinging`, `PlayToBridge`, `SetMute`,
`StartSnoop(ch, SnoopRequest{Spy, Whisper, SnoopID})` (replaces the stub),
`HangupWithCause`, `ReportTransferProgress`, `SetDeviceState`, `SetMailbox`,
`ListLiveChannels`, `ListLiveBridges`.

**Pure domain functions** (`telephony/domain`, no I/O, 100 % unit-covered):

| Function | Purpose |
|---|---|
| `NextParticipantState(from, ev) (ParticipantState, error)` | §11.2 table |
| `(*Call) Hold / Resume / BeginBlindTransfer / TargetAnswered / Park / Unpark / PickUp / Rehome / Kick (…) ([]event.DomainEvent, error)` | aggregate methods; each enforces its guards |
| `(*Call) EndDecision() (bool, string)` | §11.2 end rule |
| `NextOperationState(kind, from, ev) (OpState, error)` | §11.3 per-kind tables |
| `AdmitToRoom(limit, live int, locked, moderatorPresent, isModerator, waitForModerator bool) Admission` | §18 (`Admit`, `Wait`, `Full`, `Locked`) |
| `NextRoomState(from, ev) (RoomState, error)` | §18 |

`SelectPickup` (oldest first), `ChooseSlot`, `RoleForPin` and the lamp
mapping are `pbx-core`'s pure functions (LLD-03 §3.8–§3.11).

### 11.6 Re-homing a channel to another Call

A conference entrant, a picker and a phone's consult call all arrive as
their own provisional Call (StasisStart creates one, §4.1). When the
router says the channel belongs to another Call, telephony **re-homes**
it in one transaction:

1. the provisional Participant → `Disconnected(rehomed)`; its usage stops at `t`;
2. a new Participant on the target Call starts at the same `t` with the
   same extension/principal attribution;
3. the provisional Call → `Terminated(merged)`, `merged_into_call_id` set;
4. the ACL re-points correlation and rewrites `ATSA_PARTICIPANT_ID` /
   `ATSA_CALL_ID` on the channel (`POST /channels/{id}/variable`);
5. capacity: the provisional call's reservation key moves to the new
   Participant's `capacity_key`, and is released when that Participant
   leaves, not when the provisional Call terminates (§11.8).

No second of usage is lost or counted twice (D-24, PRD T-5). A Participant
belongs to one Call; continuity across calls is the `rehomed` link.

### 11.7 Shared configuration (defaults are defaults)

"Owner" says where the value is stored and edited. Values owned by
`pbx-core` reach telephony inside a decision, a command or a `Continue`
answer; telephony stores none of them.

| Key | Default | Range | Scope | Owner | Used in |
|---|---|---|---|---|---|
| MOH class | tenant default class | an existing class | tenant; per extension, queue, lot, room | pbx-core (LLD-03 §3.15) via `HoldClass`/`MOHClass` | §12, §14, §16, §18 |
| `hold.reminder_interval` | 120 s | 0 (off), 30–600 s | installation (env `ATSAPBX_HOLD_REMINDER`); tenant override later | telephony | §12 |
| hold orphan recall | ring holder's devices 20 s, then holder's no-answer rule | — | extension | pbx-core (`Continue` `hold_orphaned`) | §12 |
| transfer target ring | destination's ring time (20 s default) | 5–120 s | destination | pbx-core (plan) | §13, §14 |
| blind transfer no answer | `destination_rule` (OD-CC-3) | `destination_rule`, `recall_transferor` | tenant | pbx-core (plan `OnExhausted`) | §13 |
| transfer recall after / ring | 15 s / 20 s | 5–120 s / 10–60 s | tenant | pbx-core | §13, §14 |
| transfer fallback | none | a D-91 destination | tenant | pbx-core | §12, §13 |
| `transfer.consult_ring` | 30 s | 5–120 s | tenant | pbx-core (consult plan) | §14 |
| `transfer.attended_drop_completes` | true | bool | installation | telephony | §14 |
| park slots / timeout / comeback | 701–720 / 60 s / 20 s, then fallback | LLD-03 §3.10 | lot | pbx-core | §16 |
| `conference.adhoc_max` | 5 parties | 3–10 | installation | telephony | §18 |
| room limit, PIN attempts, wait, end-with-moderator | 25 / 3 / off (600 s) / off | LLD-03 §3.11 | room | pbx-core (`JoinCommand`) | §18 |
| call waiting | on, 1 waiting call | off, 1–2 | extension | pbx-core (busy rule) | §19 |
| `control.ari_step_timeout` | 5 s | 1–15 s | installation (env) | telephony | all |
| feature codes | `*8`, `**<ext>`; park = lot number 700, retrieve = slot number | — | tenant | pbx-core (LLD-03 §3.13) | §15–§17 |

### 11.8 Capacity, emergency and tenancy rules for every flow

| Rule | How it holds |
|---|---|
| Capacity is per simultaneous call (LLD-08) | A blind-transfer target, recall, pickup or retrieval replaces a party: no new reservation. **OWNER DECISION (OD-CC-1):** a consult leg and each ad-hoc party beyond two reserve one unit, key `<call_id>/<participant_id>`, released when that Participant leaves. Recommended: yes (3CX counts a consult as a call). Refusal → `RESOURCE_EXHAUSTED` and the call is untouched |
| INV-01 emergency | `InboundDecision.BypassScreening` (assigned only in `pbx/application`) skips capacity for an emergency target. An emergency Call is never ended by a hold, park or transfer timer or fallback (§11.2); with no fallback it stays held and keeps recalling |
| INV-03 | No call-control failure ends a live conversation. A failed step returns every party to the last stable position (§22) |
| INV-10 | Every row is tenant-scoped under RLS; a call, lot, slot or room of another tenant is `NOT_FOUND`, identical to a missing one; device-state names carry ids, never tenant-local numbers |

### 11.9 Data model (migrations 0030–0033)

Every table: `ENABLE` + `FORCE ROW LEVEL SECURITY`, policy
`USING (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid)`
with the same `WITH CHECK`, grants to `atsapbx_app` only, and retention with
the call record (`ON DELETE CASCADE` from `calls`; LLD-09 sets the period).

```sql
-- 0030_call_control_participants.up.sql
ALTER TABLE calls ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'standard'
    CHECK (kind IN ('standard','room'));
ALTER TABLE calls ADD COLUMN merged_into_call_id UUID REFERENCES calls(id);
ALTER TABLE calls                          -- §23.3: a dialler-originated call carries its gate decision
    ADD COLUMN compliance_decision_id UUID,
    ADD COLUMN dialer_attempt_id UUID,
    ADD COLUMN pci_paused BOOLEAN NOT NULL DEFAULT false,   -- §23: PCI pause is call state (LLD-09 RC-07)
    ADD COLUMN consent VARCHAR(16) NOT NULL DEFAULT 'not_evaluated' CHECK (consent IN
      ('not_evaluated','not_required','pending','satisfied','failed'));
ALTER TABLE call_participants
    ADD COLUMN left_reason VARCHAR(32),
    ADD COLUMN hold_origin VARCHAR(16) CHECK (hold_origin IN ('api','phone','consult','queue','system')),
    ADD COLUMN audibility VARCHAR(8) NOT NULL DEFAULT 'full' CHECK (audibility IN ('full','listen','whisper')),
    ADD COLUMN muted BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN ringing_since TIMESTAMPTZ,
    ADD COLUMN state_changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN rehomed_from UUID REFERENCES call_participants(id),
    ADD COLUMN capacity_key VARCHAR(80),
    ADD CONSTRAINT call_participants_state_ck CHECK (state IN
      ('Invited','Ringing','Connected','OnHold','Parked','Transferring','Disconnected')),
    ADD CONSTRAINT call_participants_left_ck CHECK ((state = 'Disconnected') = (left_reason IS NOT NULL)),
    ADD CONSTRAINT call_participants_left_reason_ck CHECK (left_reason IN
      ('normal','api_hangup','transferred','transfer_failed','parked','picked_up','pickup_failed',
       'answered_elsewhere','cancelled','busy','no_answer','rejected','unreachable','rehomed',
       'kicked','pin_failed','no_moderator','room_ended','hold_abandoned','monitor_stopped',
       'voicemail_dropped','leg_lost','engine_lost','app_restart'));
-- rows written before 0030 get left_reason 'normal' in the same migration
ALTER TABLE usage_seconds                 -- LLD-09 RC-06: no fabricated quality
    ALTER COLUMN quality_mos DROP NOT NULL, ALTER COLUMN quality_mos DROP DEFAULT,
    ALTER COLUMN quality_jitter DROP NOT NULL, ALTER COLUMN quality_jitter DROP DEFAULT,
    ALTER COLUMN quality_loss DROP NOT NULL, ALTER COLUMN quality_loss DROP DEFAULT;
-- PJSIP Realtime projection columns (D-47). ps_* tables are projections owned
-- by pbx-core's ACL (pbx/acl/asterisk): it writes these columns in the same
-- transaction as the extension row; telephony-core only adds the columns the
-- engine needs for §12 and §15 and never writes ps_*. No tenant_id, no RLS;
-- the engine role keeps SELECT only.
ALTER TABLE ps_endpoints
    ADD COLUMN moh_suggest VARCHAR(80),        -- §12: the extension's MOH class (tenant default, LLD-03 §3.15)
    ADD COLUMN transfer_handling VARCHAR(16);  -- §15: 'ari-only' when SPIKE-CC-1 passes; NULL = engine default

-- 0031_call_operations.up.sql
CREATE TABLE call_operations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    call_id UUID NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    kind VARCHAR(24) NOT NULL CHECK (kind IN
      ('hold','blind_transfer','attended_transfer','park','pickup','adhoc_add')),  -- monitor: no row (§11.3)
    origin VARCHAR(16) NOT NULL CHECK (origin IN
      ('api','phone_refer','phone_native','phone_hold','feature_code','timer')),
    state VARCHAR(24) NOT NULL,
    subject_participant_id UUID NOT NULL REFERENCES call_participants(id),
    initiator_participant_id UUID REFERENCES call_participants(id),
    target_participant_id UUID REFERENCES call_participants(id),
    actor_principal_id UUID,
    destination JSONB,            -- D-91 {kind, ref}; never an engine address
    active_side VARCHAR(8) CHECK (active_side IN ('caller','target')),
    related_call_id UUID REFERENCES calls(id),
    deadline_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    outcome VARCHAR(24),
    reason VARCHAR(48),
    related_operation_id UUID REFERENCES call_operations(id), -- ConvertedToBlind → the blind op (§14)
    CHECK ((ended_at IS NULL) = (outcome IS NULL)),
    CHECK (outcome IS NULL OR outcome = state),
    CONSTRAINT call_operations_state_ck CHECK (
         (kind = 'hold'              AND state IN ('Held','Recalling','Resumed','FellBack','Abandoned','Ended','Superseded','Lost'))
      OR (kind = 'blind_transfer'    AND state IN ('TargetRinging','Recalling','Completed','Recalled','FellBack','Failed','TransfereeLeft','Lost'))
      OR (kind = 'attended_transfer' AND state IN ('ConsultRinging','Consulting','Completed','Cancelled','ConvertedToBlind','TransfereeLeft','Failed','Lost'))
      OR (kind = 'park'              AND state IN ('Parked','Recalling','Retrieved','FellBack','Abandoned','Lost'))
      OR (kind = 'pickup'            AND state IN ('Claimed','Completed','CallerLeft','Lost'))
      OR (kind = 'adhoc_add'         AND state IN ('TargetRinging','Completed','NoAnswer','Cancelled','Lost'))),
    CONSTRAINT call_operations_terminal_ck CHECK (ended_at IS NULL OR state NOT IN
      ('Held','Recalling','TargetRinging','ConsultRinging','Consulting','Parked','Claimed')),
    CONSTRAINT call_operations_reason_ck CHECK (reason IS NULL OR reason IN
      ('plan_terminal','hold_orphaned','transfer_no_answer','transfer_recall','park_timeout',
       'pickup_failed','queue_overflow','superseded','transferor_left','target_no_answer',
       'target_left','transferee_left','initiator_left','moh_failed','engine_lost','app_restart'))
);
CREATE UNIQUE INDEX call_operations_open_subject
    ON call_operations (tenant_id, subject_participant_id) WHERE ended_at IS NULL;
CREATE UNIQUE INDEX call_operations_open_transfer
    ON call_operations (tenant_id, call_id) WHERE ended_at IS NULL
    AND kind IN ('blind_transfer','attended_transfer');
CREATE INDEX call_operations_call ON call_operations (tenant_id, call_id, started_at);

-- 0032_conference_sessions.up.sql
CREATE TABLE conference_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    room_id UUID NOT NULL,         -- pbx-core's room
    call_id UUID NOT NULL UNIQUE REFERENCES calls(id) ON DELETE CASCADE, -- the room Call (kind='room')
    state VARCHAR(8) NOT NULL CHECK (state IN ('Waiting','Active','Ended')),
    locked BOOLEAN NOT NULL DEFAULT false,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    peak_participants SMALLINT NOT NULL DEFAULT 0,
    end_reason VARCHAR(32)
);
CREATE UNIQUE INDEX conference_sessions_live ON conference_sessions (tenant_id, room_id) WHERE ended_at IS NULL;
CREATE TABLE conference_members (
    participant_id UUID PRIMARY KEY REFERENCES call_participants(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    session_id UUID NOT NULL REFERENCES conference_sessions(id) ON DELETE CASCADE,
    is_moderator BOOLEAN NOT NULL DEFAULT false,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 0033_monitor_sessions.up.sql
CREATE TABLE monitor_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    call_id UUID NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    target_participant_id UUID NOT NULL REFERENCES call_participants(id),
    supervisor_participant_id UUID REFERENCES call_participants(id),
    supervisor_principal_id UUID NOT NULL,
    mode VARCHAR(8) NOT NULL CHECK (mode IN ('listen','whisper','barge')),
    state VARCHAR(12) NOT NULL CHECK (state IN ('Connecting','Active','Ended')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    end_reason VARCHAR(32)
);
CREATE UNIQUE INDEX monitor_sessions_one_per_supervisor
    ON monitor_sessions (tenant_id, supervisor_principal_id) WHERE ended_at IS NULL;
CREATE UNIQUE INDEX monitor_sessions_one_per_call      -- LLD-03 §3.17: one monitor per call
    ON monitor_sessions (tenant_id, call_id) WHERE ended_at IS NULL;
```

Each `.down.sql` drops what its `.up.sql` adds, in reverse order.
Migration `0034` is **released to LLD-03** (hot-desk); telephony uses
0030–0033 only. **`parked_calls` is
`pbx-core`'s** (LLD-03 §3.10, migration 0041): it is the slot claim;
telephony's park history is the `call_operations` row of kind `park`.

### 11.10 Events (outbox → `tenant.<tenant_id>.event.call.<action>`)

Payloads carry platform ids only (no channel, bridge or endpoint).
Common fields: `call_id`, `tenant_id`, `operation_id` (when any),
`occurred_at`, `origin`.

| Event | Extra payload |
|---|---|
| `call.held` / `call.resumed` | `participant_id`, `hold_origin`, `held_seconds` (resumed) |
| `call.hold_reminder` | `participant_id`, `held_seconds` |
| `call.transfer_started` | `kind` (blind/attended), `subject_participant_id`, `transferor_participant_id`, `destination` {kind, ref} |
| `call.consult_started` / `call.consult_swapped` / `call.consult_cancelled` | `subject_participant_id`, `target_participant_id`, `active_side` |
| `call.transferred` | `subject_participant_id`, `transferor_participant_id`, `target_participant_id`, `destination` |
| `call.transfer_recalled` / `call.transfer_failed` | as above + `reason` |
| `call.parked` | `participant_id`, `lot_id`, `slot`, `parker_participant_id`, `timeout_at` |
| `call.park_recalling` / `call.unparked` / `call.park_fallback` / `call.parked_abandoned` / `call.parked_lost` | `parked_call_id`, `lot_id`, `slot`, `retriever_participant_id` (unparked) |
| `call.picked_up` | `picked_participant_id`, `picker_participant_id`, `picker_extension_id`, `picker_principal_id` |
| `call.participant_added` / `call.participant_muted` / `call.participant_removed` | `participant_id`, `muted`, `by_principal_id` |
| `call.conference_started` / `call.conference_ended` | `room_id`, `session_id`, `peak_participants`, `end_reason` |
| `call.monitor_started` / `call.monitor_mode_changed` / `call.monitor_stopped` | `session_id`, `target_participant_id`, `supervisor_principal_id`, `mode` |
| `participant.state_changed` (subject `…event.participant.state_changed`) | `participant_id`, `extension_id`, `from`, `to`, `left_reason`. For external consumers (webhooks); `pbx-core` presence and lamps use the in-process `CallEventSink` (§21) |
| `participant.joined` / `participant.left` (existing subjects) | widened per §23: `role`, `extension_id`, `principal_id`, `direction`, `audibility`, `left_reason`, `consent` (`not_evaluated` \| `not_required` \| `pending` \| `satisfied` \| `failed`), `pci_paused` |
| `call.consent_satisfied` / `call.consent_failed` | `at`, `method` (`announce` \| `on_demand_announced` \| …, LLD-09), `policy_id` / `at`, `reason` |
| `call.pci_paused` / `call.pci_resumed` | `at`, `actor_id?`, `auto` |
| `recording.started` / `recording.stored` | LLD-09 payloads, each with `segment_no` |
| `ai.tap_lost` (internal subject, not published to webhooks) | `stream_id`, `reason`; only on an **unexpected** loss of a tap channel, never for telephony's own deletions (stop, suspend, call end) |
| `call.terminated` (v2) | full snapshot per §23 / LLD-09 §3.3 |

**Audit** (identity `audit_logs`, written by `pbx-core` in its own
transaction before it calls telephony): any action on a call where the
actor is not a party (`call.control.any`), kick and mute of another
person, pickup of a directed target outside the actor's groups, and every
monitor start/mode change/stop (AC-16.2). Hold and transfer of one's own
call are recorded by the events and the CDR, not audited.

### 11.11 API contract shared by §12–§18 (`PbxService`, ConnectRPC)

Permission names are **decided** (LLD-03 adopts the same; LLD-02's role
table gains them in the change that ships each flow):

| Action | Grants | Default roles |
|---|---|---|
| `call.control` | hold, resume, transfer, consult, add party, on a call where the actor's own Participant is live | every role that can `call.place` |
| `call.control.any` | the same on any call of the tenant | SUPERVISOR, TENANT_ADMIN |
| `call.park` | park a call the actor is a party to | as `call.control` |
| `call.park.retrieve` | retrieve from lots the actor's extension may use | as `call.control` |
| `call.pickup` | group and directed pickup within the actor's pickup groups | as `call.control` |
| `call.pickup.any` | directed pickup outside one's groups (receptionists) | none by default; TENANT_ADMIN grants |
| `conference.moderate` | mute/kick/lock in a room where the actor is a moderator Participant; any room with `call.control.any` | as `call.control` |

| Refusal | Connect code | Console wording |
|---|---|---|
| Not signed in / no permission | `UNAUTHENTICATED` / `PERMISSION_DENIED` | "You don't have permission to do that." |
| Call, participant, operation, lot, slot or room unknown **or another tenant's** | `NOT_FOUND` | "This call has ended or isn't yours." |
| State does not allow it (named per RPC) | `FAILED_PRECONDITION` | per RPC below |
| Lost a race with another operation | `ABORTED` | "Someone else just changed this call. Try again." |
| Capacity or slots exhausted | `RESOURCE_EXHAUSTED` | "No line is free right now." / "All parking spaces are in use." |
| Engine not ready (D-73) or step timeout | `UNAVAILABLE` | "Calls can't be changed right now. Your call is unaffected." |
| Target refused by class or fraud guard (D-82, D-85) | `PERMISSION_DENIED` | "You can't transfer to that number." |
| Bad id or destination shape | `INVALID_ARGUMENT` | "Check the destination and try again." |

The recording RPCs (§23) are on `TelephonyService` as LLD-09 specifies;
park, pickup and room RPCs are LLD-03's. Every RPC response carries `operation_id` (when one is created) and the
resulting `participant_state`. Errors never include an engine identifier.

## 12. Hold, resume and music on hold (PB-08)

1. **Trigger and actors.** Console/API `HoldCall` by an employee or agent
   on their own call (`call.control`) or a supervisor (`call.control.any`);
   a desk phone or softphone sending a hold re-INVITE (`a=sendonly` or
   `inactive`), seen as ARI `ChannelHold` on the holder's channel.
2. **Preconditions.** Call `Active`; subject `Connected` in `M`; no open
   operation on the subject. In a call with three or more live parties the
   request must name `participant_id`. Licence state is irrelevant (hold
   adds no leg). Failure → table in item 7.
3. **Sequence (API hold of A by B).**
   1. `pbx-core` authorises, then `CallControl.Hold`.
   2. telephony: insert `call_operations(kind=hold, state=Held)` and
      A → `OnHold(api)` with the `call.held` outbox row, one transaction.
   3. ACL: `POST /bridges/{M}/removeChannel?channel=A`, then
      `POST /channels/{A}/moh?mohClass=<HoldCommand.MOHClass>` (else the call's `HoldClass`).
   4. Arm the reminder timer (`hold.reminder_interval`).
   5. Resume: `DELETE /channels/{A}/moh`, `POST /bridges/{M}/addChannel?channel=A`,
      A → `Connected`, op `Resumed`, `call.resumed`.
   **Phone hold (decided: the engine supplies MOH to the held party in
   both cases).** B's phone holds → `ChannelHold(B)`. Two-party `M`: the
   engine's own hold indication plays the endpoint's MOH class
   (`ps_endpoints.moh_suggest`: a D-47 projection column added by 0030 and
   written by `pbx-core`'s ACL from the extension's class, LLD-03 §3.15) to A;
   telephony records A `OnHold(phone)` and opens a `hold` op with
   `origin=phone_hold`; no bridge moves. `ChannelUnhold(B)` resumes.
   Three or more parties: B is marked `muted` (others keep talking, no
   MOH). **SPIKE-CC-2** confirms on 22.11.0 that a held peer in an ARI
   mixing bridge hears MOH (native and softmix). **Fallback:** on
   `ChannelHold`, run steps 3–5 for the peer exactly as an API hold.
4. **State machine.** Participant: §11.2. `hold` operation:

   | From | Event | Guard | To | Side effect |
   |---|---|---|---|---|
   | — | `Hold` | subject Connected, no open op | Held | MOH starts |
   | Held | `Resume` | actor is holder or `call.control.any` | Resumed (terminal) | MOH stops, back in `M` |
   | Held | reminder tick | holder still live | Held | short tone to holder (`POST /channels/{B}/play?media=sound:beep`), `call.hold_reminder` |
   | Held | holder leaves | — | Recalling | `InboundRouter.Continue{reason: hold_orphaned, destination: holder's extension}`; A stays on MOH and runs the returned plan (default: holder's devices 20 s, then their no-answer rule) |
   | Recalling | a plan leg answers | — | Resumed | new holder Participant joins `M`, A unheld |
   | Recalling | plan reaches an application terminal (voicemail, queue) | — | FellBack | A `Transferring` → `Connected` to the application |
   | Recalling | plan exhausted / `Refuse`, not emergency | — | Abandoned | play `sound:vm-goodbye`, A `Disconnected(hold_abandoned)` |
   | Recalling | plan exhausted, emergency call | — | Recalling | `Continue` again (INV-01) |
   | Held | subject leaves | — | Ended | MOH gone with the channel |
   | Held | `BlindTransfer`/`StartConsult`/`Park` on subject | — | Superseded | `reason=superseded`; the new op opens in the same transaction |
   | Held, Recalling | engine/app lost | — | Lost | §22 |
5. **Data model.** `call_operations` (kind `hold`), `call_participants.hold_origin`. No new table.
6. **Ports.** `CallControl.Hold/Resume`; `InboundRouter.Continue`;
   `CallEventSink` (`ParticipantHeld/Resumed`); `MediaGateway.RemoveChannelFromBridge/StartMOH/StopMOH`;
   pure `(*Call).Hold/Resume`, `NextOperationState(hold, …)`.
7. **API.** `HoldCall{call_id, participant_id?}` → `{operation_id, participant_state}`;
   `ResumeCall{call_id, operation_id}` → `{participant_state}`.

   | Refusal | Code | Console |
   |---|---|---|
   | subject not Connected (ringing, parked, transferring) | `FAILED_PRECONDITION` | "Only a connected call can be put on hold." |
   | already held | `FAILED_PRECONDITION` | "This call is already on hold." |
   | resume when not held / op ended | `FAILED_PRECONDITION` | "This call isn't on hold." |
   | 3+ parties, no `participant_id` | `INVALID_ARGUMENT` | "Choose who to put on hold." |
   | hold and resume race | `ABORTED` | shared wording |
8. **Configuration.** MOH class, `hold.reminder_interval` (120 s), the
   orphan-recall chain (holder 20 s, then their no-answer rule) (§11.7).
   **OWNER DECISION (OD-CC-2):** a maximum hold time that forces the recall
   even while the holder is live. Recommended default: off (reminder only;
   3CX and FreePBX do not force it).
9. **Failure and recovery.** MOH start fails → A stays out of `M` in
   silence is not allowed: re-add A to `M`, op `Ended` (`reason=moh_failed`),
   `UNAVAILABLE`. `removeChannel` fails → nothing changed, `UNAVAILABLE`.
   Holder hangs up → orphan recall (item 4). Held party hangs up → op
   `Ended`; the call ends by the §11.2 rule. Hold and hangup race → the
   hangup wins (channel gone → op `Ended`). Engine/app restart → §22.
10. **Invariants and events.** INV-03 (a failed hold leaves the call as
    it was), INV-01 (an emergency call is never ended by hold
    abandonment), INV-10. Events `call.held`, `call.resumed`,
    `call.hold_reminder`, `participant.state_changed`. Audit only with
    `call.control.any`.
11. **Tests.**

    | Given | When | Then | Tier |
    |---|---|---|---|
    | A Connected | `Hold` | A OnHold, op Held, one `call.held` | U |
    | A OnHold | `Hold` again | `FAILED_PRECONDITION`, no second op (unique index) | U, I |
    | A Ringing | `Hold` | refused | U, W |
    | two concurrent `HoldCall` for A | both commit | exactly one succeeds, other `ABORTED` | I |
    | A held by API on the rig | listen on A | MOH audible on A, B hears silence; resume restores two-way audio | E |
    | B's phone sends sendonly | — | A hears MOH; A `OnHold(phone)`; unhold resumes | E (SPIKE-CC-2) |
    | A held, B hangs up | recall rings B's extension 20 s, unanswered, no rule | A hears goodbye, `hold_abandoned` | U, E |
    | emergency call held, holder gone, recall fails | timer | still recalling; call not ended | U |
    | MOH start fails (fake gateway) | `Hold` | A back in `M`, `UNAVAILABLE` | U |
    | console | click Hold, Resume | button state and timer follow `call.held`/`call.resumed` | C, B |
    | AC-03.2 | hold from browser, desk phone and toward an external party | MOH heard each way | A |
12. **Industry parity.** 3CX, VitalPBX, FreePBX and FusionPBX all play the
    PBX's MOH to the held party whatever side holds, with per-tenant or
    per-queue classes; we match. FreePBX has no orphan-hold recall by
    default (the held caller hears MOH until they hang up); 3CX returns the
    caller to the holder. **Deliberate difference:** we recall the holder
    and then follow their no-answer rule, so a caller is never left on MOH
    with nobody behind it.

## 13. Blind transfer (PB-09)

1. **Trigger and actors.** Console/API `BlindTransfer` by a party to the
   call (`call.control`) or a supervisor (`call.control.any`); a phone's
   blind REFER (§15). Transferor B, transferee A, target D (D-91 kinds:
   extension, ring group, queue, flow, voicemail, external, park, conference).
2. **Preconditions.** Call `Active`; A `Connected` or `OnHold`; B live (or
   the actor holds `call.control.any`); no open transfer on the call. D
   resolved by `pbx-core`; an external D passed outbound class and fraud
   screening (D-82, D-85) as if B dialled it; emergency D carries
   `BypassScreening`.
3. **Sequence.**
   1. `pbx-core` authorises, resolves D with its own `ResolveInbound`
      logic (`Purpose=transfer`) into an `InboundDecision` (a `RingPlan`, a
      `Program` or a `Feature`), calls `CallControl.BlindTransfer`.
   2. telephony (one tx): op `blind_transfer/TargetRinging`, A → `Transferring`,
      B → `Disconnected(transferred)`, new Participant C `Invited`, outbox.
   3. ACL: `POST /bridges/{M}/removeChannel?channel=B`, hang up B
      (`DELETE /channels/{B}`); A stays in `M`; `POST /channels/{A}/ring`
      (or `…/moh` when the tenant prefers MOH while transferring).
   4. Plan: run it for A exactly as for an arriving caller — each leg is
      `POST /channels` (originate C, `channelId=atsa-part-<C>-…`, step
      seconds as `timeout`, `originator=A` so connected line and CDR link);
      first answer wins, siblings `answered_elsewhere`.
      Program (voicemail, IVR) or Feature (park, conference): skip 4–5;
      hand A to it; op `Completed`.
   5. C answers (`StasisStart` + `ChannelStateChange Up`) →
      `DELETE /channels/{A}/ring`, `POST /bridges/{M}/addChannel?channel=C`;
      A, C `Connected`; op `Completed`; `call.transferred`.
   6. Every leg fails (`Dial` `BUSY|NOANSWER|CHANUNAVAIL`, or step
      timers): the plan's `OnExhausted`/`OnAllBusy` terminal applies; a
      `Destination` terminal goes through `InboundRouter.Continue{reason:
      transfer_no_answer}`. Whether that terminal is the target's own rule
      or a recall of B is `pbx-core`'s plan (OD-CC-3); telephony only runs it.
4. **State machine** (`blind_transfer` op):

   | From | Event | Guard | To | Side effect |
   |---|---|---|---|---|
   | — | `Start` | preconditions | TargetRinging | B leaves; A ringback |
   | TargetRinging | C answered | — | Completed | A ⇄ C in `M` |
   | TargetRinging | every leg fails | terminal is a destination | TargetRinging (next hop) | C `Disconnected(no_answer)`; `Continue{transfer_no_answer}`; LLD-03's 5-hop guard bounds the chain |
   | TargetRinging | every leg fails | terminal is voicemail/Program | Completed | A → application |
   | TargetRinging | `Continue{transfer_no_answer, RecallOf: B}` returns a decision with `RecallOf = B` (the recall marker; `pbx-core` echoes reason `transfer_recall`) | — | Recalling | `reason=transfer_recall`; B's devices ring with A (default 15 s target then 20 s recall when OD-CC-3 = `recall_transferor`) |
   | Recalling | recall answered | — | Recalled | new B Participant in `M` with A |
   | Recalling | recall fails, next terminal | — | FellBack | A → the tenant fallback (one hop) |
   | Recalling, TargetRinging | terminal `Refuse` / plan exhausted, not emergency | — | Failed | announce `sound:pbx-invalid` then A `Disconnected(transfer_failed)` |
   | Recalling, TargetRinging | plan exhausted, emergency | — | Recalling | `Continue` again (INV-01) |
   | any open | A hangs up | — | TransfereeLeft | cancel C (`DELETE /channels/{C}`, `reason=normal`, C `Disconnected(cancelled)`), `call.transfer_failed{reason: transferee_left}`; call ends by §11.2 |
   | any open | engine/app lost | — | Lost | §22 |

   **B busy on recall:** `pbx-core` sees B busy in its presence hub and
   returns a plan without B (or `OnAllBusy`), so the recall goes straight
   to the fallback.
5. **Data model.** `call_operations` (`destination` JSONB, `deadline_at` =
   ring deadline). C is a `call_participants` row on the **same** call.
6. **Ports.** `CallControl.BlindTransfer`; `InboundDecision` (plan runner);
   `InboundRouter.Continue`; `CallEventSink`; `MediaGateway.Originate`
   (with `originator`), `StartRinging/StopRinging`, `RemoveChannelFromBridge`;
   pure `(*Call).BeginBlindTransfer/TargetAnswered`, `NextOperationState`.
7. **API.** `BlindTransfer{call_id, participant_id (A), destination {kind, ref}}`
   → `{operation_id}`.

   | Refusal | Code | Console |
   |---|---|---|
   | A not Connected/OnHold | `FAILED_PRECONDITION` | "This call can't be transferred right now." |
   | transfer already in progress | `FAILED_PRECONDITION` | "A transfer is already in progress." |
   | D is the transferor's own extension | `INVALID_ARGUMENT` | "You can't transfer a call to yourself." |
   | D refused by class/fraud | `PERMISSION_DENIED` | "You can't transfer to that number." |
   | D unknown / deleted | `NOT_FOUND` | "That destination doesn't exist." |
8. **Configuration** (all `pbx-core`, §11.7). Target ring (destination's,
   20 s), blind no-answer mode, recall after 15 s, recall ring 20 s,
   tenant fallback. **OWNER DECISION (OD-CC-3):** what an unanswered
   blind transfer does by default. Recommended `destination_rule` (the
   target's own voicemail/forwarding, 3CX behaviour), with
   `recall_transferor` after 15 s available per tenant (FreePBX parity).
9. **Failure and recovery.** Originate fails → that leg counts as failed
   (the plan's next leg or terminal). Engine step timeout before B is removed → nothing changed,
   `UNAVAILABLE`. After B is removed, every failure path ends in recall,
   fallback or the announcement: A is never left ringing forever.
   C answers after A hung up → C hung up, `call.transfer_failed`.
   Restart → §22.
10. **Invariants and events.** INV-01 (emergency D bypasses capacity;
    emergency A is never dropped by the `Failed` path, it recalls again),
    INV-03, INV-10 (D resolved inside the tenant). Events
    `call.transfer_started`, `call.transferred`, `call.transfer_recalled`,
    `call.transfer_failed`; CDR shape (LLD-09): one call, Participants
    A (continuous), B (`transferred`), C (answered), linked by
    `operation_id`. Audit only with `call.control.any`.
11. **Tests.**

    | Given | When | Then | Tier |
    |---|---|---|---|
    | A⇄B | blind to extension C, C answers | A,C Connected; B `transferred`; A's usage has no gap or duplicate | U, I, E |
    | C rings 20 s, has voicemail | timeout | A in C's voicemail; op Completed | U, E |
    | mode `recall_transferor`, C silent 15 s | — | B's phone rings with A; answered → A⇄B | U, E |
    | recall unanswered, no fallback | — | announcement, `transfer_failed` | U |
    | A hangs up while C rings | — | C cancelled; no orphan channel on `GET /channels` | U, E |
    | B busy on recall | — | straight to fallback | U |
    | external D blocked by class | `BlindTransfer` | `PERMISSION_DENIED`; call untouched | W, I |
    | two transfers on one call | concurrent | one `FAILED_PRECONDITION` (unique index) | I |
    | transfer to voicemail | — | A hears greeting, no ringing leg | E |
    | console | drag/enter destination, Transfer | call leaves my list; toast on failure | C, B |
    | AC-03.2 | browser → desk phone → external | each direction completes | A |
12. **Industry parity.** All four do blind transfer from phone and UI.
    FreePBX recalls the transferor after `transferdigittimeout`/`atxfernoanswertimeout`
    (15 s default); 3CX follows the target's no-answer forwarding;
    FusionPBX follows the target's no-answer. **Difference:** both modes
    are offered per tenant, and the transfer is executed inside the
    platform (originate and swap), not by SIP `redirect` (HLD-21 §0), so
    the caller's Participant, recording and usage stay one record.

## 14. Attended (consultative) transfer, swap and cancel (PB-10)

1. **Trigger and actors.** Console/API `StartConsult` → `SwapConsult`* →
   `CompleteTransfer` or `CancelConsult`, by the transferor B
   (`call.control`); a phone's attended REFER with `Replaces` (§15).
2. **Preconditions.** As §13 item 2, plus D is an **endpoint** kind
   (extension, ring group member, external). Voicemail, queue, flow, park
   and conference are blind-only. OD-CC-1 capacity for the consult leg.
3. **Sequence (API).**
   1. `StartConsult`: one tx → op `attended_transfer/ConsultRinging`,
      `active_side=target`, A → `OnHold(consult)`, C `Invited`.
   2. ACL: remove A from `M`, `POST /channels/{A}/moh`; create `K`
      (`atsa-k-<op>`); remove B from `M`, add B to `K`; originate C
      (`timeout=transfer.consult_ring`); B hears ringback in `K` (`POST /channels/{B}/ring`).
   3. C answers → add C to `K`; op `Consulting`; `call.consult_started`.
   4. `SwapConsult` (toggle): when `active_side=target`: remove C from `K`,
      `POST /channels/{C}/moh`, C → `OnHold(consult)`; remove B from `K`, add
      B to `M`; `DELETE /channels/{A}/moh`, add A to `M`, A → `Connected`;
      `active_side=caller`. The reverse swaps back. `call.consult_swapped`.
   5. `CompleteTransfer`: stop MOH on whoever is held; add C to `M`
      (and A if not there); remove B and hang B up; destroy `K`. A, C
      `Connected` in `M`; B `Disconnected(transferred)`; op `Completed`;
      `call.transferred`. **`M` is kept, so A's bridge recording is
      continuous.** The consult in `K` is recorded only if LLD-09's rule
      covers B's extension.
   6. `CancelConsult`: hang up C (`cancelled`), destroy `K`, B back to `M`,
      A unheld; op `Cancelled`.
   **Three-way then drop** (HLD-21 §1.3 variant): `AddParticipant` (§18)
   then B hangs up; no attended op.
4. **State machine** (`attended_transfer` op):

   | From | Event | Guard | To | Side effect |
   |---|---|---|---|---|
   | — | `StartConsult` | preconditions, OD-CC-1 capacity | ConsultRinging | A held, B in `K` |
   | ConsultRinging | C answers | — | Consulting | B ⇄ C |
   | ConsultRinging | C fails / `consult_ring` (30 s) | — | Cancelled(`target_no_answer`) | B back with A (auto-resume) |
   | ConsultRinging | `CompleteTransfer` | `attended_drop_completes` | ConvertedToBlind | **conversion** (below): a blind op takes over A and the ringing C (Asterisk `atxferdropcall` parity) |
   | ConsultRinging, Consulting | `CancelConsult` | actor is B or `.any` | Cancelled | C hung up |
   | Consulting | `SwapConsult` | — | Consulting | `active_side` flips |
   | Consulting | `CompleteTransfer` | A and C live | Completed | A ⇄ C in `M` |
   | Consulting | C hangs up | — | Cancelled(`target_left`) | B back with A |
   | ConsultRinging, Consulting | A hangs up | — | **TransfereeLeft** (terminal) | `call.transfer_failed{reason: transferee_left}`; B (and C, once answered or while still ringing) move from `K` into `M`, `K` destroyed: the call continues as an ordinary B⇄C call (or B hearing C ring); later `CompleteTransfer`/`CancelConsult` → `FAILED_PRECONDITION` "This transfer has already finished." |
   | Consulting | B hangs up | `attended_drop_completes` | Completed | as `CompleteTransfer` (FreePBX/3CX parity) |
   | ConsultRinging | B hangs up | `attended_drop_completes` | ConvertedToBlind | **conversion** (below); C keeps ringing; §13's recall rules apply with `RecallOf = B` |
   | ConsultRinging, Consulting | B hangs up | not `attended_drop_completes` | Cancelled(`transferor_left`) | C hung up; **telephony** opens a `hold` op on A (origin `timer`, state `Recalling`, `reason=hold_orphaned`) in the same transaction and calls `Continue{hold_orphaned, RecallOf: B}` — §12's orphan recall owns A from here |
   | any open | engine/app lost | — | Lost | §22 |

   **Conversion ConsultRinging → blind, one transaction:** (1) `UPDATE` the
   attended row to `state = outcome = 'ConvertedToBlind'`, `ended_at = now`;
   (2) `INSERT` a `blind_transfer` row (`TargetRinging`, subject A,
   target C, `initiator` B, `related_operation_id` → nothing; the attended
   row's `related_operation_id` → the new row). The update runs first, so
   `call_operations_open_transfer` (one open transfer per call) never sees
   two open rows. Media: A stays out of `M` hearing MOH, then ringback
   (`POST /channels/{A}/ring`); C's leg is moved from `K` to `M` on answer;
   `K` destroyed; B `Disconnected(transferred)`.
5. **Data model.** `call_operations` (`active_side`, `target_participant_id`).
   C is on the same call.
6. **Ports.** `CallControl.StartConsult/SwapConsult/CompleteTransfer/CancelConsult`;
   `MediaGateway.CreateNamedBridge/DestroyBridge/RemoveChannelFromBridge/StartMOH`;
   pure `NextOperationState(attended_transfer, …)`.
7. **API.** `StartConsult{call_id, participant_id (A), destination}` →
   `{operation_id, target_participant_id}`; `SwapConsult{operation_id}` →
   `{active_side}`; `CompleteTransfer{operation_id}` → `{}` (idempotent:
   completing a `Completed` op returns OK); `CancelConsult{operation_id}` → `{}`.

   | Refusal | Code | Console |
   |---|---|---|
   | D is an application kind | `FAILED_PRECONDITION` | "Use blind transfer for voicemail, queues and menus." |
   | Complete while A gone | `FAILED_PRECONDITION` | "The caller hung up. You're still talking to your colleague." |
   | Swap before C answers | `FAILED_PRECONDITION` | "Wait for them to answer before switching." |
   | Complete/Cancel on a finished op | `FAILED_PRECONDITION` | "This transfer has already finished." |
   | consult capacity (OD-CC-1) | `RESOURCE_EXHAUSTED` | "No line is free for the consultation. Your caller is still on the line." |
8. **Configuration.** `transfer.consult_ring` (30 s),
   `transfer.attended_drop_completes` (true), `moh_class`, OD-CC-1.
9. **Failure and recovery.** `K` creation or C originate fails → undo in
   reverse (B to `M`, A unheld), `UNAVAILABLE`. Partial completion (C
   added to `M`, B removal fails) → hang B up by channel; the outcome is
   still `Completed`. A hangs up while held → op continues as B⇄C (never
   dropped). C and A both gone → B left alone; call ends by §11.2.
   Restart → §22.
10. **Invariants and events.** INV-03, INV-01 (emergency consult target
    bypasses capacity), INV-10. Events `call.consult_started`,
    `call.consult_swapped`, `call.consult_cancelled`, `call.transferred`,
    `call.transfer_failed`.
11. **Tests.**

    | Given | When | Then | Tier |
    |---|---|---|---|
    | A⇄B | consult C, C answers, complete | A⇄C in the same `M`; B `transferred`; recording file of `M` covers before and after | U, E |
    | consulting | swap, swap | each time the other party hears MOH; audio verified | E |
    | consult ringing 30 s | — | auto-cancel; A⇄B again | U, E |
    | consulting | C hangs up | B⇄A resumed automatically | U, E |
    | consulting | A hangs up | B⇄C continue; Complete → `FAILED_PRECONDITION` | U, W |
    | consulting | B hangs up | A⇄C (drop completes) | U, E |
    | consult to voicemail | `StartConsult` | `FAILED_PRECONDITION` | U, W |
    | Complete called twice | — | second returns OK, no duplicate event | U, I |
    | console | consult panel with Swap/Complete/Cancel | buttons follow op state | C, B |
    | AC-03.2 | announced transfer browser ↔ desk phone ↔ external | completes each way | A |
12. **Industry parity.** 3CX and VitalPBX expose consult, swap
    ("toggle"), complete and cancel in the client; FreePBX uses `*2`
    attended transfer with `atxferdropcall`, `atxferloopdelay`,
    `atxfernoanswertimeout` 15 s; FusionPBX uses `att_xfer` with the same
    drop-completes behaviour. **Difference:** the consult lives in the
    same Call (one record, one recording bridge), and in-call DTMF feature
    codes (`*2`, `##`) are **off** in R1.0 (**OWNER DECISION OD-CC-4**,
    recommended off: phones use their transfer key, browsers use the
    console).

## 15. Phone-initiated transfer: SIP REFER (PB-09/PB-10 from a phone)

Decided design for both branches; the spike picks the branch, not the behaviour.

1. **Trigger and actors.** A desk phone or softphone's transfer key sends
   `REFER` (blind: `Refer-To: <sip:201@…>`; attended: `Refer-To` with
   `Replaces=<dialog of B's consult call>`). Actor: the extension's
   assigned principal; permission is the extension's own outbound class
   (no RBAC check: a phone acts as its owner, as when it dials).
2. **Preconditions.** The REFERring channel is a correlated live
   Participant. For attended, the replaced dialog is a call the platform
   knows (B's consult call arrived through Stasis as its own Call).
3. **Sequence.**
   - **Primary (ARI-handled):** the ACL-owned PJSIP Realtime projection
     (D-47: `ps_*` are projection tables written by `pbx-core`'s
     `pbx/acl/asterisk` in the same transaction as the extension row)
     sets `ps_endpoints.transfer_handling = 'ari-only'` (column added by
     migration 0030). Asterisk raises
     `ChannelTransfer` (refer-to destination, referred-by, and for attended
     the replaced channel) instead of acting.
     - Blind: telephony calls `InboundRouter.ResolveInbound{Purpose: transfer,
       Dialled: <Refer-To user part>, transferor extension, CallID}` (a REFER
       to the lot number resolves to `Feature=Park`, LLD-03 §3.10) → runs §13 with `origin=phone_refer`; reports progress with
       `POST /channels/{B}/transfer_progress?states=channel_progress`
       then `channel_answered` or `channel_declined`, so the phone shows
       the outcome; B's phone then hangs up its leg.
     - Attended: the replaced channel identifies B's consult Call 2
       (`B'⇄C`). telephony re-homes C into Call 1 (§11.6), adds C to `M`,
       unholds A, hangs up B and B'; Call 2 → `Terminated(merged)`,
       `merged_into_call_id`=Call 1; op `attended_transfer/Completed`,
       `origin=phone_refer`.
   - **Fallback (native):** `transfer_handling` default. Asterisk performs
     the transfer; telephony reconciles:
     - Blind: `BridgeBlindTransfer` arrives; the transferee leaves `M` and
       re-enters Stasis through `[stasis-in]` with the dialled target and its
       existing `ATSA_PARTICIPANT_ID`/`ATSA_CALL_ID` variables. The event loop
       treats a correlated re-entry as "continue this Participant": A
       `Transferring`, target resolved by the router, §13 from step 4.
     - Attended: `BridgeAttendedTransfer` names the surviving channels
       (`transferee`, `transfer_target`) and result. telephony re-homes C
       into Call 1 as in the primary path and reconciles `M` membership
       (Asterisk may have moved channels between our bridges: `GET
       /bridges/{M}` is the truth).
4. **State machine.** As §13/§14 with `origin=phone_refer` or `phone_native`.
   REFER-specific: `ChannelTransfer` while an op is open on the call →
   `transfer_progress=channel_declined` (phone shows failure); unresolvable
   target → `channel_unavailable`.
5. **Data model.** `call_operations.origin`, `related_call_id` (Call 2),
   `calls.merged_into_call_id`.
6. **Ports.** `InboundRouter.ResolveInbound` (`Purpose=transfer`); `MediaGateway.ReportTransferProgress`;
   event-loop translations `ChannelTransfer`, `BridgeBlindTransfer`,
   `BridgeAttendedTransfer` → `TransferRequested`/`NativeTransferObserved`.
7. **API.** None (phone path). The result is visible through `GetCall` and events.
8. **Configuration.** `ps_endpoints.transfer_handling` (D-47 projection
   column, written by `pbx-core`'s ACL, added by 0030): `ari-only` for every
   extension endpoint if SPIKE-CC-1 passes, else NULL (engine default);
   trunk endpoints stay NULL. Timers as §13/§14.
9. **Failure and recovery.** Target refused by class/fraud →
   `channel_declined`; the call is untouched (B still has A on hold on the
   phone). Native-fallback events arriving for an unknown channel → log
   at ERROR and run the reconcile sweep for that call (§22): no channel
   may stay outside a known bridge without a Participant.
10. **Invariants and events.** INV-01 (REFER to an emergency number is
    resolved with bypass, never refused for capacity), INV-10 (a REFER
    target is resolved only within the transferor's tenant; a `Replaces`
    naming another tenant's dialog is refused), events as §13/§14.
11. **Tests.**

    | Given | When | Then | Tier |
    |---|---|---|---|
    | baresip/desk phone on the rig, `ari-only` | blind REFER to 1002 | `ChannelTransfer` seen; A⇄1002; Participants as §13 | E (SPIKE-CC-1) |
    | same | attended REFER with Replaces | Call 2 `merged`; C re-homed; usage of C continuous across the two calls | E, I |
    | REFER to a blocked external | — | `channel_declined`; call unchanged | E |
    | native fallback configured | blind REFER | correlated re-entry continues A's Participant | E |
    | REFER replacing a dialog of another tenant | — | refused | U |
    | AC-03.2 | transfer key on desk phone and in browser softphone | identical record to API transfer | A |
12. **Industry parity.** FreePBX and FusionPBX let the engine (Asterisk /
    FreeSWITCH) execute REFER natively; 3CX handles REFER in its own call
    controller. **Difference:** we keep REFER under platform control (primary)
    so a phone transfer and a console transfer produce the same record.
    **SPIKE-CC-1** confirms `transfer_handling=ari-only`, `ChannelTransfer` and
    `transfer_progress` on 22.11.0 (added in the 22.x line); the native
    branch is the fallback.

## 16. Park, retrieve, comeback (PB-11)

`pbx-core` owns the lot, the slot claim (`parked_calls`, conditional
updates, `ChooseSlot`, `Sweep`) and the comeback chain (LLD-03 §3.10).
Telephony owns the media, the timer and the park's history
(`call_operations` kind `park`). The parked party stays in the call's main
bridge `M` (D-99): no park bridge exists.

1. **Trigger and actors.** Park: console `ParkCall` (LLD-03 RPC,
   permission `call.park`, actor is a party to the call), a phone's blind
   or attended REFER to the lot number or a slot number (§15,
   `Purpose=transfer` → `Feature=Park`). Retrieve: a phone dialling the
   slot number, console `RetrieveParkedCall` (`call.park.retrieve`), a
   BLF key. Timer: telephony's, armed at `expires_at`.
2. **Preconditions.** Subject `Connected` or `OnHold`; after the parker
   leaves, the subject is the only live party in `M` (a three-party call
   cannot be parked: `FAILED_PRECONDITION` "Only a two-party call can be
   parked"); `pbx-core` has claimed a slot (else the parker hears
   `pbx-park-full` and the call is untouched; console "All parking spaces
   are in use."). Retrieve: `pbx-core`'s conditional `PARKED → RETRIEVED`
   succeeded.
3. **Sequence.**
   1. `pbx-core` `AllocateSlot` (its tx) → `CallControl.Park{call,
      subject, lot, slot, MOHClass, ExpiresAt}`.
   2. telephony (one tx): op `park/Parked`, A → `Parked`, parker B →
      `Disconnected(parked)`, `call.parked`; `CallEventSink(ParticipantParked)`.
   3. ACL: if A is held, `DELETE /channels/{A}/moh` and `addChannel` A
      back to `M`; `removeChannel` B from `M`; `POST /bridges/{M}/moh?mohClass=`
      (A is alone, so only A hears it); hang up B (API) or
      `transfer_progress=channel_answered` (REFER).
   4. **Slot announcement to the parker:** API → the RPC returns `slot`;
      attended phone park → `POST /channels/{B'}/play?media=digits:<slot>`
      on the consult leg before the phone completes; blind phone park →
      not audible (B's leg has gone): the lamp (§21) and console show it.
   5. Arm the timer at `ExpiresAt` (re-armed by `pbx-core` `Sweep` after
      a restart, LLD-03).
   6. Retrieve: `pbx-core` `RetrieveSlot` wins its conditional update →
      `CallControl.Retrieve{call, parked, retriever}`: the retriever's
      provisional Call is re-homed into A's Call (§11.6), or the console
      path originates the retriever's device first (`Leg.Originate`);
      answer it; `DELETE /bridges/{M}/moh`; `addChannel` R to `M`; A, R
      `Connected`; op `Retrieved`; `call.unparked`.
   7. Timeout: `InboundRouter.Continue{reason: park_timeout}` → `pbx-core`
      `ParkTimeout` (conditional `PARKED → COMEBACK`, slot freed) returns
      a `RingPlan` (parker's devices `comeback_seconds`, caller-id "Parked
      701: <caller>", then the lot fallback). Op `Recalling`; A →
      `Transferring` (`ComebackStarted`) and keeps hearing the bridge MOH
      while the plan rings; a leg answers → stop MOH, add that leg to `M`,
      A `Connected`, op `Retrieved`. If `pbx-core` lost the race (already
      retrieved) it returns no decision and telephony does nothing.
   8. Plan reaches voicemail/IVR → `DELETE /bridges/{M}/moh`, A →
      `Connected` to the application (`ComebackToApplication`), op
      `FellBack`. A queue → A `OnHold(queue)` (§23.1), op `FellBack`.
      `Refuse` or exhausted → announcement, A
      `Disconnected(hold_abandoned)`, op `Abandoned` (an emergency call is
      never dropped: `Continue` again).
4. **State machine** (op `park`; LLD-03's row states in brackets):

   | From | Event | Guard | To | Participant A | Side effect |
   |---|---|---|---|---|---|
   | — | `Park` | slot claimed, A alone after B leaves | Parked [`PARKED`] | Connected/OnHold → Parked | `call.parked`, lamp `INUSE` |
   | Parked | `Retrieve` | pbx conditional update won | Retrieved [row deleted] | Parked → Connected | `call.unparked` |
   | Parked | timer | `Continue` returned a plan | Recalling [`COMEBACK`, slot free] | Parked → Transferring | `call.park_recalling` |
   | Parked | timer | `Continue` returned an application | FellBack | Parked → Connected (application) | `call.park_fallback` |
   | Parked | timer | `Continue` returned nothing (lost race) | Parked | — | none |
   | Recalling | plan leg answers | — | Retrieved | Transferring → Connected | comeback siblings `answered_elsewhere` |
   | Recalling | application terminal | — | FellBack | Transferring → Connected / OnHold(queue) | `call.park_fallback` |
   | Recalling | exhausted, not emergency | — | Abandoned | → Disconnected(hold_abandoned) | announcement |
   | Parked, Recalling | A hangs up | — | Abandoned [`ABANDONED`] | → Disconnected(normal) | `call.parked_abandoned` |
   | Parked, Recalling | engine or app lost (§22) | — | Lost [`LOST`] | → Disconnected(engine_lost/app_restart) | `call.parked_lost` |

   **Races.** Retrieve vs timeout and two retrievers are decided by
   `pbx-core`'s conditional update on its row; telephony acts only on the
   winner's call. Retrieve during `Recalling` is not possible: the slot is
   already free (FreePBX behaviour; the comeback leg is the way back). A
   `Retrieve` for an op that is no longer `Parked` returns `ErrNotParked`.
5. **Data model.** `call_operations` (kind `park`, `deadline_at` =
   `ExpiresAt`); `parked_calls` is LLD-03's (0041).
6. **Ports.** `CallControl.Park/Retrieve`; `InboundRouter.Continue`;
   `CallEventSink(ParticipantParked)`; `DeviceStatePublisher` (called by
   `pbx-core`); `MediaGateway.StartBridgeMOH/StopBridgeMOH`; pure
   `NextOperationState(park, …)`.
7. **API.** LLD-03 `ParkCall` (`call.park`), `RetrieveParkedCall`
   (`call.park.retrieve`), `ListParkedCalls`. Telephony errors map:
   subject not Connected/OnHold → `FAILED_PRECONDITION` "This call can't
   be parked right now"; three-party call → `FAILED_PRECONDITION` "Only a
   two-party call can be parked"; lot full (pbx-core) →
   `RESOURCE_EXHAUSTED` "All parking spaces are in use."; engine failure →
   `UNAVAILABLE`; `ErrNotParked` → `ABORTED` "That call has already been
   picked up."
8. **Configuration.** LLD-03 §3.10: slots 701–720, timeout 60 s,
   comeback 20 s, fallback (parker's mailbox, else hang up), lot MOH.
9. **Failure and recovery.** Bridge MOH start fails → A stays in `M` in
   silence is not allowed: `CallControl.Park` returns `UNAVAILABLE`, B is
   not hung up, A is restored to its previous state, and `pbx-core`
   deletes its row in compensation. App restart, engine alive → the open
   `park` row and `M` with A identify the park; `Sweep` re-arms timers.
   Engine restart → op `Lost`; `pbx-core` `Sweep` marks its row `LOST`.
10. **Invariants and events.** INV-10 (slot numbers tenant-local, RLS on
    both tables), INV-01 (an emergency call is never dropped by the
    no-fallback path), INV-03. Recording: `M` is unchanged, so a park adds
    **no** recording segment; the recording captures the MOH while parked.
    Telephony events `call.parked`, `call.park_recalling`,
    `call.unparked`, `call.park_fallback`, `call.parked_abandoned`,
    `call.parked_lost` (LLD-03 emits its `pbx.call_parked…` for
    configuration-side consumers).
11. **Tests.**

    | Given | When | Then | Tier |
    |---|---|---|---|
    | A⇄B | `Park` | A alone in `M` hearing MOH; B gone; op Parked; no new bridge on `GET /bridges` | U, E |
    | A⇄B⇄C | `Park` A | `FAILED_PRECONDITION` | U, W |
    | parked | `Retrieve` with R's provisional call | A⇄R in `M`; R's usage continuous across re-home | I, E |
    | parked | timer fires after a retrieve won | `Continue` returns nothing; no ringing | U |
    | comeback plan answered by parker | — | A `Transferring → Connected`; op Retrieved | U, E |
    | comeback returns voicemail | — | A in voicemail; FellBack | U, E |
    | A hangs up while parked | — | Abandoned; no orphan channel | U, E |
    | Asterisk restarted with a parked call | reconnect | op Lost, `call.parked_lost` | E |
    | bridge MOH fails (fake) | `Park` | B still connected to A, `UNAVAILABLE` | U |
    | recorded call parked and retrieved | — | one recording file covers before, during (MOH) and after | E |
    | console | park, retrieve | panel follows events | C, B |
    | AC-03.2 | external caller on a desk phone is parked, then retrieved in the browser | external party hears MOH, then the retriever; both directions | A |
    | AC-03.2 | browser user parks an internal call, desk phone retrieves | works | A |
12. **Industry parity.** As LLD-03 §3.10 (FreePBX 70/71–78, 45 s,
    comeback then destination; 3CX shared park; VitalPBX/FusionPBX valet
    with BLF). **Difference:** the parked caller stays the same
    Participant in the same Call, bridge and recording, and a park lost to
    an engine restart is recorded as `parked_lost`.

## 17. Call pickup (PB-12, AC-03.13)

`pbx-core` selects the candidate (`SelectPickup`, oldest first by
`ringing_since`, from its presence hub; LLD-03 §3.9). Telephony claims and
bridges.

1. **Trigger and actors.** A phone dials `*8` (group) or `**<ext>`
   (directed), or console `PickupCall` (LLD-03). Group membership is the
   permission (`call.pickup`); `call.pickup.any` widens directed pickup
   outside one's groups.
2. **Preconditions.** The chosen Participant is `Ringing` and was
   originated pickable. **`Pickable` is carried end to end:** `pbx-core`
   sets it on each `RingLeg` (true for extension, ring-group, forwarding
   and follow-me legs); `QueueOffer` and dialler legs always carry
   `Pickable=false` (LLD-03 §3.9, §23.1, §23.3); telephony stores it on the
   in-memory Participant and reports it in `CallEventSink`
   `ParticipantRinging{Pickable}`, so `pbx-core`'s `SelectPickup` only
   sees pickable legs; `CallControl.Pickup` re-checks it and returns
   `ErrNotPickable` (`FAILED_PRECONDITION` "That call can't be picked up")
   otherwise.
3. **Sequence.**
   1. Picker's channel arrives (provisional Call) → `ResolveInbound` →
      `Feature=Pickup{CallID, ParticipantID}`; console path: `pbx-core`
      calls `CallControl.Pickup` with `Picker.Originate` (rings the
      actor's device first, click-to-call D-78).
   2. Claim, atomically: per-call lock (one Stasis owner, D-73), then
      `UPDATE call_participants SET state='Disconnected', left_reason='picked_up'
      WHERE id=$1 AND tenant_id=$2 AND state='Ringing'`. 0 rows →
      `ErrNotRinging`; `pbx-core` retries `SelectPickup` once, then plays
      `pbx-nothing-to-pickup` (LLD-03).
   3. Re-home the picker into the target Call (§11.6) with the picker's
      extension/principal; op `pickup/Claimed`.
   4. ACL: hang up the claimed leg and every sibling of the same plan step
      with `reason_code=26` (answered elsewhere, no missed call on the
      phones); answer the caller if not yet answered; add caller and
      picker to `M`. Picker `Connected`; op `Completed`; `call.picked_up`.
4. **State machine.** Target `Ringing → Disconnected(picked_up)`;
   siblings `Ringing → Disconnected(answered_elsewhere)`; picker `Invited →
   Connected`. Op `Claimed → Completed`; `Claimed → CallerLeft` when the
   caller hangs up between claim and bridge (picker hears
   `pbx-nothing-to-pickup`, hung up); `Claimed → Lost` when the bridge
   step fails or the engine/app is lost (§22).
5. **Data model.** `call_operations(kind=pickup, subject=claimed leg,
   target=picker)`; `call_participants.ringing_since`, `left_reason`.
   **Who answered (AC-03.13):** the CDR's answering party is the
   Participant with `answered_at` — the picker, with `extension_id` and
   `principal_id` (LLD-03's "`answered_by` attribution") — and the op row
   links it to the extension that was rung.
6. **Ports.** `InboundDecision.Feature=Pickup`; `CallControl.Pickup`
   (`ErrNotRinging`); `CallEventSink` (`ParticipantRinging` feeds
   `pbx-core`'s alerting list); `MediaGateway.HangupWithCause`.
7. **API.** LLD-03 `PickupCall`. Telephony errors map: `ErrNotRinging` →
   `ABORTED` "Someone else answered it"; engine failure → `UNAVAILABLE`.
8. **Configuration.** Codes and groups: LLD-03 §3.9, §3.13.
9. **Failure and recovery.** Claim committed, bridge fails → the caller
   cannot get the cancelled leg back: the picker is hung up
   (`Disconnected(pickup_failed)`), telephony calls
   `Continue{reason: pickup_failed, Destination: the rung extension}` and
   A runs the returned no-answer chain (A `Transferring`); op `Lost`. Restart between claim and bridge →
   §22.
10. **Invariants and events.** INV-10 (claim filters by tenant; candidates
    come only from the picker's tenant hub), INV-03. Event `call.picked_up`.
11. **Tests.**

    | Given | When | Then | Tier |
    |---|---|---|---|
    | 1001 ringing, 1002 in its group | 1002 dials `*8` | 1002⇄caller; 1001 stops with no missed call; CDR answerer 1002 | E, A (AC-03.13) |
    | two pickers claim one leg | concurrent `Pickup` | exactly one wins; the other `ErrNotRinging` | U, I, E |
    | ring group of three | pickup | all three legs stop | E |
    | caller hangs up during claim | — | op Lost, picker hears the prompt | U |
    | leg of another tenant id | `Pickup` | `ErrNotRinging` (0 rows, RLS) | I |
    | console | Answer on a ringing colleague | my device rings, then connected | C, B |
12. **Industry parity.** FreePBX `*8`/`**ext`, 3CX `*20*`, FusionPBX and
    VitalPBX the same. **Difference:** the picker is recorded as the
    answering party of the original call, where FreePBX's CDR shows a
    separate `PICKUP` leg.

## 18. Conference: ad hoc and rooms (PB-13)

Rooms are configured and entered under `pbx-core`'s control (number,
Argon2id PINs, `RoleForPin`, `BuildRoomEntry` Program, moderator RPCs;
LLD-03 §3.11). Telephony owns the room's media, session and Participants.

1. **Trigger and actors.** Ad hoc: `AddToCall` (this LLD, `call.control`).
   Room: any caller reaching a `conference` destination; `pbx-core`'s entry
   Program collects the PIN and then decides `Feature=Conference` →
   `ConferenceControl.Join`. Moderator actions: LLD-03
   `MuteConferenceParticipant` / `RemoveConferenceParticipant` /
   `LockConference` → `ConferenceControl.Mute/Remove/Lock`.
2. **Preconditions.** Ad hoc: call `Active`, live parties <
   `conference.adhoc_max` (5), OD-CC-1 capacity. Room: entrant passed the
   PIN; `AdmitToRoom` admits (not locked, live < `Limit`). Each entrant
   already holds its own capacity reservation from its own Screening
   ("each entrant is a call for licensing", LLD-03 §3.11), so the licence
   caps a room naturally.
3. **Sequence.**
   - **Ad hoc** (`AddToCall{call_id, destination}`; the **actor** is a
     live party B — the initiator; the op's **subject** is the new party C):
     1. `pbx-core` authorises (`call.control` on the actor's own live leg,
        or `call.control.any`), resolves the destination
        (`Purpose=transfer`) into a ring plan (endpoint kinds only;
        voicemail, queue, flow, park and conference are refused).
     2. telephony (one tx): OD-CC-1 capacity for C (key
        `<call_id>/<C>`); C `Invited`; op `adhoc_add/TargetRinging`
        (subject C, initiator B). A and B stay talking in `M`.
     3. Originate C's plan legs; ringback is played to the initiator only
        (`POST /channels/{B}/play?media=tone:ring` loop, stopped on answer
        or failure) — A hears nothing new.
     4. C answers → `addChannel` C to `M`; C `Connected`; op `Completed`;
        `call.participant_added`. Asterisk switches `M` to softmix by
        itself: no bridge upgrade, recording continuous.

     **`adhoc_add` op states:**

     | From | Event | Guard | To | Side effect |
     |---|---|---|---|---|
     | — | `AddToCall` | live < `adhoc_max`, capacity granted | TargetRinging | C `Invited` |
     | TargetRinging | C answers | call still has a live party | Completed | C into `M` |
     | TargetRinging | plan exhausted / busy / rejected | — | NoAnswer | C `Disconnected(no_answer\|busy\|rejected)`; capacity released; initiator hears `sound:beeperr` once; `call.participant_add_failed` |
     | TargetRinging | initiator B hangs up | — | TargetRinging (kept) | C keeps ringing; the add continues for A (A will be joined by C) — B's departure never cancels it |
     | TargetRinging | every other party has left (no live party for C to join) | — | Cancelled(`initiator_left`) | C's legs hung up `cancelled`; capacity released |
     | TargetRinging | `RemoveParticipant(C)` by the initiator | — | Cancelled | C's legs hung up |
     | TargetRinging | engine/app lost | — | Lost | §22 |
   - **Room join** (`ConferenceControl.Join`):
     1. One in-process lock per room (single Stasis owner, D-73): find the
        live session (`conference_sessions_live`) or create it with a new
        Call `kind=room` (Initiated → Screening without a capacity check →
        Routing → Presenting) and its `M` (`atsa-m-<room call>`).
     2. `AdmitToRoom` → `Full`/`Locked`: return `ErrFull`/`ErrLocked`
        (`pbx-core` plays `conf-full`/`conf-locked` and hangs up).
     3. Re-home the entrant into the room Call (§11.6);
        `conference_members` row (moderator flag from `JoinCommand`).
     4. `Wait` (wait for moderator, none present): session `Waiting`;
        `POST /bridges/{M}/moh?mohClass=` so entrants hear MOH; timer
        `WaitTimeout` (600 s) → all entrants hear `conf-leaderhasleft`,
        hung up `no_moderator`. A moderator joins → `DELETE …/moh`,
        session `Active`.
     5. Add to `M` (`Muted` → `POST /channels/{id}/mute?direction=in`);
        entry tone `POST /bridges/{M}/play?media=sound:beep`. The room
        Call → `Active` at two live participants. If the room records,
        `pbx-core`'s Program played the consent prompt first (BR-03).
     6. Leave: leave tone; last live participant, or last moderator with
        `EndWithLastModerator` → everyone hung up, session `Ended`, room
        Call `Terminated`, `M` destroyed.
   - **Mute:** `POST`/`DELETE /channels/{id}/mute?direction=in`, `muted`
     flag. **Remove:** hang up (`kicked`). **Lock:** `locked=true`; new
     entrants `ErrLocked`. In-call `*` self-mute (LLD-03) uses the same
     primitive unless a moderator muted.
4. **State machine** (session; LLD-03 §3.11's table, telephony-held):

   | From | Event | Guard | To | Side effect |
   |---|---|---|---|---|
   | — | first admitted entrant | no moderator needed, or moderator | Active | `call.conference_started` |
   | — | first admitted entrant | waiting for moderator | Waiting | bridge MOH |
   | Waiting | moderator joins | — | Active | MOH stops |
   | Waiting | `WaitTimeout` | — | Ended(`no_moderator`) | all hung up, `left_reason=no_moderator` |
   | Active | `Lock(on/off)` | — | Active | `locked` toggled |
   | Active, Waiting | entrant, live = `Limit` | — | unchanged | `ErrFull` |
   | Active | last live leaves | — | Ended | `call.conference_ended` |
   | Active | last moderator leaves | `EndWithLastModerator` | Ended | all hung up, `left_reason=room_ended` |
   | Active, Waiting | engine/app lost | — | Ended(`engine_lost`/`app_restart`) | §22 |
5. **Data model.** `calls.kind='room'`, `conference_sessions`,
   `conference_members` (0032), `call_participants.muted`.
   `conference_rooms` is LLD-03's (0042).
6. **Ports.** `ConferenceControl.Join/Mute/Remove/Lock`;
   `CallControl.AddParticipant`; pure `AdmitToRoom`, `NextRoomState`.
7. **API.** Ad hoc `AddToCall{call_id, destination}` → `{participant_id
   (the new party C, the op's subject), operation_id}` (`call.control`;
   the actor's own live leg is the initiator); refusals: application
   destination → `FAILED_PRECONDITION` "You can only add a person or a
   number to a call"; full →
   `RESOURCE_EXHAUSTED` "This call already has the maximum number of
   people"; capacity → `RESOURCE_EXHAUSTED` "No line is free right now").
   Room RPCs are LLD-03's; telephony errors map `ErrFull` →
   `RESOURCE_EXHAUSTED`, `ErrLocked` → `FAILED_PRECONDITION` "This room is
   locked", participant gone → `NOT_FOUND`.
8. **Configuration.** `conference.adhoc_max` (5); room values from
   LLD-03 §3.11 (limit 25, wait off / 600 s, end-with-moderator off,
   mute-on-entry off).
9. **Failure and recovery.** Re-home fails → entrant stays on its own
   call, hears `conf-errormenu`, hung up; no half-member row (one tx).
   App restart, engine alive → if FL-04 re-adopts, `M` found by name and
   members rebuilt from channel variables; otherwise ended (§22). Engine
   restart → session `Ended(engine_lost)`. A muted entrant who rejoins
   starts with the room's mute-on-entry value.
10. **Invariants and events.** INV-10 (session unique per tenant and room;
    bridge named by the room Call id), BR-03, INV-03. Events
    `call.conference_started/ended`, `participant.joined/left`,
    `call.participant_muted/removed`. Audit of moderator actions is
    `pbx-core`'s.
11. **Tests.**

    | Given | When | Then | Tier |
    |---|---|---|---|
    | A⇄B | add C | three parties hear each other; same `M`; recording continuous | E |
    | five parties ad hoc | add a sixth | `RESOURCE_EXHAUSTED` | U, W |
    | A⇄B, add C | C does not answer | op NoAnswer; A⇄B untouched; capacity released | U, E |
    | A⇄B, add C ringing | B hangs up | C still rings; answers → A⇄C | U, E |
    | A⇄B, add C ringing | A and B hang up | op Cancelled(`initiator_left`); no orphan C leg | U, E |
    | room limit 3 | fourth `Join` | `ErrFull`, others unaffected | U, E |
    | concurrent joins at limit − 1 | two `Join` | exactly one admitted (room lock) | U, I |
    | wait for moderator | two entrants | both hear MOH; moderator joins → MOH stops | E |
    | waiting 600 s | timer | all hung up `no_moderator` | U |
    | moderator mutes B | — | B not mixed (energy check), B hears all | E |
    | last participant leaves | — | session Ended, room Call Terminated | U, I |
    | entrant re-homed | joins | usage has no gap or duplicate | I |
    | AC-03.2 | browser, desk phone and external in one room | all hear each other | A |
12. **Industry parity.** FreePBX ConfBridge, 3CX conference, VitalPBX and
    FusionPBX rooms (see LLD-03 §3.11). **Difference:** a room is one Call
    with N Participants (D-17, D-18), so one record holds the meeting.

## 19. Call waiting (part of PB-15 busy handling)

1. **Trigger and actors.** A new call routes to an extension that already
   has a live Participant. `pbx-core` decides from its presence hub
   (fed by `CallEventSink`): ring it (call waiting) or apply the busy rule
   (`RefusalBusy`/forward, LLD-03 §3.6).
2. **Preconditions.** Call waiting on for the extension and fewer than
   its maximum waiting legs; else the busy rule.
3. **Sequence.** The plan simply includes the busy extension's devices;
   telephony originates the second leg (the phone plays its waiting
   tone). The user answers: a desk phone holds call 1 itself
   (`ChannelHold` → §12 phone hold, the far party hears MOH); the browser
   console calls `HoldCall` on call 1 automatically before answering.
4. **State machine.** No new states.
5. **Data model.** None.
6. **Ports.** `CallEventSink`; `CallControl.LiveParticipants` (resync).
7. **API.** None new (the console uses `HoldCall`).
8. **Configuration.** Call waiting on, one waiting call (LLD-03, per extension).
9. **Failure and recovery.** The phone rejects the second INVITE (486) →
   the leg fails `busy`; the plan's `OnAllBusy` applies.
10. **Invariants and events.** INV-03 (call 1 is never touched by call
    2's failure). Events: `participant.joined/left`, `call.held`.
11. **Tests.** On a call, a second call arrives → waiting tone; answering
    holds the first with MOH (E); call waiting off → busy rule (E, LLD-03);
    browser Answer on a second call holds the first (C, B).
12. **Industry parity.** 3CX, FreePBX (`CW`), FusionPBX and VitalPBX
    default call waiting on per extension. Same here.

## 20. Supervisor monitoring execution primitives (IC-08, AC-16.1)

Policy (`MayMonitor`, who may monitor whom, the tenant/queue switch, the
audit row, the RPCs and their wording) is `pbx-core`'s (LLD-03 §3.17).
Telephony executes. Monitoring writes **no** `call_operations` row:
`monitor_sessions` is its record.

```go
type MonitorMode string // "listen" | "whisper" | "barge"
type MonitorCommand struct {
    TenantID   shareddomain.TenantID
    CallID     shareddomain.CallID
    Target     shareddomain.ParticipantID // the agent's Connected Participant (role AGENT)
    Supervisor shareddomain.PrincipalID
    Device     Leg              // Leg.Originate: owned extension or browser phone (D-81 auto-answer)
    Mode       MonitorMode
    AgentBeep  bool             // tenant agent_notice = beep (start, and change to whisper/barge)
    BargeBeep  bool             // beep to both parties on barge (LLD-03: always true)
}
type ParticipantView struct { // CallControl.LiveParticipants rows (presence resync, §21)
    CallID        shareddomain.CallID
    ParticipantID shareddomain.ParticipantID
    Role          domain.ParticipantRole
    State         domain.ParticipantState
    HoldOrigin    string // "" unless OnHold
    Audibility    string // full | listen | whisper
    ExtensionID   *shareddomain.ExtensionID
    PrincipalID   *shareddomain.PrincipalID
    Pickable      bool
    RingingSince  *time.Time
    AnsweredAt    *time.Time
}
```

1. **Trigger and actors.** `pbx-core` calls `CallControl.StartMonitor` /
   `ChangeMonitorMode` / `StopMonitor` after its policy check and audit
   (supervisor with `pbx.supervise.listen|whisper|barge`).
2. **Preconditions.** Target Participant `Connected`, role `AGENT`; no live
   session on the call (`monitor_sessions_one_per_call`) nor for the
   supervisor (`…_one_per_supervisor`); the target is not itself a
   monitoring supervisor. **Licence:** the supervisor leg is a channel
   (LLD-03 §3.17 row 8): telephony — the only caller of `ValidateCapacity`
   (LLD-08) — reserves one unit keyed `<call_id>/<supervisor participant>`
   before originating, released when that Participant leaves;
   refusal → `ErrCapacity`.
3. **Sequence.**
   1. One tx: session row `Connecting`; supervisor Participant `Invited`,
      role `SUPERVISOR`, `audibility` = `listen`, `whisper` or `full`
      (barge); capacity reserved.
   2. Originate the supervisor device (ring 20 s).
   3. Supervisor answers →
      - listen: `POST /channels/{agent}/snoop?spy=both&whisper=none&app=<ARI app>&snoopId=atsa-snoop-<session>`;
      - whisper: `spy=both&whisper=out` (SPIKE-CC-3);
      - snoop `StasisStart` (args `snoop,<session>`; never a Participant)
        → create `S` (`atsa-s-<session>`), add snoop and supervisor to `S`;
      - barge: add the supervisor to `M`; if `BargeBeep`,
        `POST /bridges/{M}/play?media=sound:beep` (both parties hear it).
      `AgentBeep` and mode ≠ listen → `POST /channels/{agent}/play?media=sound:beep`.
      Session `Active`; `CallEventSink(MonitorStarted)`; `call.monitor_started`.
   4. Mode change (`ChangeMonitorMode`), supervisor never re-rung:

      | From → to | Execution | Gap |
      |---|---|---|
      | listen ↔ whisper | hang up the snoop, create one with the other `whisper`, add to `S`; `AgentBeep` on → whisper | ≤ 200 ms |
      | listen/whisper → barge | remove supervisor from `S`, add to `M`, hang up snoop, destroy `S`; `BargeBeep` | — |
      | barge → listen/whisper | remove supervisor from `M`; create the snoop and `S` as in step 3; supervisor into `S`; `audibility` back to `listen`/`whisper` | ≤ 200 ms |
   5. Stop: hang up the supervisor leg and any snoop, destroy `S`; session
      `Ended`; supervisor `Disconnected(monitor_stopped)`; capacity
      released; `CallEventSink(MonitorStopped{session, reason})`;
      `call.monitor_stopped`.
4. **State machine** (`monitor_sessions.state`; `end_reason` = LLD-03's list):

   | From | Event | Guard | To | Side effect |
   |---|---|---|---|---|
   | — | `StartMonitor` | preconditions | Connecting | capacity reserved |
   | Connecting | supervisor answers, snoop/bridge ready | — | Active | `MonitorStarted` |
   | Connecting | no answer 20 s / originate or snoop fails | — | Ended(`supervisor_no_answer` / `snoop_failed`) | nothing left on the call |
   | Active | `ChangeMonitorMode` | allowed by pbx-core | Active | mode, `audibility` updated |
   | Active | `StopMonitor` / supervisor hangs up | — | Ended(`supervisor_stopped` / `supervisor_left`) | — |
   | Active (listen/whisper) | target Participant leaves or is re-homed | — | Ended(`target_left`) | supervisor leg hung up |
   | Active (barge) | **agent leaves**, customer still live | — | Ended(**`barge_takeover`**) | call continues (INV-03); the supervisor's Participant stays `Connected`, role `SUPERVISOR`, `audibility=full`, and now counts as live; its capacity unit stays until it leaves |
   | Active (barge) | customer leaves | — | Ended(`call_ended`) | call ends as normal |
   | Active | pbx-core stops it (`policy_disabled`, `permission_revoked`) | — | Ended(reason) | call untouched |
   | Connecting, Active | engine/app lost | — | Ended(`engine_lost`/`app_restart`) | §22 |
5. **Data model.** `monitor_sessions` (0033) with both partial unique
   indexes; `call_participants.audibility`; no `call_operations` row.
6. **Ports.** `CallControl.StartMonitor/ChangeMonitorMode/StopMonitor`;
   `CallEventSink` kinds `MonitorStarted`, `MonitorStopped`;
   `MediaGateway.StartSnoop(ch, SnoopRequest{Spy, Whisper, SnoopID})`,
   `PlayToBridge`; `LicenseManager.ValidateCapacity/ReleaseCapacity`.
7. **API.** Defined in LLD-03 §3.17. Telephony errors: target not
   Connected → `ErrTargetNotConnected` (`FAILED_PRECONDITION` "That call
   isn't connected"); call already monitored → `ErrMonitorBusy`
   (`ALREADY_EXISTS`, LLD-03 wording); supervisor busy →
   `ErrSupervisorBusy` (`FAILED_PRECONDITION` "You're already monitoring a
   call."); `ErrCapacity` (`RESOURCE_EXHAUSTED` "No call capacity left;
   try again shortly").
8. **Configuration.** Supervisor ring 20 s (installation); mode-change gap
   target ≤ 200 ms; notices and switches in LLD-03.
9. **Failure and recovery.** Snoop fails → session `Ended(snoop_failed)`,
   supervisor hung up, the monitored call untouched (D-23 spirit, INV-03).
   Beep playback fails → logged, the barge proceeds. Restart → §22
   (sessions ended; snoops and supervisor legs hung up).
10. **Invariants and events.** INV-03, INV-10, AC-16.1, AC-16.2 (audit by
    `pbx-core` before `StartMonitor`; no audit, no monitor). Listen/whisper
    Participants never count as live and are excluded from the CDR's
    party count; a barge-takeover supervisor is a normal party from then
    on. Events `call.monitor_started`, `call.monitor_mode_changed`,
    `call.monitor_stopped`.
11. **Tests.**

    | Given | When | Then | Tier |
    |---|---|---|---|
    | agent⇄customer, listen | supervisor speaks | `M`'s recording has no supervisor audio; neither party hears it | E, **A (AC-16.1)** |
    | whisper | supervisor speaks | agent channel recording has it, customer's has not | E (SPIKE-CC-3) |
    | whisper on the rig with real phones | supervisor coaches | the agent hears the coaching; the customer, listening on a desk phone, hears nothing; verified in the recording | **A (AC-16.1)** |
    | listen → barge → listen | mode changes | no re-ring; beep to both on barge; three-way, then inaudible again | E |
    | barge | agent hangs up | customer⇄supervisor continue; session `barge_takeover` | U, E |
    | listen | agent hangs up | session `target_left`; supervisor leg hung up | U, E |
    | call ends while monitoring | — | no orphan snoop on `GET /channels` | E |
    | capacity exhausted | `StartMonitor` | `ErrCapacity`; nothing created | U, I |
    | two supervisors on one call | concurrent | exactly one session (index) | I |
12. **Industry parity.** FreePBX ChanSpy (`q`, `w`, `B`), 3CX listen/
    whisper/barge, VitalPBX and FusionPBX eavesdrop. **Difference:** each
    action is an audited platform session with its own row, not a
    dialplan feature code, and a barge can take over a call the agent
    leaves.

## 21. Device state for BLF and park lamps (PB-11, PB-18 seam)

No dialplan is generated (D-47), so lamps use ARI-controlled device
states. `pbx-core` decides names and values (LLD-03 §3.8, `PresenceOf`);
telephony owns the engine write and the facts.

| Piece | Owner | Definition |
|---|---|---|
| Facts | telephony | `CallEventSink` (in process): `ParticipantRinging/Answered/Held/Resumed/Parked/Transferring/Left`, each with `extension_id`, `call_id`, `participant_id`, `ringing_since`; `CallControl.LiveParticipants` for a resync after overflow or a `pbx-core` restart |
| Value | `pbx-core` (pure) | `NOT_INUSE`, `INUSE`, `BUSY`, `RINGING`, `RINGINUSE`, `ONHOLD`, `UNAVAILABLE`, `INVALID` (e.g. DND → `BUSY`, unregistered → `UNAVAILABLE`, occupied slot → `INUSE`) |
| Write | telephony port, ACL adapter | `DeviceStatePublisher.Set(ctx, name, state)` → `PUT /deviceStates/Stasis:<name>?deviceState=`; `Clear` → `DELETE` |

```go
type DeviceState string // the eight ARI values above; anything else is refused
type DeviceStatePublisher interface {
    // name is pbx-core's (e.g. "atsa-<ext hex>", "atsa-park-<lot hex>-<slot>"):
    // must match ^atsa-[a-z0-9-]{1,64}$ so it can never carry a tenant-local number.
    Set(ctx context.Context, name string, state DeviceState) error
    Clear(ctx context.Context, name string) error
}
type MailboxPublisher interface {
    Set(ctx context.Context, name string, newMsgs, oldMsgs int) error // PUT /mailboxes/{name}
}
```

- **Prefix.** ARI controls only `Stasis:` devices (`PUT` on any other
  technology is refused as uncontrolled), so the ACL owns the prefix and
  LLD-03's `Custom:` (§3.5.1, §3.8, SPIKE-P1 hint) reads `Stasis:`. SPIKE-P1
  (LLD-03 §3.8) proves the pattern hint `Stasis:atsa-${EXTEN}` and its
  fallback (console presence only).
- **Writes only, no subscription** (asterisk/asterisk#2170). On app start
  `pbx-core` republishes every name (idempotent), so engine restarts heal.
- A write failure is logged and retried by the next change, never a call
  failure (INV-06). Budget: LLD-03 §3.8 (device state ≤ 1 s).
- **Tests:** name with a digit-only suffix refused (U); `Set` → `GET
  /deviceStates/Stasis:atsa-…` returns the value (E); `CallEventSink` never
  blocks the event loop when `pbx-core` is slow (overflow → `Resync`) (U).
- **Industry parity.** FreePBX and VitalPBX generate `hint` lines and use
  `Custom:` device states written from dialplan (`DEVICE_STATE()`);
  FusionPBX publishes presence from FreeSWITCH's `mod_valet_parking` and
  call events; 3CX uses its own BLF presence server. **Difference:** no
  generated dialplan (D-47): lamps are written over ARI as `Stasis:`
  devices from platform events, with tenant-safe opaque names.

## 22. Engine loss, Stasis reconnect and app restart

Whole-call re-adoption is FL-04's decision (HLD-09). This section fixes
what call control does in each case so no leg is orphaned whichever way
FL-04 goes.

| Case | Detect | Call-control behaviour |
|---|---|---|
| ARI websocket gap, both alive | event stream reconnects | **Reconcile per call:** `GET /channels`, `GET /bridges`; every correlated channel missing → its `ChannelDestroyed` is synthesised; then each open op is re-evaluated by the table below |
| Asterisk restarted | reconnect finds none of our channels | every open op → outcome **`Lost`** with `reason=engine_lost` (the one outcome every kind has, §11.3: `hold`, `blind_transfer`, `attended_transfer`, `park` — with `call.parked_lost`, `pbx-core` `Sweep` marks its row `LOST` — `pickup`, `adhoc_add`); room sessions → `Ended(engine_lost)`; monitor sessions → `Ended(engine_lost)`; queue media per §23.1; dialler attempts per §23.3; Participants `Disconnected(engine_lost)`; calls `Terminated(engine_lost)`; `pbx-core` republishes lamps |
| App restarted, engine alive | startup | Durable rows say what was in flight. **If FL-04 re-adopts:** rebuild correlation from `ATSA_*` channel variables and bridges by name (`atsa-m/k/s/q-…`, dialler session bridges `atsa-d-…`), then the table below. **If not (today's accepted limitation):** hang up every `atsa-` channel, destroy every `atsa-` bridge (AI tap channels `aitap-`/`aixm-`/`aitapbr-` are swept by LLD-10), then close rows as for an engine restart with `reason=app_restart` (outcome `Lost`) |
| `ApplicationReplaced` (D-73) | event | not ready; no reconcile from this instance; the new owner reconciles |

**Per-operation reconcile** (only when the parties survive):

| Op state found | Engine shows | Action |
|---|---|---|
| hold `Held` | A out of `M` | restart MOH on A; re-arm reminder |
| hold `Recalling`, blind `TargetRinging`/`Recalling` | plan legs absent | `Continue` again from the terminal (the plan restarts; legs are never resumed) |
| blind `TargetRinging` | C up, not in `M` | add C to `M`, `Completed` |
| attended `ConsultRinging/Consulting` | `K` missing | cancel: B (if alive) and A back to `M` |
| attended any | A alone, B gone | orphan recall (§12) |
| park `Parked` | A alone in `M` | restart bridge MOH; `pbx-core` `Sweep` re-arms the timer from `expires_at` (expired → `Continue(park_timeout)` now) |
| park `Recalling` | A in `M`, no comeback legs | `Continue(park_timeout)` again |
| adhoc_add `TargetRinging` | C's legs absent | op `NoAnswer`; capacity released; the call is untouched |
| adhoc_add `TargetRinging` | C up, not in `M` | add C to `M`, `Completed` |
| room session `Waiting` | `M` with entrants | restart bridge MOH; re-arm the moderator-wait timer for the **remaining** time (`started_at + WaitTimeout − now`; already past → end `no_moderator` now) |
| pickup `Claimed` | picker up, not bridged | bridge if the caller is alive, else hang up the picker |
| room session | `M` with members | keep members whose channels exist; others `Disconnected(engine_lost)` |
| monitor any | — | end session, hang up snoop and supervisor |

Every ARI step has `control.ari_step_timeout` (5 s); a timed-out step is
a failure of that step, compensated as each flow's item 9 says.
Outcomes on loss are per kind: `Lost` for every `call_operations` kind
(never `Failed`, which only means "a transfer plan ended with nobody");
`Ended(engine_lost|app_restart)` for room and monitor sessions.
Compensation never hangs up a party that was in conversation before the
operation began (INV-03). **Tests:** websocket dropped for 10 s during a
blind transfer whose target answered in the gap → `Completed` after
reconnect (E, fault-injected); Asterisk restarted with held, parked and
consulting calls → every row closed, `GET /channels` and `GET /bridges`
empty of `atsa-` (E); app killed mid-consult with FL-04 "end" → no
`atsa-` channel survives startup (E); websocket gap while a room waits
for its moderator → timer re-armed for the remaining time (U).

**Industry parity.** FreePBX, VitalPBX and FusionPBX drop all calls on an
engine restart and keep no record of in-flight transfers or parks; 3CX
likewise loses calls when its media server restarts. **Difference:** every
in-flight operation is a durable row closed explicitly (`Lost`,
`parked_lost`), and an app restart with the engine alive either re-adopts
or sweeps every `atsa-` resource, so no leg is ever orphaned.

## 23. Contracts other LLDs rely on (records, AI, provisioning)

These are telephony-core changes requested by LLD-09, LLD-10 and LLD-12;
their designs are final there. Each lands in the OpenSpec change named.

| Contract | Owner LLD | Telephony change |
|---|---|---|
| `call.terminated` **v2** | [LLD-09 §3.3](LLD-09-reporting.md) | On `Terminated`, the outbox row in the same transaction carries the full snapshot: `call_id, direction, source_digits, dest_digits, started_at, answered_at?, ended_at, termination_reason, disposition, service_type, department_id?, route_id?, carrier_id?, trunk_id?, cost_micros, participants[]{participant_id, role, extension_id?, principal_id?, joined_at, answered_at?, left_at, billable_seconds, usage_seconds, cost_micros}`, UTC microseconds. `disposition` is derived (pure) from the Participants' `left_reason`s; `service_type`, `department_id`, `route_id`, `carrier_id`, `trunk_id` come from `pbx-core`'s decision attribution (`InboundDecision`/`PlaceCall` gain an opaque `CallAttribution`); cost fields are 0 until rating exists. v1 consumers ignore extra fields. Merged calls (§11.6) carry `merged_into_call_id` |
| `participant.joined` / `participant.left` | [LLD-10 §3](LLD-10-ai-pipeline.md) (AI-01 starts from it) | Payload gains `role`, `extension_id?`, `principal_id?`, `direction`, `joined_at`/`left_at`, `left_reason`; emitted for every Participant including re-homed and supervisor ones (`audibility` included so the tap can skip monitors). Telephony never calls `ai-pipeline`. Also `consent` (`not_evaluated`, `not_required`, `pending`, `satisfied`, `failed`) and `pci_paused` (bool), both read from the call's state at emission |
| Consent and PCI events | [LLD-09](LLD-09-reporting.md), [LLD-10](LLD-10-ai-pipeline.md) | `call.consent_satisfied{call_id, at, method, policy_id}` (method includes `on_demand_announced` when an on-demand recording's announcement played) and `call.consent_failed{call_id, at, reason}`, each in the transaction that records the consent outcome. `call.pci_paused` / `call.pci_resumed{call_id, at, actor_id?, auto}`. **`pci_paused` is call state** (`calls.pci_paused`, migration 0030), so a Participant joining while paused is published with `pci_paused=true` and no tap is started. `recording.started` / `recording.stored` carry `segment_no` |
| Recording RPCs `StartCallRecording`, `StopCallRecording`, `PauseCallRecording`, `ResumeCallRecording` | [LLD-09 §4–§5](LLD-09-reporting.md) (RC-01, RC-07) | On `TelephonyService`; inbound port `RecordingControl{Arm, Start, Stop, Pause, Resume}`; ACL port `EngineRecorder{Play, RecordBridge, StopLive, Mute, Unmute, FetchStored, DeleteStored}` over `POST /bridges/{id}/record`, `POST /recordings/live/{name}/stop|mute|unmute`, `GET /recordings/stored/{name}/file`, `DELETE /recordings/stored/{name}`. Because every flow keeps `M` (§11.4), hold, blind transfer, completed attended transfer, park and pickup need **no** new segment; a park keeps the parked party in `M` (D-99), so **no new segment while parked** (the segment records the MOH). Only a recorded transferor moving into the consult bridge `K` starts a segment there, per LLD-09 §4.3 step 7; a queue caller in `Q` before any agent answers is not yet recorded (LLD-09 starts recording on `M`). **PCI pause** is allowed when the call has a live recording **or** an AI tap (either suffices): it mutes live segments and calls `MediaTap.Suspend` (not `Stop`) on every tap of the call synchronously, sets `calls.pci_paused`, and emits `call.pci_paused`; resume reverses with `MediaTap.Resume` and `call.pci_resumed` |
| `MediaTap` inbound port | [LLD-10 §3](LLD-10-ai-pipeline.md) (AI-01) | `Start(ctx, TapRequest{CallID, ParticipantID, StreamID, RelayAddr}) (TapHandle, error)` (returns `ErrPCIPaused` while the call is PCI-paused), `Stop(ctx, TapHandle) error`, `Suspend(ctx, TapHandle) error` (deletes the snoop `aitap-<stream>` only; the externalMedia channel and bridge stay, so no audio flows) and `Resume(ctx, TapHandle) error` (a new snoop joins the same bridge): snoop `spy=in&whisper=none&snoopId=aitap-<stream>`, `POST /channels/externalMedia` (`encapsulation=rtp`, `transport=udp`, `format=slin16`, `channelId=aixm-<stream>`), a mixing bridge `aitapbr-<stream>`. `StasisStart` for `aitap-`/`aixm-` ids never creates a Participant, reserves capacity or writes a CDR. The tap follows the Participant: re-home (§11.6) or a new channel ends it (`participant.left`), and AI-01 restarts it on the new `participant.joined`. Gated by SPIKE AI-S1. **Unexpected loss** of `aitap-`/`aixm-` (not caused by `Stop`, `Suspend` or call end) emits internal `ai.tap_lost{stream_id, reason}`; telephony's own deletions never do |
| `usage_seconds.quality_mos` | [LLD-09 §8](LLD-09-reporting.md) (RC-06) | Migration 0030 also runs `ALTER TABLE usage_seconds ALTER COLUMN quality_mos DROP NOT NULL, ALTER COLUMN quality_mos DROP DEFAULT` (and the same for `quality_jitter`, `quality_loss`): quality comes from RTCP SR/RR windows (`participant.rtcp_window`, SPIKE R-S1), never a fabricated 4.50 |
| `MaintenanceGate` | [LLD-12 Part B](LLD-12-provisioning-and-lifecycle.md) | Call admission (`Screening`, and arriving-call refusal before it) calls `Admit(ctx, emergency)` first: `ErrDraining` → `RefusalUnavailable` (SIP 503, Q.850 cause 41); emergency always admitted (INV-01). New legs **inside** an existing call (transfer, consult, recall, pickup leg, ad-hoc party) are not call setups and are always admitted (INV-03); a new arriving call for pickup, retrieve or room entry is gated like any other. `ActiveCallStats()` feeds `drain status` |

### 23.1 Queue media for the ACD (LLD-03 §3.3.1–§3.3.3)

`pbx-core` owns entries, the dispatcher, reservations, announcements'
timing and every queue decision (LLD-03 §3.3). Telephony executes the
media through two ports and reports outcomes back. The operations below
are the names LLD-03 uses.

1. **Trigger and actors.** A `HoldInQueue{entry_id, HoldClass}` command at
   the end of a queue decision's `Program`; then the dispatcher (system)
   calls `QueueMedia`. The caller has no permission; agents act through
   their devices.
2. **Preconditions.** The caller's Participant is live and the call is not
   already in a queue (one live entry per call, LLD-03's partial unique
   index; telephony refuses a second `Hold` with `ErrAlreadyQueued`). The
   call already holds its licence capacity; agent legs are internal legs
   of the same call and reserve nothing (LLD-03 §3.3.1 item 2). New
   arriving calls are gated by `MaintenanceGate` before they reach the
   queue; offers to agents are not.
3. **Sequence.**

   | Operation | Telephony steps (ARI) | Participant effect |
   |---|---|---|
   | `QueueMedia.Hold(entry, call, mohClass)` | `POST /channels/{caller}/answer` (if not yet); create `Q` = `POST /bridges?type=holding&bridgeId=atsa-q-<entry>`; `removeChannel` from `M` if the caller was in it (transfer into a queue); `addChannel` caller to `Q`; `POST /bridges/{Q}/moh?mohClass=`; set channel var `ATSA_QUEUE_ENTRY=<entry>` | caller → `OnHold(queue)` |
   | `QueueMedia.Announce(entry, prompts)` | `DELETE /bridges/{Q}/moh`; `POST /channels/{caller}/play?media=<p1>,<p2>…&playbackId=qa-<entry>-<n>`; on `PlaybackFinished` (or failure) `POST /bridges/{Q}/moh`; DTMF during MOH or playback → `QueueOutcome.DtmfPressed(entry, digit)` | none |
   | `QueueMedia.Offer(offer)` | per agent device: new Participant (role `AGENT`, D-87 attribution, `Pickable=false`) `Invited`; `POST /channels/acdo-<offer_id>-<n>?endpoint=…&app=<ARI app>&timeout=<ring_seconds>&callerId=<queue>: <caller>` with `ATSA_*` variables and `ATSA_QUEUE_OFFER=<offer>`. ARI `409` on the explicit id = already sent (idempotent retry, SPIKE-Q3) | agents `Invited → Ringing` |
   | first agent answers | `StasisStart`/`Up` → `QueueOutcome.Answered(offer_id, principal)` **synchronously**; `Connect` → continue; `Reject` → hang that leg up `answered_elsewhere` | — |
   | `QueueMedia.Connect(offer, principal)` | optional `POST /channels/{agent}/play?media=<agent_announce>` (agent only); recording consent per LLD-09 (BR-03); `DELETE /bridges/{Q}/moh`, `removeChannel` caller from `Q`, `addChannel` caller and agent to **`M`** (the call's main bridge, kept — LLD-03's "mixing bridge"); destroy `Q`; other legs of the offer `DELETE /channels/{id}?reason_code=26` | caller `OnHold(queue) → Connected` by the `QueueConnected` transition (§11.2; no `hold` op exists or is needed for `hold_origin=queue`); agent `Connected`; siblings `Disconnected(answered_elsewhere)` |
   | `QueueMedia.Cancel(offer, reason)` | `DELETE` every live leg of the offer (`reason_code=26` for `answered_elsewhere`, else `normal`) | legs `Disconnected(cancelled)` |
   | no answer / 486 / 603 / 480 | `Dial`/`ChannelDestroyed` cause → `QueueOutcome.Missed(offer_id, PartyRef{PrincipalID: agent}, cause)` | leg `Disconnected(no_answer\|busy\|rejected\|unreachable)` |
   | `QueueMedia.Leave(entry, destination)` | `DELETE /bridges/{Q}/moh`, `removeChannel` caller, destroy `Q`; `InboundRouter.Continue{reason: queue_overflow, Destination, Routing}` → run the returned decision | caller `OnHold → Transferring` (plan) or `Connected` (application) |
   | caller hangs up while queued/offered | `ChannelDestroyed` → `QueueOutcome.CallerLeft(entry)`; cancel the offer's legs; destroy `Q` | caller `Disconnected(normal)` |
   | agent or caller leaves after connect, or the agent transfers the caller away | `QueueOutcome.Ended(entry, reason)` with `agent_left` / `caller_left` / `transferred` | — |
   | **Callback** (LLD-03 §3.3.3 steps 5–9) | `Offer` with `Callback{entry, number}` creates a **new** Call (outbound, `service_type=contact_centre`) whose first Participant is the agent; on answer the agent → `OnHold(queue)` with channel MOH; pbx-core then calls `QueueMedia.DialCallback(offer, decision)` (a route plan already gated by LLD-04 and presentation; telephony reserves the customer leg's capacity as the call's own reservation — it is the only `ValidateCapacity` caller, LLD-08) → the customer leg runs the plan, then the decision's confirm `Program`; on `1` → `Connect` (agent unheld, both in `M`); failure → `QueueOutcome.Missed(offer, PartyRef{Number: customer E.164}, cause)` and the agent leg is hung up | as rows above |
   | `LiveCalls.List(ctx, tenant)` | in-memory calls plus `GET /channels` reconciliation; returns `[{call_id, participant_id, role, state, queue_entry?, queue_offer?, answered_at?}]` from the `ATSA_QUEUE_*` variables, so a new dispatcher can recover after any restart | — |

4. **State machine.** No new Participant states: queued = `OnHold`
   (`hold_origin=queue`), offered agent legs = `Invited/Ringing`,
   connected = `Connected`. Entry states are LLD-03's. Telephony's own
   per-entry media state (pure `NextQueueMediaState`):

   | From | Event | To |
   |---|---|---|
   | — | `Hold` | `Waiting` (MOH) |
   | `Waiting` | `Announce` | `Announcing` |
   | `Announcing` | `PlaybackFinished` / playback failed | `Waiting` (MOH restarted) |
   | `Waiting`, `Announcing` | `Connect` | `Bridged` (`Q` destroyed) |
   | `Waiting`, `Announcing` | `Leave` | `Left` (`Q` destroyed) |
   | any | caller gone | `Gone` (`Q` destroyed) |

   `Announce` while `Announcing` queues the new prompts after the current
   ones; `Connect` during `Announcing` stops the playback first.
5. **Data model.** No new table: entries are LLD-03's `queue_entries`.
   `call_participants.hold_origin` gains `queue` (migration 0030 check).
   Correlation survives restarts through `ATSA_QUEUE_ENTRY` /
   `ATSA_QUEUE_OFFER` channel variables.
6. **Ports.**

   ```go
   type QueueMedia interface { // inbound, implemented by telephony, called by pbx-core
       Hold(ctx context.Context, entry QueueEntryID, call shareddomain.CallID, mohClass string) error // ErrAlreadyQueued, ErrCallGone
       Announce(ctx context.Context, entry QueueEntryID, prompts []string) error                      // ErrCallGone
       Offer(ctx context.Context, o QueueOffer) error                                                 // idempotent by OfferID
       Cancel(ctx context.Context, offer OfferID, reason string) error                                // idempotent
       Connect(ctx context.Context, offer OfferID, agent shareddomain.PrincipalID) error              // ErrCallGone, ErrOfferGone
       Leave(ctx context.Context, entry QueueEntryID, dest Destination, routing []byte) error
       DialCallback(ctx context.Context, offer OfferID, target InboundDecision) error
   }
   type QueueOffer struct {
       OfferID OfferID; Entry QueueEntryID; TenantID shareddomain.TenantID
       CallID  *shareddomain.CallID           // nil for a callback: telephony creates the call
       Agents  []OfferedAgent                 // {PrincipalID, ExtensionID, DeviceURIs}
       RingSeconds int; CallerID, AgentAnnounce string
       Callback *CallbackTarget               // {Number, Display} for IC-05
   }
   type QueueOutcome interface { // outbound, implemented by pbx-core; called synchronously for Answered
       Answered(ctx context.Context, offer OfferID, agent shareddomain.PrincipalID) (ConnectVerdict, error) // Connect | Reject
       Missed(ctx context.Context, offer OfferID, who PartyRef, cause string) error // agent or callback customer
       Ended(ctx context.Context, entry QueueEntryID, reason string) error
       CallerLeft(ctx context.Context, entry QueueEntryID) error
       DtmfPressed(ctx context.Context, entry QueueEntryID, digit string) error
   }
   // PartyRef names who missed: an agent, or a callback customer who is not a principal.
   type PartyRef struct {
       PrincipalID *shareddomain.PrincipalID // agent leg
       Number      string                    // E.164 of an external party (callback customer); empty for an agent
       ParticipantID shareddomain.ParticipantID
   }
   type LiveCalls interface { List(ctx context.Context, tenant shareddomain.TenantID) ([]LiveCall, error) }
   ```

   `Answered` has a 2 s deadline: on timeout or error telephony treats the
   verdict as `Reject` (the agent leg is hung up, the caller keeps waiting,
   the dispatcher's sweeper restores the entry). Other outcome calls are
   non-blocking with retry from the event loop's queue (`CallEventSink`
   rules).
7. **API.** None on telephony (queue RPCs are LLD-03 §3.3). Errors to
   pbx-core: `ErrAlreadyQueued`, `ErrCallGone` (caller left before the
   step), `ErrOfferGone` (offer cancelled or answered), engine failure
   wrapped as `ErrEngineUnavailable` (pbx-core retries on its 1 s tick).
8. **Configuration.** All queue values are LLD-03's. Telephony:
   `QueueOutcome.Answered` deadline 2 s (installation); offer leg channel
   id prefix `acdo-` fixed.
9. **Failure and recovery.**

   | Situation | Behaviour |
   |---|---|
   | `Q` creation or MOH fails at `Hold` | caller stays in `M` (or unbridged) with channel MOH; `Hold` succeeds degraded; logged. Silence is never left |
   | Announcement playback fails | MOH restarted at once; `Announce` returns nil (an announcement is not worth an error) |
   | `Connect`: caller left between `Answered` and bridge | `ErrCallGone`; agent leg hung up; pbx-core ends the entry |
   | Agent answers after `Cancel` | leg hung up `answered_elsewhere` (LLD-03 "Reject") |
   | Repeated `Offer` after a dispatcher restart | `409` on the explicit id = already sent; no duplicate leg |
   | App restart, engine alive | if FL-04 re-adopts, `Q` bridges are found by name and the `ATSA_QUEUE_*` variables rebuild correlation; `LiveCalls.List` feeds LLD-03 recovery (a)–(f). If not, §22's sweep ends the calls and `LiveCalls.List` returns none, so every entry closes `lost` |
   | Engine restart | callers and legs gone; `LiveCalls.List` empty; LLD-03 recovery closes entries |
10. **Invariants and events.** INV-03 (an announcement or outcome failure
    never ends the caller; a queue caller is `OnHold`, so the §11.2 end rule
    keeps the call while it waits), INV-10 (entry and offer ids are checked
    against the tenant of the call; `LiveCalls.List` is per tenant), INV-01
    (an emergency number is never a callback target — LLD-03 refuses it).
    Events: `call.held`/`call.resumed` with `hold_origin=queue`,
    `participant.joined/left` (agents), `call.terminated` v2 with
    `service_type=contact_centre`; queue events are LLD-03's.
11. **Tests.**

    | Given | When | Then | Tier |
    |---|---|---|---|
    | caller answered by a queue decision | `Hold` | caller in `atsa-q-<entry>` with MOH; `OnHold(queue)`; call not ended | U, E |
    | queued | `Announce([pos, ewt])` | MOH stops, prompts play, MOH back; gap < 300 ms | E (SPIKE-Q2) |
    | DTMF `1` during MOH | — | `DtmfPressed(entry, "1")` once | U, E |
    | ring-all offer of two agents | first answers | `Answered` called once; caller and agent in `M`; other leg stopped with no missed call; `Q` destroyed | E |
    | `Answered` returns `Reject` / times out | — | agent leg hung up; caller still on MOH | U |
    | offer repeated with the same id | `Offer` twice | one leg per device (409 path) | E (SPIKE-Q3) |
    | agent no answer 15 s | — | `Missed(…, no_answer)`; caller still queued | U, E |
    | caller hangs up while offered | — | `CallerLeft`; agent legs cancelled; no `acdo-` channel left | E |
    | max wait | `Leave(entry, voicemail)` | caller leaves `Q` and reaches voicemail via `Continue` | E |
    | caller transferred into a queue from an active call | `Hold` | caller removed from `M` into `Q`; transferor already gone; call not ended | U, E |
    | callback | `Offer{Callback}`, agent answers, `DialCallback`, customer presses 1 | new call; agent heard MOH, then both in `M` | E |
    | dispatcher restart with an offered entry | `LiveCalls.List` | offer legs and entry ids returned from channel variables | I, E |
    | tenant B entry id in a tenant A call | `Hold` | refused | U |
12. **Industry parity.** Asterisk `app_queue` (FreePBX), 3CX queues,
    VitalPBX and FusionPBX `mod_callcenter` all keep the caller on MOH in
    the queue, announce position/hold time, ring agents and bridge the
    first to answer. **Difference:** queue state is durable in Postgres
    under one dispatcher (no Redis, D-56); telephony keeps one main bridge
    per call, so a queue call that is later transferred or recorded has one
    continuous record; callback is agent-first (LLD-03 §3.3.3).

### 23.2 One monitor per call

LLD-03 §3.17 enforces one live monitor per call, and the database backs
it: migration 0033 adds
`CREATE UNIQUE INDEX monitor_sessions_one_per_call ON monitor_sessions
(tenant_id, call_id) WHERE ended_at IS NULL;` next to the per-supervisor
index. A second `StartMonitor` on the same call fails the insert and
returns `ErrMonitorBusy` (pbx-core: `FAILED_PRECONDITION` "Someone is
already monitoring this call."). Test: two supervisors start at once →
exactly one session (I).

### 23.3 Dialer media (LLD-06 OB-02, OB-03, OB-07, OB-08, OB-10, OB-11)

The dialler (LLD-06) owns campaigns, the hopper, pacing, attempts, the
compliance gate call (`Gate.Decide`, LLD-04) and dispositions. Telephony
owns every channel, bridge and Call, **and the licence reservation**:
LLD-08 makes telephony the only caller of `ValidateCapacity`, so LLD-06's
own `Capacity` port is replaced by this contract.

1. **Trigger and actors.** The pacer (power), the agent's *Dial* (preview,
   manual) and *Drop* (voicemail drop), the agent's *Join campaign*
   (nailed-up leg); permissions are LLD-06's (`dialer.agent`). No customer
   permission.
2. **Preconditions.**

   | Condition | When it does not hold |
   |---|---|
   | The attempt carries a **permitted compliance decision id** from `Gate.Decide` (LLD-04), made in the dialler's transaction | `ErrNoDecision` (programming error; nothing dialled) |
   | Destination is not an emergency number of the tenant (catalogue as LLD-03 `IsEmergency`) | `ErrEmergencyRefused`: the dialler must never place an emergency call (INV-01 protects emergency *callers*; a campaign to 112 is refused) |
   | Licence capacity for the customer leg (`ValidateCapacity(tenant, call_id, 1)`, telephony) | `ErrCapacity` → attempt `NOT_PLACED` `capacity` |
   | `MaintenanceGate.Admit(false)` (LLD-12) | `ErrDraining` → `NOT_PLACED` `draining` |
   | Agent session leg `Connected` (preview, power reserved mode, manual) | `ErrSessionDown` |
3. **Sequence.**

   **Nailed-up agent leg** (`DialerMedia.OpenSession`):
   1. One tx: new Call (`direction=OUTBOUND`, `service_type=campaign`,
      Screening **skipped as an internal leg**, no capacity per LLD-06
      §14.2 recommendation, owner decision there); agent Participant
      `Invited`, role `AGENT`, `Pickable=false`.
   2. `POST /bridges?type=mixing&bridgeId=atsa-d-<session>`; originate the
      agent device `POST /channels/atsa-dag-<session>-<n>?app=<ARI app>&appArgs=dialer-agent,<session>&timeout=30`;
      on answer add to `atsa-d-<session>`; the agent Participant
      `Connected`; `DialerEvents.AgentLegUp(session)`.
   3. **Keepalive:** while no customer is in the bridge, the agent hears
      silence (no MOH, so customer audio is never late); every 30 s
      telephony checks `GET /channels/{agent}`; a failure, `StasisEnd` or
      `ChannelDestroyed` → `DialerEvents.AgentLegLost(session, cause)`,
      agent `Disconnected(leg_lost)`. Re-origination (≤ 3 tries, 5 s apart)
      is the dialler's decision (`OpenSession` again with the same session
      id; the bridge is reused).
   4. `CloseSession` → hang up the agent leg, destroy the bridge after any
      customer has left.

   **Customer attempt** (`DialerMedia.PlaceAttempt`):
   1. Telephony creates the **Call and `call_id`** (returned to the
      dialler, which stores it on `dialer_attempts.call_id`): one tx —
      `calls` row (`direction=OUTBOUND`, `service_type=campaign`,
      `compliance_decision_id`, `dialer_attempt_id`), Screening runs
      **capacity and `MaintenanceGate` only and does not call
      `ComplianceEngine` / `Gate.Decide` again** — it records the
      carried decision id (a missing id refuses, see above); customer
      Participant `Invited` (role `CALLER`, `Pickable=false`).
   2. `POST /channels/create?endpoint=<route endpoint from pbx-core>&app=<ARI app>&appArgs=dialer,<attempt>&channelId=atsa-dc-<attempt>`
      with `ATSA_*` variables and the presentation `pbx-core`'s
      `ChoosePresentation` gave (CALLERID(num/name), P-Asserted-Identity).
   3. Preview/manual/reserved: `POST /bridges/{atsa-d-<session>}/addChannel`
      **before** dial, so the agent hears ringback and early media
      (SPIKE-D2); dial-ahead: no bridge yet.
   4. `POST /channels/{id}/dial?timeout=<RingTimeout>` (≥ the profile's
      minimum ring, LLD-04; telephony never cancels earlier on its own).
   5. `StasisStart` with `appArgs=dialer,<attempt>` is routed to the
      dialler path (never to `InboundRouter`, never a new Call: the Call
      exists from step 1).
   6. Progress: `Dial` events and `ChannelStateChange` →
      `DialerEvents.Progress{attempt, kind: dialing|ringing|progress|answer|busy|noanswer|congestion|chanunavail, at}`;
      the **first 180 or 183** sets `first_alert_at` (the minimum-ring
      clock, OB-07) exactly once; answer sets `answered_at` (customer
      `Connected`, usage ticks start).
   7. Dial-ahead answer: the dialler claims an agent and calls
      `DialerMedia.Bridge(attempt, session)` →
      `POST /bridges/{atsa-d-<session>}/addChannel` (target < 300 ms p95,
      SPIKE-D4); optional zip tone `POST /channels/{agent}/play?media=sound:beep`.
   8. Abandon (no agent by the deadline): `DialerMedia.Play(attempt, media)`
      → `POST /channels/{customer}/play`; `ChannelDtmfReceived` →
      `DialerEvents.Dtmf(attempt, digit)`; `PlaybackFinished` →
      `DialerEvents.PlaybackDone(attempt, ok)`; the dialler then calls
      `Hangup`.
   9. **Voicemail drop** (`DialerMedia.Drop(attempt, media)`):
      `POST /bridges/{session}/removeChannel` customer; play `media` on the
      customer channel; agent free at once; `PlaybackFinished` → hang up
      the customer (`left_reason=voicemail_dropped`).
   10. Hang-up: `ChannelHangupRequest` / `ChannelDestroyed` →
       `DialerEvents.Ended{attempt, q850 cause, sip_code?}`; the SIP code
       is read best effort with `HANGUPCAUSE(<chan>,tech)` on
       `ChannelHangupRequest` (**SPIKE-D1**; fallback Q.850 only).
       Capacity released (idempotent, D-58); Call → `Terminated` with
       `call.terminated` v2 (`service_type=campaign`, disposition).
   11. Transfer/3-way of a campaign customer uses §13/§14: the customer
       leg moves from the session bridge into the Call's own main bridge
       `M`, the agent leg stays in the session bridge (LLD-06 §11).
4. **State machine.** Customer Participant: `Invited → Ringing →
   Connected → Disconnected` (§11.2 table; `voicemail_dropped`,
   `no_answer`, `busy`, `rejected`, `unreachable`). Agent leg: `Invited →
   Connected → Disconnected(leg_lost | normal)`. Telephony's per-attempt
   media state (pure `NextAttemptMediaState`):

   | From | Event | To |
   |---|---|---|
   | — | `PlaceAttempt` ok | `Created` |
   | `Created` | `dial` accepted | `Dialing` |
   | `Dialing` | first 180/183 | `Alerting` (`first_alert_at`) |
   | `Dialing`, `Alerting` | answer | `Answered` |
   | `Answered` | `Bridge` / pre-bridged | `Bridged` |
   | `Answered` | `Play` (abandon) | `Messaging` |
   | `Bridged` | `Drop` | `Messaging` |
   | any | hang-up | `Ended` |

   The attempt's business states (QUEUED … DISPOSED) are LLD-06's.
5. **Data model.** `calls.compliance_decision_id`, `calls.dialer_attempt_id`
   (0030). No other table; correlation is the explicit channel and bridge
   ids plus `appArgs`.
6. **Ports.**

   ```go
   type DialerMedia interface { // inbound, implemented by telephony, called by the dialler
       OpenSession(ctx context.Context, s SessionRequest) error                    // idempotent by SessionID
       CloseSession(ctx context.Context, session SessionID) error
       PlaceAttempt(ctx context.Context, a AttemptRequest) (shareddomain.CallID, error) // ErrNoDecision, ErrEmergencyRefused, ErrCapacity, ErrDraining, ErrSessionDown
       Bridge(ctx context.Context, attempt AttemptID, session SessionID) error     // ErrCallGone
       Play(ctx context.Context, attempt AttemptID, mediaURI string) error
       Drop(ctx context.Context, attempt AttemptID, mediaURI string) error
       Hangup(ctx context.Context, attempt AttemptID, reason string) error         // idempotent
   }
   type AttemptRequest struct {
       TenantID shareddomain.TenantID; AttemptID AttemptID; CampaignID CampaignID
       ComplianceDecisionID uuid.UUID          // required; Screening records it and never re-decides
       DestinationE164 string; EndpointURI string // route chosen by pbx-core
       Presentation Presentation               // from pbx-core ChoosePresentation (BR-06)
       Session *SessionID                      // pre-bridge for preview/manual/reserved
       RingTimeout time.Duration
   }
   type DialerEvents interface { // outbound, implemented by the dialler, non-blocking (buffered, as CallEventSink)
       Progress(ctx context.Context, p AttemptProgress) error // includes FirstAlertAt once
       Dtmf(ctx context.Context, attempt AttemptID, digit string) error
       PlaybackDone(ctx context.Context, attempt AttemptID, ok bool) error
       Ended(ctx context.Context, e AttemptEnded) error        // Q850, SIPCode *int, Cause
       AgentLegUp(ctx context.Context, session SessionID) error
       AgentLegLost(ctx context.Context, session SessionID, cause string) error
   }
   ```
   `DialerMedia.LiveAttempts(tenant)` (with `LiveCalls`, §23.1) returns live
   attempts and sessions from `appArgs` for the dialler's reconcile.
7. **API.** None on telephony (dialler RPCs are LLD-06 §7). Error mapping
   to attempt outcomes is LLD-06's (`NOT_PLACED` reasons above).
8. **Configuration.** Agent-leg ring 30 s, keepalive 30 s (installation);
   ring timeout, minimum ring and abandon deadline are LLD-04/06 values
   passed per attempt.
9. **Failure and recovery.**

   | Situation | Behaviour |
   |---|---|
   | `/channels/create` or `/dial` fails | capacity released; Call `Terminated(failed)`; `Ended{cause: engine}`; dialler records `NOT_PLACED engine` |
   | Agent leg lost while a customer is bridged | the customer is hung up `normal` (a party left the conversation, LLD-06 §5.4); `AgentLegLost` |
   | Agent leg lost while a customer rings (not answered) | customer channel hung up (nothing in progress) |
   | Event sink slow | buffered; overflow → `Resync` and the dialler reads `LiveAttempts` |
   | App restart, engine alive | with FL-04 re-adoption: attempts and sessions rebuilt from `appArgs` and `atsa-d-` bridge names; without: §22 sweep ends them and `LiveAttempts` is empty, so LLD-06 reconcile marks attempts `lost` |
   | Engine restart | all legs gone; `Ended{cause: engine_lost}` for each known attempt; sessions `AgentLegLost` |
10. **Invariants and events.** INV-09 (a call is never placed without a
    permitted decision id: a NOT NULL check in `PlaceAttempt` and the
    LLD-06 CHECK), INV-01 (no dialler call to an emergency number; a
    customer who dials emergency is unaffected), BR-06 (presentation only
    from `pbx-core`), INV-03 (pause/stop never hang up ringing or bridged
    calls: telephony only hangs up on `Hangup`/`Drop` or a party leaving),
    INV-10. Events: `call.initiated`, `participant.joined/left`,
    `call.terminated` v2 with `service_type=campaign`; attempt events are
    LLD-06's.
11. **Tests.**

    | Given | When | Then | Tier |
    |---|---|---|---|
    | attempt without a decision id | `PlaceAttempt` | `ErrNoDecision`; no channel, no Call | U |
    | permitted attempt | `PlaceAttempt` | Call created with `compliance_decision_id`; `ComplianceEngine` not called (spy) | U, I |
    | destination is the tenant's emergency number | `PlaceAttempt` | `ErrEmergencyRefused` | U |
    | capacity exhausted | `PlaceAttempt` | `ErrCapacity`; nothing dialled | U, I |
    | preview attempt pre-bridged | dial | agent hears ringback/early media | E (SPIKE-D2) |
    | customer 180 then 183 | — | `first_alert_at` = the 180 time, set once | U, E |
    | dial-ahead answer | `Bridge` | answer→bridge < 300 ms p95 | E (SPIKE-D4) |
    | abandon | `Play`, DTMF 9 | `Dtmf(9)`, then `PlaybackDone`; hang-up after | E |
    | voicemail drop | `Drop` | customer removed, message played, agent free at once | E |
    | carrier 486 | — | `Ended{q850 17, sip_code 486}` (or Q.850 only) | E (SPIKE-D1) |
    | agent leg killed | — | `AgentLegLost`; bridged customer hung up | E |
    | `StasisStart` with `appArgs=dialer,…` | — | never reaches `InboundRouter` | U |
12. **Industry parity.** VICIdial dials through `Originate` to a
    campaign context and parks agents in a MeetMe/ConfBridge "nailed-up"
    conference; Five9 and Amazon Connect hold a persistent agent
    connection the same way. **Difference:** no dialplan (D-47); one Call
    per attempt with the compliance decision id on the record, so every
    dialled call proves which gate decision allowed it.

## 24. Spikes, owner decisions and OpenSpec handoff

| Spike | Question (Asterisk 22.11.0) | Fallback |
|---|---|---|
| SPIKE-CC-1 | `transfer_handling=ari-only`, `ChannelTransfer`, `POST /channels/{id}/transfer_progress` | Native REFER + reconcile from `BridgeBlindTransfer`/`BridgeAttendedTransfer` (§15) |
| SPIKE-CC-2 | A phone's hold in an ARI mixing bridge plays MOH to the peer (2-party native, 3-party softmix); `ChannelHold`/`ChannelUnhold` delivered | Platform hold of the peer on `ChannelHold` (§12) |
| SPIKE-CC-3 | Snoop `whisper` direction isolates the agent; listen inaudible (AC-16.1; HLD-21 §10 item 3) | Opposite direction; else whisper disabled (§20) |
| SPIKE-CC-4 | `DELETE /channels/{id}?reason_code=26` gives "answered elsewhere" on phones | `reason=normal` (phone may show a missed call) |
| SPIKE-P1 (LLD-03) | Pattern hint `Stasis:atsa-${EXTEN}` for PJSIP BLF subscriptions | Console presence only (§21) |

| Owner decision | Recommended default |
|---|---|
| OD-CC-1 consult legs and ad-hoc parties beyond two reserve capacity | Yes, one unit each, released when that Participant leaves |
| OD-CC-2 forced maximum hold time | Off (reminder tone every 120 s) |
| OD-CC-3 unanswered blind transfer | Destination's own no-answer rule; recall-transferor after 15 s selectable per tenant |
| OD-CC-4 in-call DTMF transfer/park codes (`*2`, `##`) | Off in R1.0 (phones use their keys, browsers the console) |

**Handoff.** `telephony-call-control` is delivered as separate OpenSpec
changes, in order: (1) model, migrations 0030–0031, end rule, `CallEventSink`,
`call.terminated` v2, `participant.*` payloads, `MaintenanceGate`, and
hold/resume after SPIKE-CC-2; (2) plan/program runner and `Continue`,
blind and attended transfer; (3) REFER after SPIKE-CC-1; (4) park and
pickup with `pbx-pickup-park`; (5) conference (0032) with
`pbx-conference-rooms`; (6) monitor primitives (0033) after SPIKE-CC-3;
(7) device state with `pbx-presence`; (8) queue media (§23.1) with
`pbx-queues`, after SPIKE-Q2/Q3; (9) dialer media (§23.3) with LLD-06's
`dialer-agent-session`, after SPIKE-D1/D2/D4. Recording control and `MediaTap`
land with LLD-09's and LLD-10's changes. Each `pbx-core` counterpart
lands in the same change or before it.
