<!-- OpenSpec: TRD-HLD-16 -->
# Call Origination Sequence

Outbound call origination end to end: a ConnectRPC request becomes a
screened, routed, bridged call with durable domain events. This diagram
covers the **originate path only** — inbound IVR flow lives in
[02-system-context.md §4.1](02-system-context.md#41-inbound-call-via-visual-ivr-to-webrtc-agent),
carrier-failover behavior in [§4.2](02-system-context.md#42-carrier-failover-during-outbound-dialling).

## 1. Architectural principles

- **Asterisk is a black box behind ARI.** Every interaction crosses the
  ARI REST / WebSocket boundary (originate with our `channelId`, answer,
  bridge, hangup; `StasisStart` / `ChannelStateChange` / `ChannelDestroyed`
  events in). No SIP, RTP, dialplan, or bridge internals appear below.
- **Ubiquitous language throughout** (D-17/D-18/D-32): `Call` aggregate of
  N `CallParticipant`s; Asterisk channel IDs live only in
  `channel_history` rows inside the ACL, never in APIs or events.
- **Transactional outbox** (HLD [01](01-architecture.md) §3.2): domain
  writes and outbox rows commit in one PostgreSQL transaction
  (`pgx/v5`); a worker publishes with `FOR UPDATE SKIP LOCKED` to
  tenant-partitioned NATS subjects.
- **Lifecycle states are PRD §11.1's** (`Initiated → Screening → Routing →
  Presenting → Active → Terminating → Terminated`); this diagram moves a
  call through them, it does not redefine them (state authority stays in
  [03](03-domain-model.md) §2).

## 2. Sequence diagram

```mermaid
sequenceDiagram
    autonumber
    participant Client as Client (portal / partner integration)
    participant ConnectRPC as ConnectRPC ingress (in-monolith)
    participant CallSvc as telephony-core orchestrator
    participant Postgres as PostgreSQL 16 (pgx)
    participant ARI as Asterisk ARI (black box)
    participant OutboxWorker as Outbox worker (in-monolith)
    participant NATS as NATS JetStream

    Client->>ConnectRPC: InitiateCall + Idempotency-Key + JWT
    ConnectRPC->>ConnectRPC: Validate JWT, inject tenant context
    ConnectRPC->>CallSvc: InitiateCall(cmd)
    CallSvc->>Postgres: BEGIN; INSERT calls state=Initiated
    CallSvc->>CallSvc: Screening — LicenseManager + ComplianceEngine ports
    alt Screening rejects (capacity / compliance / suspension)
        CallSvc->>Postgres: UPDATE calls state=Terminated + reason; INSERT outbox; COMMIT
        CallSvc-->>Client: RPC error (RESOURCE_EXHAUSTED / PERMISSION_DENIED)
    else Screening passes
        CallSvc->>CallSvc: Routing — resolve callee endpoint
        CallSvc->>ARI: POST /channels (endpoint, channelId=atsa-part-ids, ATSA_PARTICIPANT_ID var)
        alt ARI allocation fails
            CallSvc->>Postgres: ROLLBACK; slog error fields per D-39
            CallSvc-->>Client: RPC error (INTERNAL)
        else ARI allocation succeeds
            CallSvc->>Postgres: INSERT participants + channel_history + outbox (event.call.initiated); UPDATE calls state=Routing; COMMIT
            CallSvc->>CallSvc: Presenting — callee alerted (20s default timeout)
            ARI-->>CallSvc: WS StasisStart (carries ATSA_PARTICIPANT_ID)
            ARI-->>CallSvc: WS ChannelStateChange Up (answered)
            CallSvc->>ARI: POST /bridges + addChannel (both legs)
            CallSvc->>Postgres: BEGIN; UPDATE calls state=Active + answered_at; INSERT outbox (event.call.active); COMMIT
            CallSvc-->>Client: InitiateCall returns call_id (participants observable via GetCall)
            par Media plane
                ARI-->>CallSvc: WS events drive participant Connected / Disconnected
            and Event publishing
                OutboxWorker->>Postgres: SELECT outbox FOR UPDATE SKIP LOCKED
                OutboxWorker->>NATS: publish tenant.tenant-id.event.call.*
                OutboxWorker->>Postgres: UPDATE outbox SET published_at
            end
        end
    end
```

## 3. Step breakdown

1. **Ingress & tenant context** (HLD [01](01-architecture.md) §4): JWT
   validated at the ConnectRPC layer; `tenant_id` injected into
   `context.Context` and enforced by RLS (`SET LOCAL app.tenant_id`) on
   every query. The `Idempotency-Key` header makes retries safe — a replay
   returns the original result, never a second call.
2. **Screening before contact** (PRD §11.1): capacity, suspension, spend,
   and compliance verdicts are collected with zero external contact made.
   Emergency numbers never reach this path — they bypass Screening
   entirely (INV-01).
3. **Atomic origination:** the call row, participant rows,
   `channel_history` rows, and the `event.call.initiated` outbox row
   commit together. ARI is called with our caller-supplied `channelId`
   plus `ATSA_PARTICIPANT_ID` (D-19), so every later event is
   self-describing to the ACL.
4. **Black-box bridging:** answer and bridge operations go to ARI; media
   never touches Go. Participant `Connected` and Call `Active` (2+
   connected participants) are derived from WS events, and each
   transition carries its outbox row in the same transaction.
5. **Downstream delivery:** the outbox worker publishes per-tenant
   (`tenant.<tenant_id>.event.call.<action>`); a NATS outage only delays
   delivery — calls are never blocked (INV-06).

## 4. Timeouts (documented values)

| Operation | Limit | Source |
|---|---|---|
| Screening pass | Imperceptible, <50 ms normal operation | HLD [03](03-domain-model.md) §2.1 |
| Presenting alerting timeout | 20 s default, then roll-over or terminate | HLD [03](03-domain-model.md) §2.1 |
| Carrier failover for new calls | <5 s | PRD AC-02.2 |
| Usage tick cadence | 1 s per connected participant | D-24 seam 4 |
| Webhook/event redelivery | 1m, 5m, 15m, 1h, 6h, 24h schedule | PRD §13.3 |

## 5. Error & retry matrix

| Failure | Visible result | System action | Client retries? |
|---|---|---|---|
| Capacity exhausted | SIP `503` + `Retry-After: 30` inbound; RPC `RESOURCE_EXHAUSTED` on API originate | Reject with distinct telemetry reason; active calls untouched (INV-03) | After capacity frees |
| Screening rejects (suspension/compliance) | Call `Terminated` + reason | No external contact made; reason on the record | No — fix the cause |
| ARI unreachable at originate | RPC `INTERNAL` | Transaction rolled back; no partial call row; slog with D-39 fields | With backoff |
| ARI event-stream gap mid-call | None to the caller | Reconnect loop reattaches; events during the gap are not replayed (documented gap, LLD-01 §9) | N/A |
| NATS down | None to the caller | Events wait in `outbox`; worker retries (INV-06) | N/A — handled via outbox |
| Duplicate originate (retry) | Original `call_id` returned | `Idempotency-Key` dedupes; no second call | Safe to retry |

## 6. Traceability

- Requirements: PRD §11.1 (lifecycle), EPIC-03 (calling), EPIC-06
  (capacity), INV-01/INV-03/INV-06.
- Decisions: D-19 (caller-supplied channel ID), D-24 (API-first + seams),
  D-33 (ConnectRPC), D-39 (log fields), D-40 (Go standards).
- Related: [01](01-architecture.md) §§2–4, [02](02-system-context.md) §4,
  [03](03-domain-model.md) §§2/5, [04](04-bounded-contexts.md) §§1/10,
  [LLD-01](../lld/LLD-01-telephony-core-walking-skeleton.md).
