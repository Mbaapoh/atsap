## Why

D-52 made every entitlement an activated signed token: Free Community means 4 channels, 10 extensions, 1 tenant — but nothing enforces the 10 and the 1. A Free installation can provision a hundred extensions and five tenants today, silently exceeding its licence, with no error anywhere. D-49, D-51, LLD-02 §4a.1 and LLD-11 all assign the fix to the same place: refuse at provisioning time, in the contexts that create tenants and extensions. Without this change the caps are display text.

## What Changes

- **`identity`: `CreateTenant` refuses at the cap** (LLD-02 §4a.1, D-51, already designed — this change builds it). The cap arrives through a port `identity` owns, satisfied at the composition root; `identity` still depends on nothing. `MaxTenants = 0` means unlimited. Refusal is `FAILED_PRECONDITION` with an entitlement reason, never `PERMISSION_DENIED` (AC-06.14). Reads are never gated. RLS stays enabled and forced in every mode.
- **`pbx-core`: `CreateExtension` refuses at `MaxExtensions`**, mirroring D-51 exactly: a port `pbx` owns, cap supplied in, same error semantics, same read/gate split, same RLS-untouched rule.
- **Grandfathering everywhere** (AC-06.13): existing tenants, extensions and trunks are never removed or hidden; only new provisioning is refused. The only new refusal fires on installations with nothing to lose.
- **e2e stays green under the dev token** (D-54): the suites activate rather than bypass. If the committed dev token mirrors Free Community caps, the two-tenant e2e test gains a licence fixture in the same change (design D6).
- **Out of scope — the trunk cap, removed on review.** BRD §12.2 varies trunks across three levels by edition (single / multi-trunk with failover / plus least-cost routing), not one boolean. That is capability-by-edition, which is module gating, which LLD-11 §9 assigns to `entitlement-module-gating` and explicitly places after Phase A. Deriving it from an edition string here would put the commercial catalogue inside `pbx-core`, which is the coupling the `entitlement` context exists to prevent. See design D3.
- **Out of scope**: module gating (`MayUse`, grants table, enable/disable RPCs) — `entitlement-module-gating`, explicitly not Phase A (LLD-11 §9). LCR-by-edition with it. Any change to the stored entitlement or its columns — `licensing` owns those (D-53). Console upgrade affordances (EPIC-10).

## Capabilities

### New Capabilities

(none — every behaviour here extends an existing capability)

### Modified Capabilities

- `identity/tenant-provisioning`: provisioning refuses at the installation's tenant cap.
- `pbx-core/extension-management`: provisioning refuses at the installation's extension cap.

## Impact

- **Changed:** `identity/application` + `identity/ports` (cap port, `CreateTenant` check), `pbx/application` + `pbx/ports` (mirror port, both creation checks), composition-root wiring satisfying both ports over `licensing`, e2e fixture (conditional, design D6).
- **Not touched:** anything under `internal/telephony/` or `internal/licensing/`; no new imports into `identity` or `pbx` (ports are locally owned, adapted at the root — HLD-04 §10.1 holds); no RLS policy change; no migration (caps are entitlement data, counts are queries).
- **Sequencing:** after `licensing-capacity-grace` — there is no published entitlement to read until it lands. No longer contingent on `pbx-trunks-routes`, since the trunk cap left this change (design D3). Parallel-safe meanwhile: disjoint files.
