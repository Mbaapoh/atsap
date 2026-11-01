## 1. Prove and decide

- [x] 1.1 Spike on the rig (D-25), as e2e evidence:
  - **(a)** a realtime endpoint `carrier-ingress` with an identify row covering a container carrier's address receives its INVITE into Stasis;
  - **(b)** ARI reads `CHANNEL(pjsip,remote_addr)` on that arriving channel as the container's address;
  - **(c)** an identify row with 500 CIDR entries still answers carrier requests promptly.

  Record the measurements here. If (a) or (b) fails, stop and revise the design with the owner.

  *(done 2026-11-01, `e2e/ingress_spike_test.go`, all three pass on the rig. The rows are written and removed by the tests.)*
  - **(a)** A realtime endpoint with no tenant, whose identify row covers the container carrier, took its INVITE into Stasis. The app's event loop logged it with `source_endpoint` set to the spike endpoint, and refused it 404 for want of a tenant, which is exactly where attribution will slot in. An unknown source gets 401 before.
  - **(b)** ARI `GET /channels/{id}/variable?variable=CHANNEL(pjsip,remote_addr)` returned `172.18.0.8:60217`: the carrier container's address and source port. It needs host-and-port parsing (an IPv6 address would be bracketed). The channel was held in the engine's echo test (`ael-demo` 600), because one the app refuses lives for milliseconds.
  - **(c)** With a 500-entry identify row (499 × /24 plus the carrier), the worst of five INVITEs took 3.127 s against a 3.131 s baseline for an unknown source. The harness reads replies for a fixed 3 s window; the difference is nil.
  - **Found:** `ps_endpoint_id_ips.match` was `VARCHAR(80)` (migration 0009), so a trunk with several D-76 ranges failed to create with an internal error. Fixed in its own commit: migration 0015 makes it `TEXT`, with a regression test of 20 IPv6 ranges. The attribution function therefore becomes migration 0016.)*
- [x] 1.2 Record D-77 (carrier traffic enters through one ingress and is attributed by source address and number; it amends D-75 and D-76's per-trunk recognition). Cite the OR-ed identify evidence and INV-10. Verify `make docs` passes.

  *(done 2026-11-01: D-77 recorded with five rejected alternatives and the spike evidence; `make docs` passes.)*

## 2. Attribution

- [x] 2.1 Migration 0016 adds `carrier_ingress_match(source inet, number text)` (`SECURITY DEFINER`, `search_path` pinned, executable by the app role only), with a down migration.

  Integration tests:
  - one match;
  - none;
  - two tenants with the same number (two rows);
  - a range match;
  - a trunk whose range does not contain the source (no row);
  - no other column or tenant is readable through it.

  Verify the integration tier passes.

  *(done 2026-11-01, with a design change from D2: **no `SECURITY DEFINER` and no new role.**
  - **Why:** the tables are FORCE RLS and owned by the app, which has no BYPASSRLS. A BYPASSRLS role can only be created by the superuser at bootstrap (the outbox-worker pattern), which existing installations, this rig included, have already passed.
  - **Instead:** migration 0016 adds a SELECT-only policy per table that applies only while `app.ingress_lookup` is `on`. `carrier_ingress_match`, in plpgsql with a pinned `search_path` and `EXECUTE` revoked from `PUBLIC`, sets the flag for its single query and clears it before returning, within the caller's transaction. It returns only (tenant_id, trunk_id). The trust model equals the existing one, in which the app sets `app.tenant_id`.
  - **Test:** `TestCarrierIngressMatch` covers A's address and number; the same address in B's range with B's number; a range match; a number whose tenant's trunk does not recognise the source (none); an unknown address (none); a number in two tenants (both returned, for the caller to refuse). In the same transaction afterwards, `carrier_trunks` reads zero rows; outside it, `inbound_numbers` reads zero.
  - **Verified:** the `internal/postgres` integration package passes, including full down and up; `make lint` 0.)*
- [x] 2.2 In pbx-core, add the `InboundAttributor` port and its implementation over the function: exactly one routes; none or several refuse, with an operator-only warning on several.

  Unit tests for the rule; integration tests across two tenants. Verify `make test` and the integration tier pass.

  *(done 2026-11-01.
  - **Telephony:** owns the port, `ports.InboundAttributor` (`AttributionRequest`, `Attribution`). It answers `Applies=false` for any endpoint but the ingress, so a configured endpoint still names its own tenant.
  - **pbx-core:**
    - `domain.CarrierIngressEndpoint`, `ParseRemoteAddr` (port dropped, IPv6 bracketed, zones refused) and `AttributeIngress` (exactly one routes);
    - `postgres.IngressStore` over `carrier_ingress_match`, in its own transaction;
    - `application.IngressAttributor`, which returns the matched trunk's own endpoint (`t_…`), so telephony's router treats the call as one that arrived on it.

    Several matches log the trunk ids, operator-only, never the tenants and never on the wire.
  - **Tests:**
    - unit `TestIngressAttributor`: another endpoint; exactly one; none (quiet); several (logged); an unreadable address; a lookup error is an error;
    - unit `TestParseRemoteAddr`, `TestAttributeIngress`;
    - integration `TestIngressStore_AttributesAcrossTenants`: two tenants on one address, each number to its own trunk, the shared number refused.
  - **Verified:** `make test`, `make lint` 0, and every `pbx` integration package plus `archtest`.)*

## 3. Projection and the arriving-call path

- [x] 3.1 The projector writes the `carrier-ingress` endpoint, AOR and identify (the union across tenants) in every trunk write and address change, and stops projecting per-trunk identify rows. Startup ensures the ingress. Reconcile reports and repairs ingress drift and leftover per-trunk rows.

  Integration tests:
  - the union across two tenants;
  - a delete removes that trunk's part;
  - per-trunk rows removed on startup;
  - reconcile.

  Verify the `pbx` integration tiers pass.

  *(done 2026-11-01.
  - **The union is read** through a second flag-guarded function, `carrier_ingress_sources()` (added to the still-unreleased migration 0016), which returns addresses only.
  - **`ProjectIngress`:**
    - takes a transaction advisory lock, so writers in different tenants cannot overwrite each other's union;
    - writes the endpoint (no `set_var`: it never names a tenant), its AOR, and one identify row, or none when nothing is recognised;
    - is called from every trunk projection, removal and address change. Those also delete the trunk's own identify row.
  - **`EnsureIngress`:** run at app startup (`run()` returns 1 if it fails); it also removes every per-trunk row left from before.
  - **Reconcile:** `DiffIngress` (endpoint fields and identify against the union, never resolving a name) appears in the report and the CLI, and `Repair` rebuilds the ingress last. `DiffTrunk` reports a leftover per-trunk row; `ListProjected` treats the ingress as the platform's own.
  - **Tests:**
    - projector: `RecognitionIsTheIngress`, `IsTheUnionAcrossTenants` (two tenants; tampered as a name, a subset or a superset, each repaired; a removed trunk's ranges leave), `LeftoverTrunkRowsAreRemoved`, `WritesTenantVariable` (rewritten), `RemoveTrunk_StopsRecognisingItsCarrier`;
    - application: the identify helpers now read the ingress and assert no per-trunk row, with expectations updated to unions; `TestReconcile_CarrierIngressDriftIsReportedAndRepaired`;
    - endpoint-count assertions exclude the ingress, which is platform infrastructure, not configuration.
  - **Verified:** `make test`, `make lint` 0, and every Go integration package on `atsapbx_test`.)*
- [x] 3.2 Telephony:
  - `handleArrivingChannel` reads `CHANNEL(pjsip,remote_addr)` for the ingress endpoint;
  - `CallArrived` attributes before recording, rejecting `404` with no call row when refused;
  - the router receives the attributed trunk.

  Unit tests with fakes; integration as needed. Verify every Go tier passes.

  *(done 2026-11-01.
  - **The event loop:** reads `ATSA_TENANT_ID`. A missing variable means no tenant, which is no longer a warning; the ingress carries none. When there is no tenant it reads `CHANNEL(pjsip,remote_addr)` and passes it on, through a new `sourceAddress` parameter on the sink's `CallArrived`.
  - **The service:** `arrivalTenant`, extracted to keep `CallArrived` under the complexity limit, attributes before anything is recorded.
    - Refused, or not the ingress: rejected `404`, with no call row.
    - Attribution itself failed: rejected `503`.
    - Otherwise the call is recorded under the attributed tenant, and the router receives the attributed trunk's endpoint.
  - **Wiring:** `main` gives the call service `pbxapp.NewIngressAttributor(pbxpostgres.NewIngressStore(appPool), logger)`.
  - **Tests:**
    - `TestEventLoop_ATenantlessChannelCarriesItsSourceAddress`: the address is read only when there is no tenant;
    - `TestCallArrived_TheCarrierIngressIsAttributedFirst`: attributed (recorded under the tenant, routed as `t_…`); refused, not-ingress and lookup failure (rejected, nothing recorded, never routed).
  - **Verified:** `make test`; `make lint` 0; every Go integration package passes.)*

## 4. Live proof

- [ ] 4.1 Go e2e on the rig, with two tenants whose trunks both recognise one container carrier:
  - a call to tenant A's number rings A's extension and is recorded in A only;
  - a call to B's number rings B's;
  - a number configured in both is refused `404`, with the warning logged;
  - a header naming the other tenant is ignored;
  - the existing inbound and D-75/D-76 e2e tests still pass.

  Verify the full Go e2e tier, the three phone UATs and the console UAT pass after `make rig-restore` (the owner's phones are stopped and restarted with consent).

## 5. Docs and close

- [ ] 5.1 Update:
  - LLD-03 §4.2, §7.5–7.6, §11.1 (sequence) and history;
  - PROGRESS (close the High shared-address gap);
  - `docs/uat/trunk-host-resolution.md` (a note: shared carriers just work);
  - `coverage.md`.

  Ratchet the floors. Verify `make docs`, `make quality` and `make console-check` pass.
