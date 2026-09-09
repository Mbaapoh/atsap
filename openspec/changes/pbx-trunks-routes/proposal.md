## Why

Phase A cannot place an outbound call without carriers: extensions register, but every external call needs a trunk and a route. LLD-03 §12 sequences this slice second — the projector it needs landed with `pbx-extensions-projection` — and it unblocks `pbx-call-placement` third. PRD EPIC-02 (BYOT: no lock-in, no markup) starts here.

## What Changes

- **`pbx/domain`: `Trunk` and `CarrierRoute`** exactly as LLD-03 §3 specifies, plus the pure `SelectRoutes` (longest-prefix wins, ties by priority then cost) with property-based tests over prefix sets including overlaps (LLD-03 DoD 14).
- **Migration for `carrier_trunks` + `carrier_routes`** per HLD-03 §5 — `tenant_id`, `ENABLE` and `FORCE ROW LEVEL SECURITY`, `tenant_isolation_*` policies, the pattern `0004` established. Number is the next free at apply time (`0006` if `licensing-capacity-grace` lands first with `0005`).
- **`TrunkStore` / `RouteStore`** operating within caller-supplied transactions (LLD-03 §4.4).
- **Projector gains `ProjectTrunk` / `RemoveTrunk`** (LLD-03 §4.4, absent from code today): PJSIP trunk endpoint + auth rows written in the same transaction as the domain row. Per §7.4 the trunk secret rests in `ps_auths`, readable by `asterisk_engine` — no new grants, same three tables — and never appears in an API response, log, or audit row after creation; `credential_ref` keeps the secret out of the domain table.
- **RPCs on `PbxService`**: `CreateTrunk`, `ListTrunks`, `UpdateTrunk`, `DeleteTrunk`, `CreateRoute`, `ListRoutes`, `DeleteRoute` (LLD-03 §6). Authorization through `identity.AuthorizeAction` with tenant matching on every method (INV-10 pattern from the extensions change). `docs/API.md` §1/§3 rows added by the landing change.
- **Health is observed, never set**: `Healthy | Degraded | Unhealthy`, default `Healthy`. `ReportRouteHealth` stays port-only (LLD-03 §6); selection skips `Unhealthy` and deprioritizes `Degraded`. The first reporter lands with `pbx-call-placement` (call-outcome reporting) — stated sequencing, not a gap.
- **`channel_limit` stored and validated positive** (blast-radius control, LLD-03 §10.2); runtime enforcement needs live per-trunk counts and lands with `pbx-call-placement`.
- **Edition gating is deferred, explicitly.** The Unregistered single-trunk cap and LCR-by-edition join the D-49 provisioning-enforcement follow-up — the same change taking the extension and tenant caps. No entitlement port exists until licensing lands, so gating now would mean gating against nothing.
- **Out of scope**: `PlaceCall`/`HangupCall` (`pbx-call-placement`), `InboundRouter`, `emergency_numbers` and `IsEmergency` wiring (`pbx-inbound-and-emergency`), `Outbound`-class enforcement (`pbx-call-placement`), per-department caller identity (not Phase A), cost visibility (`reporting`, LLD-09), any rating model (§9 stays: prefix-and-priority with stored cost, not true LCR).

## Capabilities

### New Capabilities

- `pbx-core/trunk-routing`: trunk and route lifecycle through the public API, deterministic route selection, observed trunk health, projection of trunks into engine state.

### Modified Capabilities

- `pbx-core/endpoint-projection`: projection extends from extensions to trunks, and the "credentials never recoverable" requirement gains the §7.4 trunk exception (engine-readable secret, stated controls, recorded residual risk).

## Impact

- **New:** `pbx/domain` trunk + route types and matcher, `pbx/postgres` trunk/route stores, application orchestration, `pbx/rpc` handlers, migration (number per above), `PbxService` proto additions, `docs/API.md` rows.
- **Changed:** `pbx/ports` (`ConfigService` trunk/route ops mirroring extensions, `RoutePlanner`, both stores, projector methods), `pbx/acl/asterisk` projector. Regenerated protobuf stubs.
- **Docs:** `docs/API.md` §1/§3 (by the landing change, LLD-03 §6 convention). No LLD/HLD change — LLD-03 already specifies all of this.
- **Not touched:** anything under `internal/telephony/` (no telephony change in this slice — the risky one is deliberately `pbx-inbound-and-emergency`'s, LLD-03 §12), `internal/identity/`, `internal/licensing/`, existing migrations.
- **Parallel-safe vs `licensing-capacity-grace`:** disjoint files except migration numbering (assumption above) and `cmd/atsap-api` wiring order — whoever lands second rebases the wiring. No shared tables, no shared ports.
