# LLD-04 — Compliance

| | |
|---|---|
| **Bounded context** | `compliance` (Tier 0, HLD 04 §3) |
| **Status** | **Draft — design only.** No production code yet; the Phase A fraud guard (D-85) is a separate `pbx-core` configuration control |
| **Traces to** | BRD FBR-R2-03/BR-10; PRD EPIC-15 (US-15.1–15.5, AC-15.1–15.5), INV-09; HLD 04 §3; DECISIONS D-21, D-45, D-61, D-79, D-85 |
| **Why this LLD** | D-45 makes power dialling R1.0; a dialler without enforced DNC, calling hours and opt-out places unlawful calls. LLD-04 is a release gate for LLD-06, not an optional dependency |

## 1. Scope and non-goals

**In scope:** enforceable do-not-call (DNC) at tenant and campaign level,
calling windows in the contact's timezone, opt-out handling, abandonment
limits (R2 predictive), a deterministic decision record for every attempt,
and evidence/reporting for auditors.

**Not in this LLD:** rating/spend caps (D-85 is `pbx-core`'s configuration
guard; rating is LLD-09), the dialer itself (LLD-06), the queue/agent
model (LLD-03), or a legal opinion about any jurisdiction. The product
enforces a policy; the partner supplies the policy and its legal basis.

**Hard invariants:** no role can override a prohibition (INV-09); an
emergency call bypasses every compliance rule (INV-01); an active call is
never dropped by a compliance decision (INV-03); a refusal names a stable
reason, never raw engine text.

## 2. Prior art (2026-11-04)

| Source | What it establishes | What we adopt |
|---|---|---|
| [Asterisk ARI](https://docs.asterisk.org/Configuration/Interfaces/Asterisk-REST-Interface-ARI/) | ARI exposes channels/bridges/media and asynchronous events; the application owns call logic | Compliance is a decision service in the call path, not a dialplan expression |
| [Asterisk CDR/CEL reporting](https://docs.asterisk.org/Configuration/Reporting/) | CDR/CEL are the engine's records, not the platform's authority | The platform records its own decision and links it to the call; Asterisk CDR is diagnostic only |
| [3CX IVR](https://www.3cx.com/docs/manual/ivr/) | Office hours/holiday routing and IVR are configured per department/DID | Time and destination data are first-class platform configuration |
| [FusionPBX](https://docs.fusionpbx.com/en/latest/) | Time conditions, call centre queues and CDR are normal PBX features | Calling windows are a first-class condition, not an operator note |
| [VitalPBX](https://wiki.vitalpbx.com/) | Includes dialer, stats and recording modules | Enforcement and evidence must be API-visible, not a UI-only setting |

Modern PBX products mostly *advise* on DNC/hours. AtsaPBX **enforces**
them (PRD INV-09), so this context is stricter than the prior art we
reviewed.

## 3. Model

```go
type Policy struct {
    TenantID      shared.TenantID
    Profile       JurisdictionProfile // named, tenant-owned
    Windows       []CallingWindow      // weekdays in the contact's zone
    Timezone      string               // fallback IANA zone
    AbandonLimit  float64              // percent; 0 = disabled until R2
    DNCScope      DNCScope             // tenant | campaign
}

type ContactContext struct {
    Number      string    // E.164, normalized
    Timezone    string    // from list data, else country fallback
    CampaignID  *shared.CampaignID
    LocalTime   time.Time // computed once at the decision
}

type Attempt struct {
    TenantID   shared.TenantID
    CampaignID *shared.CampaignID
    Contact    ContactContext
    At         time.Time
}
```

The **engine is pure** (HLD 04 §3): it takes a `Policy`, a `DNCSet`, an
`Attempt`, and returns a `Verdict`:

```go
type Verdict struct {
    Permitted bool
    Reason    string   // dnc | outside_hours | opted_out | abandoned_limit | permitted
    RuleID    string   // the exact window/DNC row, for the audit trail
    LocalTime time.Time
}
```

No database, network, clock or logger is reachable from this function.
The orchestration around it (below) is deliberately thin.

## 4. Storage

All tables are tenant-scoped with RLS, created by `compliance` migrations.

```sql
compliance_policies (tenant_id, profile, timezone, abandon_limit, updated_at,
                     PRIMARY KEY (tenant_id))
calling_windows      (tenant_id, profile, weekday SMALLINT, start_minute, end_minute)
dnc_entries          (tenant_id, number_digits, scope, campaign_id NULL, source, created_at,
                     PRIMARY KEY (tenant_id, number_digits, scope, campaign_id))
opt_outs             (tenant_id, number_digits, source, created_at,
                     PRIMARY KEY (tenant_id, number_digits))
compliance_decisions (id, tenant_id, call_id NULL, campaign_id NULL, number_digits,
                      permitted BOOL, reason, rule_id, local_time, decided_at)
```

`compliance_decisions` is append-only; a correction is another row. Numbers
are stored normalized (digits, no plus) and never as free text. DNC
uploads are bounded and streamed into a staging table, then merged inside
one transaction per tenant.

## 5. Ports and API

**Ports consumed by the call path and dialer:**

```go
type Evaluator interface {
    Decide(ctx context.Context, tenantID shared.TenantID, attempt Attempt) (Verdict, error)
}
type PolicyStore interface {
    Policy(ctx context.Context, tx pgx.Tx, tenantID shared.TenantID) (Policy, error)
    DNC(ctx context.Context, tx pgx.Tx, tenantID shared.TenantID, number string, campaign *shared.CampaignID) (DNCSet, error)
    RecordDecision(ctx context.Context, tx pgx.Tx, d Decision) error
}
```

`Decide` is the only call on the outbound path. It loads a snapshot in the
caller's transaction, runs the pure engine, persists the decision **in
the same transaction** as the call attempt, and returns the verdict. A
store failure fails closed (`ErrComplianceUnavailable`); the call is not
placed.

**Public API** (ConnectRPC `ComplianceService`), for the console and
partner API:

- `GetCompliancePolicy` / `UpdateCompliancePolicy`
- `ListCallingWindows` / `ReplaceCallingWindows`
- `ImportDNC` (bounded upload, returns accepted/rejected counts)
- `DeleteDNCNumber`, `ListDNC` (paged, hashed/display number policy)
- `RecordOptOut` (public authenticated endpoint or console)
- `ListComplianceDecisions` (auditor/partner export; filter by campaign,
  period, outcome)
- `ExportComplianceDecisions` (CSV, audited)

Every mutation is audited with before/after. There is no `override`
RPC, and no request field can set `Permitted`.

## 6. Enforcement points

| Path | Decision |
|---|---|
| Manual agent dial | `Decide` before the agent's phone is rung; refusal is a fixed reason on the wire |
| Power/predictive dialer | `Decide` **at the moment of each dial**, not at list load (AC-15.2); queued attempts re-check |
| API-originated call | Same `Decide` in the placement use case; no bypass parameter |
| Emergency | Bypasses compliance entirely; provenance is the platform's emergency matcher only (INV-01) |
| Inbound | No DNC/hours check; the caller chose to call us |

The call's `compliance_decision_id` is stored on the attempt and on the
call record so the evidence view can reconstruct every refused or
permitted attempt. A refusal never creates a billable call.

## 7. Time, DNC and opt-out details

- **Calling hours are re-checked at dial time.** Local time comes from the
  contact list's zone, then a country fallback; no silent server-local
  default. A missing zone is a configuration error, not permission.
- **DNC is checked tenant-wide and campaign-scoped**, with the wider
  tenant list winning. Normalization is E.164 digits; a leading `+` is not
  significant (same rule as DIDs).
- **Opt-out is immediate and permanent**: it writes `opt_outs` and the
  next `Decide`, including one already queued, sees it. The cache, if any,
  is invalidated through the same transaction, not a timer.
- **Abandonment** is a rolling ratio per campaign; approaching the
  configured limit slows the pacer, breaching it stops new attempts;
  calls in progress continue (INV-03). **It applies to power dialling in
  R1.0 as well as predictive in R2**, because a power dialler can abandon
  too (HLD-21 §4, D-95).
- **The jurisdiction profile carries the abandonment rules as
  parameters** (2026-11-07, HLD-21 §4). None of them is a constant:
  - `abandon_connect_limit` (FCC: 2 s after the greeting completes);
  - `abandon_limit` and `abandon_window` (FCC: 3 % per campaign per 30
    days);
  - `min_ring` (FCC: 15 s or 4 rings before giving up);
  - `abandon_message_required` (an identification and opt-out message);
  - `no_redial_after_abandon` (a window during which the number is not
    dialled again; UK practice, confirm the current Ofcom figures before
    adopting);
  - whether AMD time counts against the connect limit.

  US values come from 47 CFR 64.1200(a)(6)–(7). Other jurisdictions are
  tenant-entered, like the emergency catalogue.

## 8. Security and failure model

- RLS on every table; no privileged read path in the API.
- DNC and opt-out numbers are personal data: list endpoints return a
  masked form by default; export requires the auditor scope and is
  audited.
- Store/engine failure fails closed for **new** calls and never touches a
  live call. The error is a fixed `FAILED_PRECONDITION`, with no raw DB or
  legal text.
- Decisions are immutable; deletion of a tenant cascades, but no role can
  edit a decision row.
- No compliance decision is ever written to logs with a full number;
  hashed/tenant-scoped identifiers only.

## 9. Tests and Definition of Done

- Property tests: a DNC-hit number is never permitted for any role/entry
  path; a window boundary is exact at start/end; a missing timezone is
  refused; emergency always permits.
- Integration: DNC import/merge/delete, opt-out propagation within 60 s,
  decision rows join to attempts, RLS cross-tenant, rollback.
- Wire: every refusal code is fixed and stable; no override field exists
  in any proto.
- Live: a listed number is never dialled by API or agent; an out-of-hours
  manual dial is refused; an emergency call still connects.
- DoD: AC-15.1, 15.2, 15.4, 15.5 proven; R2 abandonment (15.3) deferred
  with D-27 but the engine seam and tests exist.

## 10. Open decisions for the owner

1. Which jurisdiction profiles ship in R1.0 (the product must not guess a
   legal rule).
2. Whether DNC import accepts CSV only or also partner API batches.
3. Retention of compliance decision rows (proposed: 24 months, exportable).
4. Whether opt-out has a public unauthenticated endpoint or only an
   authenticated API/console.

## 11. Handoff

`pbx-compliance` is proposed after `pbx-ivr-menu`/queues when campaigns
start (D-45). It gates LLD-06; it does not retroactively gate manual Phase A
calls, whose fraud guard is D-85.

**References:** [Asterisk ARI](https://docs.asterisk.org/Configuration/Interfaces/Asterisk-REST-Interface-ARI/),
[Asterisk reporting](https://docs.asterisk.org/Configuration/Reporting/),
[3CX IVR](https://www.3cx.com/docs/manual/ivr/),
[FusionPBX](https://docs.fusionpbx.com/en/latest/),
[VitalPBX wiki](https://wiki.vitalpbx.com/),
[TRD](../TRD.md), [PRD EPIC-15](../PRD.md).
