<!-- OpenSpec: TRD-HLD-10 -->
# Failure Model, Resilience & Self-Healing

Every failure mode in AtsaPBX has a deterministic, defined behavior (PRD §13). "Undefined behavior" is strictly prohibited.

**Designed vs built (revised 2026-11-07, D-98 audit).** This document
states the target behaviour. Each row names the LLD that owns the
detail, and the **Status** column says whether it is built, designed to
the D-98 Definition of Ready, or still only a target. Where this file and
an LLD differ, the LLD is the source and this file is corrected.

---

## 1. Failure Modes & Effects Analysis (FMEA)

| Failure Scenario | Detection Mechanism | Immediate Call Impact | User & Carrier Visible State | Automated Recovery / Fallback | Owner · Status |
|---|---|---|---|---|---|
| **F-01 Carrier Route Failure** | SIP 5xx, timeout, or SIP OPTIONS failure | **Calls in progress continue**; cannot be migrated (Known limitation) | New calls route to the next carrier by priority | Route marked unhealthy and removed from selection; auto-return after sustained healthy probes [PRD 13.4, EPIC-02] | D-80 · Built |
| **F-02 Capacity Exhaustion** | Atomic counter reaches licensed limit | **Zero impact on live calls (INV-03)**; active calls run to completion | New inbound setup receives SIP `503` (never `486`, D-11); `Retry-After` is not sent (Asterisk limitation, HLD-20 FL-01); outbound agent sees a clear limit message | Burst allowance absorbs short peaks. Emergency calls ALWAYS connect (INV-01) [PRD 13.2] | LLD-08 · Built |
| **F-03 AI Provider Interruption** | Write stall or no provider keepalive for 3 s, connection reset, EOF | **Zero impact on call audio (INV-04)**; the tap is a snoop copy | Stream state `Reconnecting`, then `Failed`; admin alerted; (R2 assist shows the agent a reconnecting badge) | Reopen attempts within 5 s; resume with the gap recorded, else `Failed` and the tap is torn down; per-provider breaker; tenant suspension after repeated failures [PRD 13.1] | LLD-10 AI-06 · Designed |
| **F-04 Webhook Delivery Failure** | Non-2xx, timeout, connect/TLS error, SSRF refusal | **Zero impact on calls (INV-06)**; delivery is asynchronous | `Unhealthy` after 3 consecutive failures; `Disabled` after 24 h of continuous failure; later events `held` | Retries at +1 m, +5 m, +15 m, +1 h, +6 h, +24 h from the first attempt (±10 % jitter); then `dead`; manual replay from console and API; automatic recovery on first success [PRD 13.3] | LLD-05 §4 · Designed |
| **F-05 Entitlement Unreachable** | Daily HTTP check fails | **Zero impact during 7-day grace period**; full operation | Admin sees persistent warning banner from day 2 | On day 8+, degrades to reduced capacity; NEVER disables phone system or emergency calling [PRD 11.2, D-14] | LLD-08 · Built |
| **F-06 Asterisk Node Crash** | ARI WebSocket connection dropped; `/readyz` ARI/AMI checks fail | Active calls on that node end (accepted limitation) | Callers hear disconnect; console call state goes stale until reconnect | Container supervisor restarts Asterisk; the app's ARI/AMI reconnect loops (with backoff) resume new-call handling once Asterisk is ready. **No recovery-time target is designed or measured**; reconciliation of calls that ended during the gap is LLD-01 §9 follow-up work | LLD-01 · Partial (reconnect built; reconcile not) |
| **F-07 Core API Crash** | Process exit / OOM signal | Bridges in Asterisk keep media flowing, **but no controller exists for them** and new inbound calls are not answered | Control plane unavailable until restart | Target: §4 reconciliation. **Today:** the in-memory correlation registry is lost and a crash ends in-flight calls (accepted limitation, LLD-01 §9); recordings in progress are uploaded on restart (LLD-09 §4.8); AI tap channels are swept (LLD-10 §3.10) | LLD-01 · Target only |
| **F-08 PostgreSQL Unavailable** | Connection pool checkout error | Calls currently bridged continue; new calls fail closed | Admin sees storage-degraded state | Writes retry with exponential backoff; quality windows are accumulated in memory per participant and emitted when the outbox is writable (bounded to the participant's lifetime); nothing is fabricated for the gap | LLD-01, LLD-09 §8 · Partial |
| **F-09 NATS JetStream Down / restarted** | JetStream publish timeout; consumer disconnect | **Zero impact on telephony media or call control** | Event delivery delayed; live streams send heartbeats then `Unavailable`; webhooks wait | Outbox retains unpublished rows and drains on reconnect. Because durable consumers (CDR ledger, webhooks) depend on JetStream holding published messages, **production runs JetStream file storage on a volume** (72 h `MaxAge`); after a state loss the operator re-publishes retained outbox rows (`atsapbx outbox republish --since`), and consumers dedupe (D-58) | D-57, LLD-05, LLD-09 §3 · Partial (publisher built; consumers designed) |
| **F-10 Object Store Down** | `PutObject`/`Stat` errors | Recording continues; the file stays in the Asterisk recording spool | Admin alerted after 15 min; recordings show "upload pending" | Uploader retries every 5 min (backoff to 1 h); below `recording.min_free_disk` new recordings are not started and the policy's `on_record_failure` applies [PRD 13.5] | LLD-09 §4.8 · Designed |
| **F-11 Recording Fails to Start / Consent Fails** | `RecordingFailed`, ARI error, `PlaybackFinished{failed}` | Call continues (default); never recorded without a required announcement (BR-03) | Record marked not recorded with the reason; admin alerted | Policy `on_record_failure` / `on_consent_failure` = `decline_call` is an explicit tenant choice, applied only before the answering party joins (INV-03) [PRD 13.5] | LLD-09 §4 · Designed |
| **F-12 CDR Missing or Chain Mismatch** | `call.initiated` without a ledger row after 24 h; `VerifyCDRChain` mismatch | None | Admin alert naming call ids / first bad sequence | Republish from the outbox; a mismatch is evidence and is never "repaired" | LLD-09 §3 · Designed |


---

## 2. Telephony Response Codes & Carrier Signalling (BR-05, D-11)

AtsaPBX strictly implements semantic correctness in SIP response signaling:

```text
               Carrier SIP Trunk
                       │
                       ▼ SIP INVITE
            [AtsaPBX Screening Engine]
                       │
       ┌───────────────┼───────────────┐
       │               │               │
  [Capacity Full]  [Fraud Block]   [Normal Busy]
       │               │               │
       ▼               ▼               ▼
    SIP 503         SIP 603         SIP 486
 Service Unavailable    Decline       Busy Here
 (Retry-After: 30)
```

1. **Capacity Limit Exhaustion (`SIP 503 Service Unavailable`):**
   - Informs the carrier that AtsaPBX local resources are temporarily exhausted.
   - Includes `Retry-After: 30` header, enabling intelligent carrier session border controllers (SBCs) to reroute the call to alternate partner data centers.
   - **Never use `486 Busy Here` for capacity limits (D-11):** `486` falsely reports that the called party is on the phone, distorting Answer-Seizure Ratios (ASR) and obscuring capacity alerts.
2. **Fraud / Administrative Suspension (`SIP 603 Decline`):**
   - Informs carrier that the destination or tenant is globally forbidden.
3. **Genuine Destination Busy (`SIP 486 Busy Here`):**
   - Used exclusively when the destination extension or agent is genuinely busy on another active call.

---

## 3. Circuit Breakers & Degradation Cascades

### 3.1 Webhook Circuit Breaker (PRD §13.3; detail in LLD-05 §4.4)

```text
[Active] ──(3 consecutive failed attempts)──> [Unhealthy] ──(24 h continuous failure,
   ▲                                              │          or a delivery goes dead)──> [Disabled]
   └──────────(first successful attempt)──────────┘                                         │
   ▲                                                                                        │
   └─────────────────────(EnableWebhook: held events delivered oldest first)────────────────┘
   (HTTP 410 Gone or DisableWebhook → Disabled from any state)
```

- In `Unhealthy` the retry schedule is unchanged (it is already slow):
  +1 m, +5 m, +15 m, +1 h, +6 h, +24 h from the first attempt.
- Delivery is ordered per destination by default, so the head event
  blocks later ones; the head's +24 h failure is the 24 h of continuous
  failure that disables the destination.
- In `Disabled`, no automatic attempts are made; new events are `held`
  for the tenant's retention window (default 30 days) and replayable.

### 3.2 AI Provider Degradation Cascade (INV-04, PRD §13.1; detail in LLD-10 §3.4–§3.5, §5)

If an external AI provider is slow or drops the connection:
1. The per-stream ring buffer (12 frames, 240 ms) **drops the oldest
   frame** when full; the relay that reads from Asterisk never blocks.
2. After 3 s without a successful write or provider keepalive, the
   stream moves to `Reconnecting`.
3. Reopen attempts at 0, 0.5, 1 and 2 s; success within 5 s resumes the
   stream and records the gap; otherwise `Failed`, and only the tap
   channels are deleted.
4. Per (tenant, provider) breaker: open after 5 failures in 60 s,
   half-open after 30 s. 20 failures in 15 min suspend AI for the tenant
   until an admin resumes it.
5. **The underlying call continues with zero audio interruption.** In
   R1.0 no AI feature is in the caller's path; the R2 consumers (agent
   assist, AI menus reverting to DTMF, incomplete transcripts) build on
   these states.

---

## 4. State Reconciliation on Core Restart (PRD §2, T-5)

> **Status: target design, not built.** Channel re-adoption and
> reconciliation are owned by `telephony-core` (LLD-01 §9 follow-up, and
> HLD-20 FL-04). Until that lands, an app crash ends in-flight calls (the
> accepted limitation) and the steps below must not be tested as current
> behaviour. Pieces that *are* designed for restart: recording upload of
> finished files (LLD-09 §4.8), the AI tap sweep (LLD-10 §3.10) and the
> outbox drain (built).

When the AtsaPBX Go monolithic process restarts (e.g. following an upgrade or host reboot), it recovers running state without dropping calls:

```text
[AtsaPBX Core Boot]
        │
        ▼
[1. Query Asterisk ARI: GET /channels and GET /bridges]
  - Collects all running Asterisk channels and mixing bridges
        │
        ▼
[2. Read Local Correlation Tokens from Channel Variables]
  - Extracts Atsa-Call-ID and Atsa-Participant-ID from channel vars
        │
        ▼
[3. Reconcile with PostgreSQL calls and participants]
  - If call state in DB is 'Active' and channel exists in bridge -> Re-attach ARI event listener
  - If channel destroyed during downtime -> Execute graceful HangupCall cleanup in DB
  - If unknown channel found without AtsaPBX token -> Quarantine channel and log security alert
        │
        ▼
[4. Drain Transactional Outbox]
  - Resume polling unpublished events and publish to NATS JetStream
```

---

## 5. Verification & Chaos Testing Plan

1. **Carrier Failover Chaos Test:** Cuts network interface to primary carrier during an active call run; verifies new call setups reroute to secondary carrier in <5s with zero dropped calls on secondary (AC-02.2).
2. **AI Hard-Kill Test:** Sends `SIGKILL` to the fake/local AI provider during an active conversation; verifies call audio continues with no jitter change, the stream reaches `Failed` within 5 s and an alert fires (AC-04.3; LLD-10 E-02).
3. **Capacity Rejection Signal Test:** Drives concurrent load to 100% capacity; asserts incoming SIP INVITE receives `503 Service Unavailable` with `Retry-After: 30` header and distinctly tagged telemetry record (BR-05).
4. **Outbox Recovery Soak Test:** Simulates 1-hour database outage while Asterisk maintains active calls; verifies that when DB restores, all accrued usage seconds and outbox events are safely committed.
5. **Webhook Outage Test:** Stops a receiver for 10 minutes and then 25 hours (fake clock); verifies `Unhealthy` after 3 failures, `Disabled` at 24 h, held events delivered in order after Enable (LLD-05 W-08, W-16).
6. **Object-Store Outage Test:** Stops MinIO during recorded calls; verifies calls are unaffected and files upload after restart (LLD-09 R-09).
7. **Backing-Service Kill Test:** Stops Postgres, NATS, Asterisk, and the app one at a time (then all at once); verifies each row of the §6 table — `/readyz` flips to 503 naming the failed check, compose restarts in dependency order, and no event is lost that the outbox had committed.

## 6. Backing-Service Health, Liveness/Readiness & Dependency Map

How the four dev-compose services depend on each other, what each
failure costs, and which probe catches it. Compose implements this with
per-service `healthcheck`s, `depends_on` conditions, and
`restart: unless-stopped` (`deploy/docker-compose.yml`); the app
implements it with liveness vs readiness split (`internal/server`).

```text
                        ┌────────────┐
                        │  Postgres  │◄──────────────────┐
                        │ 16 + RLS   │                   │ RLS reads/writes (app),
                        └────────────┘                   │ outbox worker (BYPASSRLS)
                             ▲                           │
              CDR/CEL writes │                           │ domain writes + outbox
              (Asterisk)     │                           │ rows, same transaction
                             │                           │
┌──────┐   ARI/AMI control ┌──────────┐   NATS publish ┌──────┐
│ app  │◄────────────────►│ Asterisk │───────────────►│ NATS │
│      │  reconnect loops │ 22.x LTS │  fire-and-     │ Jet- │
└──────┘  on both sockets └──────────┘  forget        │Stream│
                                                     └──────┘
Durability lives in Postgres (volume `postgres-data`), not in NATS:
NATS is intentionally ephemeral (no volume) — a NATS restart only
delays delivery, it never loses events, because the outbox in Postgres
is the durable store and the worker republishes from it (INV-06).
```

**Liveness vs readiness.** `/healthz` answers 200 whenever the process
serves HTTP — it proves the process is alive, nothing more. `/readyz`
aggregates one `Checker` per backing dependency (Postgres ping, NATS
connection, ARI reachable, AMI logged in) and answers 200 only when all
pass, with a per-check `ok`/`error` body that never leaks secrets
(D-39). With zero checkers registered it answers 503 `unconfigured`
rather than a misleading 200. Compose and any orchestrator must probe
`/readyz` for traffic decisions; `/healthz` is for process liveness
only. (The app container's own `healthcheck` stanza needs a binary
probe flag since the image is distroless — it lands with the `main.go`
wiring in change `telephony-core-originate-bridge-hangup` task 9.2, which
also registers the four checkers.)

**One service down.**

| Down | Calls in progress | New calls | Events / usage | Caught by |
|---|---|---|---|---|
| Postgres | Continue on Asterisk bridges; usage ticks + outbox rows accrue and commit on restore (§5 item 4) | Rejected — no durable record can be created; never half-created (transaction atomicity) | Buffered, then published on recovery | `pg_isready` healthcheck; app `/readyz` postgres check; `atsapbx_outbox_queue_age_seconds` pages |
| NATS | Fully unaffected (INV-06) | Fully unaffected | Wait in `outbox`; worker retries on recovery | NATS `/varz` healthcheck; outbox age metric |
| Asterisk | End on that node (accepted limitation) | Fail — nothing to originate or answer on; app reconnect loops with backoff | Transitions stop; outbox holds what was committed | `asterisk -rx` healthcheck; app `/readyz` ARI/AMI checks |
| App | Asterisk keeps running but new inbound calls reaching Stasis have no controller — no answer, no media until the app returns; alert immediately | Cannot be set up or controlled | Nothing new published; committed outbox rows wait | `/healthz` liveness + (from 9.2) `/readyz`; container restart policy |

**All down / host reboot.** Compose restarts in dependency order
(Postgres and NATS to healthy, then Asterisk, then app). The only
durable dev state is the Postgres volume; NATS replays nothing (by
design — see above); the outbox worker resumes publishing on boot.

**Restart recovery note.** §4 above describes the target
restart-reconciliation behavior (re-query ARI, re-derive correlation
from channel vars, re-attach or clean up). It depends on channel
re-adoption, which is tracked follow-up work (LLD-01 §9) — until that
lands, a process crash ends in-flight calls per the accepted platform
limitation, and §4 reads as the design to build toward, not the behavior
to test today.


