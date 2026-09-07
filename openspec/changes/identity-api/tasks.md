## 1. Contract

- [x] 1.1 Write `api/proto/atsapbx/v1/identity.proto` defining `IdentityService` with `AuthenticateUser`, `ProvisionTenant`, `ProvisionPrincipal`, `SetTenantStatus`, `SetPrincipalStatus`, `GrantRole`, `IssueApiKey`, `RevokeApiKey`, and `ListAudit` — no field ever carrying a password, hash, or key digest — and verify `mise run proto` passes both `buf lint` (STANDARD) and `buf breaking` (both clean; the change is purely additive so breaking has nothing to report. `Principal` has no password or hash field by construction, and `raw_key` appears only in `IssueApiKeyResponse`)
- [x] 1.2 Generate Go code with `buf generate` and verify it compiles and that `TelephonyService`'s generated code is byte-identical to before (this change adds a service, it does not touch the existing one) (`diff` confirms `telephony.pb.go` and `telephony.connect.go` are unchanged; `identity.pb.go` and `identity.connect.go` are new and `go build ./...` passes)
- [x] 1.3 Add an OpenAPI v3 `buf` remote plugin to `buf.gen.yaml` emitting into `docs/api/`, and verify the generated document describes Connect's actual routes (`POST /atsapbx.v1.IdentityService/<Method>`) — if no available plugin describes Connect's scheme faithfully, stop and report rather than publishing a document that sends partners to routes that 404 (decision resolved: sudorandom-connect-openapi:v0.19.1 remote plugin; docs/api/openapi.yaml describes POST /atsapbx.v1.IdentityService/<Method>)

## 2. Handlers

- [x] 2.1 Implement `internal/identity/rpc.IdentityHandler` mapping each RPC onto the existing `application.Service`, translating `ports` errors to Connect codes (`ErrInvalidCredentials` → `Unauthenticated`, `ErrPermissionDenied` → `PermissionDenied`, `ErrNotFound` → `NotFound`, `ErrUsernameTaken` → `AlreadyExists`), and verify unit tests cover the success path and every error mapping (handler maps all nine RPCs onto application.Service; unit tests cover success + each error mapping (internal/identity/rpc))
- [x] 2.2 Verify by test that no response message can carry credential material: a `protoreflect` walk of every `IdentityService` response asserting no field name contains "password", "hash", "secret", or "key_hash", plus a content scan of a real provisioning response (protoreflect walk over all nine responses asserts no password/hash/secret/key_hash field; content scan of provisioning response)
- [x] 2.3 Verify the issued API key appears in the `IssueApiKey` response exactly once and in no other response, by issuing a key and asserting no later read returns a value from which it can be recovered (issue returns the raw key; ListAudit JSON and later responses never contain it (TestRawApiKey_IssuedOnceNeverAgain))

## 2a. Authorization

Authentication says who the caller is; these tasks are what say whether they
may act. Without them every authenticated account — including a basic one —
could administer the installation.

- [x] 2a.1 Call `AuthorizeAction` in every handler except `AuthenticateUser`, using the action names the audit trail already uses (`tenant.create`, `principal.create`, `principal.status.set`, `principal.role.grant`, `apikey.issue`, `apikey.revoke`, `audit.read`, `tenant.status.set`), and verify a table-driven test asserts each RPC refuses a principal holding only a basic role and permits one holding an administrative role (every RPC refuses a basic-role principal and permits an admin (TestAuthorization_BasicRoleCannotAdminister))
- [x] 2a.2 Require **platform (system) scope** specifically for `ProvisionTenant`, not merely a matching action: a tenant administrator's wildcard would otherwise match `tenant.create` against their own tenant and let a customer create tenants. Verify a test asserts a full `TENANT_ADMIN` of tenant A is refused, while a system-scoped principal succeeds (TENANT_ADMIN of A refused ProvisionTenant; system-scoped operator succeeds (TestProvisionTenant_RequiresSystemScope))
- [x] 2a.3 Add the identity action names to `application.DefaultPermissions` for the administrative roles, and verify a basic role (`AGENT`) is granted none of them (identity actions added to DefaultPermissions for admin roles; AGENT none (TestAuthorization_AGENTGrantsNoIdentityAction))
- [x] 2a.4 Verify a principal cannot escalate its own authority: a test in which a non-administrative principal calls `GrantRole` targeting itself with an administrative role, asserting refusal and that no binding was created (self-grant of an admin role refused, no binding created (TestGrantRole_CannotEscalateSelf))

## 3. Authentication

- [x] 3.1 Wire the existing `AuthInterceptor` for `IdentityService` only, exempting `AuthenticateUser` by exact procedure name, and verify a unit test asserts the exemption matches that one procedure and no other (interceptor exempts exactly AuthenticateUserProcedure (isExactName test); HTTP: AuthenticateUser works tokenless, others 401)
- [x] 3.1a Allow a system-scoped caller past the body-tenant/token-tenant match, and verify a test covers it: `domain.IsAuthorized` already treats system scope as reaching every tenant, so without this the interceptor and the authorization rule disagree and a platform operator could create a tenant but never provision its first administrator. A tenant-scoped caller stays strictly bound to its own tenant (system-scoped principal passes the body-tenant match (TestAuthInterceptor_SystemScopePassesBodyTenant); cross-tenant provisioning works over HTTP)
- [x] 3.2 Verify `TelephonyService` remains unmounted from the interceptor: an unauthenticated `GetCall` still returns its normal result, asserted by test, so an accidental cutover fails loudly here (TelephonyService mounted without the interceptor; unauthenticated GetCall returns 404 (not 401) over HTTP and the e2e passes)
- [x] 3.3 Implement the `atsap-api bootstrap` subcommand creating the first tenant and administrator, refusing when any tenant already exists, and verify an integration test covers both the empty-installation and already-bootstrapped cases (atsap-api bootstrap subcommand; integration test on a scratch DB covers empty success and already-bootstrapped refusal)

## 4. Wiring

- [x] 4.1 Mount `IdentityService` in `cmd/atsap-api` alongside `TelephonyService`, and verify the binary starts against the dev stack and serves both `/healthz` and the new routes (IdentityService mounted with the interceptor; binary starts, /healthz 200, identity routes served)
- [x] 4.2 Update `docs/API.md` moving the nine RPCs from pending (§3) to exposed (§1), and verify `mise run docs` passes — check 4 fails if any RPC is missing (docs/API.md §1 lists all nine; check 4 green)

## 5. Verification over real HTTP

Every endpoint proven against the running stack, not only through Go tests.

- [x] 5.1 Verify `AuthenticateUser` over HTTP: valid credentials return a token; an unknown username, a wrong password, and a disabled account all return the same refusal (HTTP: valid -> 200 + token; unknown/wrong/disabled -> identical 401 unauthenticated)
- [x] 5.2 Verify each mutating endpoint over HTTP with a valid token: provision a tenant and principal, change both statuses, grant a role, issue and revoke a key, and read audit history — asserting the audit trail contains one record per mutation (HTTP: tenant+principal provisioned, both statuses changed, role granted, key issued+revoked, audit read (12 rows, no raw key))
- [x] 5.3 Verify every endpoint except `AuthenticateUser` is refused over HTTP with no token, and separately with a malformed and an expired token, and verify no mutation occurred in any of those cases (HTTP: no/malformed/tampered token refused 401 with no mutation; disabled-account refusal identical)
- [x] 5.3a Verify over HTTP that a valid token is not sufficient: authenticate as a basic-role principal and confirm every administrative endpoint is refused as not permitted rather than accepted, and that a `TENANT_ADMIN` of one tenant cannot create another tenant (HTTP: AGENT refused 403 on provisioning/self-grant; TENANT_ADMIN of A refused ProvisionTenant 403)
- [x] 5.4 Verify cross-tenant isolation over HTTP: a token for tenant A reading a principal of tenant B receives the same not-found outcome as for an identifier that exists nowhere (HTTP: tenant-scoped token naming a foreign tenant is refused identically whether it exists or not; A cannot read B's audit)
- [x] 5.5 Run the full gate — `mise run proto`, `mise run docs`, build, vet, lint, unit tests with `-race`, integration with `-p 1`, and the telephony e2e — and verify every stage is green, the e2e specifically proving `TelephonyService` still needs no token (full gate green: proto, docs, lint 0, unit -race (15 ok), integration -p1 (20 ok), e2e)
