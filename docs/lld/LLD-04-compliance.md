# LLD-04 — Compliance

| | |
|---|---|
| **Bounded context** | `compliance` (Tier 0, HLD 04 §3) |
| **Status** | **Draft — design only, rewritten to the Flow Definition of Ready (D-98) on 2026-11-07.** No production code yet; the Phase A fraud guard (D-85) is a separate `pbx-core` configuration control |
| **Release** | **R1.0** for DNC, calling hours, opt-out, consent facts **and the abandonment rules for power dialling** (D-45, D-97, FBR-R1-19, AC-15.6). R2 adds only the predictive pacer's use of the same rules (FBR-R2-03) |
| **Traces to** | BRD FBR-R1-19, FBR-R2-03, BR-10; PRD EPIC-15 (US-15.1–15.6, AC-15.1–15.6), INV-01, INV-03, INV-09, INV-10; HLD 04 §3; HLD-20 OB-05, OB-06, OB-07; HLD-21 §4; DECISIONS D-21, D-45, D-56, D-61, D-79, D-85, D-95, D-97, D-98 |
| **Flows owned here** | OB-05 (compliance gate), the policy half of OB-06 (abandonment measure and limit) and OB-07 (message, minimum ring). LLD-06 owns the dialler half of each |
| **Why this LLD** | D-45 makes power dialling R1.0; a dialler without enforced DNC, calling hours, opt-out and abandonment limits places unlawful calls. LLD-04 is a release gate for LLD-06, not an optional dependency |

## 0. Contradictions resolved in this revision

| Was | Now | Authority |
|---|---|---|
| §1/§3/§9 said abandonment limits are R2 (predictive only), `AbandonLimit 0 = disabled until R2` | Abandonment rules are **R1.0 for power dialling**; the US profile ships 3 % / 30 days / 2 s / 15 s | D-97, AC-15.6, FBR-R1-19 |
| `Decide` writes a decision row on every call, but LLD-06's hopper "pre-checks" contacts | Two entry points: **`Precheck`** (non-recording, advisory, never authorises a dial) and **`Decide`** (recording, the only authority) — §5.2 | D-21, AC-15.2 |
| "AMD time counts against the connect limit" with no definition of the clock | The connect clock is defined in §7.4: **from answer** when no AMD runs (R1.0), **from greeting end** with AMD (R2), and AMD that has not decided by the deadline is treated as *human* | HLD-21 §4, 47 CFR 64.1200(a)(7)(i) |
| §9 "emergency always permits" | Emergency **never reaches** compliance: the screening path bypasses it before `Decide`, and the dialler rejects emergency numbers outright (INV-01) | PRD INV-01 |
| `compliance_policies` keyed by tenant (one policy) while campaigns reference a named profile | A tenant has **several named profiles**; a campaign references one by id | US-15.6 |

## 1. Scope and non-goals

**In scope (R1.0):**
- DNC at tenant, campaign and national-registry scope, with import formats and registry freshness;
- calling windows in the contact's local time, with per-region stricter windows and blackout dates;
- opt-out, immediate and permanent, including the automated opt-out inside the abandon message;
- per-contact consent facts (express / express written) and line type, enforced when the profile asks (OWNER DECISION §12.4);
- the **jurisdiction profile** holding every abandonment parameter (connect limit, minimum ring, limit and window, message, no-redial period, AMD permission);
- the abandonment **measure** (counters and ratio) and the verdict that refuses unreserved attempts when the limit is reached;
- a deterministic decision record for every attempt, and the auditor's evidence and export.

**Not in this LLD:** the dialler loop and pacer (LLD-06), the agent model
(LLD-03), rating/spend caps (D-85, LLD-09), caller-ID inventory (LLD-03
§3.12 `tenant_numbers`), and any legal opinion. The product enforces a policy;
the partner supplies the values and their legal basis. Defaults below are
the platform's best reading of US federal rules and are labelled as such.

**Hard invariants:** no role can override a prohibition (INV-09); an
emergency call never reaches compliance (INV-01); an active call is never
dropped by a compliance decision (INV-03); a refusal names a stable
reason, never raw engine or database text; no cross-tenant data, ever
(INV-10).

## 2. Prior art (2026-11-04, extended 2026-11-07)

| Source | What it establishes | What we adopt |
|---|---|---|
| [Asterisk ARI](https://docs.asterisk.org/Configuration/Interfaces/Asterisk-REST-Interface-ARI/) | The application owns call logic | Compliance is a decision service in the call path, not a dialplan expression |
| [Asterisk CDR/CEL](https://docs.asterisk.org/Configuration/Reporting/) | CDR/CEL are engine records | The platform records its own decision and links it to the attempt |
| [VICIdial](https://www.vicidial.org/docs/) | Internal + campaign DNC lists; `call_times` with per-state overrides (`state_call_times`); `drop_rate_group`; safe-harbour message; `drop_call_seconds` | Per-region windows, a campaign drop-rate measure, a configurable message — but enforced in a pure function and evidenced per attempt |
| [Amazon Connect outbound campaigns](https://docs.aws.amazon.com/connect/latest/adminguide/enable-outbound-campaigns.html) | Communication time windows, time zone from postal code then area code, abandonment-rate target for predictive | Zone derivation chain (§7.1); the abandonment target is a profile parameter |
| Five9-class diallers | Max abandon rate per campaign, DNC scrub at load and dial, time zone by area code, consent fields per record | Scrub at import **and** dial; consent as data, not as a free-text note |
| [3CX](https://www.3cx.com/docs/manual/ivr/), [FusionPBX](https://docs.fusionpbx.com/en/latest/), [VitalPBX](https://wiki.vitalpbx.com/) | Time conditions and DNC are advisory configuration | We **enforce** (INV-09); stricter than this prior art |

## 3. Model (pure, D-21)

```go
package domain // internal/compliance/domain — no I/O, no clock, no logger

type ProfileID string

// Policy is one named jurisdiction profile. Every legal number is a field.
type Policy struct {
    TenantID  shared.TenantID
    ID        ProfileID
    Name      string
    Template  Template          // US_FCC | CUSTOM (UK_OFCOM: OWNER DECISION §12.1)
    DefaultTZ string            // IANA; last resort only when Country maps to one zone
    Windows   []CallingWindow   // region "" = all regions
    Blackouts []Blackout        // dates with no calling (region-scoped)

    AbandonLimitPct        float64       // 3.0  (0–100)
    AbandonWindow          time.Duration // 30 days (1–90 days)
    AbandonMarginPct       float64       // 0.5  approaching = limit − margin (0.1–2.0)
    AbandonMinSample       int           // 100 live answers before ratio>1 pacing is allowed (LLD-06 §6.4)
    AbandonConnectLimit    time.Duration // 2 s (0.5–10 s)
    MinRing                time.Duration // 15 s (0–60 s); originate timeout floor
    AbandonMessageRequired bool          // true
    AbandonMessageMedia    string        // tenant media URI; required when the flag is set
    AbandonOptOutDigit     string        // "9"; "" disables the key-press opt-out
    NoRedialAfterAbandon   time.Duration // 0 (US); 0–30 days
    AMDPermitted           bool          // false in R1.0 (no AMD exists); profile switch for R2
    AMDCountsAgainstConnect bool         // true; cannot be false under US_FCC
    ConsentRequired        ConsentRule   // NONE | WIRELESS | ALL  (OWNER DECISION §12.4)
    NationalDNCRequired    bool          // true under US_FCC
    NationalDNCMaxAge      time.Duration // 31 days (16 CFR 310.4(b)(3)(iv))
    RetentionDays          int           // 760 (≥ 730 enforced)
}

type CallingWindow struct {
    Region      string       // "" or ISO 3166-2 subdivision, e.g. "US-FL"
    Weekday     time.Weekday
    StartMinute int          // 0–1439, inclusive
    EndMinute   int          // 1–1440, exclusive
}
type Blackout struct{ Region string; Date civil.Date }

type ContactFacts struct {        // gathered by the orchestration, passed in
    NumberDigits   string         // E.164 without "+"
    Zones          []string       // candidate IANA zones (§7.1); empty = unknown
    Region         string         // ISO 3166-2 when known
    LineType       LineType       // WIRELESS | LANDLINE | VOIP | UNKNOWN
    Consent        ConsentType    // NONE | EXPRESS | EXPRESS_WRITTEN
    LastAbandonAt  *time.Time     // most recent abandon to this number in this tenant
}

type DNCFacts struct {            // hits only; the full list never crosses the port
    Tenant, Campaign, National bool
    OptedOut                   bool
    NationalSyncedAt           *time.Time // nil = never synced
    RuleID                     string     // the matching row, for the audit trail
}

type AbandonStats struct {        // rolling window for the campaign (§7.3)
    LiveAnswered int
    Abandoned    int
}

type Attempt struct {
    TenantID      shared.TenantID
    CampaignID    *shared.CampaignID  // nil for a manual dial outside a campaign
    Kind          AttemptKind         // DIALER | PREVIEW | MANUAL | CALLBACK
    AgentReserved bool                // a specific agent is reserved for this attempt (cannot abandon)
    At            time.Time           // the decision instant, passed in
}

type Verdict struct {
    Permitted bool
    Reason    Reason      // stable enum, §7.6
    RuleID    string      // window/DNC/profile row that decided it
    LocalTime time.Time   // in the zone that decided it (most restrictive)
    Zone      string
}

// Evaluate is THE compliance function. Pure: same inputs, same verdict.
func Evaluate(p Policy, c ContactFacts, d DNCFacts, s AbandonStats, a Attempt) Verdict

// ConnectDeadline and RingTimeout are pure helpers the dialler calls.
func ConnectDeadline(p Policy, answeredAt time.Time, greetingEnd *time.Time, amdRan bool) time.Time
func RingTimeout(p Policy, campaignRing time.Duration) time.Duration // max(campaignRing, p.MinRing)
func AbandonRatio(s AbandonStats) (pct float64, ok bool)            // ok=false when LiveAnswered==0
func Approaching(p Policy, s AbandonStats) bool                     // ratio ≥ limit − margin
func Breached(p Policy, s AbandonStats) bool                        // ratio ≥ limit
```

**Evaluation order** (first refusal wins; the order is part of the
contract and is tested):

| # | Check | Refusal reason |
|---|---|---|
| 1 | Profile present and valid | `profile_missing` |
| 2 | Number is a valid E.164 | `number_invalid` |
| 3 | Opt-out | `opted_out` |
| 4 | Tenant DNC, then campaign DNC | `dnc_tenant`, `dnc_campaign` |
| 5 | National DNC required and list older than `NationalDNCMaxAge` (or never synced) | `dnc_registry_stale` |
| 6 | National DNC hit | `dnc_national` |
| 7 | Consent rule (`WIRELESS`: line type WIRELESS **or UNKNOWN** needs consent ≥ EXPRESS) | `consent_missing` |
| 8 | Zones empty | `timezone_unknown` |
| 9 | Blackout date in any candidate zone | `blackout_date` |
| 10 | Window: permitted only if inside a window **in every candidate zone** (most restrictive); a region-specific window replaces the all-region window for that weekday | `outside_hours` |
| 11 | `NoRedialAfterAbandon` > 0 and `At − LastAbandonAt` < it | `no_redial_after_abandon` |
| 12 | Kind = DIALER, not `AgentReserved`, and `Breached` | `abandon_limit` |
| — | otherwise | `permitted` |

Rule 12 never refuses a reserved-agent attempt, a preview or a manual
dial: those cannot abandon, so they cannot add to the ratio. This is
what lets a breached campaign be resumed in reserved mode (LLD-06 §6.4).

## 4. Data model (migrations 0050–0052; 0053–0059 are LLD-06)

RLS policy on every table below, exactly:

```sql
ALTER TABLE <t> ENABLE ROW LEVEL SECURITY;
ALTER TABLE <t> FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_<t> ON <t>
    USING (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid);
```

### 4.1 `0050_compliance_profiles`

```sql
CREATE TABLE compliance_policies (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name VARCHAR(64) NOT NULL,
  template VARCHAR(16) NOT NULL CHECK (template IN ('US_FCC','CUSTOM')),
  default_timezone VARCHAR(64) NOT NULL DEFAULT '',
  abandon_limit_pct NUMERIC(5,2) NOT NULL DEFAULT 3.00 CHECK (abandon_limit_pct BETWEEN 0 AND 100),
  abandon_window_days SMALLINT NOT NULL DEFAULT 30 CHECK (abandon_window_days BETWEEN 1 AND 90),
  abandon_margin_pct NUMERIC(4,2) NOT NULL DEFAULT 0.50 CHECK (abandon_margin_pct BETWEEN 0.10 AND 2.00),
  abandon_min_sample INT NOT NULL DEFAULT 100 CHECK (abandon_min_sample BETWEEN 0 AND 10000),
  abandon_connect_limit_ms INT NOT NULL DEFAULT 2000 CHECK (abandon_connect_limit_ms BETWEEN 500 AND 10000),
  min_ring_seconds SMALLINT NOT NULL DEFAULT 15 CHECK (min_ring_seconds BETWEEN 0 AND 60),
  abandon_message_required BOOLEAN NOT NULL DEFAULT TRUE,
  abandon_message_media_uri TEXT NOT NULL DEFAULT '',
  abandon_optout_digit VARCHAR(1) NOT NULL DEFAULT '9' CHECK (abandon_optout_digit ~ '^[0-9*#]?$'),
  no_redial_after_abandon_hours SMALLINT NOT NULL DEFAULT 0 CHECK (no_redial_after_abandon_hours BETWEEN 0 AND 720),
  amd_permitted BOOLEAN NOT NULL DEFAULT FALSE,
  amd_counts_against_connect BOOLEAN NOT NULL DEFAULT TRUE,
  consent_required VARCHAR(8) NOT NULL DEFAULT 'NONE' CHECK (consent_required IN ('NONE','WIRELESS','ALL')),
  national_dnc_required BOOLEAN NOT NULL DEFAULT FALSE,
  national_dnc_max_age_days SMALLINT NOT NULL DEFAULT 31 CHECK (national_dnc_max_age_days BETWEEN 1 AND 31),
  retention_days INT NOT NULL DEFAULT 760 CHECK (retention_days >= 730),
  version INT NOT NULL DEFAULT 1,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, name),
  -- the US template cannot be weakened below the federal floor
  CHECK (template <> 'US_FCC' OR (abandon_limit_pct <= 3.00 AND abandon_window_days >= 30
         AND abandon_connect_limit_ms <= 2000 AND min_ring_seconds >= 15
         AND abandon_message_required AND amd_counts_against_connect AND national_dnc_required)),
  CHECK (NOT abandon_message_required OR abandon_message_media_uri <> '')
);
CREATE TABLE compliance_calling_windows (
  tenant_id UUID NOT NULL, policy_id UUID NOT NULL REFERENCES compliance_policies(id) ON DELETE CASCADE,
  region VARCHAR(8) NOT NULL DEFAULT '',          -- '' = all; 'US-FL' = stricter override
  weekday SMALLINT NOT NULL CHECK (weekday BETWEEN 0 AND 6),
  start_minute SMALLINT NOT NULL CHECK (start_minute BETWEEN 0 AND 1439),
  end_minute SMALLINT NOT NULL CHECK (end_minute BETWEEN 1 AND 1440),
  CHECK (end_minute > start_minute),
  PRIMARY KEY (policy_id, region, weekday, start_minute)
);
CREATE TABLE compliance_blackout_dates (
  tenant_id UUID NOT NULL, policy_id UUID NOT NULL REFERENCES compliance_policies(id) ON DELETE CASCADE,
  region VARCHAR(8) NOT NULL DEFAULT '', day DATE NOT NULL, label VARCHAR(64) NOT NULL DEFAULT '',
  PRIMARY KEY (policy_id, region, day)
);
-- Installation reference data (no tenant_id, no RLS, read-only to the app role):
CREATE TABLE compliance_npa_zones (
  country CHAR(2) NOT NULL, prefix VARCHAR(8) NOT NULL,   -- e.g. 'US','1305'
  region VARCHAR(8) NOT NULL, zones TEXT[] NOT NULL,      -- several zones = split NPA
  PRIMARY KEY (country, prefix)
);
GRANT SELECT ON compliance_npa_zones TO atsapbx_app;
```

A new `US_FCC` profile is seeded with Mon–Sun 08:00–21:00 (480–1260) for
region `''` (47 CFR 64.1200(c)(1)). Stricter state rules are entered by
the tenant as region rows (example in the console help: `US-FL` 08:00–20:00;
the product does not ship state law). `compliance_npa_zones` is
loaded by a migration from a versioned CSV in the repo (NANPA NPA list
with zones); updating it is a normal migration.

### 4.2 `0051_compliance_dnc`

```sql
CREATE TABLE dnc_entries (
  tenant_id UUID NOT NULL, number_digits VARCHAR(15) NOT NULL CHECK (number_digits ~ '^[1-9][0-9]{6,14}$'),
  scope VARCHAR(8) NOT NULL CHECK (scope IN ('TENANT','CAMPAIGN','NATIONAL')),
  campaign_id UUID,                                    -- NOT NULL iff scope='CAMPAIGN'
  source_id UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK ((scope = 'CAMPAIGN') = (campaign_id IS NOT NULL))
);
CREATE UNIQUE INDEX dnc_entries_key ON dnc_entries
  (tenant_id, number_digits, scope, COALESCE(campaign_id, '00000000-0000-0000-0000-000000000000'));
CREATE TABLE dnc_sources (               -- one row per upload/registry, for evidence and freshness
  id UUID PRIMARY KEY, tenant_id UUID NOT NULL,
  scope VARCHAR(8) NOT NULL, campaign_id UUID, name VARCHAR(128) NOT NULL,
  format VARCHAR(16) NOT NULL CHECK (format IN ('LINES','CSV','FTC_10DIGIT','API')),
  mode VARCHAR(8) NOT NULL CHECK (mode IN ('ADD','REMOVE','REPLACE')),
  accepted INT NOT NULL DEFAULT 0, rejected INT NOT NULL DEFAULT 0,
  synced_at TIMESTAMPTZ NOT NULL DEFAULT now(), created_by UUID NOT NULL
);
CREATE TABLE opt_outs (
  tenant_id UUID NOT NULL, number_digits VARCHAR(15) NOT NULL,
  source VARCHAR(16) NOT NULL CHECK (source IN ('AGENT','ABANDON_IVR','API','CONSOLE','IMPORT')),
  campaign_id UUID, call_id UUID, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), created_by UUID,
  PRIMARY KEY (tenant_id, number_digits)
);
CREATE TABLE contact_consents (          -- consent facts by number, written by import/API (§7.5)
  tenant_id UUID NOT NULL, number_digits VARCHAR(15) NOT NULL,
  consent_type VARCHAR(16) NOT NULL CHECK (consent_type IN ('NONE','EXPRESS','EXPRESS_WRITTEN')),
  line_type VARCHAR(8) NOT NULL DEFAULT 'UNKNOWN' CHECK (line_type IN ('WIRELESS','LANDLINE','VOIP','UNKNOWN')),
  source VARCHAR(256) NOT NULL DEFAULT '', consented_at TIMESTAMPTZ, revoked_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, number_digits)
);
```

Grants: `SELECT, INSERT, DELETE` on `dnc_entries`; `SELECT, INSERT` on
`dnc_sources`, `opt_outs` (no `DELETE` — an opt-out is permanent; a
correction is an audited `RemoveOptOut` that writes an audit row and is
restricted to `compliance.optout.remove`, OWNER DECISION §12.5);
`SELECT, INSERT, UPDATE` on `contact_consents`.

### 4.3 `0052_compliance_decisions`

```sql
CREATE TABLE compliance_decisions (               -- append-only (INV-08)
  id UUID PRIMARY KEY, tenant_id UUID NOT NULL,
  policy_id UUID, policy_version INT, campaign_id UUID, attempt_id UUID, call_id UUID,
  kind VARCHAR(8) NOT NULL, number_hash BYTEA NOT NULL, number_last4 CHAR(4) NOT NULL,
  permitted BOOLEAN NOT NULL, reason VARCHAR(32) NOT NULL, rule_id VARCHAR(128) NOT NULL DEFAULT '',
  zone VARCHAR(64) NOT NULL DEFAULT '', local_time TIMESTAMP, decided_at TIMESTAMPTZ NOT NULL,
  abandon_live INT, abandon_count INT        -- the stats the verdict saw, for the auditor
);
CREATE INDEX compliance_decisions_period ON compliance_decisions (tenant_id, decided_at DESC);
CREATE INDEX compliance_decisions_campaign ON compliance_decisions (tenant_id, campaign_id, decided_at DESC);
CREATE TABLE compliance_abandon_buckets (         -- the rolling-ratio counters (§7.3)
  tenant_id UUID NOT NULL, campaign_id UUID NOT NULL, day DATE NOT NULL,   -- UTC day
  live_answered INT NOT NULL DEFAULT 0 CHECK (live_answered >= 0),
  abandoned INT NOT NULL DEFAULT 0 CHECK (abandoned >= 0 AND abandoned <= live_answered),
  PRIMARY KEY (tenant_id, campaign_id, day)
);
CREATE TABLE compliance_abandon_events (          -- the abandonment record kept ≥ 2 years
  id UUID PRIMARY KEY, tenant_id UUID NOT NULL, campaign_id UUID NOT NULL, attempt_id UUID NOT NULL UNIQUE,
  number_digits VARCHAR(15) NOT NULL, answered_at TIMESTAMPTZ NOT NULL, abandoned_at TIMESTAMPTZ NOT NULL,
  message_played BOOLEAN NOT NULL, message_started_ms INT,   -- ms after the connect clock started
  opted_out BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX compliance_abandon_events_number ON compliance_abandon_events (tenant_id, number_digits, abandoned_at DESC);
```

Grants: `SELECT, INSERT` on `compliance_decisions` and
`compliance_abandon_events` (no `UPDATE`/`DELETE` for the app role; a
retention job running as the migration owner deletes rows older than the
policy's `retention_days`); `SELECT, INSERT, UPDATE` on buckets. The
number is stored in decisions as an HMAC (tenant-keyed) plus last four
digits, so an auditor can match a complaint's number without the log
holding the full number; `compliance_abandon_events` keeps the full
number because the no-redial rule and the regulator's record need it.

**Retention (decided):** decisions, abandon events and buckets are kept
**760 days (25 months) by default, never less than 730**. FCC
64.1200(a)(7)(i)(D) and the TSR safe harbour (16 CFR 310.4(b)(4)) both
expect records sufficient to show compliance for two years; the extra
month covers a 30-day window that straddles the boundary. This closes
the former open decision §10.3.

## 5. Ports

### 5.1 Inbound ports (called by `dialer` and by `telephony-core` Screening)

```go
package ports // internal/compliance/ports

// Gate is the only path to permission. Not pure: it reads, evaluates, records.
type Gate interface {
    // Decide loads facts in tx, runs domain.Evaluate, inserts the
    // compliance_decisions row IN tx, and returns it. The caller's
    // attempt row and this decision commit or roll back together.
    Decide(ctx context.Context, tx pgx.Tx, req DecideRequest) (Decision, error)

    // Precheck runs the same Evaluate on the same facts but writes
    // nothing and returns no decision id. Advisory only: used to keep
    // hopeless contacts out of the hopper and to warn at scheduling.
    // A Precheck verdict can never be stored on an attempt.
    Precheck(ctx context.Context, tx pgx.Tx, req DecideRequest) (domain.Verdict, error)

    // Bulk pre-check for hopper refill: one round trip for n numbers.
    PrecheckMany(ctx context.Context, tx pgx.Tx, base DecideRequest, numbers []string) ([]domain.Verdict, error)
}

type DecideRequest struct {
    TenantID      shared.TenantID
    PolicyID      domain.ProfileID       // the campaign's profile; MANUAL outside a campaign uses the tenant default
    CampaignID    *shared.CampaignID
    AttemptID     *shared.UUID
    Kind          domain.AttemptKind
    AgentReserved bool
    NumberDigits  string
    ContactZone   string                 // contact's own IANA zone, "" if none
    ContactRegion string
    ContactCountry string                // ISO 3166-1 alpha-2, from list default
    At            time.Time              // from the caller's clock port, never time.Now inside
}

type Decision struct { ID shared.UUID; Verdict domain.Verdict }

// AbandonLedger is written by the dialler in the same tx as the attempt outcome.
type AbandonLedger interface {
    RecordLiveAnswer(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, campaign shared.CampaignID, at time.Time) error
    RecordAbandon(ctx context.Context, tx pgx.Tx, ev AbandonEvent) error  // also counts as live answer already recorded
    Stats(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, campaign shared.CampaignID, policy domain.ProfileID, at time.Time) (domain.AbandonStats, error)
}

// OptOuts is written by the agent console, the abandon IVR and the API.
type OptOuts interface {
    RecordOptOut(ctx context.Context, tx pgx.Tx, o OptOut) error   // idempotent per (tenant, number)
}

// Policies is read by the dialler to configure ring timeout, connect clock, message.
type Policies interface {
    Get(ctx context.Context, tx pgx.Tx, tenant shared.TenantID, id domain.ProfileID) (domain.Policy, error)
}
```

`telephony-core`'s existing `ports.ComplianceEngine.Evaluate(tenant,
destNumber)` (Screening) is satisfied by an adapter that calls `Decide`
with `Kind=MANUAL` in its own transaction; the adapter, not
`telephony-core`, knows about the profile (LLD-01 §5 seam unchanged).

### 5.2 Why two entry points (pre-check vs decide)

| | `Precheck` | `Decide` |
|---|---|---|
| Writes a row | Never | Exactly one, in the caller's tx |
| Authorises a dial | Never | Yes — the attempt stores `Decision.ID` |
| Used by | hopper refill, `ScheduleCallback` validation, import scrub, console "would this be allowed?" | dialler at dial instant, preview Dial click, manual dial, Screening |
| Same pure function | yes | yes |

A contact that passes `Precheck` at 08:59 and is dialled at 21:00:30 is
refused by `Decide` (AC-15.2). An opt-out written at 10:00:00 is seen by
every `Decide` after that commit, because `Decide` reads `opt_outs` in
the dial transaction with no cache (D-56): AC-15.4 holds with margin.

## 6. API contract (`compliance.v1.ComplianceService`)

Permissions (new action strings; role grants are an LLD-02 change, §12.6):

| Permission | Default grant |
|---|---|
| `compliance.policy.read` | TENANT_ADMIN, SUPERVISOR, proposed COMPLIANCE_OFFICER |
| `compliance.policy.manage` | TENANT_ADMIN, COMPLIANCE_OFFICER |
| `compliance.dnc.read` / `compliance.dnc.manage` | TENANT_ADMIN, COMPLIANCE_OFFICER |
| `compliance.optout.record` | TENANT_ADMIN, SUPERVISOR, AGENT, COMPLIANCE_OFFICER |
| `compliance.optout.remove` | COMPLIANCE_OFFICER only (OWNER DECISION §12.5) |
| `compliance.decision.read` / `compliance.decision.export` | TENANT_ADMIN, COMPLIANCE_OFFICER, proposed AUDITOR |

| RPC | Request → Response | Permission | Refusals (Connect code — console words) |
|---|---|---|---|
| `CreateCompliancePolicy` | `name, template, fields…` → `Policy` | policy.manage | `INVALID_ARGUMENT` "Abandon limit must be between 0 and 100 %"; `FAILED_PRECONDITION` "The US template cannot be set below the federal minimum: {field}"; `ALREADY_EXISTS` "A profile with this name already exists" |
| `GetCompliancePolicy` / `ListCompliancePolicies` | `id` / paging → `Policy` | policy.read | `NOT_FOUND` "Profile not found" |
| `UpdateCompliancePolicy` | `id, version, fields, update_mask` → `Policy` | policy.manage | as Create; `ABORTED` "Someone else changed this profile; reload" (version mismatch) |
| `ReplaceCallingWindows` | `policy_id, windows[]` → windows | policy.manage | `INVALID_ARGUMENT` "Window {n}: end must be after start" |
| `ReplaceBlackoutDates` | `policy_id, dates[]` → dates | policy.manage | `INVALID_ARGUMENT` |
| `ImportDNC` | client stream: `header{scope, campaign_id?, name, format, mode}` then `chunk bytes` → `{source_id, accepted, rejected, errors[≤100]{line, reason}}` | dnc.manage | `INVALID_ARGUMENT` "Line {n}: not a phone number"; `RESOURCE_EXHAUSTED` "File exceeds 50 MB / 5 million numbers" |
| `AddDNCNumbers` / `DeleteDNCNumber` | `scope, campaign_id?, numbers[≤1000]` | dnc.manage | `INVALID_ARGUMENT` |
| `ListDNC` | filter, paging → masked numbers (`+1•••••1234`), source | dnc.read | — |
| `CheckNumber` | `number, policy_id, campaign_id?` → `Verdict` (Precheck) | dnc.read | — (advisory; console labels it "Would be allowed now") |
| `RecordOptOut` | `number, source, campaign_id?, call_id?` → `{}` | optout.record | `INVALID_ARGUMENT` "Not a phone number" |
| `RemoveOptOut` | `number, reason (≥ 10 chars)` → `{}` | optout.remove | `FAILED_PRECONDITION` "Opt-outs recorded by the contact cannot be removed" when source ∈ {ABANDON_IVR, AGENT} |
| `GetAbandonmentStatus` | `campaign_id` → `{live_answered, abandoned, ratio_pct, limit_pct, state: OK/APPROACHING/BREACHED, window_start}` | policy.read | `NOT_FOUND` |
| `ListComplianceDecisions` | `period, campaign_id?, permitted?, reason?, paging` | decision.read | `INVALID_ARGUMENT` "Period may not exceed 92 days" |
| `ExportComplianceDecisions` | same filter → server-streamed CSV | decision.export | audited; CSV cells formula-escaped |

No RPC overrides a verdict; no request field sets `Permitted`;
`RemoveOptOut` cannot remove an opt-out the contact made. A compliance
store failure returns `FAILED_PRECONDITION` with detail
`compliance_unavailable` and console words "Calling is paused: the
compliance check is unavailable" — the attempt is not placed.

## 7. Rules in detail

### 7.1 Time zone derivation (contact-local time, AC-15.2)

| Step | Source | Result |
|---|---|---|
| 1 | Contact `timezone` column (valid IANA) | that zone only |
| 2 | Country +1 (NANP): NPA (or NPA-NXX where split) via `compliance_npa_zones` | the listed zones; **several = evaluate all, permit only if inside every one** |
| 3 | Other country with exactly one zone (from Go's tz data, `country → zones`) | that zone |
| 4 | Profile `default_timezone` **only when the country has one zone** | that zone |
| — | none | `timezone_unknown` (refused, never server-local time) |

Region (for stricter state windows) comes from the contact's `region`
column, then the NPA row. A ported mobile number may live far from its
area code; that is why step 1 wins and why the multi-zone rule is
conservative. Postal-code lookup (Amazon Connect's first step) is R2.

### 7.2 DNC

- **Scopes:** tenant (internal list), campaign, national registry. All
  are checked; any hit refuses. Opt-out is separate and wider (all
  campaigns, permanent).
- **Import formats:** `LINES` (one number per line, E.164 or national with
  the import's default country); `CSV` with a `number` (or mapped) column;
  `FTC_10DIGIT` (the US registry download: 10 digits per line, or
  `area_code,number` pairs, interpreted as +1); `API` batches of ≤ 1000
  via `AddDNCNumbers`. UTF-8, ≤ 50 MB, ≤ 5 million rows per file.
- **Modes:** `ADD`; `REMOVE` (the registry's deletion file);
  `REPLACE` (staging table, then delete-and-insert for that `source`
  inside one transaction per tenant, so a reader sees the old or new set,
  never neither).
- **Freshness:** `NATIONAL` sources record `synced_at`. A profile with
  `national_dnc_required` refuses every dial with `dnc_registry_stale`
  once the newest national source is older than 31 days (16 CFR
  310.4(b)(3)(iv)); the console warns from day 25. Fetching the registry
  itself is the tenant's (SAN-holder's) job; the platform imports it.
- **Scrub at import and at dial:** LLD-06 runs `PrecheckMany` at list
  import (rows flagged, not deleted) and `Decide` at every dial.

### 7.3 Abandonment measure (OB-06)

| Term | Definition (US_FCC defaults) |
|---|---|
| **Live answer** | A dialler attempt answered by a person: every BRIDGED attempt plus every ABANDONED attempt. Machine answers (agent-marked or AMD) and preview attempts are **not** counted |
| **Abandoned** | A live answer not connected to an agent before `ConnectDeadline` (§7.4), including one where the customer hangs up while waiting |
| **Ratio** | `abandoned ÷ live_answered × 100`, per campaign |
| **Window** | **Rolling**: the current UTC day plus the previous `window_days − 1` days (decided: rolling, not calendar, because 64.1200(a)(7)(i) says "a 30-day period" and a rolling window is never looser than any calendar alignment). A campaign younger than the window is measured over its life |
| **Approaching** | ratio ≥ limit − margin (2.5 %) |
| **Breached** | ratio ≥ limit (3.0 %) |

Counters: `RecordLiveAnswer` increments `live_answered` for today;
`RecordAbandon` increments `abandoned` and inserts the event. Both run
in the dialler's outcome transaction, so the ratio can never disagree with
the attempts. `Stats` is one `SUM` over at most 90 rows. The pacing
reaction (slow-down step, stop, resume) is the dialler's (LLD-06 §6.4);
compliance only says OK / APPROACHING / BREACHED and refuses unreserved
attempts once breached (rule 12).

### 7.4 Connect clock, minimum ring and message (OB-07)

| Parameter | Rule |
|---|---|
| **Clock start without AMD (R1.0)** | the customer channel's answer (`Dial` event `dialstatus=ANSWER`) — stricter than "greeting end", because the greeting cannot have ended before answer |
| **Clock start with AMD (R2)** | greeting end = the first `ChannelTalkingFinished` after answer, minus the TALK_DETECT silence threshold (500 ms); no speech within `initial_silence` → clock starts at answer |
| **Deadline** | `ConnectDeadline = start + abandon_connect_limit` (2 s) |
| **AMD and the deadline** | under `amd_counts_against_connect` the AMD decision must be made by `deadline − 300 ms`; an undecided AMD is treated as **human**. AMD may not run at all where `amd_permitted=false` (all profiles in R1.0) |
| **Minimum ring** | `RingTimeout = max(campaign ring_timeout, min_ring)`; becomes the ARI dial `timeout` (seconds). Measured from dial start, which is never shorter than from first ringing. The dialler cannot cancel an unanswered attempt earlier, including on pause (INV-03 extended to ringing attempts) |
| **Message** | played to the customer starting before the deadline: seller identity, a telephone number that accepts DNC requests during business hours, and (prerecorded telemarketing) a key-press opt-out — digit `abandon_optout_digit` writes `opt_outs` source `ABANDON_IVR` and plays a confirmation. Content is the tenant's recording; the console checklist names the three elements, but the platform cannot verify speech |
| **Evidence** | `compliance_abandon_events.message_started_ms` records how long after the clock start playback began |

### 7.5 Consent and line type (OWNER DECISION §12.4)

Consent is a fact per number in `contact_consents`, written by list
import (columns `consent_type`, `consent_source`, `consent_at`,
`line_type`) or the API. The profile's `consent_required` decides
enforcement (rule 7). Line type comes only from the list or the API; the
platform does no carrier lookup in R1.0 (a lookup is a paid dependency).
`UNKNOWN` is treated as `WIRELESS` when the rule is `WIRELESS`, so an
unknown never permits more than a known.

### 7.6 Reasons (stable wire values)

`permitted`, `profile_missing`, `number_invalid`, `opted_out`,
`dnc_tenant`, `dnc_campaign`, `dnc_national`, `dnc_registry_stale`,
`consent_missing`, `timezone_unknown`, `blackout_date`, `outside_hours`,
`no_redial_after_abandon`, `abandon_limit`, `compliance_unavailable`.
Adding one is additive; renaming one is a breaking API change.

## 8. Enforcement points

| Path | Entry | Kind | Notes |
|---|---|---|---|
| Power dialler | `Decide` in the attempt tx, at the dial instant | DIALER | Hopper used `Precheck` earlier; irrelevant to the verdict |
| Preview | `Decide` when the agent clicks Dial | PREVIEW | `NextContact` used `Precheck` |
| Callback | `Decide` at dial; `Precheck` when scheduled | CALLBACK | A callback time outside the window is refused at scheduling |
| Manual dial from the agent console | `Decide` before origination | MANUAL | AC-15.1 |
| Phone/API trunk call by a principal in an active dialler session | Screening adapter → `Decide` | MANUAL | OWNER DECISION §12.3 for other PBX calls |
| Emergency | **never** — matched before Screening (INV-01); the dialler rejects emergency numbers at import and dial | — | |
| Inbound | none | — | the caller chose to call |

## 9. Failure and recovery

| Failure | Behaviour |
|---|---|
| DB error inside `Decide` | returns `ErrComplianceUnavailable`; caller rolls back; no attempt row, no call; pacer backs off 5 s; alert after 3 consecutive |
| Policy edited while a campaign runs | next `Decide` reads the new `version`; decisions record `policy_version`; calls in progress unaffected (INV-12) |
| Policy deleted while referenced | refused: `FAILED_PRECONDITION` "Profile is used by {n} campaigns" |
| DNC `REPLACE` import fails mid-way | staging rolled back; old set remains; `dnc_sources` row not written |
| National list goes stale | every Decide refuses `dnc_registry_stale`; LLD-06 auto-pauses campaigns using that profile with reason `COMPLIANCE_BLOCKING` after 20 consecutive refusals |
| Concurrent opt-out and dial | both in separate tx; the dial either commits before the opt-out (call placed, lawful at that instant) or after (refused). Never both after |
| Clock skew between app instances | `At` comes from one clock port per instance; windows are minute-granular; a 1 s skew is accepted and documented |

## 10. Invariants, events and audit

| Rule | How it holds |
|---|---|
| INV-09 / BR-10 | `Evaluate` is the only producer of `Permitted`; the dialler cannot insert an attempt in state DIALING without a non-null `compliance_decision_id` whose verdict permitted (DB `CHECK` in LLD-06 §4) |
| INV-01 | No emergency number reaches `Decide`; property test asserts the dialler rejects every number in the tenant's emergency catalogue |
| INV-03 | Compliance never hangs up a call; breach only stops new attempts |
| INV-08 | Decisions and abandon events are insert-only for the app role |
| INV-10 | RLS on every tenant table; `compliance_npa_zones` holds no tenant data |

Events (outbox → NATS, subject `tenant.<id>.event.compliance.<action>`):

| Action | Payload |
|---|---|
| `optout.recorded` | `number_hash, source, campaign_id?, at` |
| `dnc.imported` | `source_id, scope, accepted, rejected` |
| `policy.updated` | `policy_id, version, changed_fields[]` |
| `abandon.approaching` / `abandon.breached` / `abandon.recovered` | `campaign_id, ratio_pct, limit_pct, live_answered, abandoned` (emitted on state change only) |
| `registry.stale` | `policy_id, synced_at` |

Decisions are **not** events (volume); they are queried. Audit (via
`AppendAuditTx`, before/after): policy create/update/delete, windows,
blackouts, DNC import/add/delete, opt-out record/remove, decision export.

## 11. Tests (Given/When/Then)

| # | Tier | Given | When | Then |
|---|---|---|---|---|
| C1 | U | a tenant-DNC number and every `Kind` and role | `Evaluate` | never permitted (property, 10 000 cases) |
| C2 | U | US_FCC, contact zone America/New_York | at 07:59:59 / 08:00:00 / 20:59:59 / 21:00:00 local | refused / permitted / permitted / refused |
| C3 | U | NPA mapping to Central and Mountain | 20:30 Central (19:30 Mountain) | permitted; at 21:05 Central refused (most restrictive) |
| C4 | U | contact zone empty, country with several zones, profile default set | Evaluate | `timezone_unknown` |
| C5 | U | region `US-FL` window 08:00–20:00 and all-region 08:00–21:00 | 20:30 local in FL | `outside_hours`, RuleID names the FL row |
| C6 | U | stats 97/3 (3.09 %), Kind DIALER, not reserved | Evaluate | `abandon_limit`; same with `AgentReserved` → permitted; PREVIEW → permitted |
| C7 | U | stats 1000/25 (2.5 %), margin 0.5 | `Approaching` / `Breached` | true / false |
| C8 | U | `no_redial_after_abandon` 72 h, last abandon 71 h ago | Evaluate | `no_redial_after_abandon` |
| C9 | U | national required, last sync 32 days ago | Evaluate | `dnc_registry_stale`, even for a number not on any list |
| C10 | U | consent WIRELESS, line UNKNOWN, consent NONE | Evaluate | `consent_missing`; LANDLINE → permitted |
| C11 | U | US_FCC, campaign ring 10 s | `RingTimeout` | 15 s |
| C12 | U | no AMD, answered at T | `ConnectDeadline` | T+2 s; with AMD and greeting-end event at T+3.1 s → T+4.6 s |
| C13 | U | evaluation-order table | each pair of simultaneous refusals | the earlier rule's reason wins |
| C14 | I | `Decide` inside a tx that then rolls back | — | no decision row; no attempt row (atomicity) |
| C15 | I | opt-out committed | the next `Decide` for that number in any campaign | `opted_out` within the same second (AC-15.4) |
| C16 | I | tenants A and B | A lists B's decisions/DNC | zero rows (RLS, INV-10) |
| C17 | I | DNC REPLACE of 1 M rows, injected failure at row 900 000 | import | old set intact; no source row |
| C18 | I | 31 daily buckets | `Stats` on day 31 | sums only the last 30 |
| C19 | I | US_FCC profile | update `abandon_limit_pct` to 5 | `FAILED_PRECONDITION` (DB CHECK and service) |
| C20 | I | app role | `UPDATE compliance_decisions` | permission denied |
| C21 | W | every refusal path of every RPC | call | fixed Connect code and detail; no SQL or legal text; no field named `permitted` in any request message |
| C22 | B | compliance officer | creates a US profile, adds an FL window, uploads an FTC file | sees counts and masked numbers; console shows the 31-day freshness date |
| C23 | E | listed number in a running power campaign and via the agent's manual dial | dial | never reaches the trunk (rig SIP trace) (AC-15.1) |
| C24 | E | contact at local 21:00:30 that passed Precheck at 20:59 | its hopper turn | refused at dial (AC-15.2) |
| C25 | A | auditor, a test period | exports decisions | every attempt in the period has one decision row with rule and reason (AC-15.5) |

## 12. OWNER DECISIONS (with recommended defaults)

1. **Profiles shipped in R1.0.** *Recommend:* `US_FCC` template plus
   `CUSTOM`. A UK/Ofcom template is offered only after its values (3 %
   per 24 h, 72 h no-redial after an AMD false positive, 15 s ring, 2 s
   message) are confirmed by the owner's counsel; until then a UK tenant
   enters them in `CUSTOM`.
2. **DNC import channels.** *Recommend:* CSV/lines/FTC files **and** API
   batches (both in §6).
3. **Manual-dial enforcement scope** (AC-15.1 vs ordinary PBX calls).
   *Recommend:* full `Decide` for every dial from the agent console or by
   a principal with an active dialler session; ordinary extension calls
   check only tenant-scope DNC and opt-out (no hours), because DNC law
   targets telemarketing, not an employee returning a client's call.
4. **Consent enforcement.** *Recommend:* store consent and line type in
   R1.0; default `consent_required=NONE` for `CUSTOM` and `WIRELESS` for
   `US_FCC` (conservative reading after *Facebook v. Duguid*; counsel to
   confirm). Prerecorded telemarketing (voicemail drop) requires
   `EXPRESS_WRITTEN` regardless (LLD-06 §9.3).
5. **Opt-out removal and public opt-out endpoint.** *Recommend:* only
   `COMPLIANCE_OFFICER` may remove a non-contact opt-out, with reason;
   no unauthenticated public endpoint in R1.0 (the key-press opt-out
   covers the contact).
6. **New roles** `COMPLIANCE_OFFICER` and `AUDITOR` (PRD personas) need an
   LLD-02 change to `DefaultPermissions`. *Recommend:* add both with the
   grants in §6.

## 13. Industry parity

| Capability | VICIdial | Amazon Connect | Five9-class | AtsaPBX |
|---|---|---|---|---|
| Internal/campaign DNC | yes | via Customer Profiles / lists | yes | yes, plus national with freshness gate |
| Calling hours by state | `state_call_times` | communication time windows | yes | region windows, most-restrictive multi-zone |
| Zone derivation | phone code + postal | postal, then area code | area code | contact zone, then NPA, then single-zone country |
| Abandon limit | `drop_rate_group`, adaptive cap | predictive target | per campaign | per campaign rolling 30 days, enforced in pure `Evaluate` |
| Override | admin can disable | n/a | admin can disable | **none** (INV-09) — deliberate difference |
| Evidence per attempt | logs | CTRs | reports | one immutable decision row per attempt |

## 14. Handoff

OpenSpec changes, in order (each one change, one commit per task):
`compliance-profiles-and-gate` (0050–0051, §3, §5, §6 policy/DNC/opt-out),
`compliance-evidence` (0052 decisions, export),
`compliance-abandonment` (buckets, events, rule 12, status RPC) — the
last must merge before LLD-06's power ratio > 1.0 is enabled.

**References:** [Asterisk ARI](https://docs.asterisk.org/Configuration/Interfaces/Asterisk-REST-Interface-ARI/),
[47 CFR 64.1200](https://www.ecfr.gov/current/title-47/chapter-I/subchapter-B/part-64/subpart-L/section-64.1200),
[16 CFR 310.4](https://www.ecfr.gov/current/title-16/chapter-I/subchapter-C/part-310/section-310.4),
[TRD](../TRD.md), [PRD EPIC-15](../PRD.md), [LLD-06](LLD-06-dialer.md),
[HLD-21](../hld/21-media-and-call-control-flows.md).
