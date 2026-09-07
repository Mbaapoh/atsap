## Why

`telephony-core` is Tier 0 in the bounded-context dependency graph
(`docs/TRD.md` "Dependency graph"; `docs/hld/04-bounded-contexts.md` §10):
every other context (`pbx-core`, `dialer`, `reporting`, ...) either calls
into it or reacts to its events, and none of them can be honestly built
until it exists. Per `docs/DECISIONS.md` D-25/D-26, the media path must be
proven with a real call before anything is built on assumptions about it.
`openspec/specs/` is currently empty — this is the first capability
proposed in the project, and it is the walking-skeleton slice of
`docs/lld/LLD-01-telephony-core-walking-skeleton.md`: one two-party call,
originate → bridge → hangup, through the real Asterisk ACL.

## What Changes

- New `Call` / `CallParticipant` aggregates and their state machine
  (`Initiated → Screening → Routing → Presenting → Active → Terminating →
  Terminated`; `Degraded` exists as an enum value but nothing transitions
  into it in this change — that needs RTCP-XR sampling, added later).
- New Asterisk Anti-Corruption Layer under `api/internal/telephony/acl`:
  relocates the existing `api/internal/ari` client there and extends
  `Originate` with explicit `channelId`/`variables` injection (D-19
  correlation), plus adds `CreateBridge`/`AddChannelToBridge`/
  `DestroyBridge`/channel-variable get/set. Relocates `api/internal/ami`
  unchanged (not used by this change).
- New Postgres schema subset (`tenants` minimal, `calls`,
  `call_participants`, `channel_history`, `usage_seconds`, `outbox`), all
  with Row-Level Security, per `docs/hld/03-domain-model.md` §5.
- New transactional outbox → NATS JetStream publisher for
  `event.call.initiated|active|terminated` and
  `event.participant.joined|left`.
- New stub `LicenseManager` and `ComplianceEngine` adapters (always-permit)
  wired into the `Screening` state. **Not** real licensing/compliance
  logic — that is a separate later change against the same ports, per
  `docs/lld/README.md`'s granularity model.
- New ConnectRPC `GetCall` method — the first public API surface, proving
  no Asterisk channel ID ever crosses the API boundary.
- Two statically-configured PJSIP endpoints added to `core/conf/pjsip.conf`
  as dev-test fixtures only — not the real `pbx-core` Extension feature.

**Explicitly out of scope for this change** (each is its own later change
against the `telephony-core/call-lifecycle` capability, or a different
capability entirely — see `docs/lld/LLD-01...md` §1 for the full table):
hold/resume, attended transfer, IVR, real licensing/compliance, recording,
WebRTC transport, webhooks, AI pipeline, dialer.

## Capabilities

### New Capabilities
- `telephony-core/call-lifecycle`: a two-party call's full lifecycle
  (originate, screen, route, present, bridge to active, hang up, tear
  down), per-second usage ticking from the moment a participant connects,
  and the domain events emitted at each transition. This is the first
  capability in the project; `telephony-core/<capability>` is the path
  convention this proposal establishes for the bounded context (matching
  `docs/hld/04-bounded-contexts.md`'s context names), for later changes
  (hold, transfer, Screening-with-real-licensing) to extend as deltas
  against this same spec.

### Modified Capabilities
(none — first capability in the repo)

## Impact

- **New code:** `api/internal/shared/{domain,event}`,
  `api/internal/telephony/{domain,ports,application,acl}`,
  `api/internal/postgres`, `api/internal/nats`,
  `api/proto/atsapbx/v1/telephony.proto`.
- **Relocated code:** `api/internal/ari` → `api/internal/telephony/acl/ari`;
  `api/internal/ami` → `api/internal/telephony/acl/ami` (import paths
  change; existing tests move with the packages).
- **New migrations:** `api/migrations/0001_telephony_core.{up,down}.sql`,
  `0002_dev_tenant_seed.{up,down}.sql` (dev-gated, deleted once LLD-02
  lands real tenant provisioning).
- **Dependencies activated (already vendored as indirect in `api/go.mod`,
  per `docs/TOOLSET.md`):** `pgx/v5`, `nats.go`, `connectrpc.com/connect`,
  `google.golang.org/protobuf`, `google/uuid`.
- **Config:** `core/conf/pjsip.conf` gains two temporary dev-test
  endpoints.
- **Breaking changes:** none — nothing public exists yet.
