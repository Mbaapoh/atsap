# LLD-06 — Dialer (preview and power R1.0; predictive R2)

| | |
|---|---|
| **Bounded context** | `dialer` (Tier 2, HLD 04 §10.1) |
| **Status** | **Draft — design only, rewritten to the Flow Definition of Ready (D-98) on 2026-11-07.** R1.0 is preview and power with the abandonment rules; predictive pacing and AMD are R2 (D-27, D-45, D-97) |
| **Traces to** | BRD **FBR-R1-19**, FBR-R2-01, FBR-R2-02, FBR-R2-03, BR-06, BR-10; PRD EPIC-14 (US-14.1–14.7, AC-14.1–14.5), EPIC-15 (US-15.3, US-15.6, AC-15.1–15.6), §12.2; HLD-20 OB-01–OB-12; HLD-21 §3–§4; DECISIONS D-21, D-27, D-45, D-56, D-58, D-65, D-80, D-82, D-85, D-95, D-97, D-98 |
| **Depends on** | LLD-04 compliance (**release gate**), LLD-03 agents (§3.3) and number inventory/`ChoosePresentation` (§3.12), LLD-08 licence capacity, `telephony-core` media gateway (LLD-01), LLD-09 reporting |
| **Migrations** | `0053`–`0059` (LLD-04 holds `0050`–`0052`) |
| **Why this LLD** | D-45 makes power dialling R1.0. D-27 forbids debugging pacing and telephony at once; this LLD separates the loop (R1.0) from the algorithm (R2, §6.5) |

## 0. Contradictions resolved in this revision

| # | Was | Now | Why |
|---|---|---|---|
| 1 | §1/§9: abandonment control is R2 | **R1.0 for power** (§6.4, OB-06/07) | D-97, AC-15.6, FBR-R1-19 |
| 2 | Header traced FBR-R2-01/02 only | adds FBR-R1-19 (the R1.0 requirement) and FBR-R2-03 | BRD v1.1 |
| 3 | Voicemail drop "R1" after AMD (R2) vs HLD-20 "after an explicit decision" vs US-14.4 with no AMD in R1.0 | **R1.0: agent-triggered one-click drop (US-14.5) only. R2: automatic drop after AMD (US-14.4, AC-14.2).** OWNER DECISION §14.1, recommended | FBR-R2-02 puts AMD in R2; US-14.5 needs no AMD |
| 4 | §10.1 "never dial before a free agent" vs ratio > 1 | Ratio **1.0 = reserved mode**: every attempt reserves a Ready agent and cannot abandon. Ratio **> 1.0 = dial-ahead mode**: dials before an agent is free, governed by the abandonment rules. Default 1.0 | AC-14.1a "at the configured ratio" |
| 5 | Preview rang the agent, then dialled; HLD-21 uses a nailed-up leg | **All agent work in a campaign uses the nailed-up leg**, preview included (§5.4) | One agent-leg lifecycle; preview gains sub-second connect and agent-heard call progress; no per-contact agent ring |
| 6 | AMD time vs FCC 2 s from greeting end, undefined | LLD-04 §7.4: clock **from answer** without AMD (R1.0), from greeting end with AMD (R2); undecided AMD = human | 47 CFR 64.1200(a)(7)(i) |
| 7 | Hopper "compliance pre-checked" vs LLD-04 writing a row per `Decide` | Hopper uses **`Precheck`** (non-recording, advisory); only `Decide` at the dial instant authorises | LLD-04 §5.2 |
| 8 | Agent states `Available/OnCall/WrapUp/Paused/LoggedOut` (LLD-03 §3.1) | **HLD-21 §3 set: `LoggedOut`, `Ready`, `Reserved`, `Busy`, `WrapUp`, `NotReady(reason)`**. Mapping: Available→Ready, OnCall→Busy, Paused→NotReady. LLD-03 owns the machine; the dialler never adds an agent state | HLD-21 §3, D-98 audit |

## 1. Scope and non-goals

| Release | In scope |
|---|---|
| **R1.0** | Reusable lists and CSV import (OB-01); segments; campaigns with list mix; preview (OB-02) and power (OB-03) with the nailed-up agent leg; compliance gate per attempt (OB-05); abandonment measure, slow-down and stop (OB-06); minimum ring, abandon message with key-press opt-out (OB-07); agent one-click voicemail drop (OB-08 part); dispositions, retry and recycle rules, callbacks (OB-09); attempt evidence (OB-10); carrier-failure handling (OB-11); caller-ID selection from the LLD-03 §3.12 number inventory (OB-12); manual dial with lead lookup; pause codes; recording policy |
| **R2** | Predictive pacing (OB-04, §6.5 specifies the function now); AMD and automatic voicemail drop (OB-08, US-14.4, AC-14.2); postal-code zone lookup |
| **Not in scope** | Agentless/IVR campaigns, SMS/email, CRM connectors (API suffices), workforce scheduling, inbound queueing (LLD-03), rating (LLD-09), number-inventory design (LLD-03), carrier route selection (D-80) |

**Invariants that bind every flow:** INV-01 an emergency number is never
imported or dialled by the dialler; INV-03 a call in progress (and a
ringing attempt) is never torn down by pause, stop, compliance or
capacity; INV-04 AMD/AI failure never degrades a call (treated as human);
INV-09/BR-10 no attempt reaches the engine without a permitting
`compliance_decisions` row; INV-10 RLS on every table; BR-06 only an
authorised presentation number.

## 2. Prior art (2026-11-04, extended 2026-11-07)

| Source | What it establishes | What we adopt |
|---|---|---|
| [VICIdial](https://www.vicidial.org/docs/) | `vicidial_hopper` with `hopper_level`; `auto_dial_level`; adaptive methods; lead recycling by status; list mix; dial statuses; `drop_call_seconds` + safe-harbour message; call times per state; internal/campaign DNC; manual dial with lead search; pause codes; nailed-up agent "conference" leg | Hopper, ratio, recycle, list mix, dial statuses, pause codes, nailed-up leg — in Postgres, with compliance as a gate rather than a setting |
| [Amazon Connect outbound campaigns](https://docs.aws.amazon.com/connect/latest/adminguide/enable-outbound-campaigns.html) | Progressive/predictive/agentless modes; capacity allocation; AMD with "await prompt"; communication time windows; abandonment target | Capacity is a hard ceiling; abandonment target is a profile parameter |
| Five9-class | Preview with skip/timeout; power/progressive/predictive; dial plan per disposition with redial timers; not-ready reason codes; local presence; max abandon rate | Preview TTL and skip; per-outcome retry; reason codes; caller-ID modes |
| [Vodia](https://web.vodia.com/features), [FusionPBX](https://docs.fusionpbx.com/en/latest/applications/call_center.html), [3CX](https://www.3cx.com/), [VitalPBX](https://wiki.vitalpbx.com/) | Server-owned call lists; explicit queue/agent state; dialer and stats as API-backed modules | Server owns list and next contact; reporting reads final records |

Our deliberate differences: compliance is a hard gate on every attempt
with no override; abandonment is enforced in R1.0 power mode; all state
is Postgres (no Redis, D-56); the dialler is a platform context, not a
console script around Asterisk.

## 3. Shared model

### 3.1 Contexts and who owns what

| Concern | Owner | Dialler uses |
|---|---|---|
| Agent state machine (`Ready`, `Reserved`, `Busy`, `WrapUp`, `NotReady(reason)`) and pause-code catalogue | `pbx-core` (LLD-03) | port `AgentStates` (§5.2) |
| Verdicts, abandonment counters, profiles | `compliance` (LLD-04) | ports `Gate`, `AbandonLedger`, `OptOuts`, `Policies` |
| Presentation numbers | `pbx-core` (LLD-03 §3.12 `tenant_numbers`, `ChoosePresentation`) | port `Presentation` (§6.12) |
| Licence capacity | `licensing` (LLD-08) | port `Capacity` (= `LicenseManager.ValidateCapacity/ReleaseCapacity`, keyed by call, D-58) |
| Channels, bridges, playback, recording | `telephony-core` | port `Media` (ARI) |
| Lists, campaigns, leads, hopper, attempts, sessions, callbacks | `dialer` | — |

### 3.2 System outcome vocabulary (fixed) and default dispositions (editable)

| System outcome (attempt) | Set by | Consumes an attempt | Counts in abandon ratio |
|---|---|---|---|
| `HUMAN` (bridged to agent) | dialler | yes | live answer |
| `ABANDONED` | dialler | yes | live + abandoned |
| `MACHINE` (agent-marked R1.0; AMD R2) | agent / AMD | yes | no |
| `VOICEMAIL_DROPPED` | dialler | yes | no |
| `NO_ANSWER` | engine | yes | no |
| `BUSY` | engine | yes | no |
| `REJECTED` (SIP 603/Q.850 21) | engine | yes | no |
| `INVALID_NUMBER` (404/484, Q.850 1/28) | engine | yes, lead → `INVALID` | no |
| `CARRIER_FAILED` (503/Q.850 34/38/41/42, 20/27) | engine | **no** (≤ 3 per lead per day, then yes) | no |
| `BLOCKED` (compliance refusal) | dialler | no | no |
| `NOT_PLACED` (capacity/caller-ID/engine refusal before dial) | dialler | no | no |

Default dispositions (agent, two clicks, AC-14.6), each mapped to a
**next action**:

| Code | Label | Next action |
|---|---|---|
| `SALE` | Success | COMPLETE |
| `NOT_INTERESTED` | Not interested | COMPLETE |
| `CALLBACK` | Call back | CALLBACK (opens the scheduler) |
| `NO_DECISION` | Call again later | RETRY (rule delay) |
| `WRONG_NUMBER` | Wrong number | INVALID |
| `ANSWERING_MACHINE` | Answering machine | RETRY (outcome MACHINE) |
| `DO_NOT_CALL` | Do not call | OPT_OUT (writes `opt_outs`, source AGENT) |
| `AGENT_TIMEOUT` | (system) no disposition in time | RETRY |

A campaign may add up to 30 codes; each code's next action is one of
`COMPLETE | RETRY | CALLBACK | INVALID | OPT_OUT | DNC_CAMPAIGN`.

## 4. Data model (migrations 0053–0059)

Every table below carries `tenant_id UUID NOT NULL` and the standard RLS
block (LLD-04 §4): `ENABLE` + `FORCE ROW LEVEL SECURITY`, policy
`USING (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid)`
with the same `WITH CHECK`; grants to `atsapbx_app` as stated per table.

### 4.1 `0053_dialer_lists`

```sql
CREATE TABLE dialer_lists (
  id UUID PRIMARY KEY, tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name VARCHAR(128) NOT NULL, default_country CHAR(2) NOT NULL,          -- ISO 3166-1
  state VARCHAR(10) NOT NULL DEFAULT 'ACTIVE' CHECK (state IN ('IMPORTING','ACTIVE','ARCHIVED')),
  contact_count INT NOT NULL DEFAULT 0, created_by UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(), UNIQUE (tenant_id, name)
);
CREATE TABLE dialer_contacts (
  id UUID PRIMARY KEY, tenant_id UUID NOT NULL, list_id UUID NOT NULL REFERENCES dialer_lists(id) ON DELETE CASCADE,
  number_digits VARCHAR(15) NOT NULL CHECK (number_digits ~ '^[1-9][0-9]{6,14}$'),
  external_id VARCHAR(128) NOT NULL DEFAULT '', first_name VARCHAR(128) NOT NULL DEFAULT '',
  last_name VARCHAR(128) NOT NULL DEFAULT '', timezone VARCHAR(64) NOT NULL DEFAULT '',
  country CHAR(2) NOT NULL, region VARCHAR(8) NOT NULL DEFAULT '', postal_code VARCHAR(16) NOT NULL DEFAULT '',
  attributes JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(attributes) = 'object'),
  import_flag VARCHAR(16) NOT NULL DEFAULT '',   -- '' | dnc | opted_out | consent_missing (Precheck at import)
  UNIQUE (list_id, number_digits)                -- dedupe within a list
);
CREATE INDEX dialer_contacts_number ON dialer_contacts (tenant_id, number_digits);
CREATE TABLE dialer_imports (
  id UUID PRIMARY KEY, tenant_id UUID NOT NULL, list_id UUID NOT NULL,
  state VARCHAR(10) NOT NULL CHECK (state IN ('RECEIVING','VALIDATING','COMMITTED','FAILED')),
  file_bytes BIGINT NOT NULL DEFAULT 0, rows_total INT NOT NULL DEFAULT 0, accepted INT NOT NULL DEFAULT 0,
  rejected INT NOT NULL DEFAULT 0, duplicates INT NOT NULL DEFAULT 0, flagged INT NOT NULL DEFAULT 0,
  mapping JSONB NOT NULL, failure VARCHAR(64) NOT NULL DEFAULT '',
  created_by UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), finished_at TIMESTAMPTZ
);
CREATE TABLE dialer_import_errors (            -- first 1000 per import; the rest are counted
  import_id UUID NOT NULL REFERENCES dialer_imports(id) ON DELETE CASCADE, tenant_id UUID NOT NULL,
  line INT NOT NULL, reason VARCHAR(32) NOT NULL, PRIMARY KEY (import_id, line)
);
CREATE UNLOGGED TABLE dialer_import_staging (   -- rows of an import before its single commit tx
  import_id UUID NOT NULL, tenant_id UUID NOT NULL, line INT NOT NULL, row JSONB NOT NULL,
  PRIMARY KEY (import_id, line)
);
```

Grants: `SELECT, INSERT, UPDATE, DELETE` on all five (archive is a state;
contacts are deleted only with their list).

### 4.2 `0054_dialer_campaigns`

```sql
CREATE TABLE dialer_campaigns (
  id UUID PRIMARY KEY, tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name VARCHAR(128) NOT NULL, mode VARCHAR(10) NOT NULL CHECK (mode IN ('PREVIEW','POWER','PREDICTIVE')),
  state VARCHAR(10) NOT NULL DEFAULT 'DRAFT' CHECK (state IN ('DRAFT','RUNNING','PAUSED','COMPLETED','STOPPED')),
  pause_reason VARCHAR(24) NOT NULL DEFAULT '',  -- MANUAL|ABANDON_LIMIT|CARRIER_FAILURE|CALLER_ID_INVALID|COMPLIANCE_BLOCKING|SCHEDULE|NO_AGENTS
  policy_id UUID NOT NULL,                        -- compliance profile (LLD-04)
  ratio NUMERIC(3,1) NOT NULL DEFAULT 1.0 CHECK (ratio BETWEEN 1.0 AND 3.0),
  effective_ratio NUMERIC(3,1) NOT NULL DEFAULT 1.0,   -- pacer-owned (slow-down)
  max_concurrent INT NOT NULL DEFAULT 10 CHECK (max_concurrent BETWEEN 1 AND 500),
  ring_timeout_s SMALLINT NOT NULL DEFAULT 25 CHECK (ring_timeout_s BETWEEN 10 AND 60),
  wrapup_max_s SMALLINT NOT NULL DEFAULT 120 CHECK (wrapup_max_s BETWEEN 0 AND 900),
  preview_ttl_s SMALLINT NOT NULL DEFAULT 120 CHECK (preview_ttl_s BETWEEN 30 AND 600),
  preview_timeout_action VARCHAR(6) NOT NULL DEFAULT 'RETURN' CHECK (preview_timeout_action IN ('RETURN','DIAL')),
  preview_skip_limit_per_hour SMALLINT NOT NULL DEFAULT 0,   -- 0 = unlimited
  callerid_mode VARCHAR(16) NOT NULL DEFAULT 'FIXED' CHECK (callerid_mode IN ('FIXED','POOL','LOCAL_PRESENCE')),
  callerid_number_id UUID,  callerid_pool_id UUID,       -- tenant_numbers.id (LLD-03 §3.12) / dialer_callerid_pools.id
  callback_number_id UUID NOT NULL,                        -- tenant_numbers.id; must be a verified carrier_did (inbound-routable)
  recording_policy VARCHAR(10) NOT NULL DEFAULT 'NONE' CHECK (recording_policy IN ('NONE','ALL','ON_DEMAND')),
  zip_tone BOOLEAN NOT NULL DEFAULT TRUE,
  voicemail_drop_media_uri TEXT NOT NULL DEFAULT '',
  schedule_start TIMESTAMPTZ, schedule_end TIMESTAMPTZ,
  daily_start_minute SMALLINT, daily_end_minute SMALLINT,    -- campaign hours, intersected with the profile
  carrier_fail_window SMALLINT NOT NULL DEFAULT 50, carrier_fail_pct SMALLINT NOT NULL DEFAULT 50,
  version INT NOT NULL DEFAULT 1, created_by UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(), state_changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, name),
  CHECK (mode <> 'PREVIEW' OR ratio = 1.0),
  CHECK (callerid_mode <> 'FIXED' OR callerid_number_id IS NOT NULL),
  CHECK (callerid_mode = 'FIXED' OR callerid_pool_id IS NOT NULL)
);
CREATE TABLE dialer_campaign_lists (       -- list mix
  tenant_id UUID NOT NULL, campaign_id UUID NOT NULL REFERENCES dialer_campaigns(id) ON DELETE CASCADE,
  list_id UUID NOT NULL REFERENCES dialer_lists(id), segment JSONB NOT NULL DEFAULT '[]',
  weight SMALLINT NOT NULL DEFAULT 1 CHECK (weight BETWEEN 1 AND 100),
  priority SMALLINT NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 9),
  active BOOLEAN NOT NULL DEFAULT TRUE, PRIMARY KEY (campaign_id, list_id)
);
CREATE TABLE dialer_callerid_pools (       -- the dialler owns pools; LLD-03 owns the numbers
  id UUID PRIMARY KEY, tenant_id UUID NOT NULL, name VARCHAR(64) NOT NULL, UNIQUE (tenant_id, name)
);
CREATE TABLE dialer_callerid_pool_members (
  tenant_id UUID NOT NULL, pool_id UUID NOT NULL REFERENCES dialer_callerid_pools(id) ON DELETE CASCADE,
  number_id UUID NOT NULL,                  -- tenant_numbers.id; validated via the port, no cross-context FK
  npa VARCHAR(8) NOT NULL DEFAULT '', region VARCHAR(8) NOT NULL DEFAULT '',   -- for LOCAL_PRESENCE
  last_used_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch', PRIMARY KEY (pool_id, number_id)
);
CREATE TABLE dialer_dispositions (
  tenant_id UUID NOT NULL, campaign_id UUID NOT NULL REFERENCES dialer_campaigns(id) ON DELETE CASCADE,
  code VARCHAR(24) NOT NULL CHECK (code ~ '^[A-Z][A-Z0-9_]{1,23}$'), label VARCHAR(64) NOT NULL,
  next_action VARCHAR(12) NOT NULL CHECK (next_action IN ('COMPLETE','RETRY','CALLBACK','INVALID','OPT_OUT','DNC_CAMPAIGN')),
  sort SMALLINT NOT NULL DEFAULT 0, PRIMARY KEY (campaign_id, code)
);
CREATE TABLE dialer_retry_rules (          -- one row per outcome or disposition key
  tenant_id UUID NOT NULL, campaign_id UUID NOT NULL REFERENCES dialer_campaigns(id) ON DELETE CASCADE,
  outcome_key VARCHAR(24) NOT NULL,        -- NO_ANSWER, BUSY, MACHINE, ABANDONED, NO_DECISION, AGENT_TIMEOUT…
  delay_minutes INT NOT NULL CHECK (delay_minutes BETWEEN 5 AND 43200),
  max_for_outcome SMALLINT NOT NULL DEFAULT 0 CHECK (max_for_outcome BETWEEN 0 AND 50),  -- 0 = only global caps
  backoff NUMERIC(3,1) NOT NULL DEFAULT 1.0 CHECK (backoff BETWEEN 1.0 AND 4.0),
  PRIMARY KEY (campaign_id, outcome_key)
);
ALTER TABLE dialer_campaigns ADD COLUMN max_attempts_total SMALLINT NOT NULL DEFAULT 5 CHECK (max_attempts_total BETWEEN 1 AND 50),
  ADD COLUMN max_attempts_per_day SMALLINT NOT NULL DEFAULT 3 CHECK (max_attempts_per_day BETWEEN 1 AND 20),
  ADD COLUMN min_redial_minutes INT NOT NULL DEFAULT 60 CHECK (min_redial_minutes BETWEEN 5 AND 10080);
```

Grants: `SELECT, INSERT, UPDATE, DELETE`.

### 4.3 `0055_dialer_leads_hopper`

```sql
CREATE TABLE dialer_leads (                 -- a contact's state within one campaign (lazy-created)
  tenant_id UUID NOT NULL, campaign_id UUID NOT NULL REFERENCES dialer_campaigns(id) ON DELETE CASCADE,
  contact_id UUID NOT NULL REFERENCES dialer_contacts(id) ON DELETE CASCADE,
  state VARCHAR(12) NOT NULL DEFAULT 'READY' CHECK (state IN
    ('READY','IN_HOPPER','DIALING','RETRY_WAIT','CALLBACK','COMPLETED','EXHAUSTED','DNC','OPTED_OUT','INVALID','BLOCKED_WAIT')),
  attempts_total SMALLINT NOT NULL DEFAULT 0, attempts_today SMALLINT NOT NULL DEFAULT 0,
  attempts_day DATE, carrier_fails_today SMALLINT NOT NULL DEFAULT 0,
  outcome_counts JSONB NOT NULL DEFAULT '{}',          -- per outcome_key, for max_for_outcome
  last_outcome VARCHAR(24) NOT NULL DEFAULT '', last_attempt_at TIMESTAMPTZ,
  next_eligible_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (campaign_id, contact_id)
);
CREATE INDEX dialer_leads_eligible ON dialer_leads (campaign_id, next_eligible_at) WHERE state IN ('READY','RETRY_WAIT','BLOCKED_WAIT');
CREATE TABLE dialer_hopper (
  id BIGSERIAL PRIMARY KEY, tenant_id UUID NOT NULL, campaign_id UUID NOT NULL,
  contact_id UUID NOT NULL, list_id UUID NOT NULL, number_digits VARCHAR(15) NOT NULL,
  priority SMALLINT NOT NULL,                  -- 100 callback, 50 retry, 0–9 list priority
  callback_id UUID, owner_principal_id UUID,   -- agent-owned callback offered only to this agent
  state VARCHAR(7) NOT NULL DEFAULT 'READY' CHECK (state IN ('READY','LEASED')),
  leased_to UUID, lease_expires_at TIMESTAMPTZ,
  inserted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (campaign_id, contact_id), UNIQUE (campaign_id, number_digits)
);
CREATE INDEX dialer_hopper_pick ON dialer_hopper (campaign_id, state, priority DESC, inserted_at, id);
```

Grants: `SELECT, INSERT, UPDATE, DELETE`; `USAGE, SELECT` on the sequence.

### 4.4 `0056_dialer_attempts`

```sql
CREATE TABLE dialer_attempts (
  id UUID PRIMARY KEY, tenant_id UUID NOT NULL, campaign_id UUID, contact_id UUID, list_id UUID,
  kind VARCHAR(8) NOT NULL CHECK (kind IN ('DIALER','PREVIEW','MANUAL','CALLBACK')),
  state VARCHAR(10) NOT NULL CHECK (state IN ('QUEUED','DIALING','RINGING','ANSWERED','BRIDGED',
        'ABANDONED','MACHINE','NO_ANSWER','BUSY','FAILED','BLOCKED','NOT_PLACED','DISPOSED')),
  outcome VARCHAR(20) NOT NULL DEFAULT '',     -- §3.2 vocabulary
  disposition VARCHAR(24) NOT NULL DEFAULT '', disposition_note VARCHAR(1000) NOT NULL DEFAULT '',
  number_digits VARCHAR(15) NOT NULL, presented_number VARCHAR(15),
  agent_principal_id UUID, agent_reserved BOOLEAN NOT NULL DEFAULT FALSE,
  call_id UUID, compliance_decision_id UUID, block_reason VARCHAR(32) NOT NULL DEFAULT '',
  q850_cause SMALLINT, sip_code SMALLINT, dial_status VARCHAR(12) NOT NULL DEFAULT '',
  queued_at TIMESTAMPTZ NOT NULL, dialed_at TIMESTAMPTZ, answered_at TIMESTAMPTZ,
  connect_clock_at TIMESTAMPTZ, bridged_at TIMESTAMPTZ, ended_at TIMESTAMPTZ, disposed_at TIMESTAMPTZ,
  -- INV-09 in the schema: nothing past QUEUED without a decision
  CHECK (state IN ('QUEUED','NOT_PLACED') OR compliance_decision_id IS NOT NULL),
  CHECK (state <> 'BLOCKED' OR block_reason <> '')
);
CREATE UNIQUE INDEX dialer_attempts_one_live_per_number ON dialer_attempts (tenant_id, number_digits)
  WHERE state IN ('DIALING','RINGING','ANSWERED','BRIDGED');   -- never two live calls to one person
CREATE INDEX dialer_attempts_contact ON dialer_attempts (tenant_id, contact_id, queued_at DESC);
CREATE INDEX dialer_attempts_campaign ON dialer_attempts (tenant_id, campaign_id, queued_at DESC);
```

Grants: `SELECT, INSERT, UPDATE` (no `DELETE`; retention job as owner,
**760 days** like LLD-04, because an `ABANDONED` attempt is part of the
two-year abandonment record).

### 4.5 `0057_dialer_agent_sessions`

```sql
CREATE TABLE dialer_agent_sessions (
  id UUID PRIMARY KEY, tenant_id UUID NOT NULL, principal_id UUID NOT NULL,
  campaign_id UUID NOT NULL REFERENCES dialer_campaigns(id),
  device_endpoint VARCHAR(128) NOT NULL,      -- resolved from LLD-03 hot-desk device; never on the wire
  bridge_id VARCHAR(64), agent_call_id UUID,  -- engine refs kept server-side only
  state VARCHAR(12) NOT NULL CHECK (state IN ('CONNECTING','CONNECTED','RECONNECTING','ENDED')),
  reconnects SMALLINT NOT NULL DEFAULT 0, current_attempt_id UUID,
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(), ended_at TIMESTAMPTZ, end_reason VARCHAR(24) NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX dialer_agent_sessions_one_open ON dialer_agent_sessions (tenant_id, principal_id) WHERE state <> 'ENDED';
```

### 4.6 `0058_dialer_callbacks`

```sql
CREATE TABLE dialer_callbacks (
  id UUID PRIMARY KEY, tenant_id UUID NOT NULL, campaign_id UUID NOT NULL, contact_id UUID NOT NULL,
  owner VARCHAR(5) NOT NULL CHECK (owner IN ('AGENT','ANY')), owner_principal_id UUID,
  due_at TIMESTAMPTZ NOT NULL, contact_zone VARCHAR(64) NOT NULL,
  state VARCHAR(10) NOT NULL CHECK (state IN ('SCHEDULED','DUE','OFFERED','DIALING','COMPLETED','MISSED','CANCELLED')),
  note VARCHAR(500) NOT NULL DEFAULT '', created_by UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  delivered_at TIMESTAMPTZ, miss_reason VARCHAR(24) NOT NULL DEFAULT '',
  CHECK ((owner = 'AGENT') = (owner_principal_id IS NOT NULL))
);
CREATE INDEX dialer_callbacks_due ON dialer_callbacks (due_at) WHERE state = 'SCHEDULED';
CREATE UNIQUE INDEX dialer_callbacks_one_open ON dialer_callbacks (campaign_id, contact_id) WHERE state IN ('SCHEDULED','DUE','OFFERED','DIALING');
```

### 4.7 `0059_dialer_carrier_window`

```sql
CREATE TABLE dialer_campaign_stats (          -- pacer observations, one row per campaign, overwritten
  tenant_id UUID NOT NULL, campaign_id UUID PRIMARY KEY REFERENCES dialer_campaigns(id) ON DELETE CASCADE,
  recent_results SMALLINT[] NOT NULL DEFAULT '{}', -- ring of last N engine results: 1 = carrier fail
  answered_15m INT NOT NULL DEFAULT 0, attempts_15m INT NOT NULL DEFAULT 0,
  mean_handle_ms INT NOT NULL DEFAULT 0, mean_ring_to_answer_ms INT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Grants on 0057–0059: `SELECT, INSERT, UPDATE`.

## 5. State machines

### 5.1 Campaign

| From | Event | Guard | To | Side effects |
|---|---|---|---|---|
| — | `CreateCampaign` | valid fields; policy exists; mode ≠ PREDICTIVE in R1.0 | DRAFT | audit |
| DRAFT, PAUSED(MANUAL…) , COMPLETED | `StartCampaign` | ≥ 1 active list; policy valid (`abandon_message_media_uri` set when required); caller-ID numbers and `callback_number_id` `verified` (LLD-03 §3.12); dispositions ≥ 1; inside schedule; for PAUSED(ABANDON_LIMIT) see §6.4 | RUNNING | pacer/hopper start within 5 s; event `campaign.started`/`resumed` |
| RUNNING | `PauseCampaign` | — | PAUSED(MANUAL) | no new attempt after ≤ 10 s (AC-14.4); live and ringing attempts continue |
| RUNNING | breach (LLD-04 `Breached`) | mode POWER ratio>1 or PREDICTIVE | PAUSED(ABANDON_LIMIT) | `effective_ratio := 1.0`; alert |
| RUNNING | carrier detector (§6.11) | — | PAUSED(CARRIER_FAILURE) | alert |
| RUNNING | caller ID becomes unauthorised | — | PAUSED(CALLER_ID_INVALID) | alert |
| RUNNING | 20 consecutive `Decide` refusals of a policy-wide reason (`dnc_registry_stale`, `profile_missing`) | — | PAUSED(COMPLIANCE_BLOCKING) | alert |
| RUNNING | `schedule_end` passed | — | PAUSED(SCHEDULE) | — |
| RUNNING | no agent session for 30 min | mode ≠ PREVIEW | PAUSED(NO_AGENTS) | — |
| RUNNING | no lead eligible ever again (all leads terminal, no callbacks open) | checked every 5 min | COMPLETED | event |
| DRAFT, RUNNING, PAUSED, COMPLETED | `StopCampaign` | — | STOPPED | callbacks cancelled if `cancel_callbacks=true`, else kept for a later campaign clone; hopper emptied; sessions told "campaign stopped" and returned to `NotReady(campaign_stopped)` after their current call |
| STOPPED | any start | — | refused | `FAILED_PRECONDITION` "A stopped campaign cannot restart; duplicate it" |

**COMPLETED vs STOPPED:** COMPLETED is the system's "nothing left to
dial"; attaching a list or a lead becoming eligible lets `StartCampaign`
resume it. STOPPED is the operator's final decision; it is terminal and
may be cloned (`DuplicateCampaign`).

**Edit while RUNNING** (`UpdateCampaign`, version-checked):

| Field | RUNNING | PAUSED | DRAFT |
|---|---|---|---|
| name, lists/weights/segments, retry rules, dispositions (add, relabel), `max_concurrent`, `ratio`, `ring_timeout_s` (≥ profile min), `wrapup_max_s`, schedule, recording policy, voicemail media | yes, next tick | yes | yes |
| `mode`, `policy_id`, caller-ID mode/number/pool, `callback_number`, removing a disposition code in use | **no** (`FAILED_PRECONDITION` "Pause the campaign to change this") | yes | yes |

### 5.2 Lead (contact within a campaign)

| From | Event | To |
|---|---|---|
| (none) | hopper refill selects the contact | READY row created, then IN_HOPPER |
| READY / RETRY_WAIT / BLOCKED_WAIT | refill, `next_eligible_at ≤ now`, Precheck permits | IN_HOPPER |
| IN_HOPPER | lease expired / campaign paused / stale (15 min) | READY (row deleted from hopper) |
| IN_HOPPER | `Decide` refuses with `outside_hours`, `blackout_date`, `no_redial_after_abandon`, `abandon_limit`, `dnc_registry_stale` | BLOCKED_WAIT, `next_eligible_at` = next window opening (or +15 min) |
| IN_HOPPER | `Decide` refuses `dnc_*` | DNC (terminal) |
| IN_HOPPER | `Decide` refuses `opted_out` | OPTED_OUT (terminal) |
| IN_HOPPER | `Decide` refuses `consent_missing`, `timezone_unknown`, `number_invalid` | INVALID (terminal; reason on the attempt) |
| IN_HOPPER | attempt placed | DIALING |
| DIALING | outcome + next action RETRY, caps not reached | RETRY_WAIT (`next_eligible_at = now + delay × backoff^(n−1)`, ≥ `min_redial_minutes`, ≥ profile no-redial after ABANDONED) |
| DIALING | caps reached (`max_attempts_total`, or `max_for_outcome`) | EXHAUSTED (terminal; recyclable by `RecycleLeads`) |
| DIALING | `attempts_today = max_attempts_per_day` | RETRY_WAIT until next local midnight |
| DIALING | next action CALLBACK | CALLBACK |
| DIALING | COMPLETE / INVALID / OPT_OUT / DNC_CAMPAIGN | COMPLETED / INVALID / OPTED_OUT / DNC |
| CALLBACK | callback DUE | IN_HOPPER (priority 100) |
| EXHAUSTED, COMPLETED(with chosen dispositions) | `RecycleLeads{statuses, delay ≥ 5 min, limit}` | RETRY_WAIT with `attempts_total` reset only if `reset_attempts=true` |

`attempts_today` resets when `attempts_day` ≠ today in the contact's zone.

### 5.3 Attempt

| From | Event (ARI / domain) | To | Recorded |
|---|---|---|---|
| — | slot granted, row inserted | QUEUED | `queued_at` |
| QUEUED | `Decide` refuses | BLOCKED | decision id, `block_reason` |
| QUEUED | capacity refused / no authorised caller ID / engine refused create | NOT_PLACED | `block_reason` = `capacity` / `callerid` / `engine` |
| QUEUED | `Decide` permits, capacity reserved, `POST /channels/{id}/dial` accepted | DIALING | `dialed_at`, decision id, `presented_number` |
| DIALING | `ChannelStateChange` → `Ringing` or `Dial` `dialstatus=RINGING`/`PROGRESS` | RINGING | — |
| DIALING/RINGING | `Dial` `dialstatus=ANSWER` | ANSWERED | `answered_at`, `connect_clock_at` |
| ANSWERED | customer added to the agent bridge before the deadline | BRIDGED | `bridged_at`; ledger live answer |
| ANSWERED | deadline passed with no agent, or customer hung up before bridge | ABANDONED | ledger abandon; message played? |
| BRIDGED | agent marks machine and drops / disposes `ANSWERING_MACHINE` | MACHINE (outcome VOICEMAIL_DROPPED if dropped) | ledger: live answer **reversed is not allowed** — see note |
| DIALING/RINGING | `Dial` `NOANSWER` (timeout) / `BUSY` / `CONGESTION`, `CHANUNAVAIL` / `CANCEL` | NO_ANSWER / BUSY / FAILED / FAILED | `dial_status`, `q850_cause`, `sip_code` |
| BRIDGED, ABANDONED, MACHINE, NO_ANSWER, BUSY, FAILED, BLOCKED, NOT_PLACED | disposition (agent) or auto-disposition (system outcomes with no agent) | DISPOSED | `disposed_at`, lead transition (§5.2) |

Note on machines in R1.0: without AMD an answering machine is bridged to
the agent; it counts as a **live answer** at bridge time (the platform
cannot know), which only makes the ratio more conservative. The agent's
`ANSWERING_MACHINE` disposition does not decrement the counter (INV-08:
counters are never corrected downward).

### 5.4 Agent session (nailed-up leg) and agent state

| Session from | Event | Session to | Agent state (LLD-03) |
|---|---|---|---|
| — | `JoinCampaign` | CONNECTING | stays as is until connected |
| CONNECTING | agent leg `StasisStart` (answered) and added to session bridge | CONNECTED | → `Ready` (or `NotReady(reason)` if joined paused) |
| CONNECTING | originate timeout 30 s / rejected | ENDED(`agent_no_answer`) | `NotReady(leg_failed)` |
| CONNECTED | attempt reserved (ratio 1.0, preview, callback) | CONNECTED | `Ready → Reserved` (conditional update) |
| CONNECTED | customer bridged | CONNECTED | `Reserved/Ready → Busy` |
| CONNECTED | customer leaves bridge | CONNECTED | `Busy → WrapUp` (timer `wrapup_max_s`) |
| CONNECTED | disposition recorded | CONNECTED | `WrapUp → Ready` (or `NotReady(reason)` if a pause was requested during the call) |
| CONNECTED | wrap-up timer expires | CONNECTED | auto-disposition `AGENT_TIMEOUT`; `WrapUp → NotReady(dispo_timeout)` |
| CONNECTED | `SetPause(code)` | CONNECTED | `Ready → NotReady(code)`; during a call: applied at WrapUp end |
| CONNECTED | agent leg `StasisEnd`/`ChannelDestroyed` | RECONNECTING | `NotReady(leg_lost)`; a customer in the bridge is hung up (`normal_clearing`) — the conversation ended because a party left, not by platform condition |
| RECONNECTING | re-originate (≤ 3 tries, 5 s apart) answered | CONNECTED | `NotReady(leg_lost)` until the agent clicks Ready |
| RECONNECTING | 3 failures | ENDED(`leg_lost`) | `NotReady(leg_lost)` |
| any | `LeaveCampaign` / logout / membership disabled | ENDED | after any current call ends: `LoggedOut` or the agent's previous inbound state |

## 6. Flows

Each flow below lists the twelve DoR items; data (item 5) refers to §4,
states (item 4) to §5, API (item 7) to §7, config (item 8) to §8,
events/audit (item 10) to §9 unless stated.

### 6.1 OB-01 — Lists, import, segments, campaign lifecycle (R1.0)

1. **Trigger/actors:** campaign manager in the console or partner API.
   Permissions `dialer.list.manage` (import, archive), `dialer.campaign.manage`
   (create/edit), `dialer.campaign.control` (start/pause/stop).
2. **Preconditions:** dialler entitlement (LLD-11 feature `dialer`); for
   start, §5.1 guards. Missing entitlement → `PERMISSION_DENIED`
   "Outbound campaigns are not included in this licence".
3. **Sequence (import):**
   1. `ImportContacts` stream opens: header `{list_id | new_list{name, default_country}, mapping, dedupe_scope}`; row `dialer_imports` RECEIVING.
   2. Chunks are parsed as they arrive (RFC 4180; delimiter auto-detected from the header among `,` `;` TAB; UTF-8 only, BOM stripped; invalid UTF-8 → whole import FAILED `encoding` with the line).
   3. Each row is normalised: phone → E.164 with the list's `default_country` when no `+`; validation with the phone-number library (OWNER DECISION §14.4: dependency); reject reasons `number_invalid`, `number_too_short`, `emergency_number` (matches the tenant's emergency catalogue, INV-01), `premium_rate`, `timezone_invalid`, `attribute_too_long`, `duplicate_in_file`.
   4. Accepted rows go to `dialer_import_staging`; state VALIDATING.
   5. Scrub: `Gate.PrecheckMany` over the batch with the tenant default profile; hits set `import_flag` (`dnc`, `opted_out`, `consent_missing`). Rows are **kept and flagged**, not deleted (they are refused at dial anyway; the report shows them).
   6. One transaction: insert into `dialer_contacts` (`ON CONFLICT (list_id, number_digits) DO NOTHING` → counted as duplicates), upsert `contact_consents` for consent columns, update counts, state COMMITTED. Any error → rollback, FAILED; nothing partial (old LLD rule kept).
   7. With `dedupe_scope=TENANT_ACTIVE`, numbers already in another ACTIVE list of the tenant are counted as `duplicate_across_lists` and skipped.
   8. Event `import.completed`; audit `dialer.list.import`.
4. **States:** import RECEIVING → VALIDATING → COMMITTED | FAILED; list IMPORTING → ACTIVE → ARCHIVED; campaign §5.1.
5. **Data:** §4.1, §4.2. **CSV columns** (header row required; mapping maps file headers to these, unmapped headers → `attributes`):

   | Field | Required | Rule |
   |---|---|---|
   | `phone` | yes | E.164 or national with `default_country` |
   | `external_id` | no | ≤ 128; stable id for CRM round-trip |
   | `first_name`, `last_name` | no | ≤ 128 each |
   | `timezone` | no | IANA name, else row rejected `timezone_invalid` |
   | `country` | no | ISO alpha-2; default the list's |
   | `region` | no | ISO 3166-2, e.g. `US-FL` |
   | `postal_code` | no | stored; zone lookup by postal code is R2 |
   | `line_type` | no | `wireless|landline|voip` |
   | `consent_type`, `consent_source`, `consent_at` | no | `none|express|express_written`, ≤ 256, RFC 3339 |
   | any other | no | `attributes`: ≤ 50 keys, key `^[a-z][a-z0-9_]{0,63}$`, value ≤ 1024 chars |

   **Limits:** 50 MB and 100 000 rows per file (installation setting,
   range 1 000–1 000 000); one RECEIVING/VALIDATING import per list.
   **Segments:** `segment` is a JSON array of allowlisted predicates
   `{field, op ∈ {eq, ne, in, prefix, gt, lt, exists}, value}` over the
   fixed columns and `attributes.<key>`; compiled to parameterised SQL by
   a pure function; never raw SQL (old rule kept). ≤ 20 predicates.
6. **Ports:**
   ```go
   func ParseRow(rec []string, m Mapping, defCountry string, phones PhoneParser) (ContactRow, RowError) // pure
   func CompileSegment(preds []Predicate) (sqlWhere string, args []any, err error)                  // pure
   type ListStore interface { StageRows(ctx, tx, importID, []ContactRow) error; CommitImport(ctx, tx, importID) (Counts, error) }
   ```
7. **API:** `ImportContacts`, `ListLists`, `ArchiveList`, `ListContacts`,
   `PreviewSegment`, `CreateCampaign`, `UpdateCampaign`, `GetCampaign`,
   `ListCampaigns`, `StartCampaign`, `PauseCampaign`, `StopCampaign`,
   `DuplicateCampaign`, `AttachList`, `DetachList` (§7).
8. **Config:** import size/rows; `dedupe_scope` default `LIST`; campaign
   fields §4.2 with defaults.
9. **Failure:** client disconnect mid-stream → FAILED `aborted`, staging
   deleted; app restart during VALIDATING → a sweeper marks imports older
   than 30 min FAILED `interrupted`; concurrent `StartCampaign` and
   `UpdateCampaign` → version check, loser `ABORTED`; pause/stop is a
   single row update read by the pacer every tick (1 s) and by the hopper
   lease, so no attempt is created after ≤ 2 s (AC-14.4 budget 10 s).
10. **Invariants/events:** INV-01 (emergency rows rejected), INV-10,
    INV-12 (edits never touch live calls). Events `list.imported`,
    `campaign.created|started|paused|resumed|completed|stopped`.
11. **Tests:**

    | # | Tier | Given / When / Then |
    |---|---|---|
    | L1 | U | rows with `+44…`, `020…` (default GB), `911`, `abc` / ParseRow / E.164, E.164, `emergency_number`, `number_invalid` |
    | L2 | U | segment with `op: "; DROP"` / CompileSegment / error; with valid predicates / parameterised SQL only |
    | L3 | I | CSV of 100 001 rows / import / `RESOURCE_EXHAUSTED`, no contacts written |
    | L4 | I | CSV with a DB failure injected at commit / import / FAILED, zero contacts (no partial commit) |
    | L5 | I | same number twice in a file and once in another active list with `TENANT_ACTIVE` / import / 1 accepted, 1 duplicate, 1 across-lists duplicate |
    | L6 | I | DNC number in file / import / accepted with `import_flag=dnc` |
    | L7 | I | campaign RUNNING / `UpdateCampaign{mode}` / `FAILED_PRECONDITION` |
    | L8 | I | campaign STOPPED / `StartCampaign` / `FAILED_PRECONDITION` |
    | L9 | W | every import refusal / its Connect code and console words |
    | L10 | C | import wizard maps headers and shows per-line errors (vitest) |
    | L11 | B | manager imports a CSV, builds a segment, starts and pauses a campaign |
    | L12 | E | RUNNING power campaign on the rig / `PauseCampaign` / no `POST /channels/create` after 10 s (ARI log); bridged call still up (AC-14.4, INV-03) |
12. **Parity:** VICIdial lists belong to one campaign and use list mix by
    percentage; Five9 lists are reusable with "list penetration"; Amazon
    Connect uses segments. We use reusable lists, per-campaign lead state
    and weighted list mix, plus scrub-at-import flags rather than deletion
    (evidence survives).

### 6.2 OB-02 — Preview dialling (R1.0)

1. **Trigger/actors:** agent (`dialer.agent`) in a CONNECTED session on a
   PREVIEW campaign clicks *Next contact*.
2. **Preconditions:** session CONNECTED; agent `Ready`; campaign RUNNING;
   skip budget not exhausted.
3. **Sequence:**
   1. `NextContact` → dialler: `SELECT … FROM dialer_hopper WHERE campaign_id=$1 AND state='READY' AND (owner_principal_id IS NULL OR owner_principal_id=$agent) ORDER BY (owner_principal_id IS NOT NULL) DESC, priority DESC, inserted_at, id FOR UPDATE SKIP LOCKED LIMIT 1`; set `LEASED`, `leased_to`, `lease_expires_at = now + preview_ttl_s`; agent `Ready → Reserved`.
   2. `Gate.Precheck` (advisory) — if refused, row returns per §5.2 and step 1 repeats (≤ 5 times, then `NOT_FOUND` "No contact can be called right now").
   3. Console shows contact, history, attributes, TTL countdown.
   4. Agent clicks *Dial* → `DialPreview(lease)`: one tx — insert attempt QUEUED (kind PREVIEW, `agent_reserved=true`); `Gate.Decide`; if refused → BLOCKED, lead per §5.2, console shows reason, agent back to `Ready`.
   5. `Capacity.ValidateCapacity(tenant, attemptCallID, 1)`; `PickPoolNumber` + `Presentation.Choose` (§6.12); refused → NOT_PLACED.
   6. `POST /channels/create` (endpoint `PJSIP/<number>@<route>` chosen by `pbx-core` route selection, `app=atsapbx`, `appArgs=dialer,<attempt>`, presentation from OB-12 via `pbx-core` `ChoosePresentation`, which alone sets `CALLERID(num)`/`CALLERID(name)`/`P-Asserted-Identity` (LLD-03 §3.12 archtest); `TALK_DETECT(set)` unset in R1.0); `POST /bridges/{session}/addChannel` (customer, **before** dial, so the agent hears ringback, carrier tones and early media); `POST /channels/{id}/dial?timeout=<RingTimeout>`; attempt DIALING.
   7. `Dial` events → RINGING / ANSWER. On ANSWER: attempt BRIDGED immediately (already in the bridge); agent `Busy`; recording per policy (`POST /bridges/{id}/record` via telephony-core).
   8. Hang-up (`ChannelDestroyed` customer, or agent's *Hang up* → `DELETE /channels/{customer}`) → agent `WrapUp`; disposition → DISPOSED.
   9. *Skip* → `SkipPreview(lease, reason)`: hopper row deleted, lead READY with `next_eligible_at = now + 15 min`; skip counted; agent `Ready`.
   10. TTL expiry (sweeper every 5 s): `RETURN` → as skip with reason `timeout`; `DIAL` → step 4 automatically.
4. **States:** §5.2–§5.4; preview cannot abandon (the agent is in the bridge before dial), so it is **excluded** from the abandonment ratio (LLD-04 §7.3).
5. **Data:** hopper lease columns; attempt `kind=PREVIEW`.
6. **Ports:** `Hopper.Lease(ctx, tx, campaign, agent) (HopperRow, error)`; `Gate`; `Capacity`; `Presentation`; `Media`.
7. **API:** `NextContact`, `DialPreview`, `SkipPreview`, `HangUp`, `RecordDisposition`.
8. **Config:** `preview_ttl_s` 120 (30–600); `preview_timeout_action` RETURN; `preview_skip_limit_per_hour` 0 (0–100); skip re-eligibility 15 min (fixed).
9. **Failure:** agent closes the browser during preview → lease expires → RETURN; agent leg lost while customer ringing → customer channel hung up only **if not answered** (`DELETE /channels` with `normal_clearing` during ring is permitted: nothing is "in progress"; ring time already ≥ minimum is not required because no abandon can occur — documented); engine refuses create → NOT_PLACED `engine`, capacity released (D-58 idempotent).
10. **Invariants/events:** INV-09 (Decide at Dial, not at Next), BR-06. Events `attempt.placed`, `attempt.completed`.
11. **Tests:**

    | # | Tier | Given / When / Then |
    |---|---|---|
    | P1 | I | two agents call `NextContact` concurrently / — / different rows (SKIP LOCKED) |
    | P2 | I | contact goes on DNC between Next and Dial / Dial / BLOCKED `dnc_tenant`, no channel |
    | P3 | I | lease 120 s, no action / 125 s / row returned, lead READY+15 min |
    | P4 | I | skip limit 2/h, third skip / — / `RESOURCE_EXHAUSTED` "Skip limit reached for this hour" |
    | P5 | E | preview on rig / Dial / agent hears ringback via bridge; answer → two-way audio < 300 ms after 200 OK |
    | P6 | B | agent sees contact, countdown, dials, disposes in two clicks (AC-14.6) |
12. **Parity:** VICIdial "MANUAL" + "dial timeout"; Five9 preview with
    *skip* and *auto-dial after timeout*; Amazon Connect preview with
    timeout. Difference: the agent's nailed-up leg, so preview and power
    share one agent-leg lifecycle.

### 6.3 OB-03 — Power dialling (R1.0)

1. **Trigger/actors:** the pacer (system) for a RUNNING POWER campaign;
   agents in CONNECTED sessions.
2. **Preconditions:** campaign RUNNING; ≥ 1 agent `Ready`; the pacer holds
   the per-campaign advisory lock `pg_try_advisory_lock(hash('dialer', campaign_id))` (one active pacer across app instances; HLD-21 §3 pattern).
3. **Sequence (each tick, `tick_interval` 1 s):**
   1. Read campaign row (state, `effective_ratio`, `max_concurrent`); if not RUNNING, stop launching.
   2. `ready` = agents in this campaign's CONNECTED sessions with state `Ready` (LLD-03 `AgentStates.CountReady`); `in_flight` = attempts of this campaign in QUEUED, DIALING, RINGING, ANSWERED.
   3. **Reserved mode** (`effective_ratio = 1.0`): `n = ready − in_flight_unbridged`; each slot reserves one specific agent (`Ready → Reserved`, conditional update; HLD-21 blended rule) and the attempt carries `agent_reserved=true`.
      **Dial-ahead mode** (`effective_ratio > 1.0`): `target = floor(effective_ratio × ready + 1e-9)` (only `Ready` agents count; rounding is floor, so 1.5 × 3 = 4); `n = target − in_flight`. No agent is reserved at dial time.
   4. `n = min(n, max_concurrent − in_flight, max_new_per_tick, hopper READY rows)`; `n ≤ 0` → done.
   5. Lease `n` hopper rows: same `FOR UPDATE SKIP LOCKED` query with `LIMIT n`.
   6. For each, one tx: attempt QUEUED; `Gate.Decide` (kind DIALER, `AgentReserved`); refused → BLOCKED; else `Capacity.ValidateCapacity`; `PickPoolNumber` + `Presentation.Choose`; commit; attempt DIALING after `POST /channels/create` + `POST /channels/{id}/dial?timeout=RingTimeout` (customer **not** in any bridge while ringing; the agent hears nothing).
   7. On `Dial` ANSWER: start the connect clock (LLD-04 §7.4).
      - reserved: `POST /bridges/{agentBridge}/addChannel` customer → BRIDGED; zip tone `POST /channels/{agent}/play?media=sound:beep` in parallel when `zip_tone`.
      - dial-ahead: pick the longest-`Ready` agent of the campaign with `UPDATE agents SET state='Busy' … WHERE state='Ready' … RETURNING` (one row, `SKIP LOCKED`); found → addChannel → BRIDGED. Not found → wait on agent-ready notifications until `deadline − 200 ms`; still none → OB-07 abandon path.
   8. `ledger.RecordLiveAnswer` in the BRIDGED/ABANDONED tx.
   9. Customer or agent hang-up → `ChannelLeftBridge`/`ChannelDestroyed` → `Capacity.ReleaseCapacity(callID)`; agent `WrapUp`; disposition → DISPOSED; lead transition.
4. **States:** §5.1–§5.4.
5. **Data:** §4.3–§4.5.
6. **Ports (pure parts marked):**
   ```go
   // PowerSlots is pure: how many attempts to launch this tick.
   func PowerSlots(in PacerInput) int
   type PacerInput struct {
       EffectiveRatio float64; Ready, InFlight, InFlightUnbridged int
       MaxConcurrent, MaxNewPerTick, HopperReady int
   }
   type AgentStates interface {          // LLD-03 owns the implementation
       CountReady(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, campaign shared.CampaignID) (int, error)
       Reserve(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, principal shared.PrincipalID) (bool, error)  // Ready→Reserved
       ClaimLongestReady(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, campaign shared.CampaignID) (*shared.PrincipalID, error) // Ready→Busy
       Transition(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, principal shared.PrincipalID, ev AgentEvent) error
   }
   type Media interface { // telephony-core's ARI gateway, extended (LLD-01 §11)
       CreateChannel(ctx context.Context, req CreateChannelRequest) (ChannelRef, error)  // POST /channels/create
       Dial(ctx context.Context, ch ChannelRef, timeout time.Duration) error            // POST /channels/{id}/dial
       AddChannelToBridge(ctx context.Context, b BridgeID, ch ChannelRef) error
       StartPlayback(ctx context.Context, ch ChannelRef, mediaURI string) (string, error)
       DestroyChannel(ctx context.Context, ch ChannelRef) error
   }
   type Capacity interface {
       ValidateCapacity(ctx context.Context, tenant shared.TenantID, callID string, n int) (CapacityVerdict, error)
       ReleaseCapacity(ctx context.Context, callID string) error
   }
   ```
7. **API:** campaign control (§7); agent `JoinCampaign`, `LeaveCampaign`, `SetPause`, `SetReady`, `RecordDisposition`, `HangUp`.
8. **Config:** `ratio` 1.0 (1.0–3.0, step 0.1, campaign); `max_concurrent` 10 (1–500, campaign; also ≤ licence capacity); `tick_interval` 1 s (250 ms–5 s, installation); `max_new_per_tick` 10 (1–100, installation); `ring_timeout_s` 25 (≥ profile `min_ring`, ≤ 60).
9. **Failure:** pacer instance dies → lock released by session end; another instance acquires within one tick; in-flight attempts are reconciled from `dialer_attempts` + live engine channels (`GET /channels`, matched by `appArgs`); an attempt in DIALING with no engine channel for > ring timeout + 10 s → FAILED `lost`, capacity released. Engine (Asterisk) restart → all agent legs and customer legs gone → sessions RECONNECTING, attempts FAILED `engine_restart`, campaign keeps RUNNING but launches nothing until ≥ 1 session reconnects. Agent becomes `NotReady` between reserve and answer (reserved mode) → reservation holds the agent in `Reserved` (cannot pause until released; pause request queued). Two pacers (lock bug) → the partial unique index on live numbers and the conditional agent update prevent double dial and double assignment.
10. **Invariants/events:** INV-09 by schema CHECK; capacity is a ceiling (AC-14.1a); INV-03 pause never cancels ringing or bridged calls. Events `attempt.placed`, `attempt.answered`, `attempt.completed`.
11. **Tests:**

    | # | Tier | Given / When / Then |
    |---|---|---|
    | PW1 | U | ratio 1.5, ready 3, in-flight 2 / PowerSlots / 2 (floor 4.5 = 4 − 2) |
    | PW2 | U | max_concurrent 5, in-flight 5 / — / 0 |
    | PW3 | U | property: PowerSlots never exceeds MaxConcurrent − InFlight or HopperReady, never negative |
    | PW4 | I | ratio 1.0, 2 Ready agents / tick / 2 attempts, each `agent_reserved` with a distinct agent |
    | PW5 | I | two pacers on one campaign / ticks / one holds the lock; no duplicate number live (unique index) |
    | PW6 | I | capacity refuses / slot / attempt NOT_PLACED, lead not charged an attempt, no channel |
    | PW7 | E | 2 agents, ratio 1.0, rig SIP UAs answering / 10 min / every answered call bridged; zero ABANDONED |
    | PW8 | A | 10 agents, clean list, 2 h (rig load harness) / power at configured ratio / concurrency never > limit; only answered calls reach agents (AC-14.1a) |
12. **Parity:** VICIdial `auto_dial_level` 1.0–4.0 with `RATIO`; Five9
    power = lines per agent; Amazon Connect *progressive* with bandwidth.
    Our default 1.0 is "progressive" (cannot abandon); > 1.0 is gated by
    LLD-04's abandonment rules from R1.0.

### 6.4 OB-06 — Abandonment control (R1.0 power, R2 predictive)

1. **Trigger/actors:** the abandonment evaluator (system, per RUNNING campaign, every 10 s); compliance officer configures the profile (`compliance.policy.manage`); manager resumes (`dialer.campaign.control`).
2. **Preconditions:** campaign mode POWER or PREDICTIVE; profile loaded.
3. **Sequence:**
   1. `AbandonLedger.Stats(campaign, now)` → live, abandoned.
   2. Pure `AbandonControl(state, stats, policy, cfg) → (effectiveRatio, action)`.
   3. Warm-up: `live < abandon_min_sample` (100) → `effective_ratio = 1.0` (reserved mode; cannot abandon, so the first abandon cannot be 100 %).
   4. OK (`ratio < limit − margin`): `effective_ratio` steps up by `+0.1` per 60 s towards the configured `ratio` (after 10 min continuous OK when recovering).
   5. APPROACHING: `effective_ratio = max(1.0, effective − 0.2)` **immediately** and again every 60 s while approaching — the first step lands within ≤ 10 s, inside AC-15.3's 60 s. Event `abandon.approaching` once on entry.
   6. BREACHED: campaign → PAUSED(ABANDON_LIMIT), `effective_ratio = 1.0`; LLD-04 rule 12 refuses any unreserved attempt in flight to `Decide`. Event `abandon.breached`; alert to supervisors and compliance officers.
   7. Resume after breach: `StartCampaign` allowed, but the campaign runs **locked in reserved mode** (`effective_ratio = 1.0`) until the ratio is < limit − margin; the console says so.
4. **States:** campaign §5.1; evaluator state OK/APPROACHING/BREACHED stored in `dialer_campaign_stats` (derivable; no separate table).
5. **Data:** LLD-04 `compliance_abandon_buckets`, `compliance_abandon_events`; `dialer_campaigns.effective_ratio`.
6. **Ports:** `func AbandonControl(cur ControlState, s compliance.AbandonStats, p compliance.Policy, configured float64, now time.Time) (ControlState, float64, ControlAction)` — **pure**.
7. **API:** `GetAbandonmentStatus` (LLD-04); campaign `GetCampaign` returns `effective_ratio`, `abandon_state`.
8. **Config:** profile fields (LLD-04 §4.1); evaluator period 10 s (installation, 5–30 s); step down 0.2, step up 0.1/min, recovery hold 10 min (installation).
9. **Failure:** ledger read fails → treat as BREACHED for this tick (fail closed: reserved mode), alert after 3; evaluator instance dies → it runs inside the pacer lock holder, so failover is the pacer's.
10. **Invariants/events:** AC-15.3, AC-15.6; INV-03 (pause leaves live calls). Events `compliance.abandon.*` (LLD-04) and `dialer.campaign.paused{reason: ABANDON_LIMIT}`.
11. **Tests:**

    | # | Tier | Given / When / Then |
    |---|---|---|
    | A1 | U | live 50 / AbandonControl / ratio 1.0 (warm-up) |
    | A2 | U | 1000/26 (2.6 %), effective 2.0 / — / 1.8 now; after 60 s still approaching → 1.6 |
    | A3 | U | 1000/30 / — / PAUSE(ABANDON_LIMIT), ratio 1.0 |
    | A4 | U | recovered to 2.0 % for 10 min / — / +0.1 per minute, capped at configured |
    | A5 | I | buckets seeded at 2.9 %, one abandon pushes to 3.0 % / evaluator tick / campaign PAUSED within 10 s; bridged calls untouched |
    | A6 | I | breached campaign resumed / pacer / only reserved attempts; Decide permits them |
    | A7 | A | simulated campaign crossing 2.5 % / — / slow-down observed ≤ 60 s (AC-15.3) |
12. **Parity:** VICIdial adaptive methods (`ADAPT_HARD_LIMIT`,
    `ADAPT_TAPERED`) and `drop_rate_group`; Five9 max abandon rate.
    Difference: warm-up in reserved mode, and a breach stops dial-ahead
    rather than only lowering the ratio.

### 6.5 OB-04 — Predictive pacing (R2; specified now)

1. **Trigger/actors:** the pacer for a PREDICTIVE campaign (mode refused in R1.0 with `FAILED_PRECONDITION` "Predictive dialling is not available in this release").
2. **Preconditions:** OB-03 loop proven in production (D-27); `abandon_min_sample` reached, else reserved mode.
3. **Sequence:** identical to OB-03 except step 3: `target = PredictivePace(obs, cfg)`.
4. **States:** as OB-03.
5. **Data:** `dialer_campaign_stats` (15-min window: attempts, answers, mean ring-to-answer, mean handle = talk + wrap).
6. **Function (pure):**
   ```go
   type Observation struct {
       Ready int                 // agents Ready now
       Busy []time.Duration      // elapsed time of each Busy/WrapUp agent
       InFlight int              // ringing attempts
       AnswerRate float64        // p, answers/attempts over window W (≥ 50 answers)
       MeanHandle, MeanRingToAnswer time.Duration
       RollingAbandonPct float64 // from the ledger
   }
   type PredictiveConfig struct {
       TargetAbandonPct float64  // = profile limit − margin (2.5)
       MaxRatio float64          // 3.0
       Horizon time.Duration     // = MeanRingToAnswer (agents freed before the next answers land)
       Gain float64              // 0.5, feedback on measured vs target
   }
   func PredictivePace(o Observation, c PredictiveConfig) (dialsInFlightTarget int)
   ```
   **Algorithm:**
   - Expected free agents: `A = Ready + Σ_busy P(free within Horizon)`, with `P = min(1, Horizon / max(1 s, MeanHandle − elapsed))`.
   - Expected abandon fraction with `N` calls in flight: `X ~ Binomial(N, p)`; `f(N) = E[max(0, X − ⌊A⌋)] / (N·p)`.
   - Corrected target: `t = TargetAbandonPct × clamp(1 − Gain·(RollingAbandonPct − TargetAbandonPct)/TargetAbandonPct, 0.2, 1.0)`.
   - Result: the largest `N ≤ MaxRatio × (Ready + len(Busy))` with `f(N) ≤ t/100`, never below `Ready` (power-1.0 floor). If `AnswerRate` sample < 50 → return `Ready`.
7. **API:** campaign `mode=PREDICTIVE` (R2), metrics only.
8. **Config:** W 15 min (5–60), MaxRatio 3.0 (1–5), Gain 0.5 (0–1); target from profile.
9. **Failure:** NaN/zero inputs → returns `Ready` (safe floor). The function can only change N; compliance and capacity apply after it.
10. **Invariants:** f is monotone non-decreasing in N; result monotone non-increasing in RollingAbandonPct.
11. **Tests (test model):**

    | # | Tier | Given / When / Then |
    |---|---|---|
    | R1 | U | p 0 / — / Ready |
    | R2 | U | property: higher p ⇒ N not higher; higher rolling abandon ⇒ N not higher; N ≤ MaxRatio·agents |
    | R3 | U | **discrete-event simulator** (seeded RNG, in-repo): 10 agents, p = 0.25, ring-to-answer ~ U(5, 20) s, handle ~ lognormal(mean 180 s, σ 0.5), wrap 15 s, 2 h / run 50 seeds / abandonment over run ≤ 3 % in every seed and mean talk ≥ 45 min per agent-hour (AC-14.1) |
    | R4 | U | simulator with p dropping 0.25 → 0.10 at minute 60 / — / no 10-minute window above 3 % |
    | R5 | A | rig load harness, 10 agents, 2 h / predictive / AC-14.1 |
12. **Parity:** VICIdial `ADAPT_*` (dial level adjusted on drop %); Five9
    and Amazon Connect predictive with target abandon rate. Ours is a
    pure, simulated, published function with a 1.0 floor.

### 6.6 OB-05 — Compliance gate on every attempt (R1.0)

1. **Trigger/actors:** every dialler attempt, preview Dial, callback, and agent manual dial (`dialer.manual_dial`).
2. **Preconditions:** campaign profile (or tenant default for manual dial outside a campaign).
3. **Sequence:** attempt QUEUED and `Gate.Decide` in **one** transaction (LLD-04 §5); refused → BLOCKED with reason, lead per §5.2; permitted → continue. Hopper refill uses `PrecheckMany` (no rows). **Manual dial:** `ManualDial{number, campaign_id?}` → lead lookup `dialer_contacts` by `(tenant, number_digits)` (most recent list member in an attached list; ambiguity → the console lets the agent pick) → attempt `kind=MANUAL` with `agent_reserved=true` → Decide → OB-02 steps 5–8 through the agent's session bridge (or the agent's device via telephony-core if no session).
4. **States:** attempt QUEUED → BLOCKED | DIALING.
5. **Data:** `dialer_attempts.compliance_decision_id` + CHECK.
6. **Ports:** `compliance.Gate` (LLD-04 §5.1).
7. **API:** `ManualDial` → `{attempt_id, permitted, reason}`; refused → `FAILED_PRECONDITION` with detail `reason`, console words per reason (`outside_hours`: "It is {local time} for this contact; calling hours are {window}"; `dnc_*`: "This number is on a do-not-call list"; `opted_out`: "This contact has opted out"; `compliance_unavailable`: "Calling is paused: the compliance check is unavailable").
8. **Config:** none beyond the profile.
9. **Failure:** Decide error → tx rollback, no attempt row; pacer backs off 5 s; 3 consecutive → alert.
10. **Invariants:** INV-09/BR-10 (schema CHECK + single entry point); INV-01 (emergency numbers refused by `ManualDial` with `INVALID_ARGUMENT` "Emergency calls are placed from the phone, not the dialler"; the phone path is untouched).
11. **Tests:** LLD-04 §11 C1–C25 plus:

    | # | Tier | Given / When / Then |
    |---|---|---|
    | G1 | I | attempt in DIALING with null decision / insert / DB CHECK violation |
    | G2 | W | `ManualDial` to a DNC number / — / `FAILED_PRECONDITION`, `reason=dnc_tenant`; no channel |
    | G3 | E | DNC number via power and via manual dial / — / never on the trunk (AC-15.1, release suite) |
12. **Parity:** VICIdial checks DNC at hopper load and optionally at
    manual dial; Five9 scrubs at load. We decide at the dial instant,
    every path, no override.

### 6.7 OB-07 — Minimum ring and abandon message (R1.0)

1. **Trigger/actors:** pacer (ring timeout); answer with no agent by the deadline.
2. **Preconditions:** profile `abandon_message_required` ⇒ media present (start guard §5.1).
3. **Sequence:**
   1. Dial with `timeout = RingTimeout(policy, ring_timeout_s)` (US ≥ 15 s; default 25 s ≈ 4 US ring cycles of 6 s). The dialler never cancels a ringing attempt earlier — not on pause, stop, or breach.
   2. On ANSWER with no agent claimable by `deadline − 200 ms`: `POST /channels/{customer}/answer` is implicit (already up); `POST /channels/{customer}/play?media=<abandon_message_media_uri>` starts **before the deadline**; `message_started_ms` recorded.
   3. `ChannelDtmfReceived` digit = `abandon_optout_digit` → `OptOuts.RecordOptOut(source=ABANDON_IVR)` in a tx; `POST /channels/{id}/play?media=sound:<confirmation>`.
   4. `PlaybackFinished` → `DELETE /channels/{customer}?reason=normal`.
   5. Tx: attempt ABANDONED, `ledger.RecordLiveAnswer` + `RecordAbandon(message_played, started_ms, opted_out)`; lead RETRY_WAIT with `next_eligible_at ≥ abandoned_at + no_redial_after_abandon`.
   6. Customer hangs up before the deadline while waiting → ABANDONED as well (FCC counts it), message not played.
   7. An agent becoming Ready **after** the message started is not connected (the customer has been told the call was abandoned).
4. **States:** attempt ANSWERED → ABANDONED.
5. **Data:** LLD-04 `compliance_abandon_events`.
6. **Ports:** `compliance.Policies`, `AbandonLedger`, `OptOuts`; `Media.StartPlayback`.
7. **API:** profile RPCs (LLD-04); attempt shows `outcome=ABANDONED`, `message_played`.
8. **Config:** profile `min_ring_seconds` 15, `abandon_connect_limit_ms` 2000, `abandon_optout_digit` "9"; claim margin 200 ms (installation, 100–500).
9. **Failure:** playback fails (`PlaybackFinished` with failure or media missing) → hang up immediately and record `message_played=false` + alert (a compliance incident, visible to the auditor); engine down mid-message → attempt ABANDONED on reconcile.
10. **Invariants:** AC-15.6. Events `attempt.abandoned`.
11. **Tests:**

    | # | Tier | Given / When / Then |
    |---|---|---|
    | M1 | U | profile 15 s, campaign 10 s / RingTimeout / 15 s |
    | M2 | I | campaign paused while an attempt rings / — / the attempt runs to its timeout or answer |
    | M3 | E | ratio 2.0, 1 agent busy, 2nd customer answers (rig UA) / — / playback starts < 2 s after answer (ARI timestamps); DTMF 9 → opt-out row |
    | M4 | E | unanswered rig UA / — / hang-up not before 15 s (SIP trace CANCEL time) |
12. **Parity:** VICIdial `drop_call_seconds` + `safe_harbor_message`;
    Five9 abandon message. Ours measures from answer (stricter) and
    stores the message timing per abandon.

### 6.8 OB-08 — Voicemail drop and AMD (R1.0 agent drop; R2 AMD)

1. **Trigger/actors:** R1.0 — agent (`dialer.agent`) clicks *Drop voicemail* while BRIDGED. R2 — AMD classifier on answer.
2. **Preconditions:** campaign `voicemail_drop_media_uri` set; contact consent `EXPRESS_WRITTEN` when the profile is US_FCC (a prerecorded telemarketing message; OWNER DECISION LLD-04 §12.4); button hidden otherwise.
3. **Sequence (R1.0):** agent hears the greeting and beep → *Drop* → `POST /bridges/{agent}/removeChannel` customer; `POST /channels/{customer}/play?media=<drop>`; agent → `WrapUp` immediately (auto-disposition `ANSWERING_MACHINE`, outcome `VOICEMAIL_DROPPED`); `PlaybackFinished` → `DELETE /channels/{customer}`. The message is never played to a human the agent is talking to (the agent decides).
   **Sequence (R2 AMD):** before dial, `POST /channels/{id}/variable` `TALK_DETECT(set)=500,128`; on answer, classify from `ChannelTalkingStarted/Finished` (baseline) and optionally an AI plugin on a snoop copy (LLD-10, D-23): parameters `initial_silence` 2500 ms, `greeting` 1500 ms, `after_greeting_silence` 800 ms, `total_analysis_time` 5000 ms (capped by the connect deadline − 300 ms under `amd_counts_against_connect`), `min_word_length` 100 ms, `between_words_silence` 50 ms, `max_words` 2; AI deadline default 1200 ms, on miss → **human**. Machine → wait for silence ≥ 1000 ms after the last `ChannelTalkingFinished` (beep proxy; SPIKE-D3) → play drop (if allowed) → hang up; outcome MACHINE / VOICEMAIL_DROPPED.
4. **States:** attempt BRIDGED → MACHINE (R1.0); ANSWERED → MACHINE (R2).
5. **Data:** media URIs point into the tenant's object store (same store as recordings; closes old open decision §10.2); upload via the existing media/prompt facility.
6. **Ports:** `Media`; `func ClassifyTalk(events []TalkEvent, p AMDParams) (Class, decided bool)` — **pure** (R2).
7. **API:** `DropVoicemail{attempt_id}` → `{}`; refusals `FAILED_PRECONDITION` "This campaign has no voicemail message", "This contact has not given written consent for recorded messages".
8. **Config:** campaign media; AMD params per campaign (R2) within the ranges of app_amd.
9. **Failure:** drop playback fails → hang up, outcome MACHINE, alert; AMD/AI failure → human (INV-04).
10. **Invariants:** INV-04; AC-14.2 is R2. Events `attempt.completed{outcome}`.
11. **Tests:** V1 (I) drop without media → refused; V2 (E) agent drop on rig → agent free < 1 s, customer UA receives the audio; V3 (U, R2) `ClassifyTalk` table of greetings → class; V4 (A, R2) labelled corpus ≥ 95 % accuracy, ≤ 2 % humans as machine (AC-14.2).
12. **Parity:** VICIdial AMD via app_amd + "AMD send to VM" message;
    Five9 "VM drop" one click; Amazon Connect AMD with "await prompt".
    R1.0 ships the agent click only.

### 6.9 OB-09 — Dispositions, retry rules, callbacks (R1.0)

1. **Trigger/actors:** agent `RecordDisposition`, `ScheduleCallback`; system auto-dispositions; callback scheduler; manager `RecycleLeads` (`dialer.campaign.manage`).
2. **Preconditions:** attempt belongs to the agent's session and is not DISPOSED; callback time permitted by `Precheck` and in the future (≥ 5 min, ≤ 90 days).
3. **Sequence:**
   1. `RecordDisposition{attempt, code, note}` → tx: attempt DISPOSED; `NextAction(code)` → lead transition (§5.2) using `RetryPlan` (pure); `OPT_OUT` → `OptOuts.RecordOptOut(AGENT)`; `DNC_CAMPAIGN` → campaign DNC row; agent `WrapUp → Ready`.
   2. System outcomes with no agent (NO_ANSWER, BUSY, ABANDONED, CARRIER_FAILED…) are auto-disposed with the outcome as key.
   3. `ScheduleCallback{attempt, local_datetime, contact_zone (prefilled from LLD-04 zone derivation), owner: AGENT|ANY, note}` → convert to UTC; `Precheck` at that instant → refused `outside_hours` → `FAILED_PRECONDITION` "That time is outside the contact's calling hours ({window})"; insert SCHEDULED; lead CALLBACK.
   4. **Scheduler:** a worker (one per installation under advisory lock `hash('dialer-callbacks')`, tick 15 s) selects `state='SCHEDULED' AND due_at <= now() + 60 s FOR UPDATE SKIP LOCKED LIMIT 500` → DUE; inserts the hopper row priority 100 (`owner_principal_id` for AGENT). For an AGENT callback whose owner has no CONNECTED session at `due_at + 5 min`: `callback_fallback_to_any` (default true) → hopper row loses the owner; else MISSED `owner_absent`, returned to the owner's list in the console.
   5. In PREVIEW the row is offered first to its owner (OB-02 query); in POWER it is dialled in reserved mode to the owner if Ready, else waits up to 5 min then falls back.
   6. Delivered = attempt DIALING; `delivered_at` recorded; COMPLETED after disposition. Not delivered by `due_at + 15 min` (campaign paused, no agents) → MISSED `campaign_paused|no_agents`; lead READY priority 50; event `callback.missed`.
4. **States:** callback SCHEDULED → DUE → OFFERED → DIALING → COMPLETED | MISSED | CANCELLED; lead §5.2.
5. **Data:** §4.2 retry rules, §4.6 callbacks.
6. **Ports:**
   ```go
   // RetryPlan is pure.
   func RetryPlan(lead LeadCounters, outcomeKey string, rules RetryRules, caps Caps, policy compliance.Policy, now time.Time, zone *time.Location) LeadTransition
   func CallbackDue(now time.Time, cbs []Callback, lookahead time.Duration) []CallbackID // pure
   ```
7. **API:** `RecordDisposition`, `ScheduleCallback`, `CancelCallback`, `ListCallbacks` (mine / campaign), `RecycleLeads{campaign, statuses[], delay_minutes ≥ 5, limit ≤ 100000, reset_attempts}`, `ListDispositions`, `ReplaceDispositions`, `ReplaceRetryRules`.
8. **Config (defaults):** retry rules NO_ANSWER 120 min / BUSY 15 / MACHINE 240 / ABANDONED 1440 (and ≥ profile no-redial) / NO_DECISION 1440 / AGENT_TIMEOUT 60 / CARRIER_FAILED 5 (no attempt charged); `max_attempts_total` 5 (1–50); `max_attempts_per_day` 3 (1–20; some US states cap repeat calls — tenant sets it); `min_redial_minutes` 60; backoff 1.0; callback lookahead 60 s, tick 15 s, fallback 5 min, miss 15 min.
9. **Failure:** scheduler instance dies → lock moves; a DUE row with no hopper row older than 2 min is re-inserted (idempotent: hopper UNIQUE (campaign, contact)); disposition for an attempt already auto-disposed (wrap-up timeout) → overwrite allowed once within 10 min, audited; contact zone invalid → callback refused `timezone_unknown`.
10. **Invariants:** AC-14.3 (punctuality metric `callback_delivery_lag_seconds`, SLO p95 ≤ 300 s); a callback is still a compliance-gated attempt (INV-09). Events `callback.scheduled|due|missed|completed`; audit for `RecycleLeads`, rule changes.
11. **Tests:**

    | # | Tier | Given / When / Then |
    |---|---|---|
    | D1 | U | NO_ANSWER 3rd attempt, max 5, delay 120, backoff 2 / RetryPlan / RETRY_WAIT +480 min |
    | D2 | U | attempt 5 of 5 / — / EXHAUSTED |
    | D3 | U | 3 attempts today, max/day 3, zone Asia/Tokyo / — / next eligible = next Tokyo midnight (in UTC) |
    | D4 | U | ABANDONED, profile no-redial 72 h, rule 24 h / — / +72 h |
    | D5 | U | CARRIER_FAILED 2nd today / — / attempts_total unchanged |
    | D6 | I | callback at 20:55 contact time, window to 21:00 / schedule / accepted; at 21:05 / refused |
    | D7 | I | 1000 callbacks due in the same minute, fake clock / scheduler / all in hopper ≤ 75 s after due (AC-14.3 at I tier) |
    | D8 | I | AGENT callback, owner logged out / due + 5 min / hopper row has no owner |
    | D9 | B | agent disposes in two clicks; schedules a callback in the contact's zone with the zone shown (AC-14.6, US-14.7) |
    | D10 | A | rig run of 50 callbacks / — / ≥ 95 % within 5 min (AC-14.3) |
12. **Parity:** VICIdial lead recycle (status, delay, max), callbacks
    `USERONLY`/`ANYONE`; Five9 dial plans per disposition with redial
    timers; Amazon Connect has no per-agent callbacks. We add the
    compliance check at scheduling and at dial.

### 6.10 OB-10 — Attempt evidence (R1.0)

1. **Trigger/actors:** every attempt (system); viewers with `dialer.attempt.read`; export `dialer.attempt.export`.
2. **Preconditions:** none.
3. **Sequence:** the attempt row is written at QUEUED in the same tx as `Decide` (blocked attempts included, AC-14.5); each transition updates it; `call_id` links to the call record; hang-up cause: `ChannelDestroyed.cause` (Q.850) always; SIP code best effort from `HANGUPCAUSE(<channel>,tech)` read on `ChannelHangupRequest` (SPIKE-D1).
4. **States:** §5.3.
5. **Data:** §4.4; retention 760 days.
6. **Ports:** `AttemptStore.Insert/Transition` (transition table enforced in Go: `func NextAttemptState(s AttemptState, ev AttemptEvent) (AttemptState, error)` — **pure**).
7. **API:** `ListAttempts{contact_id | campaign_id, period ≤ 92 days, outcome?}` → attempts with masked number, decision reason, presented number, cause; `ExportAttempts` (server-streamed CSV, formula-escaped, audited).
8. **Config:** retention 760 days (installation, ≥ 730).
9. **Failure:** event publish failure never blocks (outbox, INV-06).
10. **Invariants:** INV-08 (no DELETE grant), INV-10. Events `attempt.*`.
11. **Tests:** E1 (U) every illegal transition refused; E2 (I) blocked attempt appears on the contact with reason; E3 (W) export escapes `=cmd` cells; E4 (A) every rig attempt visible on the contact and in the API (AC-14.5).
12. **Parity:** VICIdial `vicidial_log` + `vicidial_dial_log`; Amazon Connect contact records. Ours links each attempt to its compliance decision.

### 6.11 OB-11 — Carrier failure during a campaign (R1.0)

1. **Trigger/actors:** engine results (system); D-80 trunk health (FL-03); administrator.
2. **Preconditions:** campaign RUNNING.
3. **Sequence:**
   1. Route selection for each attempt is `pbx-core`'s (D-80): a degraded route is skipped; the next route is used (PRD §12.2 steps 2–3).
   2. Each `Dial` result maps to an outcome:

      | `dialstatus` | Q.850 / SIP | Outcome | Attempt charged | Carrier-fail sample |
      |---|---|---|---|---|
      | ANSWER | 16 / 200 | (answered) | yes | 0 |
      | BUSY | 17 / 486, 600 | BUSY | yes | 0 |
      | NOANSWER | 18, 19 / 408, 480, timeout | NO_ANSWER | yes | 0 |
      | CONGESTION | 21 / 603 | REJECTED | yes | 0 |
      | CONGESTION | 1, 28 / 404, 484 | INVALID_NUMBER | yes | 0 |
      | CONGESTION | 34, 38, 41, 42 / 500, 502, 503, 504 | CARRIER_FAILED | no (≤ 3/day) | 1 |
      | CHANUNAVAIL | 20, 27 / no response, 408 from proxy | CARRIER_FAILED | no (≤ 3/day) | 1 |
      | CANCEL | 487 | (only on engine restart) FAILED | no | 0 |
   3. `dialer_campaign_stats.recent_results` ring (last `carrier_fail_window` = 50); when ≥ 20 samples and fail % ≥ `carrier_fail_pct` (50) → campaign PAUSED(CARRIER_FAILURE); alert "Calls are failing at the carrier: {pct} % of the last {n}". Calls in progress continue (PRD §12.2 step 5).
   4. Administrator resumes manually once routes recover (no auto-resume: D-85 principle).
4. **States:** campaign RUNNING → PAUSED(CARRIER_FAILURE).
5. **Data:** §4.7.
6. **Ports:** `func MapDialResult(dialStatus string, q850 int, sip int) Outcome` — **pure**; `func CarrierTrip(ring []int16, window, pct int) bool` — **pure**.
7. **API:** `GetCampaign` → `pause_reason`, `carrier_fail_pct_recent`.
8. **Config:** `carrier_fail_window` 50 (20–500), `carrier_fail_pct` 50 (10–100), campaign.
9. **Failure:** all routes down → every attempt CARRIER_FAILED → trip after 20; no lead is exhausted by carrier failures (cap 3/day).
10. **Invariants:** INV-03; FL-03 alerting is D-80's. Event `campaign.paused{reason: CARRIER_FAILURE}`.
11. **Tests:** K1 (U) mapping table, row by row; K2 (U) CarrierTrip at 19 vs 20 samples; K3 (I) 20 × 503 → PAUSED, leads not charged; K4 (E) rig trunk to a UA returning 503 → campaign pauses, bridged call on another route unaffected.
12. **Parity:** VICIdial dial statuses `CONGESTION`/`DC` and no auto-pause; Five9 carrier failover. Our auto-pause protects contacts from burnt attempts.

### 6.12 OB-12 — Caller-ID selection (R1.0, BR-06)

The number inventory (`tenant_numbers`, states `pending → verified →
revoked`, evidence) and the pure precedence function
`ChoosePresentation` are **LLD-03 §3.12**. A dialler attempt is its
precedence row 1 ("campaign / number pool"): the dialler picks a
candidate from its pool and passes it as `PoolChoice`; `pbx-core`
verifies it and is the only code that writes the presented number. The
dialler designs no inventory table.

1. **Trigger/actors:** each attempt (system); manager configures pools and campaign caller ID (`dialer.campaign.manage`); numbers are added and verified by the tenant administrator in LLD-03 (`pbx.number.write`, `pbx.number.verify`).
2. **Preconditions:** every number used is `verified` (not `pending`/`revoked`) in `tenant_numbers`; `callback_number_id` is a verified `carrier_did` (it receives calls on the tenant's trunk, so it can take DNC requests — 16 CFR 310.4(a)(8) expects a number that can be called back).
3. **Sequence:**
   1. `StartCampaign` guard: `Presentation.Status` for `callerid_number_id`, every pool member, and `callback_number_id` → all `verified`; else `FAILED_PRECONDITION`.
   2. Per attempt, in the attempt tx after `Decide`: dialler's pure `PickPoolNumber(mode, members, contact)`:
      - `FIXED` → `callerid_number_id`;
      - `POOL` → member with the oldest `last_used_at` (`SELECT … FOR UPDATE SKIP LOCKED LIMIT 1`, then set `last_used_at = now()`);
      - `LOCAL_PRESENCE` → member with the contact's NPA, then the contact's region, then the pool LRU (OWNER DECISION §14.3).
   3. `Presentation.Choose(tx, tenant, {Origin: DIALER, PoolChoice: &number})` → LLD-03 `ChoosePresentation` row 1. `ErrNotOwned` (number revoked meanwhile) → attempt NOT_PLACED `callerid`; for POOL the member is skipped and the next tried once; for FIXED, or a pool with no verified member left → campaign PAUSED(CALLER_ID_INVALID) and alert.
   4. Telephony sets `CALLERID(num)`, `CALLERID(name)` (inventory `cnam`) and `P-Asserted-Identity` on `POST /channels/create`; `dialer_attempts.presented_number` stores the digits. Withholding is not offered for dialler attempts (TSR requires transmitting caller ID).
   5. STIR/SHAKEN: signing is the carrier's in R1.0; the inventory's evidence is what the partner shows the carrier to obtain A-attestation (LLD-03 `stir_attestation` placeholder).
4. **States:** inventory states are LLD-03's; campaign RUNNING → PAUSED(CALLER_ID_INVALID).
5. **Data:** §4.2 `dialer_callerid_pools`, `dialer_callerid_pool_members`, campaign `callerid_*_id`, `callback_number_id`; `dialer_attempts.presented_number`.
6. **Ports:**
   ```go
   // PickPoolNumber is pure: chooses a candidate; it does not authorise it.
   func PickPoolNumber(mode CallerIDMode, members []PoolMember, contactNPA, contactRegion string) (numberID shared.UUID, ok bool)
   // Implemented by pbx-core over LLD-03 §3.12 (NumberInventory + ChoosePresentation).
   type Presentation interface {
       Status(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, numberID shared.UUID) (NumberStatus, string /*digits*/, error)
       Choose(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, in DialerPresentation) (Presented, error) // ErrNotOwned
   }
   ```
7. **API:** `CreateCallerIDPool`, `ReplaceCallerIDPoolMembers{pool_id, number_ids[]}`, `ListCallerIDPools` (`dialer.campaign.manage` / `dialer.campaign.read`); campaign fields. Refusals `FAILED_PRECONDITION` "{number} is not a verified number of this organisation" (console links to the Numbers screen), "The callback number must be a verified number that receives calls".
8. **Config:** mode default FIXED; pool size 1–500 members.
9. **Failure:** inventory read error → NOT_PLACED `callerid` (fail closed, BR-06); number revoked while a call is up → that call continues (INV-03), the next attempt re-chooses.
10. **Invariants/events:** BR-06 (only `ChoosePresentation` sets the number); `pbx.presentation_refused` (LLD-03) on refusal plus `dialer.campaign.paused{reason: CALLER_ID_INVALID}`; audit on pool and caller-ID changes.
11. **Tests:** N1 (I) pending number at start → refused; N2 (U) `PickPoolNumber` LRU and LOCAL_PRESENCE fallbacks; N3 (I) POOL rotates across 3 numbers under two concurrent pacer txs (SKIP LOCKED, no reuse within a round); N4 (I) FIXED number revoked mid-run → next attempt NOT_PLACED, campaign PAUSED; N5 (E) rig SIP trace shows the chosen number in `From` and `P-Asserted-Identity`.
12. **Parity:** VICIdial campaign CID and "CID groups" by area code/state; Five9 local presence; Amazon Connect per-campaign number. Ours refuses any number not verified in the inventory and never falls back to an unverified one.

### 6.13 Cross-cutting agent features (R1.0)

| Feature | Design |
|---|---|
| **Pause codes** | `SetPause{code}` with codes from LLD-03's catalogue (defaults BREAK, LUNCH, TRAINING, MEETING, ADMIN; system codes `leg_lost`, `leg_failed`, `dispo_timeout`, `campaign_stopped` are not selectable). Pause during a call takes effect at end of wrap-up. Time per code is reported (LLD-09) |
| **Manual dial** | OB-05 §6.6 step 3: lead lookup, compliance, the agent's session bridge |
| **Transfer / 3-way with the lead** | Attended/blind transfer and conference are telephony-core call control (HLD-21 §1, LLD-01 §11): the customer leg moves from the session bridge into a call bridge; the agent's nailed leg stays in the session bridge; the attempt keeps the lead and the agent disposes it. An external third party is a normal trunk call through Screening (D-82/D-85), not a dialler attempt |
| **Recording** | `NONE`, `ALL` (from BRIDGED until the customer leaves), `ON_DEMAND` (agent start/stop); via telephony-core recording into the tenant object store; never records the idle nailed-up leg |
| **Licence capacity** | One reservation per **customer leg**, keyed by the attempt's call id (D-58), from QUEUED to the leg's end. The nailed-up agent leg is not reserved (OWNER DECISION §14.2) |

## 7. API contract (`dialer.v1.DialerService`)

| Permission | Default grant |
|---|---|
| `dialer.list.read`, `dialer.campaign.read`, `dialer.attempt.read` | TENANT_ADMIN, SUPERVISOR, proposed CAMPAIGN_MANAGER |
| `dialer.list.manage`, `dialer.campaign.manage` | TENANT_ADMIN, CAMPAIGN_MANAGER |
| `dialer.campaign.control` | TENANT_ADMIN, SUPERVISOR, CAMPAIGN_MANAGER |
| `dialer.attempt.export`, `dialer.contact.export` | TENANT_ADMIN, CAMPAIGN_MANAGER (audited) |
| `dialer.agent` (session, next, dial preview, disposition, callback, drop, pause) | AGENT, SUPERVISOR |
| `dialer.manual_dial` | AGENT, SUPERVISOR |

| RPC | Key request fields → response | Permission | Refusals (code — console words) |
|---|---|---|---|
| `ImportContacts` (client stream) | header `{list_id \| new_list, mapping, dedupe_scope}`, chunks → `{import_id, accepted, rejected, duplicates, flagged, errors[≤100]{line, reason}}` | list.manage | `INVALID_ARGUMENT` "Line {n}: {reason}"; `RESOURCE_EXHAUSTED` "The file is larger than {limit}"; `FAILED_PRECONDITION` "An import is already running for this list" |
| `ListLists`, `ArchiveList`, `ListContacts`, `PreviewSegment` | paging, `segment` → contacts (masked), count | list.read / list.manage | `INVALID_ARGUMENT` "Filter field {f} is not allowed" |
| `CreateCampaign`, `UpdateCampaign` (`version`, `update_mask`), `GetCampaign`, `ListCampaigns`, `DuplicateCampaign` | §4.2 fields | campaign.manage / read | `INVALID_ARGUMENT` per field range; `FAILED_PRECONDITION` "Pause the campaign to change this"; `ABORTED` "Someone else changed this campaign; reload"; `FAILED_PRECONDITION` "Predictive dialling is not available in this release" |
| `AttachList`, `DetachList` | `campaign_id, list_id, weight, priority, segment` | campaign.manage | `NOT_FOUND` |
| `StartCampaign`, `PauseCampaign`, `StopCampaign{cancel_callbacks}` | `campaign_id` → campaign | campaign.control | `FAILED_PRECONDITION` naming the failed guard ("No active list", "Caller ID not authorised", "The abandon message is missing", "Outside the campaign schedule", "A stopped campaign cannot restart; duplicate it") |
| `JoinCampaign`, `LeaveCampaign` | `campaign_id` → `{session_id, state}` | agent | `FAILED_PRECONDITION` "You are already in campaign {name}"; "No phone is logged in for you" |
| `SetReady`, `SetPause{code}` | → agent state | agent | `INVALID_ARGUMENT` "Unknown pause code" |
| `NextContact` | `session_id` → `{lease_id, contact, history[], expires_at}` | agent | `NOT_FOUND` "No contact can be called right now" |
| `DialPreview`, `SkipPreview{reason}` | `lease_id` → `{attempt_id}` | agent | `FAILED_PRECONDITION` + compliance reason; `RESOURCE_EXHAUSTED` "Skip limit reached for this hour" / "No line is free (licence capacity)" |
| `ManualDial` | `number, campaign_id?` → `{attempt_id, contact?}` | manual_dial | as OB-05 |
| `HangUp`, `DropVoicemail` | `attempt_id` | agent | `FAILED_PRECONDITION` as OB-08 |
| `RecordDisposition` | `attempt_id, code, note ≤ 1000` | agent | `FAILED_PRECONDITION` "This call is already closed" |
| `ScheduleCallback`, `CancelCallback`, `ListCallbacks` | `attempt_id, local_datetime, zone, owner, note` | agent | `FAILED_PRECONDITION` "That time is outside the contact's calling hours ({window})" |
| `RecycleLeads` | `campaign_id, statuses[], delay_minutes, limit, reset_attempts` → `{count}` | campaign.manage | `INVALID_ARGUMENT` "Delay must be at least 5 minutes" |
| `ReplaceDispositions`, `ReplaceRetryRules` | lists | campaign.manage | `FAILED_PRECONDITION` "Code {c} is in use" |
| `ListAttempts`, `ExportAttempts` | filters, period ≤ 92 days | attempt.read / export | `INVALID_ARGUMENT` "Period may not exceed 92 days" |
| `WatchCampaign` (server stream) | `campaign_id` → live counters every 2 s | campaign.read | — |

No RPC dials without `Decide`; no field sets a verdict; engine
identifiers never appear in any message (D-19).

## 8. Configuration summary

| Setting | Default | Range | Scope |
|---|---|---|---|
| Import file size / rows | 50 MB / 100 000 | 1 000–1 000 000 rows | installation |
| `ratio` | 1.0 | 1.0–3.0 (step 0.1); PREVIEW fixed 1.0 | campaign |
| `max_concurrent` | 10 | 1–500 and ≤ licence capacity | campaign |
| `ring_timeout_s` | 25 | max(10, profile min ring)–60 | campaign |
| `wrapup_max_s` | 120 | 0–900 (0 = immediate auto-dispose) | campaign |
| `preview_ttl_s` / action / skips per hour | 120 / RETURN / 0 | 30–600 / RETURN,DIAL / 0–100 | campaign |
| Pacer tick / max new per tick | 1 s / 10 | 250 ms–5 s / 1–100 | installation |
| Hopper target | `clamp(ceil(ratio × sessions × 5), 20, 2000)` rows | multiplier 2–20 | installation |
| Hopper refill | when READY < 50 % of target, checked every 5 s | 1–30 s | installation |
| Hopper stale / lease (power) | 15 min / 60 s | 5–60 min / 10–300 s | installation |
| Abandon evaluator period, step down/up, recovery hold | 10 s, 0.2, 0.1/min, 10 min | 5–30 s | installation |
| Claim margin before deadline | 200 ms | 100–500 ms | installation |
| Agent leg originate timeout / reconnects | 30 s / 3 × 5 s | 10–60 s / 0–10 | installation |
| Callback tick / lookahead / fallback / miss | 15 s / 60 s / 5 min / 15 min | — | installation |
| Retry defaults, caps | §6.9 | §4.2 checks | campaign |
| Carrier trip | 50 / 50 % / 20 min samples | §6.11 | campaign |
| Attempt retention | 760 days | ≥ 730 | installation |

**Hopper ordering:** `priority DESC` (100 callbacks, 50 retries due,
then list priority 0–9), then list mix by weight (refill draws from each
attached list in proportion to `weight` among lists with eligible leads),
then `next_eligible_at`, then `contact_id`. Refill runs inside the
pacer's lock: `INSERT INTO dialer_hopper SELECT … FROM dialer_leads JOIN
dialer_contacts … WHERE next_eligible_at <= now() AND state IN (…) AND
segment … ORDER BY … LIMIT k ON CONFLICT DO NOTHING`, then
`PrecheckMany`; refused rows are removed and the lead moved per §5.2.

## 9. Events, audit and metrics

Events (outbox → NATS, `tenant.<id>.event.dialer.<action>`; payloads
never contain a full number — `contact_id` and `number_last4` only):

| Action | Payload |
|---|---|
| `list.imported` | `list_id, import_id, accepted, rejected, duplicates, flagged` |
| `campaign.created|started|resumed|paused|completed|stopped` | `campaign_id, state, reason, actor?` |
| `attempt.placed` | `attempt_id, campaign_id, contact_id, kind, presented_number` |
| `attempt.answered` | `attempt_id, call_id, answered_at` |
| `attempt.abandoned` | `attempt_id, message_played, message_started_ms` |
| `attempt.completed` | `attempt_id, outcome, disposition, q850_cause, sip_code, duration_ms` |
| `session.joined|lost|ended` | `session_id, principal_id, campaign_id, reason` |
| `callback.scheduled|due|missed|completed` | `callback_id, due_at, owner, reason?` |

Audit (`AppendAuditTx`, before/after): campaign create/update/state
change, list import/archive, attach/detach, dispositions, retry rules,
recycle, attempt/contact export, caller-ID changes, disposition
overwrite.

Metrics (Prometheus, labels `tenant`, `campaign`):
`dialer_attempts_total{outcome}`, `dialer_attempts_in_flight`,
`dialer_effective_ratio`, `dialer_ready_agents`, `dialer_answer_rate`,
`dialer_connect_latency_seconds` (answer→bridge histogram),
`dialer_abandon_ratio_percent`, `dialer_compliance_blocks_total{reason}`,
`dialer_hopper_ready`, `dialer_callback_delivery_lag_seconds`,
`dialer_agent_occupancy_ratio`, `dialer_carrier_fail_ratio`,
`dialer_next_contact_seconds` (SLO p95 < 300 ms). Alerts: campaign
auto-paused (any reason), compliance unavailable, abandon approaching,
carrier trip, capacity ceiling hit for 5 min, connect latency p95 > 1 s.

## 10. Test plan summary and Definition of Done

Tier tags: U unit, I integration (`atsapbx_test` only), W wire, E e2e
on the rig, C console vitest, B browser (Playwright), A acceptance/UAT.
Flow tables above hold the scenarios (L, P, PW, A, R, G, M, V, D, E, K,
N series) plus LLD-04 C1–C25. Release suite (AC-15.1) = G3 + C23 + M3 +
M4 + L12, run before every release.

DoD R1.0: AC-14.1a, 14.3, 14.4, 14.5, 14.6 (via US-14.6 tests), US-14.5
(agent drop), AC-15.1–15.6 proven at their tiers; every test above
tagged R1.0 green; the predictive simulator (R3) exists and passes as a
unit test even though the mode is disabled. R2: AC-14.1, AC-14.2,
US-14.4.

## 11. Failure and recovery (cross-flow)

| Event | Behaviour | No orphan because |
|---|---|---|
| App instance restart | pacer/scheduler locks move; reconcile: attempts in DIALING/RINGING/ANSWERED matched to `GET /channels` by `appArgs`; sessions matched to bridges; unmatched → FAILED `lost`, capacity released | every engine resource carries the attempt/session id in `appArgs`/bridge name |
| Asterisk restart | all legs gone; attempts FAILED `engine_restart` (not charged); sessions RECONNECTING; capacity released | D-58 idempotent release |
| Compliance store down | no attempt created; pacer backs off; campaign stays RUNNING; alert | fail closed |
| Customer hangs up while being claimed | `ChannelDestroyed` before addChannel → ABANDONED (if after answer) | agent claim rolled back to Ready |
| Agent hangs up phone mid-call | session RECONNECTING; customer hung up; attempt to WrapUp with the agent | §5.4 |
| Concurrent pause and tick | tick re-reads state inside its tx with `FOR SHARE`; pause commits first → tick launches nothing | AC-14.4 |
| Duplicate contact in two campaigns | partial unique index refuses the second live attempt → NOT_PLACED `duplicate_live`, lead returned | INV: one live call per person |

## 12. Invariants

| Rule | Mechanism |
|---|---|
| INV-01 | emergency numbers rejected at import (`emergency_number`) and at `ManualDial`; no dialler path uses the emergency matcher |
| INV-03 | pause/stop/breach/carrier trip never touch BRIDGED, ABANDONED-message or ringing attempts |
| INV-04 | AMD/AI undecided or failed = human |
| INV-09 / BR-10 | schema CHECK + `Decide` in the attempt tx; Precheck cannot produce a decision id |
| INV-10 | RLS on every table; advisory-lock keys include tenant-scoped ids only |
| BR-06 | `Presentation.Status` at start, `Presentation.Choose` per attempt (LLD-03 §3.12); fail closed |

## 13. SPIKEs (before the OpenSpec changes)

| ID | Question | Blocks |
|---|---|---|
| SPIKE-D1 | Can ARI read the SIP response code (`HANGUPCAUSE(<chan>,tech)`) on `ChannelHangupRequest` for a channel made with `/channels/create` + `/dial`? Fallback: Q.850 only | OB-10/11 `sip_code` |
| SPIKE-D2 | `/channels/create` + `addChannel` before `/dial`: does the agent hear early media (183) through a mixing bridge on Asterisk 22? | OB-02 step 6 |
| SPIKE-D3 (R2) | TALK_DETECT accuracy for AMD and beep proxy vs `app_amd` via `continueInDialplan` | OB-08 R2 |
| SPIKE-D4 | Answer→bridge latency with the nailed-up leg on the rig (target < 300 ms p95) | OB-03/07 |

## 14. OWNER DECISIONS (recommended defaults)

1. **Voicemail drop in R1.0.** *Recommend:* agent-triggered one-click drop
   only (US-14.5); AMD and automatic drop (US-14.4, AC-14.2) stay R2 with
   FBR-R2-02. HLD-20 OB-08 should read "R1.0 agent drop; R2 AMD + auto
   drop".
2. **Licence counting for the nailed-up leg.** *Recommend:* count only
   customer legs; the agent leg is internal, and counting it would halve
   a tenant's usable channels for the same work. LLD-08 must confirm.
3. **Local presence.** *Recommend:* FIXED and POOL in R1.0;
   LOCAL_PRESENCE available but off by default and only from inventory
   numbers tagged with NPA/region (truthful, owned numbers; counsel to
   confirm state rules).
4. **Phone-number library dependency** (E.164 validation, e.g. a Go
   port of libphonenumber). *Recommend:* adopt; hand-rolled validation
   would mis-handle national formats. Needs owner approval (AGENTS.md).
5. **Default power ratio.** *Recommend:* 1.0 (cannot abandon); managers
   raise it deliberately.
6. **New role `CAMPAIGN_MANAGER`** (PRD persona) in LLD-02
   `DefaultPermissions`. *Recommend:* add with §7 grants.

## 15. Handoff

OpenSpec changes, in order, after LLD-04's `compliance-profiles-and-gate`:
1. `dialer-lists-import` (0053, OB-01 import/segments);
2. `dialer-campaigns` (0054, campaign lifecycle, dispositions, retry rules);
3. `dialer-agent-session` (0057, nailed-up leg, pause, SPIKE-D2/D4 first);
4. `dialer-preview` (0055–0056 hopper/leads/attempts, OB-02, OB-05, OB-10, OB-12 — after LLD-03 `0035_number_inventory`);
5. `dialer-power` (pacer, ratio 1.0 only, OB-03, OB-07 min ring, OB-11; 0059);
6. `dialer-abandonment` (after `compliance-abandonment`: ratio > 1.0, OB-06, OB-07 message);
7. `dialer-callbacks` (0058, OB-09 scheduler);
8. R2: `dialer-predictive` (OB-04), `dialer-amd` (OB-08 R2).

**References:** [PRD EPIC-14/15](../PRD.md), [BRD](../BRD.md),
[HLD 04 §10.1](../hld/04-bounded-contexts.md),
[HLD-20](../hld/20-call-flow-catalogue.md),
[HLD-21](../hld/21-media-and-call-control-flows.md),
[LLD-04](LLD-04-compliance.md), [LLD-03](LLD-03-pbx-core.md),
[LLD-08](LLD-08-licensing.md), [LLD README](README.md),
[Asterisk ARI](https://docs.asterisk.org/Configuration/Interfaces/Asterisk-REST-Interface-ARI/),
[VICIdial](https://www.vicidial.org/docs/),
[Amazon Connect outbound campaigns](https://docs.aws.amazon.com/connect/latest/adminguide/enable-outbound-campaigns.html).
