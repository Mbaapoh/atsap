## Context

`pbx-extensions-projection` landed: the `pbx/acl/asterisk` projector, the same-transaction projection pattern (HLD-18 §4.1), `PbxService` RPC conventions, and migration `0004` (extensions + `ps_*`, no carrier tables). LLD-03 specifies everything this slice builds (§3 domain, §4.4 ports, §6 RPCs, §7.4 trunk secrets) and sequences it second (§12). `licensing-capacity-grace` and `license-provisioning-enforcement` have since landed and been archived; this change is applied on `develop`. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**

- Trunk + route CRUD, deterministic selection, observed health, and trunk projection — all through the patterns the extensions change proved.
- A slice `pbx-call-placement` can route over without modification.

**Non-Goals:**

- The first health *reporter* (lands with call-placement as outcome reporting); this change owns state, transitions and selection only.
- Runtime `channel_limit` enforcement (needs live counts — call-placement).
- Design-level boundaries beyond that are in proposal.md's out-of-scope list, not repeated here.

## Decisions

**D1 — Migration takes the next free number at apply time (`0007`: `0005_licensing` and `0006_ps_aors_complete` landed first).**
`0004` holds extensions + `ps_*` only; carrier tables were specified but never migrated. Alternatives (renumber licensing's `0005`, share one migration) both couple two parallel tracks for no benefit. The tasks assert the number against the tree at apply time rather than hard-coding it now.

**D2 — Build ungated; edition gating belongs to `entitlement-module-gating`.**
The Unregistered single-trunk cap and LCR-by-edition need an entitlement port that does not exist until licensing lands. Sequencing this change after licensing buys nothing (Claude's parallel-safety analysis holds: disjoint files, tables and ports), and a second stub would be deleted within weeks. *(Revised at apply time:)* `license-provisioning-enforcement` took the extension and tenant caps but deliberately not this one — BRD §12.2 grades trunks across three edition levels (single / multi-trunk with failover / plus LCR), a capability ladder rather than a number, which LLD-11 §9 assigns to `entitlement-module-gating` after Phase A. Building ungated here is unchanged by that.

**D3 — Health state machine ships before its first reporter.**
`ReportRouteHealth` stays port-only per LLD-03 §6; selection honors the states from day one; everything defaults `Healthy`. A prober inside this change would report to no consumer — failover timing is call-placement's acceptance (AC-02.2), not this slice's. What ships is testable in isolation: transitions on reports, selection honoring them.

**D4 — Trunk secrets mirror the extension-secret handling, plus the §7.4 exception.**
Supplied once at creation, redacted at the constructor (never filtered at the sink — LLD-03 §10.4), reference in the domain row, usable form only in `ps_auths`. No new engine grants: the residual risk is recorded in the endpoint-projection delta, not argued away. Alternative (a separate secrets store) adds a dependency and a second revocation path for Phase A value nobody consumes.

**D5 — No `telephony-core` change, verified the cheap way.**
Unlike the inbound slice, nothing here needs a port on `telephony-core`. The tasks assert `git diff --stat api/internal/telephony/` is empty at the end — the same tripwire shape licensing uses, guarding the opposite direction (this change must not drift into the risky slice).

**D6 — Selection is pure domain, cost is stored not computed.**
`SelectRoutes(dialled, routes, trunks)` stays a pure function (D-21 shape) with property tests per DoD 14. `cost_per_minute` participates only as a tie-break; any rating model is LLD-09's, and building one here would put a pricing engine in the wrong context (§9).

**D7 — Trunk credentials follow SIP-trunk convention: optional username + secret.** *(Added at apply time.)*
LLD-03 §3 gave a trunk a secret and no username, but carrier digest authentication needs both. 3CX ("Authentication ID" / password), FreePBX and VitalPBX (username / secret) all take the pair, and all allow neither for IP-authenticated carriers. So: `auth_username` is optional configuration, stored on the trunk row and returned on reads; `secret` is optional, write-only, and refused without a username. `UpdateTrunk` takes an optional replacement secret; omitting it keeps the stored one. Projected From-user is the auth username and From-domain the host, the FreePBX defaults; neither is separately configurable in Phase A. **Outbound registration to the carrier** (FreePBX "send registration", 3CX "registration required") is deliberately not here: it needs a `ps_registrations` table and a new engine grant, which the endpoint-projection requirement confines. It is the natural follow-up with the first health reporter.

**D8 — What an observed outcome does to health.** *(Added at apply time.)*
`ReportRouteHealth(statusCode, latency)` is mapped by a pure domain function: a 2xx within 2 s → `Healthy`, a 2xx slower than that → `Degraded`, no response (0), 408 or any 5xx → `Unhealthy`; every other code (404, 486, 480…) describes the callee, not the carrier, and leaves health unchanged. Hysteresis and thresholds over a window are AC-02.5's and land with the reporter.

**D9 — Prefix patterns are digits plus `X`.** *(Added at apply time.)*
A pattern is an optional leading `+` then 1–50 characters of `0-9` or `X` (any one digit) — FreePBX's `X` without its expression syntax (§10.5). Matching strips a leading `+` from both sides. Specificity is pattern length; equal length ties on priority, then cost, then route id, so the order is total and the same input always yields the same winner.

## Risks / Trade-offs

- [Risk] Migration-number collision if licensing renumbers → Mitigation: tasks resolve the number against the tree at apply start, before writing the migration.
- [Risk] Concurrent licensing apply touches `cmd/atsap-api` wiring → Mitigation: whoever lands second rebases; both additions are additive registrations, no shared logic.
- [Risk] `channel_limit` accepted but unenforced until call-placement → Mitigation: recorded in specs as storage + validation only; enforcement is call-placement's stated scope, not a silent drop.
- [Trade-off] Overlapping prefixes permitted (warning-free): overlap *is* least-cost expression (HLD-18 §5). Forbidding it would forbid the feature.

## Migration Plan

Single migration (two domain tables + RLS + policies, `0004` pattern); down migration drops them. Rollback is drop-and-revert — no data backfill exists yet. Projector `reconcile` covers the new rows by the same mechanism as extensions (same tables family, same ACL package); tasks extend its coverage, not its design.
