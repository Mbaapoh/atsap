<!-- OpenSpec: TRD-HLD-10 -->
# Failure Model, Resilience & Self-Healing

Every failure mode in AtsaPBX has a deterministic, defined behavior (PRD §13). "Undefined behavior" is strictly prohibited.

---

## 1. Failure Modes & Effects Analysis (FMEA)

| Failure Scenario | Detection Mechanism | Immediate Call Impact | User & Carrier Visible State | Automated Recovery / Fallback |
|---|---|---|---|---|
| **F-01 Carrier Route Failure** | SIP 5xx, timeout, or SIP OPTIONS failure | **Calls in progress continue**; cannot be migrated (Known limitation) | New calls route to secondary carrier in <5s; caller notices nothing | Route marked `DEGRADED`. Auto-return to service after 5m of healthy probes [PRD 13.4, EPIC-02] |
| **F-02 Capacity Exhaustion** | Atomic counter reaches licensed limit | **Zero impact on live calls (INV-03)**; active calls run to completion | New inbound setup receives SIP `503 Service Unavailable` with `Retry-After: 30` (D-11); outbound agent sees clear limit alert | 10% burst allowance absorbs short peaks. Emergency calls ALWAYS connect (INV-01) [PRD 13.2] |
| **F-03 AI Provider Interruption** | gRPC timeout, connection reset, or EOF | **Zero impact on call audio (INV-04)**; call remains 100% active | Agent sees "Reconnecting" badge; IVR menus revert to DTMF keypad | Worker attempts reconnection for 5s; transitions to `Fallback` state; logs partial transcript marked `incomplete` [PRD 13.1] |
| **F-04 Webhook Delivery Failure** | Non-2xx response or TCP timeout | **Zero impact on calls (INV-06)**; delivery is asynchronous | Admin sees `Retrying` indicator; destination marked `Unhealthy` after 3 fails | Exponential retry (1m, 5m, 15m, 1h, 6h, 24h). Dead-letter queue (DLQ) after 24h with manual API replay [PRD 13.3] |
| **F-05 Entitlement Unreachable** | Daily HTTP check fails | **Zero impact during 7-day grace period**; full operation | Admin sees persistent warning banner from day 2 | On day 8+, degrades to reduced capacity; NEVER disables phone system or emergency calling [PRD 11.2, D-14] |
| **F-06 Asterisk Node Crash** | ARI WebSocket connection dropped | Active calls on crashed media node end (Accepted limitation) | Caller hears disconnect; agent UI updates to `Disconnected` within 3s | Container supervisor restarts Asterisk; Go Stasis app reconnects in <2s; node resumes taking new calls |
| **F-07 Core API Crash** | Process exit / OOM signal | **Active audio bridges in Asterisk continue unaffected** | Control plane paused for 5-10s; live stats show "Stale" indicator | Core restarts, queries ARI for running channels, reconciles with PostgreSQL `calls`, resumes state tracking |
| **F-08 PostgreSQL Unavailable** | Connection pool checkout error | Calls currently bridged continue; new calls fail closed | Agent sees "System Storage Degraded" message | In-memory buffer holds RTCP quality ticks for up to 60s; writes retry with exponential backoff |
| **F-09 NATS JetStream Down** | JetStream publish timeout | **Zero impact on telephony media or call control** | Event delivery delayed; live dashboards show stale data | Transactional outbox retains unpublished rows; drains automatically upon NATS reconnection |
| **F-10 S3 Storage Down** | S3 PutObject returns 5xx | Recording continues; audio streams to local spool disk | Admin alerted to storage failure | Encrypted audio chunks written to `/var/spool/atsapbx/`; background worker uploads to S3 when online [PRD 13.5] |

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

### 3.1 Webhook Circuit Breaker (PRD §13.3)

```text
[Healthy / Active] ──(3 consecutive HTTP failures)──> [Unhealthy]
       ▲                                                    │
       │                                                    │ (24 hours sustained failure)
       │ (Single successful HTTP delivery)                  ▼
       └─────────────────────────────────────────────── [Disabled / DLQ]
                                                            (Manual API Replay Required)
```

- While in `Unhealthy`, delivery attempts back off to a slow schedule (1 hour interval).
- While in `Disabled`, automatic delivery halts; new events queue in dead-letter storage for up to the tenant's retention window (default 30 days).

### 3.2 AI Provider Degradation Cascade (INV-04, PRD §13.1)

If an external AI provider experiences high latency or drops connections:
1. Frame buffers in `ai-pipeline` discard un-transmitted audio chunks (tail-drop), keeping buffer size bounded at 250 ms.
2. If no response is received for 3 seconds, UI switches to visible `Reconnecting` state (never displaying frozen or stale predictions).
3. If disconnected for 5 seconds, session moves to `Fallback`:
   - Live transcription terminates cleanly.
   - Any active IVR menu using AI voice recognition reverts immediately to DTMF tone keypad detection.
   - Partial transcripts up to the failure point are marked `status='INCOMPLETE'` and preserved.
   - **The underlying call continues with zero audio interruption.**

---

## 4. State Reconciliation on Core Restart (PRD §2, T-5)

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
2. **AI Hard-Kill Test:** Sends `SIGKILL` to local AI Whisper/Ollama container during an active conversation; verifies primary WebRTC call audio continues with zero jitter spike and agent receives fallback notice within 5s (AC-04.3).
3. **Capacity Rejection Signal Test:** Drives concurrent load to 100% capacity; asserts incoming SIP INVITE receives `503 Service Unavailable` with `Retry-After: 30` header and distinctly tagged telemetry record (BR-05).
4. **Outbox Recovery Soak Test:** Simulates 1-hour database outage while Asterisk maintains active calls; verifies that when DB restores, all accrued usage seconds and outbox events are safely committed.


