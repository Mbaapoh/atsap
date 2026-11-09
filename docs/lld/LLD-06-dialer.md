# LLD-06 — Dialer (preview and power R1.0; predictive R2)

| | |
|---|---|
| **Bounded context** | `dialer` (Tier 2, HLD 04 §10.1) |
| **Status** | **Draft — design only, rewritten to the Flow Definition of Ready (D-98) on 2026-11-07.** R1.0 is preview and power with the abandonment rules; predictive pacing and AMD are R2 (D-27, D-45, D-97) |
| **Traces to** | BRD **FBR-R1-19**, FBR-R2-01, FBR-R2-02, FBR-R2-03, BR-06, BR-10; PRD EPIC-14 (US-14.1–14.7, AC-14.1–14.5), EPIC-15 (US-15.3, US-15.6, AC-15.1–15.6), §12.2; HLD-20 OB-01–OB-12; HLD-21 §3–§4; DECISIONS D-21, D-27, D-45, D-56, D-58, D-65, D-80, D-82, D-85, D-95, D-97, D-98 |
| **Depends on** | LLD-04 compliance (**release gate**), LLD-03 agents (§3.1.1 `AgentStates`) and number inventory/`ChoosePresentation` (§3.12), `telephony-core` `DialerMedia`/`DialerEvents` ports (LLD-01 §23.3; telephony creates the Call and reserves licence capacity per attempt, LLD-08), LLD-09 reporting |
| **Migrations** | **Provisional labels (D-100):** `0053`–`0057`, `0059` here, `0050`–`0052`, `0058` in LLD-04 show grouping and order only; each OpenSpec change takes the next free numbers in `api/migrations` when proposed |
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
| 9 | Dialler called `ValidateCapacity` and drove ARI directly | **Telephony originates** through `DialerMedia` (LLD-01 §23.3): it creates the Call and `call_id`, reserves capacity (LLD-08: only telephony calls `ValidateCapacity` per call) and releases it on termination | LLD-08, D-58 |
| 10 | Minimum ring measured from dial start | **From the first 180/183** (`ChannelStateChange` → `Ringing`, or `Dial` `RINGING`/`PROGRESS`); nothing — system, agent, pause — cancels a ringing leg before `ring_started_at + min_ring` (§6.7) | 47 CFR 64.1200(a)(6) |
| 11 | Abandonment breach only paused the campaign | Breach **stops** it: state `STOPPED_COMPLIANCE`; only a compliance officer resumes, in reserved mode (OWNER DECISION §14.7) | AC-15.3 "stops" |

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
| Agent state machine (`Ready`, `Reserved`, `Busy`, `WrapUp`, `NotReady(reason)`) and pause-code catalogue | `pbx-core` (LLD-03 §3.1.1) | port `AgentStates` (§6.3); wake-up on `NOTIFY dialer_wake` written by LLD-03 on every commit into `Ready` |
| Verdicts, abandonment counters, profiles | `compliance` (LLD-04) | ports `Gate`, `AbandonLedger`, `OptOuts`, `Policies` |
| Presentation numbers | `pbx-core` (LLD-03 §3.12 `tenant_numbers`, `ChoosePresentation`) | port `Presentation` (§6.12) |
| Licence capacity | `licensing` (LLD-08), reserved **by telephony** when it originates | nothing directly; `DialerMedia.PlaceAttempt` returns `ErrCapacity` |
| Calls, channels, bridges, playback, recording | `telephony-core` | ports `DialerMedia` (commands) and `DialerEvents` (sink the dialler implements), §6.3 |
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

Default dispositions (agent, two clicks, US-14.6), each mapped to a
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

## 4. Data model (provisional migrations 0053–0057, 0059; D-100)

| Migration | Change (§15 order) | Tables |
|---|---|---|
| 0050, 0051 | `compliance-profiles-and-gate` (LLD-04) | profiles, windows, DNC, opt-outs, consents |
| 0052 | `compliance-evidence` (LLD-04) | `compliance_decisions` |
| 0053 | `dialer-lists-import` | lists, contacts, imports |
| 0054 | `dialer-campaigns` | campaigns, list mix, caller-ID pools, dispositions, retry rules |
| 0055 | `dialer-agent-session` | `dialer_agent_sessions` |
| 0056 | `dialer-preview` | leads, hopper, attempts, the INV-09 trigger |
| 0057 | `dialer-power` | `dialer_campaign_stats` (pacer, carrier and abandonment-evaluator state) |
| 0058 | `compliance-abandonment` (LLD-04) | `compliance_abandon_buckets`, `compliance_abandon_events` |
| 0059 | `dialer-callbacks` | `dialer_callbacks` |

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
  state VARCHAR(18) NOT NULL DEFAULT 'DRAFT' CHECK (state IN ('DRAFT','RUNNING','PAUSED','COMPLETED','STOPPED','STOPPED_COMPLIANCE')),
  pause_reason VARCHAR(24) NOT NULL DEFAULT '',  -- MANUAL|CARRIER_FAILURE|CALLER_ID_INVALID|COMPLIANCE_BLOCKING|SCHEDULE|NO_AGENTS; ABANDON_LIMIT with STOPPED_COMPLIANCE
  policy_id UUID NOT NULL,                        -- compliance profile (LLD-04)
  ratio NUMERIC(3,1) NOT NULL DEFAULT 1.0 CHECK (ratio BETWEEN 1.0 AND 3.0),
  effective_ratio NUMERIC(3,1) NOT NULL DEFAULT 1.0,   -- pacer-owned (slow-down)
  max_concurrent INT NOT NULL DEFAULT 10 CHECK (max_concurrent BETWEEN 1 AND 500),
  ring_timeout_s SMALLINT NOT NULL DEFAULT 25 CHECK (ring_timeout_s BETWEEN 10 AND 60),
  wrapup_max_s SMALLINT NOT NULL DEFAULT 120 CHECK (wrapup_max_s BETWEEN 0 AND 900),
  preview_ttl_s SMALLINT NOT NULL DEFAULT 55 CHECK (preview_ttl_s BETWEEN 30 AND 240),   -- reservation budget, §6.2 item 2 (service check)
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
  callback_fallback_to_any BOOLEAN NOT NULL DEFAULT TRUE,
  setup_allowance_s SMALLINT NOT NULL DEFAULT 30 CHECK (setup_allowance_s BETWEEN 5 AND 60),  -- dial → first 180/183; ≥ profile min_ring (service check, §6.7)
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

### 4.3 `0056_dialer_preview` (part 1: leads and hopper)

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
  priority SMALLINT NOT NULL DEFAULT 0 CHECK (priority IN (0, 50, 100)),  -- copied to the hopper: 100 callback, 50 retry/missed callback, 0 list order
  blocked_reason VARCHAR(32) NOT NULL DEFAULT '',                         -- why BLOCKED_WAIT (data fix resets next_eligible_at)
  next_eligible_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (campaign_id, contact_id)
);
CREATE INDEX dialer_leads_eligible ON dialer_leads (campaign_id, next_eligible_at) WHERE state IN ('READY','RETRY_WAIT','BLOCKED_WAIT');
CREATE TABLE dialer_hopper (
  id BIGSERIAL PRIMARY KEY, tenant_id UUID NOT NULL, campaign_id UUID NOT NULL,
  contact_id UUID NOT NULL, list_id UUID NOT NULL, number_digits VARCHAR(15) NOT NULL,
  priority SMALLINT NOT NULL,                  -- 100 callback, 50 retry, 0–9 list priority
  callback_id UUID, owner_principal_id UUID,   -- agent-owned callback offered only to this agent
  not_before TIMESTAMPTZ NOT NULL DEFAULT now(),   -- callbacks: due_at; never leased earlier
  state VARCHAR(7) NOT NULL DEFAULT 'READY' CHECK (state IN ('READY','LEASED')),
  leased_to UUID, lease_expires_at TIMESTAMPTZ,
  inserted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (campaign_id, contact_id), UNIQUE (campaign_id, number_digits)
);
CREATE INDEX dialer_hopper_pick ON dialer_hopper (campaign_id, state, priority DESC, inserted_at, id);
```

Grants: `SELECT, INSERT, UPDATE, DELETE`; `USAGE, SELECT` on the sequence.

### 4.4 `0056_dialer_preview` (part 2: attempts)

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
  queued_at TIMESTAMPTZ NOT NULL, dialed_at TIMESTAMPTZ, ring_started_at TIMESTAMPTZ, cancel_requested_at TIMESTAMPTZ, answered_at TIMESTAMPTZ,
  connect_clock_at TIMESTAMPTZ, bridged_at TIMESTAMPTZ, ended_at TIMESTAMPTZ, disposed_at TIMESTAMPTZ,
  CHECK (state IN ('QUEUED','NOT_PLACED') OR compliance_decision_id IS NOT NULL),
  CHECK (state <> 'BLOCKED' OR block_reason <> '')
);
-- INV-09: a CHECK cannot see another table, so a trigger proves the verdict.
CREATE FUNCTION dialer_attempt_verdict_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.compliance_decision_id IS NULL THEN RETURN NEW; END IF;   -- QUEUED / NOT_PLACED
  IF NEW.state = 'BLOCKED' THEN
    IF NOT EXISTS (SELECT 1 FROM compliance_decisions d WHERE d.id = NEW.compliance_decision_id
                   AND d.tenant_id = NEW.tenant_id AND d.attempt_id = NEW.id AND NOT d.permitted) THEN
      RAISE EXCEPTION 'blocked attempt % without a refusing decision', NEW.id USING ERRCODE = 'check_violation';
    END IF;
  ELSIF NOT EXISTS (SELECT 1 FROM compliance_decisions d WHERE d.id = NEW.compliance_decision_id
                    AND d.tenant_id = NEW.tenant_id AND d.attempt_id = NEW.id AND d.permitted) THEN
    RAISE EXCEPTION 'attempt % past QUEUED without a permitting decision', NEW.id USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER dialer_attempt_verdict_guard BEFORE INSERT OR UPDATE OF state, compliance_decision_id
  ON dialer_attempts FOR EACH ROW EXECUTE FUNCTION dialer_attempt_verdict_guard();
CREATE UNIQUE INDEX dialer_attempts_one_live_per_number ON dialer_attempts (tenant_id, number_digits)
  WHERE state IN ('DIALING','RINGING','ANSWERED','BRIDGED');   -- never two live calls to one person
CREATE INDEX dialer_attempts_contact ON dialer_attempts (tenant_id, contact_id, queued_at DESC);
CREATE INDEX dialer_attempts_campaign ON dialer_attempts (tenant_id, campaign_id, queued_at DESC);
```

Grants: `SELECT, INSERT, UPDATE` (no `DELETE`; retention job as owner,
**760 days** like LLD-04, because an `ABANDONED` attempt is part of the
two-year abandonment record).

### 4.5 `0055_dialer_agent_sessions`

```sql
CREATE TABLE dialer_agent_sessions (
  id UUID PRIMARY KEY, tenant_id UUID NOT NULL, principal_id UUID NOT NULL,
  campaign_id UUID NOT NULL REFERENCES dialer_campaigns(id),
  device_endpoint VARCHAR(128) NOT NULL,      -- resolved from LLD-03 hot-desk device; never on the wire
  bridge_id VARCHAR(64), agent_call_id UUID,  -- engine refs kept server-side only
  state VARCHAR(12) NOT NULL CHECK (state IN ('CONNECTING','CONNECTED','RECONNECTING','ENDED')),
  reconnects SMALLINT NOT NULL DEFAULT 0, current_attempt_id UUID,
  last_offered_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch',   -- reserved-mode fairness (§6.3 step 3)
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(), ended_at TIMESTAMPTZ, end_reason VARCHAR(24) NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX dialer_agent_sessions_one_open ON dialer_agent_sessions (tenant_id, principal_id) WHERE state <> 'ENDED';
```

### 4.6 `0059_dialer_callbacks`

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

### 4.7 `0057_dialer_campaign_stats`

```sql
CREATE TABLE dialer_campaign_stats (          -- pacer observations, one row per campaign, overwritten
  tenant_id UUID NOT NULL, campaign_id UUID PRIMARY KEY REFERENCES dialer_campaigns(id) ON DELETE CASCADE,
  recent_results SMALLINT[] NOT NULL DEFAULT '{}', -- ring of last N engine results: 1 = carrier fail
  answered_15m INT NOT NULL DEFAULT 0, attempts_15m INT NOT NULL DEFAULT 0,
  mean_handle_ms INT NOT NULL DEFAULT 0, mean_ring_to_answer_ms INT NOT NULL DEFAULT 0,
  -- abandonment evaluator state (§6.4), owned by the dialler
  abandon_state VARCHAR(12) NOT NULL DEFAULT 'WARMUP' CHECK (abandon_state IN ('WARMUP','OK','APPROACHING','BREACHED')),
  recovery_hold_until TIMESTAMPTZ, last_step_at TIMESTAMPTZ,
  reserved_only_lock BOOLEAN NOT NULL DEFAULT FALSE,   -- set on breach; cleared when ratio < limit − margin
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Grants on 0055, 0057, 0059: `SELECT, INSERT, UPDATE`.

## 5. State machines

### 5.1 Campaign

| From | Event | Guard | To | Side effects |
|---|---|---|---|---|
| — | `CreateCampaign` | valid fields; policy exists; mode ≠ PREDICTIVE in R1.0 | DRAFT | audit |
| DRAFT, PAUSED(MANUAL…) , COMPLETED | `StartCampaign` | ≥ 1 active list; policy valid (`abandon_message_media_uri` set when required); caller-ID numbers and `callback_number_id` `verified` (LLD-03 §3.12); dispositions ≥ 1; inside schedule | RUNNING | pacer/hopper start within 5 s; event `campaign.started`/`resumed` |
| RUNNING | `PauseCampaign` | — | PAUSED(MANUAL) | no new attempt after ≤ 10 s (AC-14.4); live and ringing attempts continue |
| RUNNING | breach (LLD-04 `Breached`), evaluator §6.4 | mode POWER or PREDICTIVE | STOPPED_COMPLIANCE (reason ABANDON_LIMIT) | `effective_ratio := 1.0`, `reserved_only_lock := true`; hopper emptied; ringing and live attempts continue (INV-03); alert supervisors and compliance officers |
| STOPPED_COMPLIANCE | `ResumeAfterCompliance{reason ≥ 10 chars}` | caller holds `dialer.campaign.compliance_resume` (COMPLIANCE_OFFICER); profile still valid | RUNNING in reserved mode (`reserved_only_lock` stays until ratio < limit − margin) | audit with reason; event `campaign.resumed{after: ABANDON_LIMIT}` |
| STOPPED_COMPLIANCE | `StartCampaign` by a manager | — | refused | `PERMISSION_DENIED` "A compliance officer must review and resume this campaign" |
| RUNNING | carrier detector (§6.11) | — | PAUSED(CARRIER_FAILURE) | alert |
| RUNNING | caller ID becomes unauthorised | — | PAUSED(CALLER_ID_INVALID) | alert |
| RUNNING | 20 consecutive `Decide` refusals of a policy-wide reason (`dnc_registry_stale`, `profile_missing`) | — | PAUSED(COMPLIANCE_BLOCKING) | alert |
| RUNNING | `schedule_end` passed | — | PAUSED(SCHEDULE) | — |
| RUNNING | no agent session for 30 min | mode ≠ PREVIEW | PAUSED(NO_AGENTS) | — |
| RUNNING | no lead eligible ever again (all leads terminal, no callbacks open) | checked every 5 min | COMPLETED | event |
| DRAFT, RUNNING, PAUSED, COMPLETED, STOPPED_COMPLIANCE | `StopCampaign` | — | STOPPED | callbacks cancelled if `cancel_callbacks=true`, else kept for a later campaign clone; hopper emptied; sessions told "campaign stopped" and returned to `NotReady(campaign_stopped)` after their current call |
| STOPPED | any start | — | refused | `FAILED_PRECONDITION` "A stopped campaign cannot restart; duplicate it" |

**COMPLETED vs STOPPED:** COMPLETED is the system's "nothing left to
dial"; attaching a list or a lead becoming eligible lets `StartCampaign`
resume it. STOPPED is the operator's final decision; it is terminal and
may be cloned (`DuplicateCampaign`). **A `STOPPED_COMPLIANCE` campaign**
may be cloned only by a holder of `dialer.campaign.compliance_resume`
(`PERMISSION_DENIED` "A compliance officer must approve copying a campaign
stopped for abandonment"), and the clone starts with
`reserved_only_lock = true` (it cannot abandon until its own ratio and
sample allow dial-ahead), so cloning never escapes the stop.

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
| IN_HOPPER | `Decide` (or refill `Precheck`) refuses `consent_missing`, `timezone_unknown` | BLOCKED_WAIT, `blocked_reason` set, `next_eligible_at = now + 24 h`; a consent upsert or contact edit for that number resets `next_eligible_at = now` (retryable: the data may be fixed) |
| IN_HOPPER | `Decide` refuses `number_invalid` | INVALID (terminal) |
| IN_HOPPER | attempt placed | DIALING |
| DIALING | outcome + next action RETRY, caps not reached | RETRY_WAIT, `priority = 50` (`next_eligible_at = now + delay × backoff^(n−1)`, ≥ `min_redial_minutes`, ≥ profile no-redial after ABANDONED) |
| DIALING | caps reached (`max_attempts_total`, or `max_for_outcome`) | EXHAUSTED (terminal; recyclable by `RecycleLeads`) |
| DIALING | `attempts_today = max_attempts_per_day` | RETRY_WAIT until next local midnight |
| DIALING | next action CALLBACK | CALLBACK |
| DIALING | COMPLETE / INVALID / OPT_OUT / DNC_CAMPAIGN | COMPLETED / INVALID / OPTED_OUT / DNC |
| CALLBACK | callback DUE | IN_HOPPER (`priority = 100`) |
| CALLBACK | callback MISSED | READY, `priority = 50` |
| EXHAUSTED, COMPLETED(with chosen dispositions) | `RecycleLeads{statuses, delay ≥ 5 min, limit}` | RETRY_WAIT with `attempts_total` reset only if `reset_attempts=true` |

`attempts_today` resets when `attempts_day` ≠ today in the contact's zone.

### 5.3 Attempt

| From | Event (ARI / domain) | To | Recorded |
|---|---|---|---|
| — | slot granted, row inserted | QUEUED | `queued_at` |
| QUEUED | `Decide` refuses | BLOCKED | decision id, `block_reason` |
| QUEUED | `Presentation.Choose` returns `ErrNotOwned`, or `DialerMedia.PlaceAttempt` returns `ErrCapacity` / `ErrDraining` / `ErrEmergencyRefused` / `ErrSessionDown` / engine error | NOT_PLACED | `block_reason` = `callerid` / `capacity` / `draining` / `emergency` / `session_down` / `engine`; reserved agent released |
| QUEUED | `Decide` permits and `DialerMedia.PlaceAttempt` accepted (telephony created the Call, reserved capacity, `POST /channels/create` + `/dial`) | DIALING | `dialed_at`, `call_id`, decision id, `presented_number` |
| DIALING | `DialerEvents.Progress` with `FirstAlertAt` (first 180/183) | RINGING | `ring_started_at` = `FirstAlertAt` (set once) — the minimum-ring clock |
| DIALING | no progress by `dialed_at + setup_allowance_s` | FAILED (`CARRIER_FAILED`, `no_progress`) | `setup_allowance_s ≥ min_ring` under US_FCC, so even a phone that rang without signalling 180/183 rang ≥ the minimum |
| RINGING | `HangUp`/agent leg lost/stop **before** `ring_started_at + min_ring` | RINGING | `cancel_requested_at`; `DialerMedia.Hangup` runs only at `ring_started_at + min_ring` |
| RINGING | `ring_started_at + RingTimeout` (dialler timer) or cancel due | NO_ANSWER | `DialerMedia.Hangup` |
| DIALING/RINGING | `Dial` `dialstatus=ANSWER` | ANSWERED | `answered_at`, `connect_clock_at` |
| ANSWERED | customer added to the agent bridge before the deadline | BRIDGED | `bridged_at`; ledger live answer |
| ANSWERED | deadline passed with no agent, or customer hung up before bridge | ABANDONED | ledger abandon; message played? |
| BRIDGED | agent marks machine and drops / disposes `ANSWERING_MACHINE` | MACHINE (outcome VOICEMAIL_DROPPED if dropped) | ledger: live answer **reversed is not allowed** — see note |
| DIALING/RINGING | `Dial` `NOANSWER` (backstop) / `BUSY` / `CONGESTION`, `CHANUNAVAIL` / `CANCEL` | NO_ANSWER / BUSY / FAILED / FAILED | `dial_status`, `q850_cause`, `sip_code`; reserved agent `ReservationReleased` |
| ANSWERED | app or engine restart, attempt unmatched on reconcile | ABANDONED (conservative; counted in the ledger) | never FAILED: a person answered and was not connected |
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
| CONNECTED | attempt reserved (ratio 1.0, preview, callback): `Reserve(principal, attemptRef)` | CONNECTED | `Ready → Reserved` (conditional update; TTL `campaign_reservation_seconds`) |
| CONNECTED | attempt ends NO_ANSWER, BUSY, FAILED, BLOCKED, NOT_PLACED; preview skip or lease expiry | CONNECTED | `Reserved → Ready` (`Transition(ReservationReleased)`) |
| CONNECTED | customer bridged | CONNECTED | `Reserved/Ready → Busy` |
| CONNECTED | customer leaves bridge | CONNECTED | `Busy → WrapUp` (timer `wrapup_max_s`) |
| CONNECTED | disposition recorded | CONNECTED | `WrapUp → Ready` (or `NotReady(reason)` if a pause was requested during the call) |
| CONNECTED | wrap-up timer expires | CONNECTED | auto-disposition `AGENT_TIMEOUT`; `WrapUp → NotReady(dispo_timeout)` |
| CONNECTED | `SetNotReady(code)` | CONNECTED | `Ready → NotReady(code)`; during a call: pending, applied after WrapUp |
| CONNECTED | `DialerEvents.AgentLegLost` (telephony's 30 s keepalive `GET /channels/{agent}` fails, `StasisEnd`, `ChannelDestroyed`) | RECONNECTING | `NotReady(leg_lost)`; a customer in the bridge is hung up (`normal_clearing`) — the conversation ended because a party left, not by platform condition |
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
   5. One transaction, in this order: (a) insert into `dialer_contacts` (`ON CONFLICT (list_id, number_digits) DO NOTHING` → counted as duplicates); (b) upsert `contact_consents` from the consent columns; (c) **then** scrub with `Gate.PrecheckMany` over the batch with the tenant default profile (LLD-04 `compliance_tenant_settings`; none set → the import is refused at step 1 with `FAILED_PRECONDITION` "Set a default compliance profile first") and each row's own context (zone, region, country) — after (b), so the file's own consent is seen; hits set `import_flag` (`dnc`, `opted_out`, `consent_missing`); rows are **kept and flagged**, not deleted (they are refused at dial anyway; the report shows them); (d) counts, state COMMITTED. Any error → rollback, FAILED; nothing partial (old LLD rule kept).
   6. A consent upsert for a number whose lead is BLOCKED_WAIT(`consent_missing`/`timezone_unknown`) sets that lead's `next_eligible_at = now()` (§5.2).
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
   skip budget not exhausted. **Reservation budget (decided: validate, no
   renewal):** the agent is `Reserved` from `NextContact` until the attempt
   bridges or is released, so the worst case is lease + setup + ring +
   connect deadline. `Create/UpdateCampaign` and `StartCampaign` refuse
   unless `preview_ttl_s + setup_allowance_s + max(ring_timeout_s,
   min_ring) + ceil(abandon_connect_limit) + 5 s slack ≤ LLD-03
   campaign_reservation_seconds` (`INVALID_ARGUMENT` "Preview time, setup
   allowance and ring time together exceed the agent reservation limit
   ({n} s)"). A delayed cancel needs no extra slack: it happens at
   `ring_started_at + min_ring ≤ ring_started_at + RingTimeout`. Defaults
   55 + 30 + 25 + 2 + 5 = 117 s fit LLD-03's default 120 s; the console
   computes and shows the budget. Power reserved mode uses the same rule without the
   preview term (30 + 25 + 2 + 5 = 62 s).
3. **Sequence:**
   1. `NextContact` → dialler: `SELECT … FROM dialer_hopper WHERE campaign_id=$1 AND state='READY' AND (owner_principal_id IS NULL OR owner_principal_id=$agent) AND not_before <= now() ORDER BY (owner_principal_id IS NOT NULL) DESC, priority DESC, inserted_at, id FOR UPDATE SKIP LOCKED LIMIT 1`; set `LEASED`, `leased_to`, `lease_expires_at = now + preview_ttl_s`; insert the attempt row QUEUED (no decision yet) and `AgentStates.Reserve(tx, tenant, agent, attemptID)` (`Ready → Reserved`, `reserved_ref` = attempt) in the same tx; `false` → rollback, `FAILED_PRECONDITION` "You are not Ready".
   2. `Gate.Precheck` (advisory) — if refused, row returns per §5.2 and step 1 repeats (≤ 5 times, then `NOT_FOUND` "No contact can be called right now").
   3. Console shows contact, history, attributes, TTL countdown.
   4. Agent clicks *Dial* → `DialPreview(lease)`: one tx — attempt kind PREVIEW, `agent_reserved=true`; `Gate.Decide`; if refused → BLOCKED, lead per §5.2, console shows reason, `ReservationReleased` → agent `Ready`.
   5. `PickPoolNumber` (§6.12) → candidate number id.
   6. In the step-4 transaction, `Presentation.Choose(tx, tenant, DialerPresentation{PoolChoice})` (LLD-03; `ErrNotOwned` → NOT_PLACED `callerid`); after commit, `DialerMedia.PlaceAttempt{AttemptID, CampaignID, ComplianceDecisionID, DestinationE164, EndpointURI (pbx-core route), Presentation, Session: &session, RingTimeout: setup_allowance_s + RingTimeout}` → telephony creates the Call (`call_id`), reserves licence capacity (LLD-08), records the decision id without re-deciding, `POST /channels/create` (`appArgs=dialer,<attempt>`), `POST /bridges/{atsa-d-<session>}/addChannel` (customer **before** dial, so the agent hears ringback, carrier tones and early media), `POST /channels/{id}/dial` with the backstop timeout. Errors `ErrCapacity`/`ErrDraining`/`ErrEmergencyRefused`/`ErrSessionDown`/engine → NOT_PLACED, `ReservationReleased`. Attempt DIALING.
   7. `Dial` events → RINGING / ANSWER. On ANSWER: attempt BRIDGED immediately (already in the bridge); agent `Busy`; recording per policy (`POST /bridges/{id}/record` via telephony-core).
   8. Hang-up after answer (`ChannelDestroyed` customer, or agent's *Hang up* → `DialerMedia.Hangup`) → agent `WrapUp`; disposition → DISPOSED; telephony releases capacity on Call termination.
   8a. Agent *Hang up* while RINGING → `cancel_requested_at` set; the console shows "Ending after the minimum ring ({s} s)"; `DialerMedia.Hangup` fires at `ring_started_at + min_ring` (or immediately if the leg never rang and the setup allowance has passed); outcome NO_ANSWER, agent `ReservationReleased`.
   9. *Skip* → `SkipPreview(lease, reason)`: hopper row deleted, QUEUED attempt marked NOT_PLACED `skipped`, lead READY with `next_eligible_at = now + 15 min`; skip counted; `ReservationReleased` → agent `Ready`.
   10. TTL expiry (sweeper every 5 s): `RETURN` → as skip with reason `timeout`; `DIAL` → step 4 automatically.
4. **States:** §5.2–§5.4; preview cannot abandon (the agent is in the bridge before dial), so it is **excluded** from the abandonment ratio (LLD-04 §7.3).
5. **Data:** hopper lease columns; attempt `kind=PREVIEW`.
6. **Ports:** `Hopper.Lease(ctx, tx, campaign, agent) (HopperRow, error)`; `Gate`; `AgentStates`; `DialerMedia`/`DialerEvents` (§6.3).
7. **API:** `NextContact`, `DialPreview`, `SkipPreview`, `HangUp`, `RecordDisposition`.
8. **Config:** `preview_ttl_s` 55 (30–240, and within the reservation budget of item 2); `preview_timeout_action` RETURN; `preview_skip_limit_per_hour` 0 (0–100); skip re-eligibility 15 min (fixed).
9. **Failure:** agent closes the browser during preview → lease expires → RETURN (before the LLD-03 sweeper could release the reservation, because of the item-2 budget); agent leg lost while customer ringing → treated as step 8a: cancel only after the minimum ring (telephony leaves the ringing leg to the dialler, LLD-01 §23.3); if the customer answers meanwhile and no agent leg is back by the connect deadline → OB-07 abandon path (message, counted); engine refuses create → NOT_PLACED `engine`; telephony releases any capacity it reserved (D-58 idempotent).
10. **Invariants/events:** INV-09 (Decide at Dial, not at Next), BR-06. Events `attempt.placed`, `attempt.completed`.
11. **Tests:**

    | # | Tier | Given / When / Then |
    |---|---|---|
    | P1 | I | two agents call `NextContact` concurrently / — / different rows (SKIP LOCKED) |
    | P2 | I | contact goes on DNC between Next and Dial / Dial / BLOCKED `dnc_tenant`, no channel |
    | P3 | I | lease 120 s, no action / 125 s / row returned, lead READY+15 min |
    | P4 | I | skip limit 2/h, third skip / — / `RESOURCE_EXHAUSTED` "Skip limit reached for this hour" |
    | P5 | E | preview on rig / Dial / agent hears ringback via bridge; answer → two-way audio < 300 ms after 200 OK |
    | P6 | B | agent sees contact, countdown, dials, disposes in two clicks (US-14.6) |
    | P7 | I | agent HangUp 3 s after ring start, min ring 15 s / — / `cancel_requested_at` set; `DialerMedia.Hangup` called at ring start + 15 s, not before (fake clock) |
    | P8 | I | reservation limit 120, setup 30, ring 25, `preview_ttl_s` 60 / UpdateCampaign / `INVALID_ARGUMENT` (122 > 120); `preview_ttl_s` 55 / accepted |
    | P9 | I | Skip / — / agent `Reserved → Ready` (`ReservationReleased`) in the same tx |
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
   0. **Wake-up:** the pacer `LISTEN`s on `dialer_wake` (payload `<tenant_id>:<campaign_id>`, `NOTIFY` issued by LLD-03 in every transaction that commits an agent into `Ready`) and also ticks every `tick_interval` (1 s) as a safety net; either runs steps 1–9.
   1. Read campaign row (state, `effective_ratio`, `max_concurrent`); if not RUNNING, stop launching.
   2. `ready` = `AgentStates.CountReady` — agents of this campaign in `Ready`; it **excludes `Reserved`**, so agents already holding an attempt are not counted; `in_flight` = attempts of this campaign in QUEUED, DIALING, RINGING, ANSWERED.
   3. **Reserved mode** (`effective_ratio = 1.0` or `reserved_only_lock`): `n = ready` (every counted agent is free and unreserved; no subtraction, because reserved agents are already outside `ready`). For each slot the dialler picks an agent from its CONNECTED sessions ordered by `last_offered_at` and calls `AgentStates.Reserve(tx, tenant, principal, attemptID)`; `false` (raced) → next session; sets `last_offered_at = now()`; the attempt carries `agent_reserved=true`.
      **Dial-ahead mode** (`effective_ratio > 1.0`): `target = floor(effective_ratio × ready + 1e-9)` (only `Ready` agents count; rounding is floor, so 1.5 × 3 = 4); `n = target − in_flight`. No agent is reserved at dial time.
   4. `n = min(n, max_concurrent − in_flight, max_new_per_tick, hopper READY rows)`; `n ≤ 0` → done.
   5. Lease `n` hopper rows: same `FOR UPDATE SKIP LOCKED` query (including `not_before <= now()`) with `LIMIT n`.
   6. For each, one tx: attempt QUEUED (+ `Reserve` in reserved mode); `Gate.Decide` (kind DIALER, `AgentReserved`); refused → BLOCKED and `ReservationReleased`; else `PickPoolNumber` and `Presentation.Choose` (LLD-03, **inside the same tx**, so the pool cursor, the presentation check and the attempt commit together; `ErrNotOwned` → NOT_PLACED `callerid`); commit; then `DialerMedia.PlaceAttempt{…, Session: nil, RingTimeout: setup_allowance_s + RingTimeout}` (telephony: Call, capacity, `POST /channels/create`, `POST /channels/{id}/dial` with that backstop timeout) → DIALING; the customer is **not** in any bridge while ringing. The dialler's own ring timer cancels at `ring_started_at + RingTimeout` (§6.7).
   7. On `Dial` ANSWER: start the connect clock (LLD-04 §7.4).
      - reserved: `DialerMedia.Bridge(attempt, session)` (`POST /bridges/{session}/addChannel`) → BRIDGED, `Transition(CustomerBridged)` `Reserved → Busy`; optional zip tone inside `Bridge` when `zip_tone`.
      - dial-ahead: `AgentStates.ClaimLongestReady(tx, tenant, campaign, attemptID)` → `(principal, reservationID, ok)` (`Ready → Busy`, `reserved_ref` = attempt); ok → `DialerMedia.Bridge` → BRIDGED. Not ok → wait for `dialer_wake` for this campaign and retry the claim until `deadline − claim_margin` (200 ms); still none → OB-07 abandon path.
   8. `ledger.RecordLiveAnswer(at = bridged_at)` in the BRIDGED tx, or `RecordAbandon` in the ABANDONED tx (LLD-04 §5.1: one `at` per outcome).
   9. Customer or agent hang-up → `DialerEvents.Ended` (customer) / `AgentLegLost` (agent) → agent `Transition(WorkEnded{ref: attempt})` `Busy → WrapUp`; disposition → DISPOSED; lead transition. Telephony ends the Call and releases capacity (LLD-08).
4. **States:** §5.1–§5.4.
5. **Data:** §4.3–§4.5, §4.7.
6. **Ports (pure parts marked):**
   ```go
   // PowerSlots is pure: how many attempts to launch this tick.
   func PowerSlots(in PacerInput) int
   // Reserved mode: n = Ready (CountReady already excludes Reserved).
   // Dial-ahead: n = floor(EffectiveRatio × Ready) − InFlight. Both then capped.
   type PacerInput struct {
       EffectiveRatio float64; ReservedOnly bool; Ready, InFlight int
       MaxConcurrent, MaxNewPerTick, HopperReady int
   }
   // Same signatures as LLD-03 §3.1.1 (LLD-03 owns the implementation).
   type AgentStates interface {
       CountReady(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, campaign shared.CampaignID) (int, error)
       Reserve(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, principal shared.PrincipalID, reservedRef uuid.UUID) (bool, error)
       ClaimLongestReady(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, campaign shared.CampaignID, attempt uuid.UUID) (principal shared.PrincipalID, reservationID uuid.UUID, ok bool, err error)
       Transition(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, principal shared.PrincipalID, ev AgentEvent) error
       ReasonCodes(ctx context.Context, tx pgx.Tx, tenant shared.TenantID) ([]ReasonCode, error)
   }
   // telephony-core, LLD-01 §23.3 "Dialer media" (final there; shown here for reference).
   // Telephony creates the Call and call_id, reserves and releases licence capacity per
   // attempt (LLD-08), records the carried compliance decision id without re-deciding,
   // refuses emergency destinations, and routes StasisStart for appArgs
   // "dialer,<attempt>" / "dialer-agent,<session>" to the dialler path.
   type DialerMedia interface {
       OpenSession(ctx context.Context, s SessionRequest) error                    // nailed-up leg + bridge atsa-d-<session>; idempotent
       CloseSession(ctx context.Context, session SessionID) error
       PlaceAttempt(ctx context.Context, a AttemptRequest) (shareddomain.CallID, error) // ErrNoDecision, ErrEmergencyRefused, ErrCapacity, ErrDraining, ErrSessionDown
       Bridge(ctx context.Context, attempt AttemptID, session SessionID) error     // addChannel (+ optional zip tone); ErrCallGone
       Play(ctx context.Context, attempt AttemptID, mediaURI string) error         // abandon message
       Drop(ctx context.Context, attempt AttemptID, mediaURI string) error         // removeChannel from the session bridge, play, hang up
       Hangup(ctx context.Context, attempt AttemptID, reason string) error         // idempotent; also the "cancel" of a ringing leg
   }
   type AttemptRequest struct {
       TenantID shareddomain.TenantID; AttemptID AttemptID; CampaignID CampaignID
       ComplianceDecisionID uuid.UUID          // required; Screening records it and never re-decides
       DestinationE164 string; EndpointURI string // route chosen by pbx-core
       Presentation Presented                  // LLD-03 §3.12 type, from Presentation.Choose (BR-06)
       Session *SessionID                      // pre-bridge for preview/manual/reserved
       RingTimeout time.Duration               // the dialler passes the BACKSTOP: setup_allowance_s + RingTimeout (§6.7)
   }
   // Implemented by the dialler; non-blocking (buffered).
   type DialerEvents interface {
       Progress(ctx context.Context, p AttemptProgress) error // dialing|ringing|progress|answer|busy|…; FirstAlertAt (first 180/183) once
       Dtmf(ctx context.Context, attempt AttemptID, digit string) error
       PlaybackDone(ctx context.Context, attempt AttemptID, ok bool) error
       Ended(ctx context.Context, e AttemptEnded) error        // Q850, SIPCode *int (SPIKE-D1), Cause
       AgentLegUp(ctx context.Context, session SessionID) error
       AgentLegLost(ctx context.Context, session SessionID, cause string) error // keepalive every 30 s, StasisEnd, ChannelDestroyed
   }
   // Reconcile: DialerMedia.LiveAttempts(tenant) lists live attempts and sessions from appArgs.
   ```
7. **API:** campaign control (§7); agent `JoinCampaign`, `LeaveCampaign`, `SetNotReady`, `SetReady`, `RecordDisposition`, `HangUp`.
8. **Config:** `ratio` 1.0 (1.0–3.0, step 0.1, campaign); `max_concurrent` 10 (1–500, campaign; telephony's capacity check is the hard ceiling); `tick_interval` 1 s (250 ms–5 s, installation); `max_new_per_tick` 10 (1–100, installation); `ring_timeout_s` 25 (≥ profile `min_ring`, ≤ 60).
9. **Failure:** pacer instance dies → lock released by session end; another instance acquires within one tick; in-flight attempts are reconciled against telephony's live Calls (`call_id` on the attempt); an attempt in DIALING/RINGING with no live Call → FAILED `lost`; an **ANSWERED** attempt with no live Call → **ABANDONED** (counted, conservative). Engine (Asterisk) restart → all legs gone → sessions RECONNECTING, DIALING/RINGING attempts FAILED `engine_restart` (not charged), ANSWERED attempts ABANDONED, BRIDGED attempts end normally (the conversation is over), campaign keeps RUNNING but launches nothing until ≥ 1 session reconnects. Agent requests a pause while `Reserved` → queued until the attempt bridges or is released; an attempt cannot outlive `campaign_reservation_seconds` because `Create/Update/StartCampaign` validate `setup_allowance_s + max(ring_timeout_s, min_ring) + ceil(connect limit) + 5 s ≤ campaign_reservation_seconds` (§6.2 item 2; at the maximums 60 + 60 + 2 + 5 = 127 s the campaign is refused unless LLD-03's setting is raised); if the LLD-03 sweeper releases it anyway (clock skew, bug), an answer finds no agent and takes the abandon path, which is counted. Two pacers (lock bug) → the partial unique index on live numbers and the conditional agent update prevent double dial and double assignment.
10. **Invariants/events:** INV-09 by the attempt trigger (§4.4); capacity is a ceiling enforced by telephony (AC-14.1a); INV-03 pause never cancels ringing or bridged calls. Events `attempt.placed`, `attempt.answered`, `attempt.completed`.
11. **Tests:**

    | # | Tier | Given / When / Then |
    |---|---|---|
    | PW1 | U | ratio 1.5, ready 3, in-flight 2 / PowerSlots / 2 (floor 4.5 = 4 − 2) |
    | PW1a | U | reserved mode, ready 2 (CountReady already excludes 3 Reserved), in-flight 3 / PowerSlots / 2 |
    | PW2 | U | max_concurrent 5, in-flight 5 / — / 0 |
    | PW3 | U | property: PowerSlots never exceeds MaxConcurrent − InFlight or HopperReady, never negative |
    | PW4 | I | ratio 1.0, 2 Ready agents / tick / 2 attempts, each `agent_reserved` with a distinct agent |
    | PW5 | I | two pacers on one campaign / ticks / one holds the lock; no duplicate number live (unique index) |
    | PW6 | I | `DialerMedia.PlaceAttempt` returns `ErrCapacity` / slot / attempt NOT_PLACED, lead not charged, agent `Reserved → Ready` |
    | PW6a | I | an agent commits into `Ready` / — / `NOTIFY dialer_wake` reaches the pacer and a slot launches before the next 1 s tick |
    | PW6b | I | attempt ANSWERED, app restarted, no live Call / reconcile / ABANDONED and counted in the ledger |
    | PW7 | E | 2 agents, ratio 1.0, rig SIP UAs answering / 10 min / every answered call bridged; zero ABANDONED |
    | PW9 | E | agent's browser phone held by a dialler session (nailed-up leg in `atsa-d-<session>`) / the agent dials 911 (rig emergency number) from the same device's dialpad / the emergency call is placed through the normal emergency path (a second line on the device, LLD-01 emergency matcher), never refused or queued by the dialler (INV-01) |
    | PW8 | A | 10 agents, clean list, 2 h (rig load harness) / power at configured ratio / concurrency never > limit; only answered calls reach agents (AC-14.1a) |
12. **Parity:** VICIdial `auto_dial_level` 1.0–4.0 with `RATIO`; Five9
    power = lines per agent; Amazon Connect *progressive* with bandwidth.
    Our default 1.0 is "progressive" (cannot abandon); > 1.0 is gated by
    LLD-04's abandonment rules from R1.0.

### 6.4 OB-06 — Abandonment control (R1.0 power, R2 predictive)

**Owner (decided):** the **dialler** owns the evaluator, its state and its
events (`tenant.<id>.event.dialer.abandon.*`). Compliance owns the
counters, the profile and rule 12 of `Evaluate` (LLD-04 §3, §7.3), and
emits no abandonment events.

1. **Trigger/actors:** the abandonment evaluator (system, per RUNNING POWER/PREDICTIVE campaign, every 10 s, inside the pacer's advisory lock); compliance officer configures the profile (`compliance.policy.manage`) and resumes after a breach (`dialer.campaign.compliance_resume`).
2. **Preconditions:** campaign mode POWER or PREDICTIVE; profile loaded. PREVIEW campaigns cannot abandon and are not evaluated.
3. **Sequence:**
   1. `AbandonLedger.Stats(tx, tenant, campaign, policy, campaignStart, now)` → `AbandonStats{Rolling, Period}`: the rolling window **and** the campaign-anchored "successive 30-day period or portion thereof" (day 0 = campaign start; the current period is `[start + 30k, start + 30(k+1))`), LLD-04 §7.3. Every decision below uses the **stricter** (higher) of the two ratios.
   2. Read `ControlState` from `dialer_campaign_stats`; pure `AbandonControl(cur, stats, policy, configuredRatio, effectiveRatio, now, cfg)` → `(next ControlState, effectiveRatio, ControlAction)`; persist `next` and `effective_ratio` in one tx; emit an event only when `next.State ≠ cur.State`.
   3. **WARMUP** (`live < abandon_min_sample`, 100): `effective_ratio = 1.0` (reserved mode: cannot abandon, so the first abandon cannot read as 100 %).
   4. **OK** (`ratio < limit − margin`): if `recovery_hold_until` is past, step up `+0.1` per 60 s (`last_step_at`) towards the configured ratio; `reserved_only_lock` is cleared on the first OK evaluation.
   5. **APPROACHING** (`ratio ≥ limit − margin`, 2.5 %): `effective_ratio = max(1.0, effective − 0.2)` immediately on entry and every 60 s while it lasts — the first step lands ≤ 10 s after the crossing, inside AC-15.3's 60 s; `recovery_hold_until = now + 10 min` on each step. Event `abandon.approaching` on entry.
   6. **BREACHED** (`ratio ≥ limit`, 3 %), with two guards. (a) **Small sample:** while `live < abandon_breach_min_sample` (LLD-04, default 50) a breach does not STOP; it sets `reserved_only_lock` (reserved mode cannot abandon, so the ratio can only fall from there and the period limit is never exceeded by dialling). One abandon after an app restart in the first live answers therefore cannot stop a campaign. (b) **After a compliance-officer resume** (`reserved_only_lock` already set) the ratio may stay ≥ limit for a while because reserved mode only adds live answers; BREACHED keeps reserved mode and does **not** STOP again. Otherwise: action `STOP` → campaign `STOPPED_COMPLIANCE` (§5.1) with reason ABANDON_LIMIT, `effective_ratio = 1.0`, `reserved_only_lock = true`, hopper emptied; ringing and live calls continue (INV-03); LLD-04 rule 12 refuses any unreserved attempt that reaches `Decide`. Event `abandon.breached`; alert to supervisors and compliance officers.
   7. **Resume after breach:** only `ResumeAfterCompliance{campaign_id, reason}` by a compliance officer; the campaign returns to RUNNING **locked in reserved mode** until the ratio falls below limit − margin (then step 4 applies). A manager's `StartCampaign` is refused (OWNER DECISION §14.7).
   8. Back below limit − margin after APPROACHING: event `abandon.recovered`.
4. **States:**

   | From | Condition | To | Action |
   |---|---|---|---|
   | WARMUP | live ≥ min sample and ratio < limit − margin | OK | `STEP_UP` allowed |
   | WARMUP / OK | ratio ≥ limit − margin (min sample reached) | APPROACHING | `STEP_DOWN` |
   | APPROACHING | still approaching, 60 s since `last_step_at` | APPROACHING | `STEP_DOWN` |
   | APPROACHING | ratio < limit − margin | OK | `HOLD` until `recovery_hold_until` |
   | any | ratio ≥ limit, `live ≥ abandon_breach_min_sample`, `reserved_only_lock = false` | BREACHED | `STOP` |
   | any | ratio ≥ limit, `live < abandon_breach_min_sample` | BREACHED | `LOCK` (reserved only, no stop) |
   | BREACHED | ratio ≥ limit, `reserved_only_lock = true` (after resume or small-sample lock) | BREACHED | `NONE` (stay reserved) |
   | BREACHED | compliance-officer resume | OK or APPROACHING by ratio, `reserved_only_lock` kept until OK | `NONE` |
5. **Data:** LLD-04 `compliance_abandon_buckets`, `compliance_abandon_events` (migration 0058); `dialer_campaign_stats.abandon_state`, `recovery_hold_until`, `last_step_at`, `reserved_only_lock` (0057); `dialer_campaigns.effective_ratio`.
6. **Ports:**
   ```go
   type ControlState struct {
       State             AbandonState // WARMUP | OK | APPROACHING | BREACHED
       RecoveryHoldUntil *time.Time
       LastStepAt        *time.Time
       ReservedOnlyLock  bool
   }
   type ControlAction int // NONE | STEP_DOWN | STEP_UP | HOLD | LOCK | STOP
   // Pure: no clock, no I/O.
   func AbandonControl(cur ControlState, s compliance.AbandonStats, p compliance.Policy,
       configured, effective float64, now time.Time, cfg ControlConfig) (ControlState, float64, ControlAction)
   type ControlConfig struct { StepDown, StepUp float64; StepEvery, RecoveryHold time.Duration } // 0.2, 0.1, 60 s, 10 min
   ```
7. **API:** `GetAbandonmentStatus` (LLD-04, counters); `GetCampaign` returns `effective_ratio`, `abandon_state`, `reserved_only_lock`; `ResumeAfterCompliance{campaign_id, reason}` (`dialer.campaign.compliance_resume`; `FAILED_PRECONDITION` "This campaign was not stopped for abandonment"; `PERMISSION_DENIED` "A compliance officer must resume this campaign").
8. **Config:** profile fields (LLD-04 §4.1); evaluator period 10 s (installation, 5–30 s); `ControlConfig` defaults above (installation).
9. **Failure:** ledger read fails → treat as BREACHED **for pacing only** (reserved mode this tick, no state change, no stop), alert after 3 consecutive; evaluator runs inside the pacer lock holder, so failover is the pacer's (§6.3).
10. **Invariants/events:** AC-15.3, AC-15.6; INV-03 (stop leaves ringing and live calls). Events `dialer.abandon.approaching|breached|recovered` (`campaign_id, ratio_pct, limit_pct, live_answered, abandoned`) and `dialer.campaign.stopped{reason: ABANDON_LIMIT}`; audit for `ResumeAfterCompliance`.
11. **Tests:**

    | # | Tier | Given / When / Then |
    |---|---|---|
    | A1 | U | live 50, abandoned 0 / AbandonControl / WARMUP, ratio 1.0 |
    | A2 | U | 1000 live / 26 abandoned (2.6 %), effective 2.0 / — / APPROACHING, 1.8; 60 s later still 2.6 % → 1.6 |
    | A3 | U | 1000 / 30 (3.0 %) / — / BREACHED, action STOP, lock set |
    | A3a | U | 20 live / 1 abandoned (restart abandon) / — / BREACHED, action LOCK, no STOP |
    | A3b | U | after resume, lock set, 1000 / 31 / — / action NONE (reserved mode continues), no second STOP |
    | A3c | U | rolling 2.0 %, current campaign period 3.1 % / — / BREACHED (stricter of the two) |
    | A3d | I | STOPPED_COMPLIANCE / manager `DuplicateCampaign` → `PERMISSION_DENIED`; compliance officer → clone with `reserved_only_lock = true` |
    | A4 | U | 1000 / 20 (2.0 %) after APPROACHING, hold until T+10 min / at T+9 min → no step; T+10 min → +0.1 per minute up to configured |
    | A5 | I | buckets seeded **999 live / 29 abandoned** (2.90 %), effective 1.5 / one more abandon (`RecordLiveAnswer` + `RecordAbandon`, same `at`) → **1000 / 30 = 3.00 %** / evaluator tick → `STOPPED_COMPLIANCE` within 10 s; bridged and ringing attempts untouched |
    | A6 | I | STOPPED_COMPLIANCE / manager `StartCampaign` → `PERMISSION_DENIED`; compliance officer resumes → RUNNING, only reserved attempts, Decide permits them |
    | A7 | A | simulated campaign crossing 2.5 % / — / slow-down observed ≤ 60 s (AC-15.3) |
12. **Parity:** VICIdial adaptive methods (`ADAPT_HARD_LIMIT`,
    `ADAPT_TAPERED`) and `drop_rate_group`; Five9 max abandon rate;
    Amazon Connect predictive keeps an abandonment-rate target but does
    not stop the campaign. Difference: warm-up in reserved mode, and a
    breach stops the campaign until a compliance officer resumes it.

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
3. **Sequence:**
   1. Attempt QUEUED and `Gate.Decide(tx, req{AttemptID, …})` in **one** transaction (LLD-04 §5): `Decide` inserts the decision row with `attempt_id`; the attempt is updated with `compliance_decision_id` and its next state in the same tx; the trigger `dialer_attempt_verdict_guard` (§4.4) rejects DIALING/RINGING/… unless that decision row exists for this attempt and `permitted`, and rejects BLOCKED unless it refused. Refused → BLOCKED with reason, lead per §5.2; permitted → `DialerMedia.PlaceAttempt` with the `ComplianceDecisionID`.
   2. Hopper refill uses `PrecheckMany(tx, base, []PrecheckContact{NumberDigits, Zone, Region, Country})` — per-number context from each contact row; writes nothing; refusals move the lead per §5.2 (`consent_missing`/`timezone_unknown` → BLOCKED_WAIT, retryable).
   3. **Manual dial from a dialler session** (`ManualDial{number, campaign_id?}`): lead lookup in `dialer_contacts` by `(tenant, number_digits)` (most recent member of a list attached to the agent's campaign; several → the console lets the agent pick; none → attempt without contact); attempt `kind=MANUAL`, `agent_reserved=true` with `Reserve(principal, attemptID)`; **one** `Decide` (kind MANUAL, the campaign's profile or the tenant default); then `DialerMedia.PlaceAttempt{ComplianceDecisionID, Session}`.
   4. **No double decision:** `AttemptRequest.ComplianceDecisionID` is required (`ErrNoDecision` otherwise); telephony Screening runs only capacity and `MaintenanceGate` for the attempt, records the carried id on `calls.compliance_decision_id`, and never calls `Decide` again (LLD-01 §23.3). The verdict itself is proven in the dialler's transaction by the trigger (§4.4). An emergency destination returns `ErrEmergencyRefused` → NOT_PLACED `emergency` (INV-01). Calls dialled on a phone, not through the dialler, go through Screening's own `Decide` (LLD-04 §8).
4. **States:** attempt QUEUED → BLOCKED | DIALING.
5. **Data:** `dialer_attempts.compliance_decision_id` + the verdict trigger (§4.4); LLD-04 `compliance_decisions.attempt_id`.
6. **Ports:** `compliance.Gate` (`Decide`, `Precheck`, `PrecheckMany`; LLD-04 §5.1); `DialerMedia.PlaceAttempt`.
7. **API:** `ManualDial` → `{attempt_id, permitted, reason}`; refused → `FAILED_PRECONDITION` with detail `reason`, console words per reason (`outside_hours`: "It is {local time} for this contact; calling hours are {window}"; `dnc_*`: "This number is on a do-not-call list"; `opted_out`: "This contact has opted out"; `compliance_unavailable`: "Calling is paused: the compliance check is unavailable").
8. **Config:** none beyond the profile.
9. **Failure:** Decide error → tx rollback, no attempt row; pacer backs off 5 s; 3 consecutive → alert.
10. **Invariants:** INV-09/BR-10 (verdict trigger + single entry point + `PlaceAttempt` requires the decision id; Screening records it and never re-decides); INV-01 (emergency numbers refused by `ManualDial` with `INVALID_ARGUMENT` "Emergency calls are placed from the phone, not the dialler"; the phone path is untouched).
11. **Tests:** LLD-04 §11 C1–C25 plus:

    | # | Tier | Given / When / Then |
    |---|---|---|
    | G1 | I | attempt set to DIALING with a null decision, with a refusing decision, or with another attempt's permitting decision / update / `check_violation` from the trigger in all three |
    | G1a | I | `PrecheckMany` with two contacts in different zones at 20:30 New York / — / NY permitted, LA… per its own zone; a contact with no zone → `timezone_unknown`, lead BLOCKED_WAIT (not INVALID); consent upsert → `next_eligible_at = now` |
    | G1b | I | manual dial from a session / — / exactly one `compliance_decisions` row for the attempt (Screening recorded the carried id on `calls.compliance_decision_id`, did not re-decide); an emergency destination → NOT_PLACED `emergency` |
    | G2 | W | `ManualDial` to a DNC number / — / `FAILED_PRECONDITION`, `reason=dnc_tenant`; no channel |
    | G3 | E | DNC number via power and via manual dial / — / never on the trunk (AC-15.1, release suite) |
12. **Parity:** VICIdial checks DNC at hopper load and optionally at
    manual dial; Five9 scrubs at load; Amazon Connect outbound campaigns
    apply communication time windows and a profile-based suppression at
    send time. We decide at the dial instant, every path, no override,
    and keep one decision row per attempt.

### 6.7 OB-07 — Minimum ring and abandon message (R1.0)

1. **Trigger/actors:** the dialler's ring timer (minimum ring and `RingTimeout`); an agent or system cancel request; answer with no agent by the connect deadline.
2. **Preconditions:** profile `abandon_message_required` ⇒ media present (start guard §5.1).
3. **Sequence:**
   1. **Minimum ring (decided):** the ring clock starts at the **first 180/183** — `DialerEvents.Progress` (`FirstAlertAt`) from `ChannelStateChange` → `Ringing` or `Dial` `dialstatus=RINGING`/`PROGRESS` — stored as `ring_started_at`. `RingTimeout = max(ring_timeout_s, min_ring)` (US ≥ 15 s; default 25 s ≈ 4 US ring cycles of 6 s) is measured **from `ring_started_at`**.
   2. The ARI dial `timeout` passed by telephony is only a backstop: `setup_allowance_s + RingTimeout` (30 + 25 = 55 s), so it can never fire before `ring_started_at + RingTimeout` as long as ringing starts within the setup allowance.
   3. No 180/183 by `dialed_at + setup_allowance_s` → `DialerMedia.Hangup`, outcome CARRIER_FAILED `no_progress`. Some carriers send no 180/183 while the phone rings, so under US_FCC `setup_allowance_s ≥ min_ring_seconds` is enforced (`INVALID_ARGUMENT` "Setup allowance must be at least the minimum ring time"): the no-progress cancel never comes before `dialed_at + min_ring`, which is never shorter than real ringing. The backstop (`setup_allowance_s + RingTimeout`) is later still.
   4. The dialler cancels at `ring_started_at + RingTimeout`. Any earlier request — agent *Hang up*, agent leg lost, `PauseCampaign`, `StopCampaign`, breach — only sets `cancel_requested_at`; `DialerMedia.Hangup` runs at `max(now, ring_started_at + min_ring)`. Nothing cancels a ringing leg before the minimum ring (47 CFR 64.1200(a)(6)).
   5. On ANSWER with no agent claimable by `deadline − claim_margin` (200 ms): `DialerMedia.Play(attempt, abandon_message_media_uri)` (`POST /channels/{customer}/play`) starts **before the deadline**; `message_started_ms` recorded.
   6. `DialerEvents.Dtmf` digit = `abandon_optout_digit` → `OptOuts.RecordOptOut(source=ABANDON_IVR)` in a tx; `DialerMedia.Play(sound:<confirmation>)`.
   7. `DialerEvents.PlaybackDone` → `DialerMedia.Hangup(attempt, normal)`.
   8. Tx: attempt ABANDONED, `ledger.RecordLiveAnswer(at)` + `RecordAbandon(at, message_played, started_ms, opted_out)` with the **same `at`** (the abandon instant); lead RETRY_WAIT with `next_eligible_at ≥ abandoned_at + no_redial_after_abandon`.
   9. Customer hangs up before the deadline while waiting → ABANDONED as well (FCC counts it), message not played.
   10. An agent becoming Ready **after** the message started is not connected (the customer has been told the call was abandoned).
   11. **Recovery:** an ANSWERED attempt that is unmatched after an app or engine restart is recorded ABANDONED (`message_played=false` unless the playback is known to have started) — never FAILED; same rule as §6.3 item 9 and §11.
4. **States:** attempt ANSWERED → ABANDONED.
5. **Data:** LLD-04 `compliance_abandon_events`.
6. **Ports:** `compliance.Policies`, `AbandonLedger`, `OptOuts`; `DialerMedia.Play/Hangup`; pure `RingCancelAt(ringStarted *time.Time, dialed, cancelRequested time.Time, p compliance.Policy, campaignRing, setupAllowance time.Duration) time.Time`.
7. **API:** profile RPCs (LLD-04); attempt shows `outcome=ABANDONED`, `message_played`.
8. **Config:** profile `min_ring_seconds` 15, `abandon_connect_limit_ms` 2000, `abandon_optout_digit` "9"; campaign `ring_timeout_s` 25, `setup_allowance_s` 30 (5–60); claim margin 200 ms (installation, 100–500).
9. **Failure:** playback fails (`PlaybackFinished` with failure or media missing) → hang up immediately and record `message_played=false` + alert (a compliance incident, visible to the auditor); engine or app down after answer → ABANDONED on reconcile (step 11); app down while ringing → telephony's backstop timeout ends the leg no earlier than the minimum ring.
10. **Invariants:** AC-15.6. Events `attempt.abandoned`.
11. **Tests:**

    | # | Tier | Given / When / Then |
    |---|---|---|
    | M1 | U | profile 15 s, campaign 10 s / RingTimeout / 15 s |
    | M1b | I | US_FCC, `setup_allowance_s` 10 / UpdateCampaign / `INVALID_ARGUMENT` |
    | M1a | U | dialed T, 183 at T+8 s, cancel requested T+10 s, min ring 15 s / `RingCancelAt` / T+23 s; no progress by T+30 s / → T+30 s, outcome CARRIER_FAILED |
    | M2 | I | campaign paused 3 s after ring start / — / no cancel before ring start + 15 s; with no request, cancel at ring start + 25 s |
    | M2a | I | answer, then app restart before bridge / reconcile / ABANDONED, ledger +1/+1 with one `at` |
    | M3 | E | ratio 2.0, 1 agent busy, 2nd customer answers (rig UA) / — / playback starts < 2 s after answer (ARI timestamps); DTMF 9 → opt-out row |
    | M4 | E | unanswered rig UA with a delayed 180 / — / CANCEL not before 180 + 15 s (SIP trace) |
12. **Parity:** VICIdial `drop_call_seconds` + `safe_harbor_message` and
    a dial timeout from originate; Five9 abandon message; Amazon Connect
    plays a configurable message when no agent is available and uses a
    ring timeout per campaign. Ours measures the connect limit from answer
    (stricter), the minimum ring from first ringing, and stores the
    message timing per abandon.

### 6.8 OB-08 — Voicemail drop and AMD (R1.0 agent drop; R2 AMD)

1. **Trigger/actors:** R1.0 — agent (`dialer.agent`) clicks *Drop voicemail* while BRIDGED. R2 — AMD classifier on answer.
2. **Preconditions:** campaign `voicemail_drop_media_uri` set; contact consent `EXPRESS_WRITTEN` when the profile is US_FCC (a prerecorded telemarketing message; OWNER DECISION LLD-04 §12.4); button hidden otherwise.
3. **Sequence (R1.0):** console shows *Drop voicemail* only when `compliance.Consent.ConsentFor(tx, tenant, number)` and pure `PrerecordedAllowed(policy, facts)` (LLD-04 §5.1) say so, re-checked on click. Agent hears the greeting and beep → *Drop* → `DialerMedia.Drop(attempt, drop_media)` (telephony: `POST /bridges/{session}/removeChannel`, play on the customer channel, hang up on `PlaybackFinished` with `left_reason=voicemail_dropped`); agent `Transition(WorkEnded{ref: attempt})` → `WrapUp` immediately (auto-disposition `ANSWERING_MACHINE`, outcome `VOICEMAIL_DROPPED`). The message is never played to a human the agent is talking to (the agent decides).
   **Sequence (R2 AMD):** an R2 extension of LLD-01 §23.3 (`AttemptRequest` AMD flag and a talk-event callback, to be added with `dialer-amd`) sets `TALK_DETECT(set)=500,128` before dial; on answer, classify from `ChannelTalkingStarted/Finished` (baseline) and optionally an AI plugin on a snoop copy (LLD-10, D-23): parameters `initial_silence` 2500 ms, `greeting` 1500 ms, `after_greeting_silence` 800 ms, `total_analysis_time` 5000 ms (capped by the connect deadline − 300 ms under `amd_counts_against_connect`), `min_word_length` 100 ms, `between_words_silence` 50 ms, `max_words` 2; AI deadline default 1200 ms, on miss → **human**. Machine → wait for silence ≥ 1000 ms after the last `ChannelTalkingFinished` (beep proxy; SPIKE-D3) → play drop (if allowed) → hang up; outcome MACHINE / VOICEMAIL_DROPPED.
4. **States:** attempt BRIDGED → MACHINE (R1.0); ANSWERED → MACHINE (R2).
5. **Data:** media URIs point into the tenant's object store (same store as recordings; closes old open decision §10.2); upload via the existing media/prompt facility.
6. **Ports:** `DialerMedia.Drop`; `compliance.Consent.ConsentFor` + pure `compliance.PrerecordedAllowed`; `func ClassifyTalk(events []TalkEvent, p AMDParams) (Class, decided bool)` — **pure** (R2).
7. **API:** `DropVoicemail{attempt_id}` → `{}`; refusals `FAILED_PRECONDITION` "This campaign has no voicemail message", "This contact has not given written consent for recorded messages".
8. **Config:** campaign media; AMD params per campaign (R2) within the ranges of app_amd.
9. **Failure:** drop playback fails → hang up, outcome MACHINE, alert; AMD/AI failure → human (INV-04).
10. **Invariants:** INV-04; AC-14.2 is R2. Events `attempt.completed{outcome}`.
11. **Tests:** V1 (I) drop without media → refused; V1a (I) US profile, consent EXPRESS → `FAILED_PRECONDITION`, EXPRESS_WRITTEN → allowed; V2 (E) agent drop on rig → agent free < 1 s, customer UA receives the audio; V3 (U, R2) `ClassifyTalk` table of greetings → class; V4 (A, R2) labelled corpus ≥ 95 % accuracy, ≤ 2 % humans as machine (AC-14.2).
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
   4. **Scheduler:** a worker (one per installation under advisory lock `hash('dialer-callbacks')`, tick 15 s) selects `state='SCHEDULED' AND due_at <= now() + 60 s FOR UPDATE SKIP LOCKED LIMIT 500` → DUE; sets the lead `priority = 100` and inserts the hopper row priority 100 with `callback_id`, `not_before = due_at` (inserted up to 60 s early so it is ready, but never leased before `due_at`; the pacer's lease query has the same `not_before <= now()` filter) (and `owner_principal_id` for AGENT). For an AGENT callback whose owner has no CONNECTED session at `due_at + 5 min`: campaign `callback_fallback_to_any` (default true, §4.2) → hopper row loses the owner; else MISSED `owner_absent`, returned to the owner's list in the console.
   5. **DUE → OFFERED** when its hopper row is leased: in PREVIEW by `NextContact` (owner first, OB-02 query); in POWER by a pacer slot, in reserved mode to the owner if Ready (`Reserve(owner, attemptID)`), else the row waits up to 5 min then falls back. Lease expiry or skip → OFFERED → DUE (row back to READY).
   6. **OFFERED → DIALING** when its attempt reaches DIALING; `delivered_at` recorded; COMPLETED after disposition. Not delivered by `due_at + 15 min` (campaign paused, no agents) → MISSED `campaign_paused|no_agents`; lead READY with `priority = 50`; event `callback.missed`.
4. **States:**

   | From | Trigger | To |
   |---|---|---|
   | — | `ScheduleCallback` (Precheck permits) | SCHEDULED |
   | SCHEDULED | scheduler: `due_at ≤ now + 60 s` | DUE (hopper row priority 100) |
   | DUE | hopper row leased (`NextContact` or pacer slot) | OFFERED |
   | OFFERED | lease expiry / skip / reservation released | DUE |
   | OFFERED | attempt DIALING | DIALING |
   | DIALING | disposition | COMPLETED (or a new SCHEDULED callback if the agent reschedules) |
   | DUE, OFFERED | `due_at + 15 min` passed, or AGENT owner absent with fallback off | MISSED |
   | SCHEDULED, DUE | `CancelCallback` / `StopCampaign{cancel_callbacks}` / opt-out | CANCELLED |

5. **Data:** §4.2 retry rules and `callback_fallback_to_any`, §4.3 `dialer_leads.priority`, §4.6 callbacks.
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
    | D9 | B | agent disposes in two clicks; schedules a callback in the contact's zone with the zone shown (US-14.6, US-14.7) |
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
12. **Parity:** VICIdial dial statuses `CONGESTION`/`DC` and no auto-pause; Five9 carrier failover; Amazon Connect records telephony failures per contact and retries per campaign settings, without a carrier-failure stop. Our auto-pause protects contacts from burnt attempts.

### 6.12 OB-12 — Caller-ID selection (R1.0, BR-06)

The number inventory (`tenant_numbers`, states `pending → verified →
revoked`, evidence) and the pure precedence function
`ChoosePresentation` are **LLD-03 §3.12**. A dialler attempt is its
precedence row 1 ("campaign / number pool"): the dialler picks a
candidate from its pool (a `tenant_numbers.id`) and passes it to
`pbx-core`'s `Presentation.Choose` as `DialerPresentation.PoolChoice`
(LLD-03 §3.12); `ChoosePresentation` verifies it and returns `Presented`,
which the dialler passes unchanged in `AttemptRequest.Presentation`;
telephony writes it to the channel. The dialler
designs no inventory table and never sets `CALLERID`.

1. **Trigger/actors:** each attempt (system); manager configures pools and campaign caller ID (`dialer.campaign.manage`); numbers are added and verified by the tenant administrator in LLD-03 (`pbx.number.write`, `pbx.number.verify`).
2. **Preconditions:** every number used is `verified` (not `pending`/`revoked`) in `tenant_numbers`; `callback_number_id` is a verified `carrier_did` (it receives calls on the tenant's trunk, so it can take DNC requests — 16 CFR 310.4(a)(8) expects a number that can be called back).
3. **Sequence:**
   1. `StartCampaign` guard: `Presentation.Status` (LLD-03, in the caller's tx) for `callerid_number_id`, every pool member and `callback_number_id` → all `verified`, callback a `carrier_did`; else `FAILED_PRECONDITION`.
   2. Per attempt, in the attempt tx after `Decide`: dialler's pure `PickPoolNumber(mode, members, contact)`:
      - `FIXED` → `callerid_number_id`;
      - `POOL` → member with the oldest `last_used_at` (`SELECT … FOR UPDATE SKIP LOCKED LIMIT 1`, then set `last_used_at = now()`);
      - `LOCAL_PRESENCE` → member with the contact's NPA, then the contact's region, then the pool LRU (OWNER DECISION §14.3).
   3. `Presentation.Choose(tx, tenant, DialerPresentation{Origin: OriginDialer, PoolChoice, CampaignID, AttemptID})` → `ChoosePresentation` precedence row 1 → `Presented{NumberID, Digits, CNAM}`. `ErrNotOwned` (number revoked meanwhile) → attempt NOT_PLACED `callerid` (LLD-03 records `presentation_refused`); for POOL the member is skipped and the next tried once; for FIXED, or a pool with no verified member left → campaign PAUSED(CALLER_ID_INVALID) and alert.
   4. Telephony sets `CALLERID(num)`, `CALLERID(name)` (inventory `cnam`) and `P-Asserted-Identity` on `POST /channels/create`; `Presented.Digits` is stored in `dialer_attempts.presented_number`. Withholding is not offered for dialler attempts (TSR requires transmitting caller ID).
   5. STIR/SHAKEN: signing is the carrier's in R1.0; the inventory's evidence is what the partner shows the carrier to obtain A-attestation (LLD-03 `stir_attestation` placeholder).
4. **States:** inventory states are LLD-03's; campaign RUNNING → PAUSED(CALLER_ID_INVALID).
5. **Data:** §4.2 `dialer_callerid_pools`, `dialer_callerid_pool_members`, campaign `callerid_*_id`, `callback_number_id`; `dialer_attempts.presented_number`.
6. **Ports:**
   ```go
   // PickPoolNumber is pure: chooses a candidate; it does not authorise it.
   func PickPoolNumber(mode CallerIDMode, members []PoolMember, contactNPA, contactRegion string) (numberID shared.UUID, ok bool)
   // Implemented by pbx-core (LLD-03 §3.12), runs in the dialler's tx:
   type Presentation interface {
       Status(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, numberID NumberID) (NumberStatus, string, error)
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

The nailed-up leg is opened with `DialerMedia.OpenSession` on `JoinCampaign` and closed with `CloseSession` on leave (§5.4); `AgentLegLost` (30 s keepalive, `StasisEnd`, `ChannelDestroyed`) drives RECONNECTING.

| Feature | Design |
|---|---|
| **Pause codes** | `SetNotReady{code}` with codes from LLD-03's catalogue (`AgentStates.ReasonCodes`) (defaults BREAK, LUNCH, TRAINING, MEETING, ADMIN; system codes `leg_lost`, `leg_failed`, `dispo_timeout`, `campaign_stopped` are not selectable). Pause during a call takes effect at end of wrap-up. Time per code is reported (LLD-09) |
| **Manual dial** | OB-05 §6.6 steps 3–4: lead lookup, one `Decide`, `DialerMedia.PlaceAttempt` into the session bridge; Screening records the decision id |
| **Transfer / 3-way with the lead** | Attended/blind transfer and conference are telephony-core call control (HLD-21 §1, LLD-01 §11): the customer leg moves from the session bridge into a call bridge; the agent's nailed leg stays in the session bridge; the attempt keeps the lead and the agent disposes it. An external third party is a normal trunk call through Screening (D-82/D-85), not a dialler attempt |
| **Recording** | `NONE`, `ALL` (from BRIDGED until the customer leaves), `ON_DEMAND` (agent start/stop); via telephony-core recording into the tenant object store; never records the idle nailed-up leg |
| **Licence capacity** | Reserved **by telephony** inside `DialerMedia.PlaceAttempt`, one per customer leg, keyed by the Call's `call_id` (D-58), released by telephony when the Call terminates (LLD-08: only telephony calls `ValidateCapacity` per call). The dialler never calls licensing. Whether `OpenSession` reserves capacity for the nailed-up leg is OWNER DECISION §14.2 |

## 7. API contract (`dialer.v1.DialerService`)

| Permission | Default grant |
|---|---|
| `dialer.list.read`, `dialer.campaign.read`, `dialer.attempt.read` | TENANT_ADMIN, SUPERVISOR, proposed CAMPAIGN_MANAGER |
| `dialer.list.manage`, `dialer.campaign.manage` | TENANT_ADMIN, CAMPAIGN_MANAGER |
| `dialer.campaign.control` | TENANT_ADMIN, SUPERVISOR, CAMPAIGN_MANAGER |
| `dialer.attempt.export`, `dialer.contact.export` | TENANT_ADMIN, CAMPAIGN_MANAGER (audited) |
| `dialer.agent` (session, next, dial preview, disposition, callback, drop, pause) | AGENT, SUPERVISOR |
| `dialer.manual_dial` | AGENT, SUPERVISOR |
| `dialer.campaign.compliance_resume` | proposed COMPLIANCE_OFFICER only (LLD-04 §12.6) |

| RPC | Key request fields → response | Permission | Refusals (code — console words) |
|---|---|---|---|
| `ImportContacts` (client stream) | header `{list_id \| new_list, mapping, dedupe_scope}`, chunks → `{import_id, accepted, rejected, duplicates, flagged, errors[≤100]{line, reason}}` | list.manage | `INVALID_ARGUMENT` "Line {n}: {reason}"; `RESOURCE_EXHAUSTED` "The file is larger than {limit}"; `FAILED_PRECONDITION` "An import is already running for this list" |
| `ListLists`, `ArchiveList`, `ListContacts`, `PreviewSegment` | paging, `segment` → contacts (masked), count | list.read / list.manage | `INVALID_ARGUMENT` "Filter field {f} is not allowed" |
| `CreateCampaign`, `UpdateCampaign` (`version`, `update_mask`), `GetCampaign`, `ListCampaigns`, `DuplicateCampaign` | §4.2 fields | campaign.manage / read | `INVALID_ARGUMENT` per field range; `FAILED_PRECONDITION` "Pause the campaign to change this"; `ABORTED` "Someone else changed this campaign; reload"; `FAILED_PRECONDITION` "Predictive dialling is not available in this release" |
| `AttachList`, `DetachList` | `campaign_id, list_id, weight, priority, segment` | campaign.manage | `NOT_FOUND` |
| `ResumeAfterCompliance` | `campaign_id, reason (≥ 10 chars)` → campaign | `dialer.campaign.compliance_resume` | `FAILED_PRECONDITION` "This campaign was not stopped for abandonment"; `PERMISSION_DENIED` "A compliance officer must resume this campaign" |
| `CreateCallerIDPool`, `ReplaceCallerIDPoolMembers`, `ListCallerIDPools` | `name` / `pool_id, number_ids[]` | campaign.manage / read | `FAILED_PRECONDITION` "{number} is not a verified number of this organisation" |
| `StartCampaign`, `PauseCampaign`, `StopCampaign{cancel_callbacks}` | `campaign_id` → campaign | campaign.control | `FAILED_PRECONDITION` naming the failed guard ("No active list", "Caller ID not authorised", "The abandon message is missing", "Outside the campaign schedule", "A stopped campaign cannot restart; duplicate it") |
| `JoinCampaign`, `LeaveCampaign` | `campaign_id` → `{session_id, state}` | agent | `FAILED_PRECONDITION` "You are already in campaign {name}"; "No phone is logged in for you" |
| `SetReady`, `SetNotReady{code}` | → agent state (LLD-03 transitions) | agent | `INVALID_ARGUMENT` "Unknown pause code" |
| `NextContact` | `session_id` → `{lease_id, contact, history[], expires_at}` | agent | `NOT_FOUND` "No contact can be called right now" |
| `DialPreview`, `SkipPreview{reason}` | `lease_id` → `{attempt_id}` | agent | `FAILED_PRECONDITION` + compliance reason; `RESOURCE_EXHAUSTED` "Skip limit reached for this hour" / "No line is free (licence capacity)" |
| `ManualDial` | `number, campaign_id?` → `{attempt_id, contact?}` | manual_dial | as OB-05 |
| `HangUp`, `DropVoicemail` | `attempt_id` → `{ending_at?}` (HangUp while ringing returns when the cancel will happen) | agent | `FAILED_PRECONDITION` as OB-08 |
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
| `max_concurrent` | 10 | 1–500 (telephony's licence check is the hard ceiling) | campaign |
| `ring_timeout_s` | 25 | max(10, profile min ring)–60 | campaign |
| `wrapup_max_s` | 120 | 0–900 (0 = immediate auto-dispose) | campaign |
| `preview_ttl_s` / action / skips per hour | 55 / RETURN / 0 | 30–240 and within the reservation budget (§6.2 item 2) / RETURN,DIAL / 0–100 | campaign |
| `setup_allowance_s` (dial → first 180/183) | 30 | 5–60; ≥ `min_ring` under US_FCC; inside the reservation budget | campaign |
| Pacer wake-up | `LISTEN dialer_wake` + 1 s tick | — | installation |
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
| `abandon.approaching|breached|recovered` | `campaign_id, ratio_pct, limit_pct, live_answered, abandoned` (on state change only; §6.4) |
| `callback.scheduled|due|missed|completed` | `callback_id, due_at, owner, reason?` |

Audit (`AppendAuditTx`, before/after): campaign create/update/state
change, `ResumeAfterCompliance` (with reason), caller-ID pools, list import/archive, attach/detach, dispositions, retry rules,
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
carrier trip, abandonment stop, `NOT_PLACED capacity` sustained for 5 min, connect latency p95 > 1 s.

## 10. Test plan summary and Definition of Done

Tier tags: U unit, I integration (`atsapbx_test` only), W wire, E e2e
on the rig, C console vitest, B browser (Playwright), A acceptance/UAT.
Flow tables above hold the scenarios (L, P, PW, A, R, G, M, V, D, E, K,
N series) plus LLD-04 C1–C25. Release suite (AC-15.1) = G3 + C23 + M3 +
M4 + L12, run before every release.

DoD R1.0: AC-14.1a, 14.3, 14.4, 14.5, 14.6 (via US-14.6 tests), US-14.5
(agent drop), AC-15.1–15.6 proven at their tiers; every test above
tagged R1.0 green; the predictive simulator (OB-04 test R3) exists and passes as a
unit test even though the mode is disabled. R2: AC-14.1, AC-14.2,
US-14.4.

## 11. Failure and recovery (cross-flow)

| Event | Behaviour | No orphan because |
|---|---|---|
| App instance restart | pacer/scheduler locks move; reconcile attempts against telephony's live Calls by `call_id` (telephony itself reconciles engine channels by `appArgs`, LLD-01 §22): unmatched DIALING/RINGING → FAILED `lost` (not charged); unmatched **ANSWERED → ABANDONED** (counted); sessions matched to bridges or RECONNECTING | every engine resource carries the attempt/session id in `appArgs`/bridge name; telephony releases capacity with the Call |
| Asterisk restart | all legs gone; DIALING/RINGING → FAILED `engine_restart` (not charged); ANSWERED → ABANDONED (counted); BRIDGED → ended normally, to WrapUp; sessions RECONNECTING | telephony terminates the Calls and releases capacity (D-58 idempotent) |
| Compliance store down | no attempt created; pacer backs off; campaign stays RUNNING; alert | fail closed |
| Customer hangs up while being claimed | `DialerEvents.Ended` before `Bridge` → ABANDONED (after answer) | claimed agent `Busy → WrapUp → Ready` via `WorkEnded{ref}` with auto-disposition; a reserved agent `ReservationReleased` |
| Agent hangs up phone mid-call | session RECONNECTING; customer hung up; attempt to WrapUp with the agent | §5.4 |
| Concurrent pause and tick | tick re-reads state inside its tx with `FOR SHARE`; pause commits first → tick launches nothing | AC-14.4 |
| Duplicate contact in two campaigns | partial unique index refuses the second live attempt → NOT_PLACED `duplicate_live`, lead returned | INV: one live call per person |

## 12. Invariants

| Rule | Mechanism |
|---|---|
| INV-01 | emergency numbers rejected at import (`emergency_number`) and at `ManualDial`; no dialler path uses the emergency matcher |
| INV-03 | pause/stop/breach/carrier trip never touch BRIDGED, ABANDONED-message or ringing attempts; no ringing leg is cancelled before `ring_started_at + min_ring` |
| INV-04 | AMD/AI undecided or failed = human |
| INV-09 / BR-10 | verdict trigger on `dialer_attempts` (§4.4) + `Decide` in the attempt tx; Precheck cannot produce a decision id; `PlaceAttempt` refuses without a decision id (`ErrNoDecision`); Screening records it and never re-decides |
| INV-10 | RLS on every table; advisory-lock keys include tenant-scoped ids only |
| BR-06 | `NumberInventory` check at start; `ChoosePresentation` (via telephony) per attempt (LLD-03 §3.12); fail closed |

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
2. **Licence counting for the nailed-up leg.** Capacity is reserved by
   telephony (decided). *Recommend:* `OpenSession` does not reserve;
   only customer legs count, since the agent leg is internal and counting
   it would halve a tenant's usable channels. LLD-08 must confirm.
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
7. **Abandonment breach stops the campaign** (AC-15.3 "stops"): state
   `STOPPED_COMPLIANCE`; only a compliance officer resumes, with a
   reason, and the campaign restarts locked in reserved mode until the
   ratio is below limit − margin. *Recommend:* adopt as written
   (alternative: an automatic pause a manager may resume — weaker
   evidence for a regulator). Needs sign-off.

## 15. Handoff

OpenSpec changes in this order across LLD-04 and LLD-06. The migration
column is **provisional** (D-100): each change takes the next free numbers
in `api/migrations` when it is proposed, in this order, and updates the
LLD in the same change; two changes never share a migration:

| # | Change | Migration | Content |
|---|---|---|---|
| 1 | `compliance-profiles-and-gate` (LLD-04) | 0050, 0051 | profiles, windows, DNC, opt-outs, consents, `Evaluate`, `Precheck` |
| 2 | `compliance-evidence` (LLD-04) | 0052 | decisions, `Decide`, export |
| 3 | `dialer-lists-import` | 0053 | OB-01 import, segments |
| 4 | `dialer-campaigns` | 0054 | campaign lifecycle, caller-ID pools, dispositions, retry rules |
| 5 | `dialer-agent-session` | 0055 | nailed-up leg, pause (SPIKE-D2/D4 first; needs LLD-01 `DialerMedia`) |
| 6 | `dialer-preview` | 0056 | leads, hopper, attempts + verdict trigger; OB-02, OB-05, OB-10, OB-12 (after LLD-03 `0035_number_inventory`) |
| 7 | `dialer-power` | 0057 | pacer at ratio 1.0, OB-03, OB-07 minimum ring, OB-11 |
| 8 | `compliance-abandonment` (LLD-04) | 0058 | buckets, abandon events, rule 12, `AbandonLedger` |
| 9 | `dialer-abandonment` | — | evaluator, ratio > 1.0, OB-06, OB-07 message |
| 10 | `dialer-callbacks` | 0059 | OB-09 scheduler |
| R2 | `dialer-predictive`, `dialer-amd` | next free numbers at propose time | OB-04, OB-08 R2 |

**References:** [PRD EPIC-14/15](../PRD.md), [BRD](../BRD.md),
[HLD 04 §10.1](../hld/04-bounded-contexts.md),
[HLD-20](../hld/20-call-flow-catalogue.md),
[HLD-21](../hld/21-media-and-call-control-flows.md),
[LLD-04](LLD-04-compliance.md), [LLD-03](LLD-03-pbx-core.md),
[LLD-08](LLD-08-licensing.md), [LLD README](README.md),
[Asterisk ARI](https://docs.asterisk.org/Configuration/Interfaces/Asterisk-REST-Interface-ARI/),
[VICIdial](https://www.vicidial.org/docs/),
[Amazon Connect outbound campaigns](https://docs.aws.amazon.com/connect/latest/adminguide/enable-outbound-campaigns.html).
