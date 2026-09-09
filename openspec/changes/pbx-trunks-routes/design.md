## Context

`pbx-extensions-projection` landed: the `pbx/acl/asterisk` projector, the same-transaction projection pattern (HLD-18 §4.1), `PbxService` RPC conventions, and migration `0004` (extensions + `ps_*`, no carrier tables). LLD-03 specifies everything this slice builds (§3 domain, §4.4 ports, §6 RPCs, §7.4 trunk secrets) and sequences it second (§12). `licensing-capacity-grace` is applying concurrently on `develop`; this change is proposed on `feature/pbx-trunks-routes`. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**

- Trunk + route CRUD, deterministic selection, observed health, and trunk projection — all through the patterns the extensions change proved.
- A slice `pbx-call-placement` can route over without modification.

**Non-Goals:**

- The first health *reporter* (lands with call-placement as outcome reporting); this change owns state, transitions and selection only.
- Runtime `channel_limit` enforcement (needs live counts — call-placement).
- Design-level boundaries beyond that are in proposal.md's out-of-scope list, not repeated here.

## Decisions

**D1 — Migration takes the next free number at apply time (`0006` if licensing lands first).**
`0004` holds extensions + `ps_*` only; carrier tables were specified but never migrated. Alternatives (renumber licensing's `0005`, share one migration) both couple two parallel tracks for no benefit. The tasks assert the number against the tree at apply time rather than hard-coding it now.

**D2 — Build ungated; edition gating joins the D-49 provisioning-enforcement follow-up.**
The Unregistered single-trunk cap and LCR-by-edition need an entitlement port that does not exist until licensing lands. Sequencing this change after licensing buys nothing (Claude's parallel-safety analysis holds: disjoint files, tables and ports), and a second stub would be deleted within weeks. The follow-up already owns the extension and tenant caps; trunk gating is the same shape in the same place.

**D3 — Health state machine ships before its first reporter.**
`ReportRouteHealth` stays port-only per LLD-03 §6; selection honors the states from day one; everything defaults `Healthy`. A prober inside this change would report to no consumer — failover timing is call-placement's acceptance (AC-02.2), not this slice's. What ships is testable in isolation: transitions on reports, selection honoring them.

**D4 — Trunk secrets mirror the extension-secret handling, plus the §7.4 exception.**
Supplied once at creation, redacted at the constructor (never filtered at the sink — LLD-03 §10.4), reference in the domain row, usable form only in `ps_auths`. No new engine grants: the residual risk is recorded in the endpoint-projection delta, not argued away. Alternative (a separate secrets store) adds a dependency and a second revocation path for Phase A value nobody consumes.

**D5 — No `telephony-core` change, verified the cheap way.**
Unlike the inbound slice, nothing here needs a port on `telephony-core`. The tasks assert `git diff --stat api/internal/telephony/` is empty at the end — the same tripwire shape licensing uses, guarding the opposite direction (this change must not drift into the risky slice).

**D6 — Selection is pure domain, cost is stored not computed.**
`SelectRoutes(dialled, routes, trunks)` stays a pure function (D-21 shape) with property tests per DoD 14. `cost_per_minute` participates only as a tie-break; any rating model is LLD-09's, and building one here would put a pricing engine in the wrong context (§9).

## Risks / Trade-offs

- [Risk] Migration-number collision if licensing renumbers → Mitigation: tasks resolve the number against the tree at apply start, before writing the migration.
- [Risk] Concurrent licensing apply touches `cmd/atsap-api` wiring → Mitigation: whoever lands second rebases; both additions are additive registrations, no shared logic.
- [Risk] `channel_limit` accepted but unenforced until call-placement → Mitigation: recorded in specs as storage + validation only; enforcement is call-placement's stated scope, not a silent drop.
- [Trade-off] Overlapping prefixes permitted (warning-free): overlap *is* least-cost expression (HLD-18 §5). Forbidding it would forbid the feature.

## Migration Plan

Single migration (two domain tables + RLS + policies, `0004` pattern); down migration drops them. Rollback is drop-and-revert — no data backfill exists yet. Projector `reconcile` covers the new rows by the same mechanism as extensions (same tables family, same ACL package); tasks extend its coverage, not its design.
