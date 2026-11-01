## 1. Prove and decide

- [ ] 1.1 Spike on the rig (D-25), as e2e evidence:
  - **(a)** a realtime endpoint `carrier-ingress` with an identify row covering a container carrier's address receives its INVITE into Stasis;
  - **(b)** ARI reads `CHANNEL(pjsip,remote_addr)` on that arriving channel as the container's address;
  - **(c)** an identify row with 500 CIDR entries still answers carrier requests promptly.

  Record the measurements here. If (a) or (b) fails, stop and revise the design with the owner.
- [ ] 1.2 Record D-77 (carrier traffic enters through one ingress and is attributed by source address and number; it amends D-75 and D-76's per-trunk recognition). Cite the OR-ed identify evidence and INV-10. Verify `make docs` passes.

## 2. Attribution

- [ ] 2.1 Migration 0015 adds `carrier_ingress_match(source inet, number text)` (`SECURITY DEFINER`, `search_path` pinned, executable by the app role only), with a down migration.

  Integration tests:
  - one match;
  - none;
  - two tenants with the same number (two rows);
  - a range match;
  - a trunk whose range does not contain the source (no row);
  - no other column or tenant is readable through it.

  Verify the integration tier passes.
- [ ] 2.2 In pbx-core, add the `InboundAttributor` port and its implementation over the function: exactly one routes; none or several refuse, with an operator-only warning on several.

  Unit tests for the rule; integration tests across two tenants. Verify `make test` and the integration tier pass.

## 3. Projection and the arriving-call path

- [ ] 3.1 The projector writes the `carrier-ingress` endpoint, AOR and identify (the union across tenants) in every trunk write and address change, and stops projecting per-trunk identify rows. Startup ensures the ingress. Reconcile reports and repairs ingress drift and leftover per-trunk rows.

  Integration tests:
  - the union across two tenants;
  - a delete removes that trunk's part;
  - per-trunk rows removed on startup;
  - reconcile.

  Verify the `pbx` integration tiers pass.
- [ ] 3.2 Telephony:
  - `handleArrivingChannel` reads `CHANNEL(pjsip,remote_addr)` for the ingress endpoint;
  - `CallArrived` attributes before recording, rejecting `404` with no call row when refused;
  - the router receives the attributed trunk.

  Unit tests with fakes; integration as needed. Verify every Go tier passes.

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
