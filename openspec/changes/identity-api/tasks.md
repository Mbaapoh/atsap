## 1. Contract

- [ ] 1.1 Write `api/proto/atsapbx/v1/identity.proto` defining `IdentityService` with `AuthenticateUser`, `ProvisionTenant`, `ProvisionPrincipal`, `SetTenantStatus`, `SetPrincipalStatus`, `GrantRole`, `IssueApiKey`, `RevokeApiKey`, and `ListAudit` — no field ever carrying a password, hash, or key digest — and verify `mise run proto` passes both `buf lint` (STANDARD) and `buf breaking`
- [ ] 1.2 Generate Go code with `buf generate` and verify it compiles and that `TelephonyService`'s generated code is byte-identical to before (this change adds a service, it does not touch the existing one)
- [ ] 1.3 Add an OpenAPI v3 `buf` remote plugin to `buf.gen.yaml` emitting into `docs/api/`, and verify the generated document describes Connect's actual routes (`POST /atsapbx.v1.IdentityService/<Method>`) — if no available plugin describes Connect's scheme faithfully, stop and report rather than publishing a document that sends partners to routes that 404

## 2. Handlers

- [ ] 2.1 Implement `internal/identity/rpc.IdentityHandler` mapping each RPC onto the existing `application.Service`, translating `ports` errors to Connect codes (`ErrInvalidCredentials` → `Unauthenticated`, `ErrPermissionDenied` → `PermissionDenied`, `ErrNotFound` → `NotFound`, `ErrUsernameTaken` → `AlreadyExists`), and verify unit tests cover the success path and every error mapping
- [ ] 2.2 Verify by test that no response message can carry credential material: a `protoreflect` walk of every `IdentityService` response asserting no field name contains "password", "hash", "secret", or "key_hash", plus a content scan of a real provisioning response
- [ ] 2.3 Verify the issued API key appears in the `IssueApiKey` response exactly once and in no other response, by issuing a key and asserting no later read returns a value from which it can be recovered

## 3. Authentication

- [ ] 3.1 Wire the existing `AuthInterceptor` for `IdentityService` only, exempting `AuthenticateUser` by exact procedure name, and verify a unit test asserts the exemption matches that one procedure and no other
- [ ] 3.2 Verify `TelephonyService` remains unmounted from the interceptor: an unauthenticated `GetCall` still returns its normal result, asserted by test, so an accidental cutover fails loudly here
- [ ] 3.3 Implement the `atsap-api bootstrap` subcommand creating the first tenant and administrator, refusing when any tenant already exists, and verify an integration test covers both the empty-installation and already-bootstrapped cases

## 4. Wiring

- [ ] 4.1 Mount `IdentityService` in `cmd/atsap-api` alongside `TelephonyService`, and verify the binary starts against the dev stack and serves both `/healthz` and the new routes
- [ ] 4.2 Update `docs/API.md` moving the nine RPCs from pending (§3) to exposed (§1), and verify `mise run docs` passes — check 4 fails if any RPC is missing

## 5. Verification over real HTTP

Every endpoint proven against the running stack, not only through Go tests.

- [ ] 5.1 Verify `AuthenticateUser` over HTTP: valid credentials return a token; an unknown username, a wrong password, and a disabled account all return the same refusal
- [ ] 5.2 Verify each mutating endpoint over HTTP with a valid token: provision a tenant and principal, change both statuses, grant a role, issue and revoke a key, and read audit history — asserting the audit trail contains one record per mutation
- [ ] 5.3 Verify every endpoint except `AuthenticateUser` is refused over HTTP with no token, and separately with a malformed and an expired token, and verify no mutation occurred in any of those cases
- [ ] 5.4 Verify cross-tenant isolation over HTTP: a token for tenant A reading a principal of tenant B receives the same not-found outcome as for an identifier that exists nowhere
- [ ] 5.5 Run the full gate — `mise run proto`, `mise run docs`, build, vet, lint, unit tests with `-race`, integration with `-p 1`, and the telephony e2e — and verify every stage is green, the e2e specifically proving `TelephonyService` still needs no token
