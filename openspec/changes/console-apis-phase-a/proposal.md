## Why

Phase A (D-46) ends with console slice A: six screens a partner uses to walk flows A1–A5 without touching a configuration file (HLD-18 §7). The console may use **only** public endpoints (D-43, AC-10.7), so a screen that cannot be built from the API means the API is incomplete. Three of the six screens cannot be built today:

- **Licence status and activation** (A1): the licence can be applied only by an environment variable read at startup. `ApplyLicenseKey` and `GetLicenseStatus` are specified as RPCs in LLD-08 §6 but were never built.
- **Users, roles, permissions** (A2): users can be created, disabled and granted roles, but cannot be **listed**, and a granted role cannot be **revoked**.
- **Call history, read-only** (A5): only `GetCall` by id exists, so there is no way to list calls.

D-46's rule is that a phase's endpoints land before its console slice. This change closes those gaps so that console slice A can be built next without a private endpoint.

## What Changes

- **`LicensingService` (new):**
  - `GetLicenseStatus` returns state (Setup, Valid, Unverified, Degraded), edition, licensed channels, channels in use, tenant and extension caps, expiry, last confirmation and the reason for a non-valid state. Any authenticated caller may read it: the console header shows it to every user.
  - `ApplyLicenseKey` takes a signed token. It requires installation-wide (system-scope) authority, since a licence belongs to the installation, not a tenant. It takes effect without a restart or interrupting a call, and is audited.
  - The token is never logged or returned. A rejected token leaves the licence in force untouched.
  - Licensing stays Tier 0 and imports nothing. The handler gets its authorization through a port licensing owns, adapted at the root over identity, the same pattern as the provisioning caps.
- **`IdentityService` additions:**
  - `ListPrincipals` lists the tenant's users with status and role bindings. It never returns a password hash.
  - `RevokeRole` removes a role binding, with the same authority rules as `GrantRole`. A caller cannot remove the last tenant administrator, so no tenant is left unmanageable. It is audited.
- **`TelephonyService.ListCalls`:** the tenant's calls, newest first, with paging and filters (state, direction, time range). It returns domain fields only (numbers, direction, state, timestamps, duration, termination reason) and **never** a channel or engine identifier. It is a minimal read for console slice A; rich call detail and reporting remain LLD-09's (Phase B).
- **Docs:** `docs/API.md` §1 rows, and the `GetLicenseStatus`/`ApplyLicenseKey` pending rows removed from §3. The LLD-03 and LLD-08 status lines are corrected (both still read "Draft").
- **Out of scope:**
  - hardware fingerprinting, 3-of-5 matching and the licensing CLI (the rest of LLD-08's `licensing-apply-key`);
  - listing tenants (operator console, not slice A);
  - API-key listing;
  - call-detail reporting (LLD-09).

## Capabilities

### New Capabilities

- `licensing/license-api`: reading licence status and applying a licence through the public API.

### Modified Capabilities

- `identity/public-api`: users are listable and roles revocable through the API.
- `telephony-core/call-lifecycle`: a tenant's calls are listable through the API, with no engine identifiers.

## Impact

- **New:** the `licensing.proto` service, a `licensing/rpc` handler with its own authorizer port, and a status read on the licensing application. Also the `ListPrincipals`/`RevokeRole` handlers and a store query, `ListCalls` with a call-store list query, regenerated stubs and `docs/API.md` rows.
- **Changed:** `cmd/atsap-api` wiring (mounting `LicensingService` behind the auth interceptor, and the authorizer adapter).
- **Not touched:** call setup, Screening, the pbx dial plan and projection. No migration: every read is over existing tables, so there is no schema change.
- **Tests:** unit, integration, wire, and e2e through the running licensed app:
  - status read;
  - applying a dev-signed token changes capacity live and is reverted afterwards;
  - a forged token is refused with the licence untouched;
  - users listed, and a role revoked;
  - a placed call appears in `ListCalls`.

  There are no phone calls in this slice, so the UAT tiers (`uat-auto`, `uat-placement`, `uat-inbound`) are re-run unchanged as a regression check.
