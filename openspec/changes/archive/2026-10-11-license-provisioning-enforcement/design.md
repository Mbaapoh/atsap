## Context

D-52/D-53 publish the entitlement (edition, capacity, `max_tenants`, `max_extensions`) from a verified payload; LLD-02 §4a.1 (D-51) fully designs the identity half; LLD-11 assigns the pbx half and defers module gating past Phase A. This change builds the two provisioning refusals and nothing else. See proposal.md for motivation. It has one parent, `licensing-capacity-grace`: there is no published entitlement to read until that lands.

## Goals / Non-Goals

**Goals:**

- The two caps refuse at creation with one shared semantics: supplied-in, `FAILED_PRECONDITION` + entitlement reason, reads never gated, estate grandfathered, RLS untouched.
- Zero new context dependencies: both checks follow the consumer-owned-port pattern (LLD-08 §3.1, LLD-11 §3).

**Non-Goals:**

- Module behaviour gating of any kind (LLD-11 §9). Design-level exclusions beyond that are in proposal.md, not repeated here.

## Decisions

**D1 — Mirror D-51 in `pbx-core` exactly; do not invent a second semantics.**
Cap in, `FAILED_PRECONDITION`, entitlement reason distinct from RBAC (AC-06.14), reads never gated, no RLS change, grandfathering per AC-06.13. One semantics in two contexts is reviewable; two semantics is a support ticket generator. `identity` follows LLD-02 §4a.1 verbatim.

**D2 — Both ports are consumer-owned and satisfied at the composition root.**
Each declares exactly the number it needs — `identity` a tenant cap, `pbx` an extension cap — and nothing else. Neither receives the edition: with the trunk cap out (D3), no consumer has any reason to know what an edition means, which is what keeps the commercial catalogue out of both contexts. Adapters over `licensing` live in `cmd/atsap-api`, the one place permitted to know both sides. No import is added to either Tier-0 context, and the Tier-1 `entitlement` context (D-50/LLD-11) can satisfy the same ports later without touching either consumer.

**D3 — The trunk cap is removed from this change, not redesigned in it.**
The first draft derived it from the published edition, on the stated
premise that "D-53 closed the payload columns with no `max_trunks`".
**That premise is wrong**: D-53 adds `signed_payload` and `signature` and
keeps the claim columns "explicitly as a cache". It never closed the
list, and adding a field is a normal change to LLD-08 §3.

Removing it rather than adding the field, for a reason the field would
not have fixed. BRD §12.2 varies trunks across **three** levels by
edition — single, multi-trunk with failover, and plus least-cost
routing — so this is not one boolean but a point on a capability ladder.
Capability-by-edition is module gating, LLD-11 §9 assigns that to
`entitlement-module-gating`, and places it after Phase A. Deciding it
from an edition string inside `pbx-core` would put the commercial
catalogue in a telephony context, which is precisely the coupling the
Tier-1 `entitlement` context exists to prevent (D-50).

Two smaller reasons agree. The delta targeted `pbx-core/trunk-routing`,
which has no living spec and no `## Purpose`, so archiving would have
created a main spec with a TBD placeholder. And it made this change
contingent on `pbx-trunks-routes`, which has not been proposed — scope
risk bought for a cap that belongs elsewhere.

The tenant and extension caps are unaffected: both are published as
*numbers*, so neither consumer needs to know what an edition means.

**D4 — Counts are tenant-scoped and exact, with no privileged counting path.**
Capped implies single-tenant (D-52: Free is 10 extensions + 1 tenant; licensed is unlimited), so counting within the provisioning tenant equals the installation count in every case the check can fire. No RLS exception, no `BYPASSRLS` widening, no cross-tenant query. A capped multi-tenant edition would reopen this — none exists.

**D5 — Refusals carry the licence reason on the wire from day one.**
AC-06.14's distinct entitlement reason is asserted against the RPC errors for both refusals, reusing the auth-cutover error mapping. `PERMISSION_DENIED` stays reserved for actual authorization failure (LLD-02 §4a.1 reasoning).

## Risks / Trade-offs

- [Risk] Dev-token content unknown: a capped dev token breaks the two-tenant e2e → Mitigation: tasks assert e2e green and add the licence fixture only if the token is capped (no fixture work otherwise).
- [Risk] Counting races under concurrent creation → Mitigation: checks run inside the creation transaction (the extensions pattern), so two racing 11th extensions cannot both commit.
- [Trade-off] Tenant-scoped counting (D4) is exact today by construction, not by mechanism. Documented as the assumption to revisit, not hidden.

## Migration Plan

None. No schema change: caps are published entitlement data, counts are queries, grandfathering needs no backfill.
