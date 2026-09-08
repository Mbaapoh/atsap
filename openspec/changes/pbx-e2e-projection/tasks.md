## 1. Automate the three G1 scenarios

- [x] 1.1 Verify the fixture Argon2id hash against the repository's own hasher before relying on it (`Verify(password, hash) -> ok=true`; wrong password -> `ok=false`) — a hash that silently failed to verify would break every test at authentication, for a reason that looks nothing like the cause
- [x] 1.2 Build the fixture: seed a synthetic tenant and a `TENANT_ADMIN`-at-tenant-scope operator, then obtain a token through the real `AuthenticateUser` RPC rather than minting one (tenant scope deliberately, not platform/system — a system binding covers every tenant and would mask a failure affecting ordinary tenant-scoped callers)
- [x] 1.3 Automate "A device registers immediately after creation": REGISTER succeeds on the **first attempt** with no sleep and no retry, contact confirmed bound in `ps_contacts`, API reports `REGISTERED`, `ps_auths.password` is `NULL`
- [x] 1.4 Automate "A wrong secret is refused": `401` for a wrong secret, then `200` for the correct one — a refusal must not lock the endpoint out
- [x] 1.5 Automate "Deletion takes effect immediately": correct secret refused with no reload, projected endpoint row gone
- [x] 1.6 Guard the interceptor wiring: unauthenticated `CreateExtension` returns `401` (a regression mounting `PbxService` without the interceptor would leave every scenario above passing while the API was open)

## 2. Verify and record

- [x] 2.1 Three consecutive runs, zero retries, zero flakes (`ok atsap-api/internal/pbx/e2e` ×3)
- [x] 2.2 Full tiers green on a restored rig: unit `-race -cover` (17 pkgs), integration `-p 1 -race` (25 pkgs, 0 failures), both e2e suites, lint 0 issues, proto lint+breaking, docs, diagrams
- [x] 2.3 Confirm no product code changed (`git diff --stat develop` touches `docs/TESTING.md` only, plus the new test package)
- [x] 2.4 Record G1 as closed and the new finding G3 in `coverage.md`; add the pbx e2e row to `docs/TESTING.md`
