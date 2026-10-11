# Tasks — console-apis-phase-a

Closes the API gaps that block console slice A (HLD-18 §7, D-46): licence status and activation, user listing and role revocation, and call history. Every task ends with a verification. The UAT tiers re-run unchanged because no call path changes.

## 1. Licensing API

- [ ] 1.1 Add `Service.Status(ctx)` to `licensing/application`: entitlement, channels in use, the verified token's expiry and the stored last confirmation. Setup returns the not-activated state and reason, never an error. Verify unit tests cover Setup, Valid, Unverified and Degraded, and that no field can carry token or key material (design D2)
- [ ] 1.2 Add `licensing.proto` (`LicensingService`: `GetLicenseStatus`, `ApplyLicenseKey`), regenerated with the pinned plugins, and `licensing/rpc` with its own `Authority` and `Auditor` ports. Verify `buf lint`, `buf breaking` against develop, and that `go list -deps ./internal/licensing/...` still reaches nothing outside licensing and shared (design D1)
- [ ] 1.3 Implement the handlers per D3. Verify unit tests with fake ports: any authenticated caller reads status; unauthenticated → UNAUTHENTICATED; a forged/malformed token → INVALID_ARGUMENT with a constant message and status unchanged; no system scope → PERMISSION_DENIED; success returns the new status; the auditor gets edition and channels only; and no response or error contains the token
- [ ] 1.4 Wire `LicensingService` in `cmd/atsap-api` behind the auth interceptor, with the `Authority` and `Auditor` adapters over identity. Verify `go build ./...`, and that `make lint` keeps `licensing-imports-nothing` green

## 2. Identity API

- [ ] 2.1 Add a `ListPrincipals` store query (RLS, paged, by username) and bindings for a page in one `ANY($1)` query, then the RPC, authorized as `principal.read` / system scope. Verify integration tests: a tenant's users with status and roles, no other tenant's, paging, PERMISSION_DENIED for an AGENT, and no password hash in any field (design D4)
- [ ] 2.2 Add `RevokeRole`, with `GrantRole`'s authority guards, a last-`TENANT_ADMIN` guard under a per-tenant advisory lock, and an audit row; add `principal.role.revoke` to `IdentityActions`. Verify tests: a revoked role stops authorizing on the next request; the last admin is refused FAILED_PRECONDITION; a tenant admin cannot revoke a platform role or another tenant's binding; and a deterministic lock test, fault-injected (fails with the lock removed)

## 3. Call history

- [ ] 3.1 Add `CallStore.ListCalls(ctx, tenant, filter, page)` in `telephony/postgres` (parameterized; validated state/direction; `[from, to)`; newest first; limit ≤ 200) and `TelephonyService.ListCalls`, guarded like `GetCall`. Verify integration tests: tenant isolation, each filter, paging, `duration_seconds` (0 when never answered), and that a body naming another tenant is refused by the tenant matcher (design D5)
- [ ] 3.2 Assert no engine identifier in `ListCalls` responses, including calls that failed in setup. Verify by rendering responses and searching for `atsa-part-`, `PJSIP/`, `[et]_`+32-hex and bridge ids, fault-injected by adding a channel field to the mapping

## 4. Docs and gates

- [ ] 4.1 Add `docs/API.md` §1 rows for the five RPCs, remove §3's pending licensing row, and correct the LLD-03 and LLD-08 status lines. Verify `make docs` passes and the proto-vs-API.md inventory diff matches for every service
- [ ] 4.2 Run `make lint`, `go build`, `go vet` under every tag, unit with `-race`, and integration with `-race -p 1`, all green. Verify no change under `internal/pbx/` or the telephony call path (`telephony/application`, `telephony/acl`)

## 5. End to end and regression

- [ ] 5.1 Add `internal/console/e2e` (or extend the pbx e2e rig) against the running licensed app: `GetLicenseStatus` = Valid/16; `ApplyLicenseKey` with a dev-key-signed token of different capacity changes `GetLicenseStatus` live, and the original `.env` token is re-applied in cleanup even on failure; a forged token → INVALID_ARGUMENT with status unchanged; a tenant admin → PERMISSION_DENIED; `ListPrincipals` returns the operator with roles; `RevokeRole` on a second user stops their next authorized call; a call placed with `PlaceCall` is first in `ListCalls`. Verify each assertion in the test
- [ ] 5.2 Re-run every e2e suite and `make uat-auto`, `make uat-placement` and `make uat-inbound` back to back. Verify all green, with `pbx reconcile` clean afterwards
- [ ] 5.3 Write `coverage.md` mapping every scenario in the three delta specs to the test that proves it, with gaps in the G-series convention
