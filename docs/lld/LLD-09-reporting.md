# LLD-09 — Reporting

| | |
|---|---|
| **Bounded context** | `reporting` (Tier 2, HLD 04 §5) |
| **Status** | **First slice implemented** (`pbx-reporting`, D-88: terminated-call CDRs, participant attribution, agent scope, audited CSV). **Draft** for quality, recording metadata, retention and exports |
| **Traces to** | BRD FBR-R1-10/BR-07; PRD EPIC-05 (US-05.4), EPIC-07 (US-07.1–07.7, AC-07.1–07.8), INV-06/INV-08; HLD 04 §5; LLD-03; DECISIONS D-87, D-88 |
| **Why this LLD** | Partners bill and auditors verify from this data. It must be immutable, attributable and complete, without becoming a call-path dependency |

## 1. Scope and non-goals

**Delivered:** terminated-call read model, participant extension/owner
attribution, agent-scoped reads, bounded CSV export, export audit.

**Remaining:** immutable CDR finalization, per-second usage export, RTCP-XR
quality aggregates, recording metadata/retention/access audit, automatic
retention/deletion, a high-volume query/export path.

**Not in scope:** rating/pricing (partner-owned), a custom report builder
(fixed reports + export in R1), a second call store, or reading Asterisk
CDR/CEL as the source of truth.

**Invariants:** records are append-only (INV-08); delivery/reporting failures
never affect a call (INV-06); no engine endpoint/channel appears in any
record or export; tenant isolation is absolute.

## 2. Prior art (2026-11-04)

| Source | What it establishes | What we adopt |
|---|---|---|
| [Asterisk CDR/CEL](https://docs.asterisk.org/Configuration/Reporting/) | The engine can emit call detail and event logs | Diagnostic input only; the platform's CDR is authoritative and immutable |
| [3CX](https://www.3cx.com/) | Call reporting and recording are expected admin surfaces | Reports and exports are public API, not console-only |
| [FusionPBX](https://docs.fusionpbx.com/en/latest/applications/call_detail_record.html) | CDR, recordings and retention are standard PBX features | Recording metadata links to the call; retention is a policy |
| [VitalPBX](https://wiki.vitalpbx.com/) | Sonata Stats/Recordings separate reporting and media management | Reporting stores metadata; media lives in a storage adapter |
| [Vodia](https://web.vodia.com/features) | Queue/agent statistics, trunk/rate tracking, CDR emails | Per-person/agent and per-queue views are first-class |

The first slice already exceeds the storage model of most PBX UIs by
attributing participants to extensions/owners. The remaining work is
quality, media and retention—not another report table.

## 3. Model and storage

The read model is built from `calls`, `call_participants`, `usage_seconds`,
`recordings`, `quality_samples`; it does not duplicate mutable call state.
`call_records` is a **finalized** view (or materialized row) written once
when a call terminates and never updated.

```sql
call_records (
  tenant_id UUID, call_id UUID, direction, source_digits, dest_digits,
  started_at, answered_at, ended_at, duration_seconds,
  termination_reason, disposition, billable_seconds,
  cost_micros, carrier_id, route_id, ai_stream_seconds,
  finalized_at, PRIMARY KEY (tenant_id, call_id)
);
call_record_participants (
  tenant_id, call_id, participant_id, role, extension_id, principal_id,
  billable_seconds, usage_seconds, quality_mos,
  PRIMARY KEY (tenant_id, participant_id)
);
recordings (
  id UUID, tenant_id, call_id, participant_id NULL,
  object_key TEXT, content_type, size_bytes, sha256,
  started_at, ended_at, retention_until, consent TEXT,
  paused_intervals JSONB, created_at
);
recording_access (
  id, tenant_id, recording_id, actor_id, action, at, source_ip
);
quality_samples (
  tenant_id, participant_id, at, mos, jitter_ms, loss_pct, source
);
report_exports (
  id, tenant_id, actor_id, kind, filter JSONB, row_count, at
);
```

Partition `call_records`/`usage_seconds` monthly by `started_at` (or
`second_ts`) when volume requires it; indexes on
`(tenant_id, started_at DESC)`, `(tenant_id, source_digits)`,
`(tenant_id, dest_digits)`, `(tenant_id, extension_id)`,
`(tenant_id, principal_id)`. RLS on every table.

## 4. Finalization and immutability

On call termination, a finalizer writes `call_records` exactly once
(`ON CONFLICT DO NOTHING`) from the terminated call and its participants,
then links the usage/quality rows. A correction is a separate
`adjustment` row naming actor, reason and amount/seconds—never an update.
The reporting API reads only finalized records; in-flight calls remain on
Call history.

If finalization fails, the event is retried from the outbox; a missing
record is an alert, never a silently absent call. A reconciliation job
compares terminated calls to records and emits an alert/correction.

## 5. Quality and recording

- **Quality (AC-07.7):** every participant gets at least one RTCP-XR sample
  per call; aggregate min/avg/max MOS, jitter and loss per call and per
  participant. A missing sample is reported as `unknown`, never as a
  fabricated 4.5.
- **Recording (AC-07.1–07.4):** recording policy is evaluated before media
  starts; an announcement must finish before capture; pause intervals are
  recorded as metadata. The media adapter writes to an object store (local
  filesystem/S3-compatible per deployment), encrypted at rest, with a
  SHA-256 and immutable object key.
- **Retention:** a job deletes expired objects and metadata; deletion is
  permanent and reported. A legal hold overrides retention and is audited.
- **Access audit:** every play/download/export writes `recording_access`
  naming the actor, action, time and source. No role bypasses it (AC-07.4).
- **Consent:** the policy records whether announcement/consent was required
  and how it was satisfied; an unknown policy is not “consent”.

## 6. API and exports

`ReportingService` already provides `ListCallRecords`/`ExportCallRecords`.
This LLD adds:
- `ListCallRecords` filters: extension, principal, number, direction,
  period, disposition, queue, campaign, has-recording, quality range.
- `ExportUsage` (per-second/aggregated usage, partner billing fields from
  AC-05.4), `ExportCallRecords`, `ExportQuality`.
- `GetRecordingMetadata`, `ListRecordings`, `GetRecordingDownloadURL`
  (short-lived signed URL; access audited), `PauseRecording` (agent action
  via the call session, not a reporting mutation).
- `ListReportExports`/`ReplayExport` (audit and reproducibility).

Exports are server-generated, bounded (10k rows in the current slice;
larger exports become an async job with a signed download URL), audited
once per request, never logged with body content. API responses carry
platform ids and numbers, never `PJSIP/*`, channel or bridge ids.

## 7. Agent and role scope

- Agent: only records where their principal is a participant; cannot widen
  by filter.
- Supervisor: tenant records for their tenant; no cross-tenant.
- Tenant admin: tenant records; recording download requires the recording
  scope and is audited.
- Platform admin: no implicit tenant read; a support operation needs an
  explicit tenant and is audited.
- Recording playback/download is a separate permission from report read.

## 8. Performance and reliability

- Target: 10 million records searched in < 2 s for a tenant-scoped filter
  (AC-07.8), exports streamed, list p95 < 500 ms.
- Reporting reads use a read-only pool/timeout; they never take the call
  path's locks. Finalization is asynchronous from the call termination.
- If reporting is unavailable, calls and recording continue; an alert is
  raised and the outbox retries finalization.
- Data residency: records/recordings stay in the tenant's configured region;
  object-store bucket/region is part of the deployment config.

## 9. Tests and Definition of Done

- Unit: filter/scope, duration/cost arithmetic, quality aggregation, CSV
  quoting/injection, retention date math.
- Integration: exactly one record per terminated call; corrections append;
  RLS; usage/quality/recording joins; retention job deletes media and
  metadata; access audit; export audit.
- Live: a real call produces a record with attributed participants, a
  recording (when policy requires), a quality sample and an audited export;
  an in-flight call never appears.
- DoD for this LLD: AC-07.5–07.8, AC-05.4 and the delivered D-88 slice;
  AC-07.1–07.4 when recording governance lands.

## 10. Open decisions for the owner

1. Object storage backend for recordings (local path vs S3-compatible) and
   encryption key ownership.
2. Retention defaults per jurisdiction and whether legal hold is R1.
3. How long finalized call records are retained (proposed 24 months with
   partner export).
4. Whether quality samples ship from Asterisk RTCP-XR now or after the
   first recording release.

**References:** [PRD EPIC-07](../PRD.md), [HLD 04 §5](../hld/04-bounded-contexts.md),
[Asterisk reporting](https://docs.asterisk.org/Configuration/Reporting/),
[FusionPBX CDR](https://docs.fusionpbx.com/en/latest/applications/call_detail_record.html),
[3CX](https://www.3cx.com/), [VitalPBX](https://wiki.vitalpbx.com/),
[Vodia](https://web.vodia.com/features).
