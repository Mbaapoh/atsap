## Why

`identity-auth-rbac` built eleven working capabilities and exposed none of
them. Today the entire wire surface is one RPC (`GetCall`), which means the
portal cannot be started, a partner developer cannot do anything at all
(US-05.1: "every action available in the console through a documented API"),
and — concretely — `auth-cutover-connectrpc` cannot ship, because it makes a
JWT mandatory while the only way to mint one is a Go method call.

D-43 fixed the rule; this change applies it to `identity`, and is pulled
ahead of the licensing work so the API lands sooner.

## What Changes

- **New `IdentityService` proto** (`atsapbx.v1`) with nine RPCs:
  `AuthenticateUser`, `ProvisionTenant`, `ProvisionPrincipal`,
  `SetTenantStatus`, `SetPrincipalStatus`, `GrantRole`, `IssueApiKey`,
  `RevokeApiKey`, `ListAudit`.
- **Auth enforced on this service from the moment it exists.** Every RPC
  except `AuthenticateUser` requires `Authorization: Bearer <JWT>`; the
  interceptor built in `identity-auth-rbac` is wired for `IdentityService`
  only. `TelephonyService` stays unauthenticated until
  `auth-cutover-connectrpc`, so the e2e suite and UAT rig keep working.
- **A bootstrap path for the first administrator.** Provisioning requires a
  token, a token requires a principal, and a principal requires provisioning
  — an operator-run bootstrap breaks that cycle without a dev-only backdoor.
- **Generated OpenAPI v3 document** produced from the protos by a `buf`
  remote plugin, so US-05.1's "documented" is satisfied by something that
  cannot drift from the contract.
- **Every endpoint verified over real HTTP** against the running stack, with
  its authenticated and unauthenticated behaviour both asserted — not only
  through Go handler tests.

Deliberate scope calls, recorded rather than absorbed:

- **Auth enforcement for this service is in scope**, though LLD-02 §9 framed
  enforcement as the cutover's job. Shipping `ProvisionTenant` or `GrantRole`
  reachable without a token would let anyone who can reach the port create a
  tenant and grant themselves administrator. The cutover remains a separate
  change because its risk is different: it breaks existing unauthenticated
  callers, and that is what needs staging.
- **The bootstrap is a CLI subcommand, not a dev-only token minter.** It runs
  in production, which the `//go:build dev` path from `identity-bootstrap`
  deliberately cannot. That change's remaining scope (UAT script updates)
  still follows separately.
- **Licensing RPCs are not here.** `ApplyLicenseKey`/`GetLicenseStatus` need
  `licensing` to exist first; they land with `licensing-apply-key`.

## Capabilities

### New Capabilities

- `identity/public-api`: the externally reachable contract for identity —
  which operations partners and the portal may invoke, what authentication
  each requires, and what each refuses to disclose on failure.

### Modified Capabilities

None. `identity/authentication`, `identity/access-control`,
`identity/tenant-provisioning` and `identity/audit-log` describe behaviour
that is unchanged; this change exposes that behaviour over a transport
rather than altering it.

## Impact

- **New**: `api/proto/atsapbx/v1/identity.proto`, its generated code, an
  `identity/rpc` handler package, a bootstrap subcommand, and the generated
  OpenAPI document.
- **Changed**: `cmd/atsap-api` mounts `IdentityService` with the auth
  interceptor; `buf.gen.yaml` gains the OpenAPI plugin; `docs/API.md` moves
  nine RPCs from pending to exposed.
- **Unchanged**: `TelephonyService` and its proto — no `buf breaking`
  impact; `internal/telephony/**`; the e2e suite and UAT rig, which still
  call `GetCall` without a token.
- **Dependencies**: one `buf` remote plugin for OpenAPI generation, vetted
  per TOOLSET §6. No new Go module.
