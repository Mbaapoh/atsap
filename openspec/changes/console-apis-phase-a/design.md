## Context

- **Licensing:** `licensing/application.Service` already has `Entitlement(ctx)` (state, edition, channels, caps, reason), `ChannelsInUse()` and `ApplyLicenseKey(ctx, token)`, which verifies before parsing, is idempotent, and leaves the licence untouched on a bad token. There is no RPC: `cmd/atsap-api` applies `ATSAPBX_LICENSE_TOKEN` at boot. Licensing is Tier 0 and imports nothing (HLD-04 §10.1; enforced by `.golangci.yml` and archtest).
- **Identity:** the store has `ListRoleBindings` and `DeleteRoleBinding`, but no principal listing and no RPC for either. `GrantRole`'s authority rules (platform roles and system scope need platform authority; no self-escalation) live in `identity/rpc`.
- **Telephony:** `CallStore` has `GetCall` only. `TelephonyService.GetCall` is guarded by the auth interceptor's tenant match (`rpc.TenantID`), with no role check.

## Goals / Non-Goals

**Goals:**

- Every console slice A screen can be built from public endpoints alone.
- No new dependency direction: licensing still imports nothing.
- No credential, licence material or engine identifier in any new response.

**Non-Goals:**

- Fingerprinting, 3-of-5 matching and the licensing CLI (the rest of LLD-08 `licensing-apply-key`).
- Tenant listing (the operator console).
- API-key listing.
- Call-detail reporting (LLD-09).
- The console itself (the next change).

## Decisions

**D1 — `LicensingService` authorizes and audits through ports licensing owns.**
`licensing/rpc` declares `Authority { RequireAuthenticated(ctx) error; RequireSystemScope(ctx, action) error }` and `Auditor { RecordLicenceApplied(ctx, edition string, channels int) error }`. `cmd/atsap-api` satisfies both over identity: the `TenantContext` the interceptor attached, `AuthorizeSystem(..., "licence.apply")`, and `RecordAudit` under the caller's tenant. This is the pattern the provisioning caps used: the Tier-0 context declares what it needs, and the composition root, which may know both sides, supplies it.
*Rejected:* putting the handler in `cmd/`. It would put request validation and error mapping outside any context and leave them untestable as a unit.

**D2 — `GetLicenseStatus` reads one status view.**
A new `Service.Status(ctx)` returns `Entitlement` plus `ChannelsInUse` plus the verified token's `ExpiresAt` plus the stored `LastConfirmedAt`. These come from the cached verified licence where present and the store otherwise. Setup returns the not-activated state and reason, never an error. Any authenticated caller may read it: the console header shows it to everyone (LLD-08 §6).

**D3 — `ApplyLicenseKey` maps outcomes, and never echoes the token.**
- Unverifiable or malformed token → INVALID_ARGUMENT "licence token does not verify", with the error built from a constant.
- No system scope → PERMISSION_DENIED.
- Success → the new status.

The audit row records edition and channels only. The licensing package already asserts that no log or error carries licence material (its `license-token` spec); a wire test extends that to the RPC.

**D4 — `ListPrincipals` and `RevokeRole` follow the existing identity RPC patterns.**
- *`ListPrincipals`:* authorized as `principal.read` for the caller's own tenant, or system scope for another, the same `authorizeForTenant` preamble as the rest of `identity/rpc`. It pages under RLS, ordered by username. Bindings are attached per principal with a single `ANY($1)` query, not N queries.
- *`RevokeRole`:* the same authority guards as `GrantRole`. A new store method takes a per-tenant advisory lock, counts tenant-scope `TENANT_ADMIN` bindings, and refuses to delete the last one, so two concurrent revocations cannot strip both of the last two administrators.
- `principal.read` already exists in `DefaultPermissions` (SUPERVISOR). `principal.role.revoke` is added to `IdentityActions` and so to the TENANT_ADMIN and PLATFORM_ADMIN lists.

**D5 — `ListCalls` mirrors `GetCall`'s guard, and reads only domain columns.**
`CallStore.ListCalls(ctx, tenant, filter, page)` runs a parameterized query on `calls` under the tenant's RLS context:
- optional state and direction (validated against the domain enums) and a `[from, to)` range on `started_at`;
- newest first;
- limit capped at 200.

Entries carry the call's domain fields plus a computed `duration_seconds` (`ended - answered`, 0 if never answered). The handler is guarded exactly as `GetCall` is: interceptor plus tenant match. It adds **no role check**. That is a known gap shared with `GetCall` (any authenticated user of the tenant may read its calls), recorded rather than widened. Role-checked reads arrive with LLD-09's reporting context.

**D6 — Docs ship with the RPCs.** `docs/API.md` §1 gains the five rows. §3's pending licensing row is removed (LLD-08 §6's own instruction), and the stale "Draft" status lines of LLD-03 and LLD-08 are corrected in the same change.

**D7 — Proving it.**
- *Unit and integration:* each RPC's authorization, outcome mapping and data shape. The revoke last-admin guard uses a deterministic lock test, fault-injected.
- *Wire:* none of the new responses carries a password hash, token, signature or `[et]_`+hex engine identifier.
- *e2e,* against the running licensed app:
  - `GetLicenseStatus` reports Valid with channels 16;
  - `ApplyLicenseKey` with a token signed by the rig's dev key (`.dev-licence/`) changes channels live, and the original `.env` token is re-applied afterwards;
  - a forged token is refused with the status unchanged;
  - a tenant admin gets PERMISSION_DENIED;
  - `ListPrincipals` lists the operator, and `RevokeRole` revokes a role, which the next request feels;
  - a call placed through `PlaceCall` appears first in `ListCalls`.
- *UAT:* no phone path changes, so the three UATs re-run unchanged as a regression check.

## Risks / Trade-offs

- [The e2e changes the running app's licence] → it re-applies the original token in cleanup, even on failure; and `make rig-restore` re-applies it from `.env` anyway.
- [`ListCalls` has no role check] → it is the same exposure `GetCall` already has, recorded as a gap rather than silently extended. Agent-scoped visibility arrives with LLD-09.
- [`ApplyLicenseKey` makes licence changes a network operation] → it needs system scope, it is audited, and verification precedes parsing (D-53). A bad token can only fail.

## Migration Plan

No schema change. Rollback is a code revert. The API is additive (`buf breaking` must stay clean).
