# LLD-09 — Reporting, Recording Governance and the CDR Ledger

| | |
|---|---|
| **Bounded context** | `reporting` (Tier 2, HLD 04 §5); recording **execution** is `telephony-core` and recording **policy** is `pbx-core` configuration — both specified here because the flows are one design (§2) |
| **Status** | **First slice implemented** (`pbx-reporting`, D-88: terminated-call read model, participant attribution, agent scope, audited CSV). **Revision 2 (2026-11-07) — Draft** to the D-98 Definition of Ready for RC-01, RC-02, RC-03, RC-05, RC-06, RC-07 |
| **Flows** | RC-01 consent + recording; RC-02 recording access; RC-03 retention, deletion and legal hold; RC-04 CDR read and export (built); RC-05 event-fed append-only CDR ledger; RC-06 per-participant quality; RC-07 PCI pause/resume (HLD-20 §5) |
| **Traces to** | BRD FBR-R1-09, FBR-R1-10, BR-03, BR-04, BR-07, BR-11; PRD EPIC-07 (US-07.1–07.7, AC-07.1–07.8), EPIC-05 (US-05.4, AC-05.4), §13.5, INV-03/06/08/10/11; HLD 04 §5; HLD-07 §1; HLD-21 §7; LLD-01 §11; LLD-03; LLD-05; DECISIONS D-24, D-56, D-57, D-58, D-87, D-88, D-92, D-95, D-98 |
| **Why this LLD** | Partners bill and auditors verify from this data. It must be immutable, attributable, complete and tamper-evident, and recording must be lawful, without any of it becoming a call-path dependency |

**Revision 2 resolves these D-98 audit findings:** §3 of revision 1 built
`call_records` from telephony's `calls` table — replaced by the D-92
event-fed ledger (RC-05); `recording_access` had no `reason` (BR-04) —
added (RC-02); the US-07.1 policy model was missing — added (RC-01); RC-06
cited "RTCP-XR", which Asterisk 22 does not emit — replaced by RTCP SR/RR
with a SPIKE; `usage_seconds.quality_mos DEFAULT 4.50` would fabricate a
score — the ledger never copies it (§8.9).

## 1. Scope and non-goals

**R1.0 (this revision):** the CDR ledger with adjustments, hash chain,
anchors and verification; recording policy, consent announcement,
mixed recording, on-demand start/stop, PCI pause/resume; encrypted object
storage; reason-audited access; automatic retention, erasure and legal
hold; per-participant quality windows and summaries; usage and CDR export.

**Not in scope:** rating/pricing (partner-owned); a custom report
builder; separated per-participant recording channels (R3, HLD-21 §7);
transcripts, summaries and post-call analytics (R2, LLD-10 §6); Asterisk
CDR/CEL as a source of truth (diagnostic only); crypto-shredding of
recordings (R2 option, §7.10).

## 2. Ownership, permissions and dependencies

| Concern | Owner | Why |
|---|---|---|
| Recording policy rows and their resolution | `pbx-core` (config) + pure resolver | Consulted in the call path at routing time; `reporting` may never be called synchronously (HLD 04 §10.1) |
| Announcement, `bridges/{id}/record`, mute/unmute, upload | `telephony-core` ACL | The only code that touches ARI |
| `StartCallRecording` / `StopCallRecording` / `PauseCallRecording` / `ResumeCallRecording` RPCs | `telephony-core` `CallService` | They act on a live call |
| Recording metadata, access, retention, holds, CDR ledger, quality, exports | `reporting` | Fed by events only (D-92) |
| Object store adapter | shared infrastructure `internal/objectstore` (like `internal/postgres`) | Used by telephony (put) and reporting (get, delete) through their own ports |

**Required HLD 04 §10.1 change:** `reporting` may subscribe to the
published events of `telephony-core`, `pbx-core` **and `ai-pipeline`**
(AI stream duration is a CDR field, AC-05.4), still with no synchronous
call into any context except `entitlement`.

**New permissions** (identity `DefaultPermissions`; `TENANT_ADMIN` keeps `*`):

| Action | Default roles | Scope rule |
|---|---|---|
| `report.read` | SUPERVISOR | AGENT keeps the D-88 own-calls read under `call.read` |
| `report.export` | SUPERVISOR | existing |
| `cdr.adjust` | TENANT_ADMIN only | — |
| `cdr.verify` | TENANT_ADMIN only | PLATFORM_ADMIN only with an explicit tenant, audited |
| `recording.policy.manage` | TENANT_ADMIN only | — |
| `recording.read` (list, metadata) | SUPERVISOR | — |
| `recording.play` | SUPERVISOR | reason required |
| `recording.download` / `recording.export` | TENANT_ADMIN only | reason required |
| `recording.delete` | TENANT_ADMIN only | refused under legal hold |
| `recording.legal_hold` | TENANT_ADMIN only | — |
| `recording.audit.read` | TENANT_ADMIN only | auditor view of `recording_access` |
| `recording.control` (start/stop/pause/resume) | AGENT (only calls they are a participant in), SUPERVISOR (tenant) | stop refused when policy is `always` |

**OWNER DECISION — compliance-officer role.** PRD personas include a
compliance officer and an auditor, which no role preset serves without
`*`. *Recommended:* add a `COMPLIANCE_OFFICER` preset (`recording.*`
except `control`, `report.read`, `report.export`, `cdr.verify`,
`recording.audit.read`), no call control.

## 3. RC-05 — Event-fed, append-only, hash-chained CDR ledger (D-92)

### 3.1 Trigger, actors, preconditions

| Item | Value |
|---|---|
| Trigger | `call.terminated` **v2** on `tenant.<id>.event.call.terminated` (durable consumer `reporting-cdr`); `AdjustCallRecord` RPC; anchor timer; `VerifyCDRChain` RPC |
| Actors | System (projector); partner finance manager / tenant admin with `cdr.adjust`; auditor/admin with `cdr.verify` |
| Preconditions | Event validates against the v2 schema (§3.3); the call is terminated; tenant exists. **Invalid event:** written to `cdr_rejects` with the raw payload, acked, alert `reporting.cdr_rejected` — never a partial ledger row |
| Cross-LLD dependency | `telephony-core` must publish `call.terminated` v2 (LLD-01 change; the walking-skeleton payload carries only `CallID`, `Reason`) |

### 3.2 Sequence

| # | Owner | Step |
|---|---|---|
| 1 | telephony | On Terminated, writes the outbox row `call.terminated` v2 (full snapshot, §3.3) in the same transaction as the state change |
| 2 | reporting | Consumer `reporting-cdr` (explicit ack, `AckWait 60s`, `MaxDeliver` unlimited, backoff 1 s → 5 min) receives it |
| 3 | reporting | `ValidateTerminated` (pure) |
| 4 | reporting | Transaction as app role under RLS: `pg_advisory_xact_lock(hashtextextended('cdr_records:' || tenant_id, 0))` |
| 5 | reporting | `SELECT 1 FROM cdr_records WHERE tenant_id=$1 AND call_id=$2` → exists: commit, ack (idempotent, D-58) |
| 6 | reporting | Read chain head: `SELECT chain_seq, row_hash … ORDER BY chain_seq DESC LIMIT 1` (genesis: seq 0, 32 zero bytes) |
| 7 | reporting | `HashRecord(prev, record)` (pure, §3.5); INSERT `cdr_records` + `cdr_participants`; DELETE the `cdr_pending` row; INSERT outbox `cdr.recorded` |
| 8 | reporting | Commit, ack |
| 9 | reporting | Consumer `reporting-cdr-pending` on `call.initiated` inserts `cdr_pending(call_id, initiated_at)` (`ON CONFLICT DO NOTHING`); a call already recorded is skipped |
| 10 | reporting | Hourly: pending rows older than `cdr.missing_after` (24 h) → alert `reporting.cdr_missing` naming call ids (AC-07.5) |
| 11 | reporting | Anchor timer (§3.8) |

Adjustment: `AdjustCallRecord` → same lock on `'cdr_adjustments:' || tenant_id`
→ head of the adjustments chain → `HashAdjustment` → INSERT
`cdr_adjustments` + outbox `cdr.adjusted` + identity audit `cdr.adjust`.

### 3.3 `call.terminated` v2 payload (published by telephony-core)

`call_id, direction, source_digits, dest_digits, started_at, answered_at?, ended_at, termination_reason, disposition (answered|no_answer|busy|failed|rejected|cancelled), service_type (pbx|contact_centre|campaign), department_id?, route_id?, carrier_id?, trunk_id?, cost_micros, participants[]{participant_id, role, extension_id?, principal_id?, joined_at, answered_at?, left_at, billable_seconds, usage_seconds, cost_micros}`.
Times are UTC, microsecond precision (Postgres precision; the hash input
must not depend on a precision the database discards).

### 3.4 State

Ledger rows have no state: they are written once. The per-call
projection has a short lifecycle in `cdr_pending`:

| From | Event | To |
|---|---|---|
| — | `call.initiated` | `pending` |
| `pending` / none | valid `call.terminated` v2 | `recorded` (pending row deleted) |
| `pending` | age > 24 h | `missing` (alert; stays pending until the event arrives or the outbox is republished) |
| `recorded` | `AdjustCallRecord` | `recorded` + adjustment (effective values = record + Σ adjustments) |

### 3.5 Hash chain

- Two chains per tenant: records and adjustments. `chain_seq` starts at 1;
  `UNIQUE (tenant_id, chain_seq)` makes a fork impossible.
- `row_hash = SHA-256("atsapbx-cdr-v1" ‖ prev_hash ‖ enc(f1) ‖ … ‖ enc(fn))`,
  where `enc(x)` is a 4-byte big-endian length followed by UTF-8 bytes;
  times are RFC 3339 UTC with exactly six fractional digits; absent
  values encode as length `0xFFFFFFFF`; participants are encoded in
  `participant_id` order after the record fields. The field order is the
  DDL column order below and is versioned by `hash_version`.
- Canonical binary, not JSON, so no JSON canonicalisation library is
  needed and a key-ordering bug cannot break verification.

### 3.6 Data model — migration `0061_cdr_ledger`

```sql
-- Shared guard for every append-only table in 0060–0066.
CREATE FUNCTION atsapbx_forbid_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' AND current_user = 'atsap_retention'
     AND current_setting('atsapbx.retention_purge', true) = 'on' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION '% on % is forbidden: append-only (BR-07, INV-08)', TG_OP, TG_TABLE_NAME;
END $$;

CREATE TABLE cdr_records (
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  call_id UUID NOT NULL,
  chain_seq BIGINT NOT NULL CHECK (chain_seq >= 1),
  direction TEXT NOT NULL CHECK (direction IN ('INBOUND','OUTBOUND','INTERNAL')),
  source_digits TEXT NOT NULL, dest_digits TEXT NOT NULL,
  started_at TIMESTAMPTZ NOT NULL, answered_at TIMESTAMPTZ, ended_at TIMESTAMPTZ NOT NULL,
  duration_seconds INT NOT NULL CHECK (duration_seconds >= 0),
  billable_seconds INT NOT NULL CHECK (billable_seconds >= 0),
  termination_reason TEXT NOT NULL,
  disposition TEXT NOT NULL,
  service_type TEXT NOT NULL,
  department_id UUID, route_id UUID, carrier_id UUID, trunk_id UUID,
  cost_micros BIGINT NOT NULL DEFAULT 0,
  source TEXT NOT NULL CHECK (source IN ('event','backfill')),
  source_event_id UUID,
  recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  hash_version SMALLINT NOT NULL DEFAULT 1,
  prev_hash BYTEA NOT NULL CHECK (length(prev_hash) = 32),
  row_hash BYTEA NOT NULL CHECK (length(row_hash) = 32),
  PRIMARY KEY (tenant_id, call_id),
  UNIQUE (tenant_id, chain_seq)
);
CREATE INDEX cdr_records_started ON cdr_records (tenant_id, started_at DESC);
CREATE INDEX cdr_records_source ON cdr_records (tenant_id, source_digits);
CREATE INDEX cdr_records_dest ON cdr_records (tenant_id, dest_digits);

CREATE TABLE cdr_participants (
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  call_id UUID NOT NULL,
  participant_id UUID NOT NULL,
  role TEXT NOT NULL, extension_id UUID, principal_id UUID,
  joined_at TIMESTAMPTZ NOT NULL, answered_at TIMESTAMPTZ, left_at TIMESTAMPTZ NOT NULL,
  billable_seconds INT NOT NULL, usage_seconds INT NOT NULL, cost_micros BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (tenant_id, participant_id),
  FOREIGN KEY (tenant_id, call_id) REFERENCES cdr_records (tenant_id, call_id)
);
CREATE INDEX cdr_participants_ext ON cdr_participants (tenant_id, extension_id);
CREATE INDEX cdr_participants_principal ON cdr_participants (tenant_id, principal_id);

CREATE TABLE cdr_adjustments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  call_id UUID NOT NULL,
  participant_id UUID,
  chain_seq BIGINT NOT NULL CHECK (chain_seq >= 1),
  kind TEXT NOT NULL CHECK (kind IN ('billable_seconds','cost_micros','disposition','annotation')),
  delta_seconds INT, delta_cost_micros BIGINT, new_disposition TEXT,
  reason TEXT NOT NULL CHECK (length(reason) BETWEEN 10 AND 500),
  actor_principal_id UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  hash_version SMALLINT NOT NULL DEFAULT 1,
  prev_hash BYTEA NOT NULL, row_hash BYTEA NOT NULL,
  UNIQUE (tenant_id, chain_seq),
  FOREIGN KEY (tenant_id, call_id) REFERENCES cdr_records (tenant_id, call_id)
);

CREATE TABLE cdr_ai_streams (                 -- AI stream duration per call (AC-05.4)
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  stream_id UUID PRIMARY KEY,
  call_id UUID NOT NULL, participant_id UUID NOT NULL, provider_id UUID NOT NULL,
  seconds INT NOT NULL CHECK (seconds >= 0),
  source_event_id UUID NOT NULL,
  recorded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE cdr_anchors (
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  anchored_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  records_seq BIGINT NOT NULL, records_head BYTEA NOT NULL,
  adjustments_seq BIGINT NOT NULL, adjustments_head BYTEA NOT NULL,
  PRIMARY KEY (tenant_id, anchored_at)
);

CREATE TABLE cdr_pending (                    -- mutable working set, not evidence
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  call_id UUID PRIMARY KEY,
  initiated_at TIMESTAMPTZ NOT NULL,
  alerted_at TIMESTAMPTZ
);
CREATE TABLE cdr_rejects (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  event_id UUID NOT NULL UNIQUE, payload JSONB NOT NULL, problem TEXT NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- RLS `ENABLE` + `FORCE` on every table, policy
  `FOR ALL USING (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid)`.
- **INSERT-only:** `REVOKE UPDATE, DELETE, TRUNCATE ON cdr_records, cdr_participants, cdr_adjustments, cdr_ai_streams, cdr_anchors FROM atsapbx_app;`
  and `CREATE TRIGGER … BEFORE UPDATE OR DELETE … FOR EACH ROW EXECUTE FUNCTION atsapbx_forbid_mutation()`
  plus a `BEFORE TRUNCATE` statement trigger. The table owner could
  re-grant itself; the trigger is the guard the owner cannot bypass
  without a migration (which CI reviews).
- **Retention role** `atsap_retention` (`NOBYPASSRLS`, created by the deploy
  bootstrap): `SELECT, DELETE` on ledger and recording tables; sets
  `app.tenant_id` per tenant and `atsapbx.retention_purge = on` per
  transaction.
- **Partitioning:** `cdr_records`/`cdr_participants` become monthly range
  partitions on `started_at` when a tenant passes 5 M rows (AC-07.8);
  the PK then includes `started_at`. Not in the first migration.
- **Retention:** `cdr.retention_months` (OWNER DECISION, §11); purge is
  by whole months after an anchor at the purge boundary (§3.8).

### 3.7 Ports

```go
// domain (pure)
func ValidateTerminated(ev TerminatedV2) error
func HashRecord(prev [32]byte, r CDRRecord, ps []CDRParticipant) [32]byte
func HashAdjustment(prev [32]byte, a Adjustment) [32]byte
func VerifyChain(start [32]byte, entries []ChainEntry) VerifyResult // first bad seq, reason
func Effective(r CDRRecord, adj []Adjustment) EffectiveRecord
func ValidateAdjustment(r CDRRecord, a Adjustment) error

// ports
type Ledger interface {
    AppendRecord(ctx context.Context, tenant TenantID, ev TerminatedV2, eventID uuid.UUID) (appended bool, err error)
    AppendAdjustment(ctx context.Context, tenant TenantID, a Adjustment) (Adjustment, error)
    AppendAIStream(ctx context.Context, tenant TenantID, s AIStreamUsage) error
    Chain(ctx context.Context, tenant TenantID, kind ChainKind, fromSeq, toSeq int64) ([]ChainEntry, error)
    Anchor(ctx context.Context, tenant TenantID) (*Anchor, error) // nil when the head has not moved
}
type Store interface { // reads, replacing the D-88 store
    List(ctx context.Context, tenant TenantID, f Filter, page Page) ([]EffectiveRecord, error)
    Get(ctx context.Context, tenant TenantID, call CallID) (EffectiveRecord, []Adjustment, error)
    Export(ctx context.Context, tenant TenantID, f Filter, max int) ([]EffectiveRecord, error)
}
type EventConsumer interface { Run(ctx context.Context, subject string, h func(context.Context, Msg) error) error }
```

### 3.8 Anchoring and verification

- Every `cdr.anchor_interval` (1 h), for each tenant whose head moved,
  INSERT `cdr_anchors` and publish `cdr.anchored` (webhook/stream
  deliverable, LLD-05 §3.2). The partner's copy is the external witness:
  rewriting the chain afterwards cannot rewrite anchors already delivered.
- `VerifyCDRChain` recomputes hashes from `from_seq` (starting from the
  `prev_hash` stored at `from_seq`, or from the earliest retained anchor
  after a purge) and compares every anchor in range.
- Purge: before dropping month M, an anchor at the last seq of M is
  written; verification thereafter starts from it.

### 3.9 API — `ReportingService`

| RPC | Fields | Permission | Refusals → console words |
|---|---|---|---|
| `ListCallRecords` (existing) | + filters `queue_id, campaign_id, disposition, has_recording, mos_below`; response adds `effective_billable_seconds, adjustments_count, chain_seq` | `call.read` (agent own) / `report.read` | `InvalidArgument` "Choose a range of at most 92 days." |
| `GetCallRecord` | `call_id` → record, participants, adjustments, quality summary, recordings, AI streams | as above | `NotFound` "No record for this call yet. Calls appear after they end." |
| `AdjustCallRecord` | `call_id, participant_id?, kind, delta_seconds?, delta_cost_micros?, new_disposition?, reason` → adjustment | `cdr.adjust` | `InvalidArgument` "A reason of at least 10 characters is required." · `FailedPrecondition` "The adjustment would make billable time negative." |
| `VerifyCDRChain` | `chain (records\|adjustments\|both), from_seq?, to_seq?` (≤ 1,000,000 rows) → `ok, verified, first_bad_seq?, problem (hash_mismatch\|seq_gap\|anchor_mismatch), head_seq, head_hash, last_anchor_at` | `cdr.verify` | `ResourceExhausted` "Verify at most 1,000,000 records per request." |
| `ExportCallRecords` (existing) | unchanged, now from the ledger; adds effective values | `report.export` | existing |
| `ExportUsage` | `from, to, granularity (call\|participant)` → CSV with AC-05.4 fields incl. `ai_stream_seconds`; > 10,000 rows → async job, `report.export_ready` | `report.export` | `InvalidArgument` "Choose a range of at most 31 days." |

### 3.10 Configuration

| Key | Default | Range | Scope |
|---|---|---|---|
| `cdr.missing_after` | 24 h | 1–72 h | installation |
| `cdr.anchor_interval` | 1 h | 5 min–24 h | installation |
| `cdr.verify_max_rows` | 1,000,000 | fixed | installation |
| `cdr.retention_months` | 24 (OWNER DECISION) | 12–120 | tenant |
| `report.sync_export_max_rows` | 10,000 | fixed | installation |

### 3.11 Failure and recovery

| Failure | Behaviour |
|---|---|
| Duplicate event | Step 5 skips; acked |
| Projector crash mid-transaction | Rolled back; redelivered |
| Two events for one tenant concurrently | Advisory lock serialises; the chain cannot fork (unique seq) |
| Event missing (lost publish, NATS without persistence) | `cdr_missing` alert after 24 h; operator runs `atsapbx outbox republish --since`; dedupe makes it safe |
| Invalid payload | `cdr_rejects` + alert; fix the producer, then republish |
| Reporting down | Calls unaffected (INV-06); consumer catches up from JetStream |
| Hash mismatch found by verify | `reporting.cdr_tamper_detected` critical alert naming the first bad seq; nothing is "repaired" — the evidence is kept |

### 3.12 Migration plan from the D-88 read model (change `reporting-cdr-ledger`)

| Step | Change | Rollback |
|---|---|---|
| 1 | Migration `0061`; projector and pending consumer deployed, writing in parallel; reads still on D-88 store | drop consumers |
| 2 | `telephony-core` publishes `call.terminated` v2 (LLD-01) | v1 consumers ignore extra fields |
| 3 | One-shot `cmd/atsap-backfill-cdr`: for terminated calls before the projector's first event, in `started_at` order per tenant, append `source='backfill'` rows (reads telephony tables — the last sanctioned read, D-92) | rows are append-only: rollback = restore |
| 4 | Shadow compare for 7 days: counts and per-call fields equal between D-88 store and ledger; any diff fails the gate | stay on step 3 |
| 5 | Switch `Store` to ledger reads; delete `internal/reporting/postgres/store.go`'s telephony queries | redeploy previous build |
| 6 | **archtest** `ScanReportingTelephonyTables` (pattern of `accounttables.go`): any SQL string in `internal/reporting/**` naming `calls`, `call_participants`, `usage_seconds` or `channel_history` fails; fault-injected once | — |
| 7 | Delete `cmd/atsap-backfill-cdr` after the R1.0 gate; D-92 marked closed | — |

### 3.13 Invariants, events, audit

INV-08/BR-07 (revoke + trigger; adjustments only); INV-06 (projector is
async); INV-10 (RLS; `NotFound` cross-tenant); AC-07.5 (unique call id +
pending/missing alert); AC-07.6 (adjustments with actor and reason).
Events: `cdr.recorded`, `cdr.adjusted`, `cdr.anchored` (public),
`reporting.cdr_missing`, `reporting.cdr_rejected`,
`reporting.cdr_tamper_detected` (internal alerts). Audit: `cdr.adjust`,
`cdr.verify`, `report.export` (existing).

### 3.14 Tests

| # | Tier | Given / When / Then |
|---|---|---|
| L-01 | U | Given a fixed record and prev hash, When `HashRecord` runs, Then it equals the golden vector; changing any field, participant order or a microsecond changes it |
| L-02 | U | Given a chain with row 5 edited, When `VerifyChain` runs, Then `first_bad_seq = 5, hash_mismatch`; with row 5 deleted, `seq_gap` |
| L-03 | U | Given an adjustment of −120 s on a 60 s record, When `ValidateAdjustment` runs, Then it is refused |
| L-04 | I | Given one `call.terminated` v2 delivered three times, When the projector runs, Then exactly one row and chain_seq advances by one |
| L-05 | I | Given 50 concurrent events for one tenant, When projected, Then seqs are 1..50 with no gap and the chain verifies |
| L-06 | I | Given a ledger row, When the app role runs UPDATE, DELETE or TRUNCATE, Then each fails; When `atsap_retention` deletes without the GUC, Then it fails |
| L-07 | I | Given tenant A's call id, When tenant B calls `GetCallRecord`, Then `NotFound` |
| L-08 | I | Given a `call.initiated` with no terminated event, When 24 h pass (fake clock), Then `cdr_missing` fires naming it |
| L-09 | I | Given the archtest rule, When a reporting file names `call_participants` in SQL, Then the test fails |
| L-10 | E | Given a real call, When it ends, Then a ledger row with attributed participants exists and `cdr.recorded` reaches a webhook within 2 s |
| L-11 | C | Given the Reports page, When an admin adjusts a record, Then the reason dialog is mandatory and the row shows original and effective values |
| L-12 | A | Given a full day of rig traffic, When ledger rows are compared with the call list, Then none is missing or duplicated (AC-07.5) and `VerifyCDRChain` is ok |

### 3.15 Industry parity

| Product | CDR model | Our difference |
|---|---|---|
| Asterisk CDR/CEL | Rows written by the engine to CSV/ODBC; editable by anyone with DB access | Diagnostic only; our ledger is event-fed, append-only, chained |
| 3CX | Call log/reports in its database; admin can purge | Purge only by retention, after anchoring |
| FusionPBX | `v_xml_cdr` table, editable | Corrections are attributable adjustments |
| Amazon Connect | Contact trace records streamed via Kinesis; a CTR can be updated after the contact (new versions) | We never update; we append adjustments, and partners receive anchors |

## 4. RC-01 — Recording policy, consent and capture

### 4.1 Trigger, actors, preconditions

| Item | Value |
|---|---|
| Trigger | A call is routed to a destination (pbx-core) — automatic recording; or `StartCallRecording` / `StopCallRecording` (on-demand) |
| Actors | Compliance officer/tenant admin sets policy (`recording.policy.manage`, US-07.1/07.2); agent or supervisor uses on-demand (`recording.control`); the system records |
| Preconditions | Entitlement: recording is core in R1.0 (no module gate); object store configured and reachable at startup (else recording is `unavailable` and §4.8 applies); Asterisk recording directory has ≥ `recording.min_free_disk` free |
| Emergency calls | **Never gated by recording** (INV-01): no announcement, no decline; recorded only if the policy says so and capture starts without delay |

### 4.2 Policy model (US-07.1) — migration `0063_recording_policies` (owned by `pbx-core`)

```sql
CREATE TABLE recording_jurisdictions (        -- installation-wide reference data, no tenant_id
  code TEXT PRIMARY KEY,                      -- ISO 3166-1 alpha-2, or alpha-2 + '-' + subdivision
  consent TEXT NOT NULL CHECK (consent IN ('one_party','all_party','unknown')),
  default_retention_days INT,
  note TEXT NOT NULL,                         -- source summary; not legal advice
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE recording_settings (             -- one row per tenant
  tenant_id UUID PRIMARY KEY REFERENCES tenants(id),
  default_jurisdiction TEXT NOT NULL REFERENCES recording_jurisdictions(code),
  default_retention_days INT NOT NULL DEFAULT 90 CHECK (default_retention_days BETWEEN 1 AND 3650),
  default_prompt TEXT NOT NULL DEFAULT 'sound:atsapbx/call-may-be-recorded',
  auto_resume_seconds INT NOT NULL DEFAULT 180 CHECK (auto_resume_seconds BETWEEN 30 AND 900),
  ai_tap_requires_consent BOOLEAN NOT NULL DEFAULT TRUE,   -- OWNER DECISION §11
  updated_by UUID NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE recording_policies (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  scope_kind TEXT NOT NULL CHECK (scope_kind IN ('tenant','department','trunk','did','queue','extension')),
  scope_id UUID,                               -- NULL only for 'tenant'
  direction TEXT NOT NULL DEFAULT 'any' CHECK (direction IN ('any','inbound','outbound','internal')),
  mode TEXT NOT NULL CHECK (mode IN ('always','on_demand','never')),
  consent TEXT NOT NULL DEFAULT 'by_jurisdiction' CHECK (consent IN ('by_jurisdiction','announce','none')),
  prompt TEXT,                                 -- NULL = tenant default
  on_consent_failure TEXT NOT NULL DEFAULT 'do_not_record' CHECK (on_consent_failure IN ('do_not_record','decline_call')),
  on_record_failure TEXT NOT NULL DEFAULT 'continue' CHECK (on_record_failure IN ('continue','decline_call')),
  retention_days INT CHECK (retention_days BETWEEN 1 AND 3650),  -- NULL = tenant/jurisdiction default
  allow_pause BOOLEAN NOT NULL DEFAULT TRUE,
  created_by UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_by UUID NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK ((scope_kind = 'tenant') = (scope_id IS NULL))   -- consent='none' is validated in the domain against the jurisdiction
);
CREATE UNIQUE INDEX recording_policies_scope ON recording_policies (tenant_id, scope_kind, COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'), direction);
```

RLS (standard policy) on `recording_settings` and `recording_policies`;
`recording_jurisdictions` is read-only to the app (`GRANT SELECT`), seeded
by migration, changed only by migration. Policies are configuration,
not evidence: updates are allowed and every change is audited with
before/after.

**Resolution (pure, `pbx-core/domain`):**

```go
func ResolveRecording(f CallFacts, ps []RecordingPolicy, s RecordingSettings, j map[string]Jurisdiction) RecordingDirective
// RecordingDirective{Record: bool, Mode, Announce: bool, Prompt, RetentionDays, OnConsentFailure, OnRecordFailure, AllowPause, PolicyID, Explanation []string}
```

| Rule | Value |
|---|---|
| Precedence (most specific wins) | extension > queue > did > trunk > department > tenant; within one scope, an exact `direction` beats `any` |
| No policy at all | `never` (recording is opt-in per tenant) |
| Announce when | policy `consent = announce`; or `by_jurisdiction` and **any** party's jurisdiction is `all_party` or `unknown` (strictest wins) |
| `consent = none` | Refused at save (`FailedPrecondition`) if the tenant default jurisdiction is `all_party`; it remains possible for one-party tenants calling all-party numbers only via `by_jurisdiction` |
| Party jurisdiction | External party: E.164 country (country level only in R1.0); internal party: tenant `default_jurisdiction` |
| Retention | policy → jurisdiction default → tenant default |
| Explanation | Ordered list of the rules applied (shown by `ExplainRecordingPolicy` and logged with the call) |

### 4.3 Sequence (automatic, inbound to an extension or queue)

| # | Owner | Step (ARI) |
|---|---|---|
| 1 | telephony | `StasisStart` for the caller; Call created (Screening per LLD-01) |
| 2 | pbx-core | Routing resolves the destination (D-91); builds `CallFacts`; `ResolveRecording` → directive (pure, < 1 ms; policies loaded per call in the routing transaction) |
| 3 | telephony | If `Announce`: `POST /channels/{caller}/answer` (if not yet), then `POST /channels/{caller}/play?media=<prompt>&playbackId=consent-<recordingId>`; await `PlaybackFinished` (timeout = prompt duration + 5 s, cap 30 s). DTMF is ignored during the prompt |
| 4 | telephony | `PlaybackFinished{state: done}` → consent satisfied (`consent_at`). Caller hangs up → no recording, normal termination. `state: failed` or timeout → `OnConsentFailure`: `do_not_record` (call proceeds, `recording.failed{stage: consent}`) or `decline_call` (play `atsapbx/cannot-connect` best effort, hang up with reason `recording_consent_failed`) |
| 5 | telephony | Bridge formed (LLD-01). If `OnRecordFailure = decline_call`: start recording while only the caller is in the bridge and add the answering party only after `RecordingStarted`; else add both, then record |
| 6 | telephony | `POST /bridges/{bridgeId}/record?name=rec-<recordingId>&format=wav&ifExists=fail&beep=false&terminateOn=none&maxDurationSeconds=<recording.max_duration>` → `RecordingStarted` → outbox `recording.started` |
| 7 | telephony | Bridge change (transfer/consult per HLD-21 §1): `POST /recordings/live/rec-<id>/stop` on the old bridge, new segment `rec-<id>-<n>` on the new bridge; pause state carries over |
| 8 | telephony | `RecordingFinished` (call end or stop) → upload (§4.5) |

Outbound: the announcement plays on the **callee** channel after answer
and before the callee joins the bridge (the internal caller keeps
ringback); then step 5. Internal calls follow the policy with
`direction = internal` and announce only when the policy says `announce`.

**On demand** (`mode = on_demand`): `StartCallRecording` → if `Announce`,
`POST /bridges/{id}/play?media=<prompt>` (every party hears it), await
`PlaybackFinished`, then step 6. `StopCallRecording` →
`POST /recordings/live/rec-<id>/stop`.

### 4.4 State machine — recording segment (telephony-owned, in memory + events)

| From | Trigger / guard | To |
|---|---|---|
| — | directive `Record` and announce required | `AwaitingConsent` |
| — | directive `Record`, no announce / on-demand start without announce | `Starting` |
| `AwaitingConsent` | `PlaybackFinished done` | `Starting` |
| `AwaitingConsent` | failure / timeout | `NotRecorded` (or call declined) |
| `Starting` | `RecordingStarted` | `Recording` |
| `Starting` | `RecordingFailed` / ARI 4xx/5xx | `Failed` (`OnRecordFailure`) |
| `Recording` | `PauseCallRecording` / pause carried | `Paused` (RC-07) |
| `Paused` | `ResumeCallRecording` / auto-resume timer | `Recording` |
| `Recording`/`Paused` | `StopCallRecording` (mode ≠ always) / bridge destroyed / max duration | `Finished` |
| `Finished` | upload ok + verified | `Stored` |
| `Finished` | upload fails after retries | `UploadPending` (file kept on engine disk; retried every 5 min, §4.8) |

### 4.5 Storage

| Step | Detail |
|---|---|
| Fetch | `GET /recordings/stored/rec-<id>/file` streamed (no full buffering) |
| Hash | SHA-256 while streaming |
| Put | `ObjectStore.Put(ctx, key, reader, size, PutOptions{SSE: true, ContentType: "audio/wav"})`, key `t/<tenant_id>/rec/<yyyy>/<mm>/<recording_id>.wav` (no number or name in the key) |
| Verify | `Stat` size equals; else retry |
| Publish | outbox `recording.stored{recording_id, call_id, segment_no, policy_id, consent, consent_at?, started_at, ended_at, duration_seconds, size_bytes, sha256, object_key, retention_until, paused_intervals[]}` |
| Clean | `DELETE /recordings/stored/rec-<id>` only after the outbox commit |

**OWNER DECISION — object store and key ownership.** *Recommended:*
S3-compatible store (MinIO in the reference deployment) with server-side
encryption under a **platform-managed per-deployment key** in R1.0; the
bucket has versioning **off** (or a 1-day non-current expiry) so deletion
is real; tenant-supplied KMS keys are R2. Adding an S3 client
(`minio-go` v7) is a new dependency and needs the owner's approval.

### 4.6 Data model — migration `0064_recordings` (owned by `reporting`)

```sql
CREATE TABLE recordings (                     -- inserted from recording.stored; INSERT-only (retention role may delete)
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  call_id UUID NOT NULL, segment_no SMALLINT NOT NULL DEFAULT 1,
  policy_id UUID, consent TEXT NOT NULL CHECK (consent IN ('announced','not_required','on_demand_announced')),
  consent_at TIMESTAMPTZ,
  started_at TIMESTAMPTZ NOT NULL, ended_at TIMESTAMPTZ NOT NULL,
  duration_seconds INT NOT NULL,
  object_key TEXT NOT NULL, content_type TEXT NOT NULL DEFAULT 'audio/wav',
  size_bytes BIGINT NOT NULL, sha256 BYTEA NOT NULL CHECK (length(sha256) = 32),
  retention_until TIMESTAMPTZ NOT NULL,
  source_event_id UUID NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, call_id, segment_no)
);
CREATE INDEX recordings_call ON recordings (tenant_id, call_id);
CREATE INDEX recordings_expiry ON recordings (retention_until);

CREATE TABLE recording_paused_intervals (
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  recording_id UUID NOT NULL REFERENCES recordings(id),
  start_offset_ms INT NOT NULL, end_offset_ms INT NOT NULL CHECK (end_offset_ms >= start_offset_ms),
  paused_by UUID, resumed_by UUID, auto_resumed BOOLEAN NOT NULL,
  PRIMARY KEY (recording_id, start_offset_ms)
);

CREATE TABLE recording_failures (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  call_id UUID NOT NULL, recording_id UUID,
  stage TEXT NOT NULL CHECK (stage IN ('consent','start','upload')),
  reason TEXT NOT NULL, call_declined BOOLEAN NOT NULL,
  source_event_id UUID NOT NULL UNIQUE, at TIMESTAMPTZ NOT NULL
);

CREATE TABLE recording_access (               -- BR-04: person, time, reason; INSERT-only
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  recording_id UUID NOT NULL,                 -- no FK: must survive the recording's deletion
  call_id UUID NOT NULL,
  actor_principal_id UUID NOT NULL,
  action TEXT NOT NULL CHECK (action IN ('play','download','export','metadata_export','delete','legal_hold','legal_hold_release')),
  reason_code TEXT NOT NULL CHECK (reason_code IN ('dispute','quality_review','legal','customer_request','training','other')),
  reason TEXT NOT NULL CHECK (length(reason) BETWEEN 10 AND 500),
  ticket_id UUID,
  source_ip INET, user_agent TEXT CHECK (length(user_agent) <= 256),
  at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX recording_access_rec ON recording_access (tenant_id, recording_id, at DESC);
CREATE INDEX recording_access_actor ON recording_access (tenant_id, actor_principal_id, at DESC);

CREATE TABLE recording_tickets (              -- short-lived playback capability; mutable (use count), purged hourly
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  token_hash BYTEA NOT NULL UNIQUE,           -- SHA-256 of the 32-byte token; token itself never stored
  recording_id UUID NOT NULL, principal_id UUID NOT NULL,
  purpose TEXT NOT NULL CHECK (purpose IN ('play','download')),
  expires_at TIMESTAMPTZ NOT NULL, uses INT NOT NULL DEFAULT 0
);

CREATE TABLE recording_legal_holds (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  case_ref TEXT NOT NULL CHECK (length(case_ref) BETWEEN 1 AND 100),
  reason TEXT NOT NULL CHECK (length(reason) BETWEEN 10 AND 500),
  placed_by UUID NOT NULL, placed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE recording_legal_hold_calls (
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  hold_id UUID NOT NULL REFERENCES recording_legal_holds(id),
  call_id UUID NOT NULL,
  PRIMARY KEY (hold_id, call_id)
);
CREATE INDEX legal_hold_calls_call ON recording_legal_hold_calls (tenant_id, call_id);
CREATE TABLE recording_legal_hold_releases (
  hold_id UUID PRIMARY KEY REFERENCES recording_legal_holds(id),
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  released_by UUID NOT NULL, reason TEXT NOT NULL CHECK (length(reason) BETWEEN 10 AND 500),
  released_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE recording_deletions (            -- tombstones: proof of deletion, no content
  recording_id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  call_id UUID NOT NULL, sha256 BYTEA NOT NULL, size_bytes BIGINT NOT NULL,
  cause TEXT NOT NULL CHECK (cause IN ('retention','erasure')),
  actor_principal_id UUID, reason TEXT,
  object_deleted_at TIMESTAMPTZ NOT NULL, deleted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- RLS (standard policy) on all; INSERT-only (revoke + trigger) on
  `recordings`, `recording_paused_intervals`, `recording_failures`,
  `recording_access`, `recording_legal_holds`, `recording_legal_hold_calls`,
  `recording_legal_hold_releases`, `recording_deletions`; the retention role
  may delete `recordings` and `recording_paused_intervals` only.
- An active hold = a hold with no release row.

### 4.7 Ports

```go
// pbx-core domain (pure)
func ResolveRecording(f CallFacts, ps []RecordingPolicy, s RecordingSettings, j map[string]Jurisdiction) RecordingDirective
func ValidatePolicy(p RecordingPolicy, s RecordingSettings, j map[string]Jurisdiction) error

// telephony-core ports
type RecordingControl interface { // inbound port used by CallService and routing
    Arm(ctx context.Context, call CallID, d RecordingDirective) error
    Start(ctx context.Context, call CallID, actor *PrincipalID) (RecordingID, error)
    Stop(ctx context.Context, call CallID, actor PrincipalID) error
    Pause(ctx context.Context, call CallID, actor PrincipalID) (PausedState, error)
    Resume(ctx context.Context, call CallID, actor *PrincipalID) error
}
type EngineRecorder interface { // ACL → ARI
    Play(ctx context.Context, ch EngineChannel, media, playbackID string) error
    RecordBridge(ctx context.Context, br EngineBridge, name string, maxSeconds int) error
    StopLive(ctx context.Context, name string) error
    Mute(ctx context.Context, name string) error
    Unmute(ctx context.Context, name string) error
    FetchStored(ctx context.Context, name string) (io.ReadCloser, int64, error)
    DeleteStored(ctx context.Context, name string) error
}
// internal/objectstore
type ObjectStore interface {
    Put(ctx context.Context, key string, r io.Reader, size int64, o PutOptions) (PutResult, error)
    Get(ctx context.Context, key string, rng *ByteRange) (io.ReadCloser, ObjectInfo, error)
    Stat(ctx context.Context, key string) (ObjectInfo, error)
    Delete(ctx context.Context, key string) error // not-found is success
}
```

### 4.8 Failure and recovery

| Failure | Behaviour (PRD §13.5) |
|---|---|
| Announcement fails | `OnConsentFailure` (default: call continues unrecorded); `recording.failed{stage: consent}`; never recorded without the required announcement (BR-03) |
| `bridges/{id}/record` fails | `OnRecordFailure`: `continue` (default: call proceeds, record marked not recorded with reason, admin alerted) or `decline_call` (only possible because the answering party is added after `RecordingStarted`) |
| Engine disk < `recording.min_free_disk` | New recordings are not started (treated as start failure); alert |
| Object store down at upload | File stays in the Asterisk spool; uploader retries every 5 min with backoff to 1 h; alert after 15 min (HLD-09 F-10); calls unaffected |
| App restart with recordings in progress | Live recordings continue in Asterisk (bridge-owned); on restart the ACL lists `GET /recordings/stored` and uploads any `rec-*` without a `recording.stored` event (idempotent by recording id); re-adopting a *live* recording depends on LLD-01 channel re-adoption — until then a crash ends the call (accepted limitation) and the partial file is uploaded with `ended_at` = file mtime |
| Party hangs up during announcement | Call terminates normally; nothing recorded |
| Transfer during recording | New segment on the new bridge (§4.3 step 7) |
| Concurrent Start/Stop | Serialised per call in the telephony call actor; second request idempotent |

### 4.9 API

Policy (`RecordingPolicyService`, pbx-core):

| RPC | Fields | Permission | Refusals → console words |
|---|---|---|---|
| `GetRecordingSettings` / `UpdateRecordingSettings` | settings row | `recording.policy.manage` | `InvalidArgument` "Unknown jurisdiction." |
| `CreateRecordingPolicy` / `UpdateRecordingPolicy` / `DeleteRecordingPolicy` / `ListRecordingPolicies` | policy row fields | `recording.policy.manage` | `AlreadyExists` "A policy for this {scope} and direction already exists." · `FailedPrecondition` "Your jurisdiction requires every party to be told; choose Announce or By jurisdiction." · `NotFound` "That {scope} does not exist." |
| `ExplainRecordingPolicy` | `direction, did?, trunk_id?, extension_id?, queue_id?, external_number?` → directive + explanation | `recording.policy.manage` | — |
| `ListRecordingJurisdictions` | → rows | `recording.policy.manage` | — |

Live control (`CallService`, telephony-core):

| RPC | Fields | Permission | Refusals → console words |
|---|---|---|---|
| `StartCallRecording` | `call_id` → `recording_id` | `recording.control` (agent: own call) | `FailedPrecondition` "Recording is turned off for this call by policy." · `FailedPrecondition` "Already recording." · `NotFound` "This call has ended." |
| `StopCallRecording` | `call_id` | `recording.control` | `FailedPrecondition` "Recording is required by policy and cannot be stopped. Use Pause for payment details." |

### 4.10 Configuration

| Key | Default | Range | Scope |
|---|---|---|---|
| `recording.format` | `wav` (8 kHz slin) | `wav`, `wav16` | installation |
| `recording.max_duration` | 4 h | 1–12 h | installation |
| `recording.consent_timeout_extra` | 5 s | 1–15 s | installation |
| `recording.min_free_disk` | 2 GiB | 0.5–100 GiB | installation |
| `recording.upload_retry` | 5 min → 1 h backoff | — | installation |
| `recording_settings.*` | see DDL | see DDL | tenant |
| `recording_policies.*` | see DDL | see DDL | object |

### 4.11 Invariants, events, audit

BR-03 (capture starts only from `Starting`, reachable from
`AwaitingConsent` only through `PlaybackFinished done` — a unit-tested
state guard); INV-01 (emergency never declined); INV-03 (a recording
failure never drops an established call; `decline_call` acts only before
the answering party joins); INV-06 (upload and metadata async); INV-10.
Events: `recording.started`, `recording.stored`, `recording.failed`
(public); audit: `recording_policy.create/update/delete`,
`recording_settings.update`, `recording.start/stop` (on demand, actor).

### 4.12 Tests

| # | Tier | Given / When / Then |
|---|---|---|
| R-01 | U | Given tenant policy `always` and extension policy `never`, When resolving a call to that extension, Then `Record = false`, explanation names the extension policy |
| R-02 | U | Given a one-party tenant calling a `+1` number with `by_jurisdiction` and US seeded `all_party`, When resolving, Then `Announce = true` |
| R-03 | U | Given an unknown country, When resolving with `by_jurisdiction`, Then `Announce = true` |
| R-04 | U | Given state `AwaitingConsent`, When `RecordingStarted` arrives without `PlaybackFinished`, Then the transition is refused |
| R-05 | I | Given a policy save with `consent = none` on an all-party tenant, Then `FailedPrecondition` |
| R-06 | I | Given a `recording.stored` event delivered twice, When reporting consumes it, Then one `recordings` row |
| R-07 | E | Given an announce policy, When a rig call is answered, Then the stored WAV's first audible speech starts after the prompt length (AC-07.1, checked by energy detection on the file) |
| R-08 | E | Given `on_record_failure = decline_call` and a stopped object store **and** full disk, When a call arrives, Then the caller is declined and the agent phone never rings |
| R-09 | E | Given MinIO stopped, When a recorded call ends, Then the call is unaffected and the file uploads after MinIO restarts |
| R-10 | C | Given the console policy page, When an admin creates a queue policy, Then the "Explain" panel shows which policy wins for a sample call |
| R-11 | A | Given a handset call to an announce-policy queue, When the owner listens to the recording, Then the announcement is not in it and the conversation is (OWNER STEP) |

### 4.13 Industry parity

| Product | Recording model | Our difference |
|---|---|---|
| 3CX | Per-extension/queue/trunk recording options, optional announcement, recordings to local/cloud storage | Jurisdiction-aware consent with an explanation of which rule won |
| VitalPBX | Recording per extension/queue/trunk with an announcement option | Policy precedence is explicit and testable (pure resolver) |
| FusionPBX | Record per extension/ring group/IVR; files on disk | Encrypted object store, per-file SHA-256, no file paths exposed |
| Amazon Connect | "Set recording and analytics behavior" flow block; recordings in the customer's S3 with KMS | Policy is data, not a flow step, so an admin cannot forget it in one flow |

## 5. RC-07 — PCI pause and resume

### 5.1 Trigger, actors, preconditions

| Item | Value |
|---|---|
| Trigger | Agent presses **Pause recording** (console) or a partner payment integration calls `PauseCallRecording`; **Resume** or the auto-resume timer |
| Actors | Agent on the call (US-07.3, `recording.control` with participant scope); supervisor (tenant scope) |
| Preconditions | A segment in `Recording`; policy `allow_pause = true`; call not terminated. Not met → refusals in §5.5 |

### 5.2 Sequence

| # | Owner | Step |
|---|---|---|
| 1 | telephony | `PauseCallRecording(call_id)` → authorisation (participant check) |
| 2 | telephony | For each live segment on the call: `POST /recordings/live/rec-<id>/mute` (SPIKE R-S2: silence is written, timeline kept) |
| 3 | telephony | For each AI tap snoop on the call's participants (LLD-10 AI-01): `DELETE /channels/{snoopId}` — synchronously, before the RPC returns, so no card audio reaches the AI path |
| 4 | telephony | Record pause start offset; start auto-resume timer (`auto_resume_seconds`); outbox `recording.paused{actor}` |
| 5 | telephony | Return `{paused_at, auto_resume_at}` — the console shows a countdown; a warning event 15 s before auto-resume on the agent's stream |
| 6 | telephony | Resume (RPC or timer): `POST /recordings/live/rec-<id>/unmute`; recreate AI snoops (`POST /channels/{id}/snoop?spy=in&whisper=none…`) and add them to the existing tap bridges; outbox `recording.resumed{actor?, auto}` |
| 7 | telephony | On call end while paused: the interval closes at segment end |

**Why `mute`, not `pause`:** ARI `pause` omits the paused audio so the
file is shorter and later speech is spliced to earlier speech; `mute`
writes silence, so `paused_intervals` offsets map directly onto the file
and a listener hears that something was withheld. AC-07.2 ("a paused
period contains no audio") holds for both.

### 5.3 State

Segment states `Recording ⇄ Paused` (§4.4). Guards: pause while `Paused`
is idempotent (returns the existing `paused_at`); resume while
`Recording` is idempotent; pause in `AwaitingConsent`/`Starting` is
queued and applied at `Recording`; a bridge change while `Paused` starts
the new segment muted.

### 5.4 Data

`recording_paused_intervals` (§4.6) from the `paused_intervals[]` in
`recording.stored`. DTMF: while paused, telephony must not log
`ChannelDtmfReceived` digits (they may be card digits); it logs only
"dtmf received while paused".

### 5.5 API (`CallService`, telephony-core)

| RPC | Fields | Permission | Refusals → console words |
|---|---|---|---|
| `PauseCallRecording` | `call_id` → `paused_at, auto_resume_at` | `recording.control` | `FailedPrecondition` "This call is not being recorded." · `FailedPrecondition` "Pausing is not allowed by your recording policy." · `PermissionDenied` "You can only pause calls you are on." · `NotFound` "This call has ended." |
| `ResumeCallRecording` | `call_id` → `resumed_at` | `recording.control` | `FailedPrecondition` "This call is not being recorded." |

### 5.6 Configuration, failure, invariants, events

| Key | Default | Range | Scope |
|---|---|---|---|
| `auto_resume_seconds` | 180 s | 30–900 s | tenant (`recording_settings`) |
| auto-resume warning | 15 s before | fixed | installation |

| Failure | Behaviour |
|---|---|
| `mute` returns an error | RPC returns `Unavailable` "Could not pause; do not read out card details." and **does not** report paused; alert |
| Snoop delete fails | Retry once; still failing → hang up the tap's externalMedia channel (the tap dies, the call does not); the RPC still succeeds only when no AI audio path remains |
| App restart while paused | Asterisk keeps the recording muted (mute is engine state) and AI taps are gone; the timer is lost — on restart the ACL unmutes any muted live recording older than `auto_resume_seconds` (SPIKE R-S2 confirms a muted state is observable) |
| Agent leaves while paused | Auto-resume still fires; a supervisor may resume |

Invariants: AC-07.2 (no audio in the interval, checked on the file);
INV-04 (the AI tap stops, the call does not); INV-03. Events
`recording.paused`, `recording.resumed`; audit `recording.pause`,
`recording.resume` with actor or `auto`.

### 5.7 Tests and parity

| # | Tier | Given / When / Then |
|---|---|---|
| P-01 | U | Given `Paused`, When pause is requested again, Then the same `paused_at` is returned and no ARI call is made |
| P-02 | I | Given an agent not on the call, When they pause it, Then `PermissionDenied` |
| P-03 | E | Given a recorded rig call, When paused for 10 s while a test tone plays and resumed, Then the WAV's interval is digital silence and the interval offsets match ±100 ms (AC-07.2) |
| P-04 | E | Given an AI tap to a fake provider, When paused, Then the provider receives no frame between pause and resume |
| P-05 | E | Given a pause and no resume, When 180 s pass, Then recording resumes and the interval is marked `auto` |
| P-06 | B | Given the agent console on a call, When Pause is pressed, Then a red "Recording paused — resumes in 3:00" banner shows and Resume is one click |

Parity: Amazon Connect offers `SuspendContactRecording`/`ResumeContactRecording`
(and CCP pause) covering recordings and Contact Lens; 3CX and VitalPBX
offer pause via feature code or client. We add the auto-resume timer, the
AI tap stop in the same operation, and intervals as audited metadata.

## 6. RC-02 — Recording access with reason

### 6.1 Trigger, actors, preconditions

| Item | Value |
|---|---|
| Trigger | A user opens a recording in the console (Reports → call → Recording) or calls the API |
| Actors | Supervisor (`recording.play`), tenant admin (`recording.download`, `recording.export`), auditor (`recording.audit.read`, US-07.5); support engineer (US-07.6) via `recording.play` |
| Preconditions | Recording exists (not deleted); principal has the permission; a reason is supplied (BR-04) |

### 6.2 Sequence

| # | Step |
|---|---|
| 1 | Console shows the call; the player is locked behind a dialog: reason code + reason text (10–500 chars) |
| 2 | `CreateRecordingAccess(recording_id, purpose, reason_code, reason)` → one transaction: INSERT `recording_access`, INSERT `recording_tickets` (token hash), identity audit `recording.play`/`recording.download` |
| 3 | Response: `media_url = /media/recordings/<token>` (same origin, D-69), `expires_at` |
| 4 | Browser `<audio>` (play) or download fetches `GET /media/recordings/<token>`; the handler hashes the token, checks expiry, tenant and purpose, streams from `ObjectStore.Get` with HTTP `Range` support; `Content-Disposition: inline` (play) or `attachment` (download) |
| 5 | Each fetch increments `uses`; > `recording.ticket_max_uses` → 410 |

### 6.3 API (`RecordingService`, reporting)

| RPC | Fields | Permission | Refusals → console words |
|---|---|---|---|
| `ListRecordings` | filters: `from, to, number, extension_id, principal_id, queue_id, has_paused` → metadata (no URL) | `recording.read` | `InvalidArgument` "Choose a range of at most 92 days." |
| `GetRecording` | `recording_id` → metadata, paused intervals, holds, failure if any | `recording.read` | `NotFound` "This recording was deleted on {date} under the retention policy." (from tombstone) |
| `CreateRecordingAccess` | `recording_id, purpose (play\|download), reason_code, reason` → `media_url, expires_at` | `recording.play` / `recording.download` | `InvalidArgument` "Enter a reason (at least 10 characters) for opening this recording." · `PermissionDenied` "You do not have permission to {play\|download} recordings." · `NotFound` as above |
| `ExportRecordings` | `filter, reason_code, reason` (≤ 500 recordings) → `export_id`; async ZIP to the object store; `report.export_ready` | `recording.export` | `ResourceExhausted` "Export at most 500 recordings at a time." |
| `ListRecordingAccess` | `recording_id? | actor_id? | from, to` → access rows | `recording.audit.read` | — |
| Media handler (HTTP, not Connect) | `GET /media/recordings/{token}` | ticket | `410` "This link has expired. Open the recording again." · `404` |

One `recording_access` row per ticket (play or download) and per
recording in an export, so US-07.5's "every occasion" is a query.

### 6.4 Configuration

| Key | Default | Range | Scope |
|---|---|---|---|
| `recording.ticket_ttl` | 300 s | 60–900 s | installation |
| `recording.ticket_max_uses` | 50 (range requests) | 1–500 | installation |
| `recording.export_max` | 500 | fixed | installation |

### 6.5 Failure, invariants, tests, parity

- Object store down → `Unavailable` "Recordings are temporarily
  unavailable." The access row is still written (the attempt is the
  access).
- Ticket leakage risk is bounded by TTL, same-origin path, tenant check
  and purpose; tokens are never logged (path logged as
  `/media/recordings/<redacted>`).
- INV-10: ticket lookup also checks `tenant_id`; AC-07.4: no role
  bypasses — `*` (tenant admin) still passes through
  `CreateRecordingAccess`, the only way to obtain media.

| # | Tier | Given / When / Then |
|---|---|---|
| A-01 | U | Given reason "ok", When validated, Then refused; 10 chars accepted |
| A-02 | I | Given a supervisor, When they create a play ticket, Then one access row with reason and one audit entry exist in the same transaction |
| A-03 | I | Given a tenant-A ticket, When used with a tenant-B session cookie or alone after expiry, Then 404/410 |
| A-04 | I | Given a deleted recording, When `GetRecording` is called, Then `NotFound` with the deletion date |
| A-05 | E | Given a stored recording, When played with seeking (Range), Then bytes match the stored SHA-256 when fully read |
| A-06 | B | Given the call detail page, When Play is pressed, Then the reason dialog appears before any audio loads; paused intervals are greyed on the timeline |
| A-07 | A | Given an auditor, When they open the access log for a recording, Then each play shows person, time and reason (US-07.5) |

Parity: Amazon Connect controls recording access by IAM/security
profile and logs to CloudTrail, without a mandatory reason; 3CX and
FusionPBX gate by role only. A mandatory reason (BR-04) is our deliberate
difference.

## 7. RC-03 — Retention, permanent deletion and legal hold

### 7.1 Trigger, actors, preconditions

| Item | Value |
|---|---|
| Trigger | Sweep timer (`recording.retention_sweep_interval`); `DeleteRecording` (erasure); `PlaceLegalHold` / `ReleaseLegalHold` |
| Actors | System (`atsap_retention` role); tenant admin / compliance officer (`recording.delete`, `recording.legal_hold`) |
| Preconditions | Not under an active hold (else skip / refuse); `retention_until ≤ now` for the sweep |

### 7.2 Sweep sequence

| # | Step |
|---|---|
| 1 | For each tenant: `SET app.tenant_id`; select ≤ 500 `recordings` with `retention_until ≤ now()` and no active hold on `call_id`, oldest first |
| 2 | `ObjectStore.Delete(key)` (not-found = success) |
| 3 | Transaction with `atsapbx.retention_purge = on`: INSERT `recording_deletions` (tombstone), DELETE paused intervals, DELETE `recordings` row, outbox `recording.deleted{cause: retention}` |
| 4 | Crash between 2 and 3 → next sweep repeats 2 (idempotent) then 3 |
| 5 | Metrics: deleted count, oldest overdue age; alert when overdue > 24 h |

Erasure (`DeleteRecording`) runs the same steps synchronously with
`cause = erasure`, the actor and reason, an access row (`action = delete`)
and audit `recording.delete`.

**Backups (AC-07.3):** the stated window is
`retention_until + backup.retention_days` (default 35 days). The object
store bucket must not keep non-current versions beyond 1 day; database
backups contain metadata only (no audio). Crypto-shredding to remove the
backup window is an R2 option.

### 7.3 Legal hold

| From | Trigger | To |
|---|---|---|
| — | `PlaceLegalHold(case_ref, reason, call_ids[] ≤ 10,000)` | `active` |
| `active` | `AddCallsToLegalHold` | `active` (rows appended) |
| `active` | `ReleaseLegalHold(reason)` | `released` (release row) |

A hold covers every recording segment **and** the CDR rows of its calls
(CDR month purge is deferred while a hold covers a call in that month).
A hold placed after a recording's retention date stops deletion only if
the sweep has not yet run; the console warns when a selected call is
already deleted.

### 7.4 API (`RecordingService`)

| RPC | Fields | Permission | Refusals → console words |
|---|---|---|---|
| `DeleteRecording` | `recording_id, reason_code, reason` | `recording.delete` | `FailedPrecondition` "This recording is under legal hold {case_ref} and cannot be deleted." |
| `PlaceLegalHold` | `case_ref, reason, call_ids[]` → `hold_id, covered_recordings, already_deleted[]` | `recording.legal_hold` | `InvalidArgument` "A hold needs a case reference and a reason." · `ResourceExhausted` "At most 10,000 calls per hold." |
| `AddCallsToLegalHold` | `hold_id, call_ids[]` | `recording.legal_hold` | `FailedPrecondition` "This hold has been released." |
| `ReleaseLegalHold` | `hold_id, reason` | `recording.legal_hold` | `FailedPrecondition` "Already released." |
| `ListLegalHolds` | `active_only` | `recording.legal_hold` | — |

### 7.5 Configuration

| Key | Default | Range | Scope |
|---|---|---|---|
| `recording.retention_sweep_interval` | 15 min | 5–60 min | installation |
| `recording.retention_batch` | 500 | 50–5,000 | installation |
| retention days | policy → jurisdiction → tenant default 90 (OWNER DECISION) | 1–3,650 | tenant/object |
| `backup.retention_days` | 35 | 1–365 | installation (stated in the partner agreement) |

Retention is fixed at capture (`retention_until`); a later policy change
applies to new recordings only.

### 7.6 Failure, invariants, events, tests, parity

| Failure | Behaviour |
|---|---|
| Object store down | Sweep skips the batch, alerts after 24 h overdue; never deletes metadata before the object |
| Hold placed during a sweep | The sweep's select and the delete transaction both re-check the hold (`FOR SHARE` on hold rows) |

BR-11 (system-enforced), AC-07.3, INV-08 (tombstone proves deletion
without content). Events `recording.deleted` (public),
`recording.legal_hold_placed/released` (internal); audit
`recording.delete`, `recording.legal_hold`, `recording.legal_hold_release`.

| # | Tier | Given / When / Then |
|---|---|---|
| D-01 | U | Given capture at T with 30-day retention, Then `retention_until = T + 30 d` (UTC, DST-safe) |
| D-02 | I | Given an expired recording, When the sweep runs, Then the object is gone, the row is gone and a tombstone exists |
| D-03 | I | Given an expired recording under an active hold, When the sweep runs, Then nothing is deleted; after release it is |
| D-04 | I | Given the app role, When it deletes a `recordings` row, Then the trigger refuses |
| D-05 | E | Given MinIO and a 1-day retention with a fake clock, When the sweep runs, Then `GetObject` returns not found and no non-current version exists |
| D-06 | C | Given the legal-hold page, When a hold is placed on 3 calls, one already deleted, Then the console lists the deleted one |

Parity: 3CX and FusionPBX delete recordings older than N days by a
maintenance job; Amazon Connect leaves deletion to S3 lifecycle rules and
S3 Object Lock for holds. We add tombstones, holds that also protect the
CDR, and an audit of every deletion.

## 8. RC-06 — Per-participant quality score

### 8.1 Trigger, actors, preconditions

| Item | Value |
|---|---|
| Trigger | RTCP reports for a participant's RTP stream (every `rtcpinterval`, Asterisk default 5 s) and participant end |
| Actors | System; support engineer reads (US-07.6, `report.read`) |
| Preconditions | Participant has an RTP stream (PJSIP/WebRTC); not for Local, snoop, externalMedia or AI channels (they are never participants) |

### 8.2 Source (SPIKE R-S1)

Asterisk 22 does **not** generate RTCP-XR (RFC 3611) VoIP metrics; HLD-07
§1's "RTCP-XR" is corrected to RTCP SR/RR.

| Source | Use | Certainty |
|---|---|---|
| AMI `RTCPReceived` (report blocks from the phone: fraction lost, cumulative lost, interarrival jitter, LSR/DLSR, plus computed RTT) | Quality the **participant hears** (`direction = to_participant`) | SPIKE: confirm emitted for chan_pjsip and WebRTC (rtcp-mux, DTLS-SRTP) on 22.8 and the exact field names |
| AMI `RTCPSent` (our report blocks about what we receive) | Quality **from** the participant (`direction = from_participant`) | SPIKE as above |
| ARI `GET /channels/{id}/variable?variable=CHANNEL(rtpqos,audio,all)` at participant leave, before hangup | Summary fallback for calls shorter than one RTCP interval | SPIKE: readable after `ChannelHangupRequest`? |
| `pjsip show channelstats` (CLI) | Diagnostics only | not used by code |

The AMI connection needs the `reporting` event class; the ACL maps the
AMI `Channel` to a participant through its correlation registry and
discards unknown channels.

### 8.3 Sequence

| # | Owner | Step |
|---|---|---|
| 1 | telephony ACL | AMI RTCP event → participant id (registry) → in-memory window accumulator |
| 2 | telephony | Every `quality.window` (30 s) and at participant leave: outbox internal `participant.rtcp_window{participant_id, call_id, direction, window_start/end, packets, lost, jitter_ms_max, jitter_ms_avg, rtt_ms_avg?, codec, source}` |
| 3 | reporting | Consumer computes `EModel` (pure) → INSERT `cdr_quality_windows`; public `participant.quality` |
| 4 | reporting | On `participant.left` + 60 s grace (late windows): INSERT `cdr_quality_summary` per direction; `status = unknown` with reason when no window arrived |

Volume at 1,000 concurrent calls: 2 participants × 2 directions / 30 s ≈
133 outbox rows/s — within the outbox's budget; `quality.window` is the
lever.

### 8.4 E-model (pure, `reporting/domain`, model `g107-simplified-v1`)

```text
d    = rtt/2 + max(2·jitter, 20 ms) + codec_delay           (one-way, ms)
Id   = 0.024·d + 0.11·(d − 177.3)·H(d − 177.3)
Ie_e = Ie + (95 − Ie) · Ppl / (Ppl + Bpl)                   (random loss, BurstR = 1)
R    = 93.2 − Id − Ie_e
MOS  = 1 if R < 0; 4.5 if R > 100; else 1 + 0.035R + R(R − 60)(100 − R)·7e−6
```

| Codec | Ie | Bpl | codec_delay | Note |
|---|---|---|---|---|
| PCMU/PCMA | 0 | 25.1 | 20 ms | ITU-T G.113 (with PLC) |
| G.722 | 0 | 25.1 | 20 ms | approximation on the narrowband scale |
| Opus | 0 | 20 | 25 ms | approximation; flagged in the model version |
| other | — | — | — | MOS NULL; raw stats kept |

`go func EModel(c Codec, rttMs, jitterMs, lossPct float64) (mos, r float64, ok bool)`
Without RTT (no DLSR yet) `rtt` defaults to 0 and the window is flagged
`rtt_estimated`.

### 8.5 Data — migration `0062_cdr_quality`

```sql
CREATE TABLE cdr_quality_windows (
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  call_id UUID NOT NULL, participant_id UUID NOT NULL,
  direction TEXT NOT NULL CHECK (direction IN ('to_participant','from_participant')),
  window_start TIMESTAMPTZ NOT NULL, window_end TIMESTAMPTZ NOT NULL,
  packets INT, lost INT, loss_pct NUMERIC(5,2), jitter_ms_avg NUMERIC(7,2), jitter_ms_max NUMERIC(7,2),
  rtt_ms NUMERIC(7,2), rtt_estimated BOOLEAN NOT NULL DEFAULT FALSE,
  codec TEXT, mos NUMERIC(3,2), r_factor NUMERIC(5,2),
  model TEXT NOT NULL, source TEXT NOT NULL CHECK (source IN ('ami_rtcp','ari_rtpqos')),
  source_event_id UUID NOT NULL UNIQUE,
  PRIMARY KEY (participant_id, direction, window_start)
);
CREATE TABLE cdr_quality_summary (
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  call_id UUID NOT NULL, participant_id UUID NOT NULL,
  direction TEXT NOT NULL CHECK (direction IN ('to_participant','from_participant')),
  status TEXT NOT NULL CHECK (status IN ('measured','unknown')),
  unknown_reason TEXT CHECK (unknown_reason IN ('no_rtp','no_rtcp','unsupported_codec','collector_down')),
  windows INT NOT NULL, mos_min NUMERIC(3,2), mos_avg NUMERIC(3,2), mos_max NUMERIC(3,2),
  loss_pct NUMERIC(5,2), jitter_ms_max NUMERIC(7,2), rtt_ms_avg NUMERIC(7,2), model TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (participant_id, direction)
);
CREATE INDEX cdr_quality_summary_call ON cdr_quality_summary (tenant_id, call_id);
CREATE INDEX cdr_quality_summary_low ON cdr_quality_summary (tenant_id, mos_min) WHERE status = 'measured';
```

RLS (standard); INSERT-only (revoke + trigger). Retention follows
`cdr.retention_months`.

### 8.6 API, configuration

Quality is read through `GetCallRecord` (summary + windows) and the
`mos_below` filter on `ListCallRecords`; `ExportQuality(from, to)`
(`report.export`) exports summaries. `participant.quality` is on the
public stream (telemetry) and opt-in for webhooks.

| Key | Default | Range | Scope |
|---|---|---|---|
| `quality.window` | 30 s | 10–120 s | installation |
| `quality.summary_grace` | 60 s | 10–300 s | installation |
| Asterisk `rtp.conf rtcpinterval` | 5000 ms | 1000–10000 | engine (projected config) |
| thresholds (HLD-07 §1.2) | Healthy ≥ 4.0; Suspect 3.5–3.9; Degraded < 3.5 | — | installation |

### 8.7 Failure, invariants, events

| Failure | Behaviour |
|---|---|
| AMI disconnected | Windows missing → summary `unknown/collector_down`; never a default score |
| App restart | In-memory accumulators lost for live calls; windows resume on reconnect; affected summaries computed from remaining windows |
| Reporting down | Events queue in JetStream; quality has no call-path effect (INV-06) |

AC-07.7 holds as "every participant has a summary row, measured or
unknown with a reason". Events: `participant.quality` (public telemetry),
`participant.rtcp_window` (internal).

### 8.8 Tests and parity

| # | Tier | Given / When / Then |
|---|---|---|
| Q-01 | U | Given G.711, 0 % loss, 20 ms RTT, 5 ms jitter, When `EModel` runs, Then MOS ∈ [4.35, 4.45]; 5 % loss → MOS < 3.9 (golden table) |
| Q-02 | U | Given an unknown codec, Then MOS is NULL, `ok = false` |
| Q-03 | I | Given a participant with no window, When `participant.left` + grace passes, Then summary `unknown/no_rtcp` |
| Q-04 | E | Given a rig call ≥ 40 s, Then each phone participant has ≥ 1 window per direction from AMI RTCP (SPIKE R-S1 becomes this test) |
| Q-05 | E | Given `tc netem loss 5%` on the rig phone, Then `mos_min` drops below the clean-call value |
| Q-06 | C | Given the call detail page, Then MOS per participant shows with "Unknown (no RTCP)" when unmeasured, never 4.5 |

Parity: 3CX shows call quality reports from RTCP for its own phones;
VitalPBX/FusionPBX rely on CDR fields such as `rtp_audio_in_mos` (FreeSWITCH)
or engine variables; Amazon Connect publishes agent-side network metrics
in Contact Lens. We measure both directions for every participant and
store the model version so scores are reproducible.

### 8.9 Note on `usage_seconds.quality_mos`

Migration 0001 defaults `usage_seconds.quality_mos` to 4.50. That column is
telephony's and is **not** read by the ledger or by any report. The
telephony change that emits `participant.rtcp_window` should drop the
default (LLD-01 follow-up).

## 9. RC-04 — Read and export (built, D-88)

Unchanged behaviour: terminated calls only; agent scope cannot widen;
bounded audited CSV. After step 5 of §3.12 the reads come from the
ledger. `report_exports` (migration `0065_report_exports`) records async
exports:

```sql
CREATE TABLE report_exports (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  actor_principal_id UUID NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('call_records','usage','quality','recordings')),
  filter JSONB NOT NULL, row_count INT, object_key TEXT, sha256 BYTEA,
  status TEXT NOT NULL CHECK (status IN ('running','ready','failed','expired')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(), expires_at TIMESTAMPTZ NOT NULL
);
```

RLS (standard); status is mutable (operational); the audit entry is the
evidence. Files expire after 7 days. CSV cells beginning with `= + - @`
are prefixed with `'` (injection).

## 10. Performance and reliability

- 10 M records searched in < 2 s for a tenant-scoped filter (AC-07.8):
  the `(tenant_id, started_at DESC)` index plus monthly partitions above
  5 M rows; number search uses the digit indexes (prefix/exact; the D-88
  `ILIKE '%x%'` is replaced by exact or prefix match).
- Reporting reads use a read-only pool with a 5 s statement timeout and
  never touch call-path locks. List p95 < 500 ms (AC-05.8).
- Reporting, recording upload and quality are asynchronous: if any is
  down, calls continue (INV-06).
- Data residency: object-store bucket and region are deployment config
  per installation (`tenants.residency_zone` must match; mismatched
  configuration refuses recording at startup).

## 11. Owner decisions and spikes

| # | Item | Recommended default |
|---|---|---|
| 1 | **OWNER DECISION — object store and encryption key ownership** (§4.5) | S3-compatible (MinIO), SSE with a platform-managed per-deployment key; tenant KMS keys R2; approve `minio-go` v7 as a dependency |
| 2 | **OWNER DECISION — default retention per jurisdiction** | Tenant default 90 days; seed `recording_jurisdictions` with consent class only and **no** retention defaults (law varies by sector, not just country); the tenant's compliance officer sets retention |
| 3 | **OWNER DECISION — jurisdiction seed content** | Seed country-level consent classes from a cited public source, label them "not legal advice", treat US and CA as `all_party` (strictest) because state/province is not derivable in R1.0, unknown → `all_party` |
| 4 | **OWNER DECISION — CDR retention** | 24 months, partner exports before purge |
| 5 | **OWNER DECISION — compliance-officer role preset** (§2) | Add it |
| 6 | **OWNER DECISION — AI tap needs recording consent** (`ai_tap_requires_consent`) | Yes by default: AI streaming is processing of the same audio |
| 7 | **OWNER DECISION — external anchor** | Anchors delivered to partners by webhook/stream/export; no third-party timestamp authority in R1.0 |
| 8 | Legal hold in R1.0 | Yes (resolved: specified in §7) |

| Spike | Question | Blocks |
|---|---|---|
| **SPIKE R-S1** | Does Asterisk 22.8 emit AMI `RTCPReceived`/`RTCPSent` for chan_pjsip (UDP and WebRTC rtcp-mux) with report blocks and RTT; is `CHANNEL(rtpqos,audio,all)` readable at hangup via ARI? | RC-06 |
| **SPIKE R-S2** | Does `POST /recordings/live/{name}/mute` write silence frames into a bridge recording (file length unchanged) and is a muted state observable after an app restart? | RC-07 |
| **SPIKE R-S3** | `GET /recordings/stored/{name}/file` streaming for a 4-hour WAV: memory, time, and behaviour while the recording is still being finalised | RC-01 upload |

**References:** [PRD EPIC-07](../PRD.md), [HLD 04 §5](../hld/04-bounded-contexts.md),
[HLD-07](../hld/07-observability.md), [HLD-21 §7](../hld/21-media-and-call-control-flows.md),
[LLD-05](LLD-05-webhook-delivery.md), [LLD-10](LLD-10-ai-pipeline.md),
[Asterisk ARI recordings](https://docs.asterisk.org/Latest_API/API_Documentation/Asterisk_REST_Interface/Recordings_REST_API/),
[Asterisk reporting](https://docs.asterisk.org/Configuration/Reporting/),
[Amazon Connect recording behaviour](https://docs.aws.amazon.com/connect/latest/adminguide/set-recording-behavior.html),
[FusionPBX CDR](https://docs.fusionpbx.com/en/latest/applications/call_detail_record.html),
[3CX](https://www.3cx.com/), [VitalPBX](https://wiki.vitalpbx.com/).
