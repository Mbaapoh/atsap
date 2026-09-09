## 1. Migration and domain

- [ ] 1.1 Resolve the migration number against the tree at apply start (`0006` if `0005_licensing` exists, else `0005`); verify `ls api/migrations/` shows no collision before writing anything (design D1)
- [ ] 1.2 Write the migration creating `carrier_trunks` + `carrier_routes` per HLD-03 §5 with `ENABLE` and `FORCE ROW LEVEL SECURITY` and `tenant_isolation_*` policies; verify by running `MigrateUp` on a scratch database and confirming both tables, both RLS flags and both policies exist (spec `trunk-routing`, LLD-03 DoD 12)
- [ ] 1.3 Add `TrunkID` and `RouteID` to `api/internal/shared/domain` following the existing `ExtensionID` pattern; verify they compile and round-trip through string form in a unit test (LLD-03 §2)
- [ ] 1.4 Implement `pbx/domain` `Trunk`, `CarrierRoute`, `Destination` types per LLD-03 §3 with allowlist validation for host/prefix (digits + fixed wildcard set, never a regex); verify unit tests reject a bad host, bad port, non-positive channel limit and a regex-style pattern (spec `trunk-routing`, LLD-03 §10.5)
- [ ] 1.5 Implement pure `SelectRoutes` (longest-prefix, priority, cost tie-break; skip `Unhealthy`, deprioritize `Degraded`) returning the matched rule identity with each selection; verify property-based tests over generated prefix sets including overlaps always return the same winner for the same input (spec `trunk-routing`, LLD-03 DoD 14, AC-02.3/02.6)

## 2. Stores, projector, application

- [ ] 2.1 Implement `pbx/postgres` `TrunkStore` + `RouteStore` with parameterized queries only, operating within caller-supplied transactions; verify integration tests cover create/read/update/delete/list under a tenant context plus cross-tenant invisibility identical to nonexistent (spec `trunk-routing`, INV-10 pattern)
- [ ] 2.2 Extend `EndpointProjector` with `ProjectTrunk`/`RemoveTrunk` and implement in `pbx/acl/asterisk`: trunk endpoint + auth rows in the caller's transaction, secret into `ps_auths` only, `credential_ref` in the domain row; verify an integration test proves a created trunk yields engine rows and a removed trunk yields none, with the secret absent from every non-`ps_auths` record (spec `endpoint-projection` delta, LLD-03 §7.2/§7.4)
- [ ] 2.3 Implement `ConfigService` trunk/route operations mirroring the extension flow (authorize → BEGIN → domain write → projection → audit row → COMMIT), secret redacted at construction; verify an injected projector failure leaves no domain row and an injected domain failure leaves no `ps_*` row (spec `trunk-routing`, LLD-03 DoD 10, §10.4)
- [ ] 2.4 Implement `RoutePlanner` (`SelectOutboundRoute` over stored routes/trunks via `SelectRoutes`, `ReportRouteHealth` transitioning observed state); verify a reported `Unhealthy` removes the trunk from subsequent selections while in-progress state is untouched, and that health accepts no caller-supplied value (spec `trunk-routing`)
- [ ] 2.5 Extend `reconcile` to the trunk-projected rows; verify it detects a hand-edited trunk `ps_*` row, reports it, and repairs it with `--fix` (LLD-03 DoD 11)

## 3. RPC surface and docs

- [ ] 3.1 Add `CreateTrunk`/`ListTrunks`/`UpdateTrunk`/`DeleteTrunk` + `CreateRoute`/`ListRoutes`/`DeleteRoute` to `PbxService` with proto regen; verify handlers cover authorized success, unauthorized refusal, and cross-tenant access returning the same "not found" as a nonexistent identifier (spec `trunk-routing`, LLD-03 §6)
- [ ] 3.2 Assert no engine identifier reaches any caller: verify a test over the trunk/route RPC surface proves no projected id, endpoint name or routing context appears in any response or error (LLD-03 DoD 13, PRD principle 4)
- [ ] 3.3 Add the `docs/API.md` §1/§3 rows for the seven RPCs in the same commit (LLD-03 §6 convention); verify the inventory matches the implemented surface

## 4. Gates and end-to-end proof

- [ ] 4.1 Run the full gates (`go build ./...`, `go vet ./...`, `go test ./... -race -cover`) green; verify the existing `depguard` boundary test still passes with trunk code present — no `ps_*` reference outside `pbx/acl/asterisk` (LLD-03 DoD 7)
- [ ] 4.2 Extend the RLS isolation test to `carrier_trunks` + `carrier_routes`; verify a row under tenant A is invisible under tenant B, and `ps_*` rows for trunks carry no `tenant_id` and no RLS, asserted deliberately (LLD-03 DoD 12)
- [ ] 4.3 Assert the telephony seam is untouched: verify `git diff --stat api/internal/telephony/` is empty (design D5)
- [ ] 4.4 Prove the slice live against the dev stack through the public API only: create trunk + route, assert the projected rows exist, delete the trunk, assert subsequent selection never returns it; verify a live placed call is explicitly out of scope here and owned by `pbx-call-placement` (LLD-03 DoD 3 split)
