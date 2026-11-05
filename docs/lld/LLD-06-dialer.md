# LLD-06 — Dialer (power dial R1.0; predictive R2)

| | |
|---|---|
| **Bounded context** | `dialer` (Tier 2, HLD 04 §10.1) |
| **Status** | **Draft — design only.** R1.0 is preview and power; predictive is R2 (D-27, D-45) |
| **Traces to** | BRD FBR-R2-01/02; PRD EPIC-14 (US-14.1–14.7, AC-14.1a–14.5), EPIC-15; HLD 04 §10.1; DECISIONS D-27, D-45, D-65, D-85 |
| **Depends on** | LLD-04 compliance (**release gate**), LLD-03 queues/agents, LLD-09 reporting, telephony-core originate/bridge |
| **Why this LLD** | D-45 makes power dialling R1.0. Debugging pacing and telephony at once is forbidden by D-27; this LLD separates the loop from the algorithm |

## 1. Scope and non-goals

**R1.0:** contact lists, visual segments, preview and power campaigns,
agent outcome capture, callbacks, voicemail drop, per-attempt evidence,
compliance gate before every dial, campaign pause/stop within 10 s
(AC-14.4).

**R2:** predictive pacing and abandonment control, answering-machine
detection (AMD), deeper pacing metrics (AC-14.1).

**Not in scope:** CRM connectors (API is sufficient), workforce
scheduling, inbound queueing (LLD-03), rating/billing (LLD-09).

**Invariants:** every outbound attempt receives a compliance verdict
(INV-09/BR-10); emergency never enters the dialer; an active call is never
dropped by campaign pause/stop (INV-03); AI/AMD failure never degrades a
call (INV-04).

## 2. Prior art (2026-11-04)

| Source | What it establishes | What we adopt |
|---|---|---|
| [Vodia](https://web.vodia.com/features) | Server-side outbound call lists, next-number dialling, queue/ACD stats, queue call limits and overflow | Server owns the list and the next contact; the agent never sees a machine (R1 target) |
| [FusionPBX](https://docs.fusionpbx.com/en/latest/applications/call_center.html) | Call-centre queues, agent tiers, max-wait/timeout actions, abandonment settings, recording | Campaign/agent state is explicit; timeout/abandonment are configured, not implicit |
| [3CX](https://www.3cx.com/) | Campaign/reporting UX around calls; queue and ring-group behavior | Console shows live campaign state and outcomes, not raw engine events |
| [VitalPBX](https://wiki.vitalpbx.com/) | Sonata Dialer and Stats as separate, API-backed modules | Pacing and reporting are separable; reporting reads final records |

Our difference: **compliance is a hard gate on every attempt**, and the
dialer is a platform context, not a console script around Asterisk.

## 3. Model

```go
type Campaign struct {
    ID, TenantID shared.ID
    Name string
    Mode Mode            // PREVIEW | POWER | PREDICTIVE
    Ratio float64        // POWER: calls per available agent
    MaxConcurrent int
    QueueID *shared.QueueID // agents come from LLD-03
    ComplianceProfile string
    State State          // DRAFT | RUNNING | PAUSED | STOPPED | COMPLETED
    StartAt, EndAt *time.Time
}

type Contact struct {
    ID, TenantID, CampaignID shared.ID
    Number string        // E.164 normalized
    Timezone string
    Attributes map[string]string // segment fields, no credentials
    State ContactState   // PENDING | DIALING | CONNECTED | COMPLETED | DNC | OPTED_OUT | CALLBACK
}

type Attempt struct {
    ID, TenantID, CampaignID, ContactID shared.ID
    AgentPrincipalID *shared.PrincipalID
    CallID *shared.CallID
    StartedAt time.Time
    Outcome AttemptOutcome // ANSWERED | NO_ANSWER | BUSY | FAILED | MACHINE | ABANDONED | BLOCKED
    BlockReason string
    ComplianceDecisionID *shared.UUID
}
```

Agent state is LLD-03's `Agent` (Available/OnCall/WrapUp/Paused/LoggedOut);
the dialer never invents a second agent state machine.

## 4. Storage

```sql
dialer_campaigns (id, tenant_id, name, mode, ratio, max_concurrent,
                  queue_id, compliance_profile, state, start_at, end_at)
dialer_contacts  (id, tenant_id, campaign_id, number_digits, timezone,
                  attributes JSONB, state, updated_at)
dialer_attempts  (id, tenant_id, campaign_id, contact_id, agent_principal_id,
                  call_id, started_at, outcome, block_reason,
                  compliance_decision_id)
dialer_callbacks (id, tenant_id, campaign_id, contact_id, due_at, timezone,
                  state, created_at)
dialer_imports   (id, tenant_id, campaign_id, status, accepted, rejected,
                  created_at)
```

All tenant-scoped/RLS. Numbers are normalized; import rejects malformed
rows with line numbers and never partially commits a list. Contact
attributes are data, not code; the segment builder is an allowlisted
filter, never SQL.

## 5. Pacing and the loop

**Preview:** agent asks for the next contact; the server checks compliance,
reserves capacity, returns one; the agent's phone rings; on answer the
contact is dialled. No machine reaches the agent.

**Power (R1.0):** a per-campaign pacer keeps
`dialling ≤ max_concurrent` and `dialling ≤ ratio × available_agents`.
Every slot runs: `Decide → reserve → originate (ordered caller-first) →
observe outcome → release slot`. The configured concurrent-call and licence
capacity are ceilings, never exceeded.

**Predictive (R2):** same loop, with pacing driven by rolling answer,
talk-time, wrap-up and abandonment estimates. The algorithm is a pure
function over a bounded observation window; it can only change the number
of simultaneous attempts, never bypass compliance or capacity.

**Pause/stop:** set campaign state, stop **new** attempts within 10 s (AC-14.4),
cancel pending callbacks if the operator asks; calls in progress run to
natural completion.

## 6. Outcomes, callbacks and voicemail

- Disposition is captured in two clicks (AC-14.6): a small campaign-defined
  outcome set plus free text; the next action (complete, retry, callback,
  schedule) is a rule, not hidden agent logic.
- Callbacks store `due_at` in the contact's zone; delivery is checked
  within 5 minutes in 95 % of cases (AC-14.3); a missed callback returns to
  the queue with a reason.
- Voicemail drop (R1) is a pre-recorded media file; it is only used after
  AMD (R2) or an explicit dialer decision; it never cuts a live human call.
- Every attempt is recorded, including blocked attempts (no call id), and
  appears on the contact and in the API (AC-14.5).

## 7. API

`DialerService` (tenant-scoped):
- Campaigns: `CreateCampaign`, `ListCampaigns`, `GetCampaign`,
  `StartCampaign`, `PauseCampaign`, `StopCampaign`
- Contacts: `ImportContacts` (streamed CSV, bounded), `ListContacts`,
  `BuildSegment` (allowlisted filters), `ListAttempts`
- Agent: `NextContact` (preview), `RecordDisposition`, `ScheduleCallback`
- Evidence: `ListAttempts`, `ExportAttempts`
- No `Dial` RPC that skips compliance; no field sets `Permitted`.

## 8. Security, failure and metrics

- Compliance store failure fails closed; capacity failure refuses with a
  fixed reason; a provider/AMD failure marks the attempt and may fall back
  to a human, never a blocked call path.
- Contact numbers/attributes are personal data: masked by default,
  export requires scope and is audited.
- SSRF/CSV injection: import is data only; exported cells are quoted and
  formula-prefixed safely.
- Metrics: attempts/s, answer rate, connect rate, agent occupancy, abandon
  rate, compliance block counts by reason, callback punctuality, slot
  utilisation. Alerts on campaign stop, compliance-store failure and a
  capacity ceiling.
- Load target: 10 agents / configured ratio; 95th percentile `NextContact`
  < 300 ms; no attempt without a decision row.

## 9. Tests and Definition of Done

- Unit: pacing ratio, max-concurrency, segment filters, outcome
  transitions, callback time zones, predictive function (R2).
- Integration: import/segment, attempt+decision atomicity, pause stops new
  attempts, callbacks, RLS, compliance block of a listed number.
- Live: preview/power campaign over the rig; two agents; a DNC number is
  never dialled; pause within 10 s; a call in progress survives pause.
- DoD: AC-14.1a/14.3/14.4/14.5 and AC-15.1/15.2 proven for R1.0;
  predictive/AMD (AC-14.1/14.2) explicitly R2.

## 10. Open decisions for the owner

1. Whether power mode may dial a contact before a free agent exists
   (affects abandonment risk and AC-14.x) — proposed **no** in R1.
2. Voicemail-drop media storage (same object store as recordings).
3. Maximum contacts per import (proposed 100k, streamed).
4. Which outcome vocabulary ships as default; campaigns may extend it.

**References:** [PRD EPIC-14/15](../PRD.md), [HLD 04 §10.1](../hld/04-bounded-contexts.md),
[LLD-04](LLD-04-compliance.md), [LLD-03](LLD-03-pbx-core.md),
[Vodia features](https://web.vodia.com/features),
[FusionPBX Call Center](https://docs.fusionpbx.com/en/latest/applications/call_center.html),
[3CX](https://www.3cx.com/).
