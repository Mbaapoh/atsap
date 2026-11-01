## 1. Prove it and decide it

- [x] 1.1 Reproduce the stall on the rig as a failing Go e2e test (`api/internal/pbx/e2e`). Tenant A has a trunk whose host is unresolvable (`locslhodt`); a call from tenant B (extension to extension, and carrier to DID) must reach its first ring within 1 s. Verify it fails today with roughly 5 s. In the same session, measure whether **placing** a call over the unresolvable trunk delays tenant B's calls, and record the numbers here. If it does, add a *High* gap to the design's Non-Goals and to PROGRESS, without widening this change.

  *(done 2026-11-01, `e2e/host_resolution_test.go`; the reproduction is by lookup count, not by time.)*
  - **Why not time:** the slowness depends on the resolver of the day. Today the rig's Docker DNS fails `locslhodt` in 6 ms (and `localhosts2` and `.invalid` alike), so a 1 s budget passes. On 2026-10-13 the same lookup took about 5 s. The deterministic proof is that Asterisk *looks the name up at all* while identifying: every failed lookup is logged (`netsock2.c: ast_sockaddr_resolve: getaddrinfo("…")`), and the test counts those lines in the engine's log.
  - **Who is affected:** phones are identified by username first (`endpoint_identifier_order = username,ip,anonymous`), so REGISTERs and phone calls never reach identification by address. **Carrier requests do, for every tenant.** That matches the incident: a carrier call stuck at Presenting.
  - **The failing test** (`TestHostResolution_AnUnresolvableHostIsNotLookedUpWhileIdentifying`): tenant A has the `locslhodt` trunk; tenant B's carrier sends three INVITEs to an unassigned DID. Each is answered 404 in 70–385 ms, but the engine made **12 lookups of `locslhodt`**: four per INVITE transaction (the INVITE and its ACK, for A and AAAA).
  - **Outbound measured** (`TestHostResolution_DiallingAnUnresolvableTrunkDelaysNoOneElse`, passing): a phone dialling a number routed to the `locslhodt` trunk ends with SIP 603 after 119 ms. Carrier INVITEs sent meanwhile were answered in 73–87 ms. Placing a call over an unresolvable trunk delays no one else, so no new gap.
  - **Found and fixed (leaks):** two e2e cleanups orphaned trunk projections.
    - `TestTrunkHealth_IsWhatTheCarrierSays` deleted its trunks with SQL before the API cleanup ran.
    - The inbound rig deleted only its own carrier trunk through the API.

    Twelve orphaned identify rows were on the rig; `pbx reconcile --fix` removed them. Both cleanups now go through `releaseConfiguration`, and reconcile reports no orphans after the tests.
  - **Left alone:** the `probe-self`, `probe-dead` and `probe-nxdomain` engine rows are the throwaway AORs from the D-74 measurement. Reconcile leaves them by design (not written by the platform); they are not identify rows. They are for the owner to remove.
- [x] 1.2 Record D-75 in `docs/DECISIONS.md`: the platform resolves carrier names for identification; identify holds addresses only; the last good addresses are kept; unresolvable hosts are accepted and flagged. Cite the alternatives from design D1. Verify `make docs` passes.

  *(done 2026-11-01: D-75 recorded with its four rejected alternatives and the measured context from 1.1; `make docs` passes.)*

## 2. Domain and probe

- [x] 2.1 Add `ResolvedAddresses` and `ResolvedAt` to `domain.Trunk`, and the pure rule `ObserveAddresses`, plus `IdentifyMatch(addresses)` (the joined literals, or none), per design D2. Unit tests cover:
  - an IP host, IPv4 and IPv6;
  - a name resolved to a new set (sorted, deduplicated, replacing the old one);
  - a failed name keeping its current set;
  - an empty set giving no match.

  Verify `make test` passes, with `internal/pbx/domain` at 100%.

  *(done 2026-11-01: `domain/trunk_addresses.go` — `AddressesForHost`, `ObserveAddresses`, `NormaliseAddresses`, `SameAddresses`, `IdentifyMatch` — at 100% statement coverage. IPv4-mapped addresses are unmapped and zoned ones refused, so equal sets compare equal and a link-local zone never reaches the engine. A lookup that "succeeds" with no address counts as a failure. `make test` and `make lint` pass. The package as a whole is at 86.5%, above its unit floor of 85%; the uncovered lines are pre-existing and covered by the integration tier.)*
- [x] 2.2 Make `sipprobe.Prober` return every resolved address in `Result.Addresses`, alongside the one it probes (still IPv4 first). Unit tests use a fake `Resolver`: a mixed A/AAAA answer, duplicates, and a failure giving no addresses with `HostNotFound`. Verify `make test` passes.

  *(done 2026-11-01: `ports.CarrierCheck` gains `Resolved` and `Addresses`. The probe normalises with the domain's `NormaliseAddresses`, so IPv4 sorts first and the check goes there. An IP host is never looked up. The addresses are reported whatever the carrier then did: a silent carrier still says where it is. An empty answer counts as a failure. Five new or extended unit tests; `sipprobe` coverage 85.1% → 89.9%. `make test` and `make lint` pass.)*

## 3. Store, projection, check

- [x] 3.1 Migration 0013: `resolved_addresses inet[] NOT NULL DEFAULT '{}'` and `resolved_at timestamptz` on `trunks`, with IP hosts backfilled to `{host}`, and a down migration. The store reads and writes the columns, and `UpdateTrunk` clears them when the host changes. Integration tests on `atsapbx_test` cover the backfill, the round trip and the clear on host change. Verify `make test` with the integration tag passes, and that down and up runs cleanly.

  *(done 2026-11-01. The table is `carrier_trunks`, not `trunks` as the design says.
  - **Backfill:** each host is tried as an address in a `DO` block, and a name is skipped on `invalid_text_representation`. So `1.2.3`, a valid name that is not an address, is not cast by accident.
  - **The store:**
    - reads and writes both columns, normalised;
    - writes `{}`, never NULL, for no address;
    - in `Update`, keeps the stored addresses when the host is unchanged, whatever the edit carries, so a configuration edit cannot overwrite a concurrent check;
    - in `Update`, sets them to the new host itself (IP) or to none (name) when the host changes.
  - **The application:** sets the same on create and on a host change.
  - **New integration tests:**
    - `TestTrunkAddressesMigration_IPHostsAreBackfilled`: IPv4, IPv6, a name, `1.2.3` and `locslhodt`; down to 12 removes both columns; up again.
    - `TestTrunkStore_AddressesFollowTheHost`.
  - **Verified:** the whole Go integration tier passes on `atsapbx_test` (30 packages); `make lint` 0 issues.)*
- [x] 3.2 `ProjectTrunk` writes identify from the addresses only, deleting the row when there are none. `DiffTrunk` compares with `IdentifyMatch(t.ResolvedAddresses)`. Integration tests:
  - an IP trunk is projected at once;
  - a name trunk with no addresses has no identify row and never a name;
  - a `match` hand-edited to a name is reported as drift, and repair restores the addresses.

  Verify the reconcile integration tests pass.

  *(done 2026-11-01.
  - **The projector:** `ProjectTrunk` and a new `ProjectTrunkIdentify` (for the check) both write identify through one `projectIdentify`. It writes `IdentifyMatch(t.ResolvedAddresses)`, or deletes the row when there is none.
  - **`DiffTrunk`:** reports `identify` when the row holds a name or other addresses, and also when a row exists that a trunk with no address should not have.
  - **Tests:** `sampleTrunk` now carries a resolved IPv4 and IPv6 pair. New tests are `TestProjectTrunk_IdentifyHoldsAddressesNeverAName` and `TestDiffTrunk_IdentifyIsComparedWithTheRecordedAddresses`; the latter tampers the row to the name, to a subset and to a superset, and repairs each.
  - **Verified:** the `pbx/acl/asterisk` and `pbx/application` integration tiers (reconcile included) pass on `atsapbx_test`; `make lint` 0 issues.)*
- [ ] 3.3 The check records addresses and health in one guarded transaction (`SetCheckedObservation`), and reprojects identify only when the set changed (design D3). Integration tests:
  - a check of a resolving name writes its addresses to identify;
  - a later failure keeps them;
  - a check that raced a host change is dropped;
  - an unchanged set writes nothing to `ps_endpoint_id_ips`.

  Verify the prober integration tests pass.

## 4. Live proof

- [ ] 4.1 Make the 1.1 e2e test pass. Add e2e tests on the rig:
  - a trunk configured by the carrier's **service name** (not its IP) has a carrier → DID call attributed to it once the first check completes;
  - changing that trunk's host stops recognising the old carrier;
  - `pbx reconcile` reports no divergence after a round.

  Verify `mise exec -- make test` with e2e (rig restored first) passes, together with `uat-inbound`, `uat-placement` and `uat-auto`, with the inbound UAT's carrier configured by name.

## 5. Docs and close

- [ ] 5.1 Update:
  - LLD-03 §7 (trunk projection: addresses only, the keep rule, and a document history row);
  - `docs/PROGRESS.md` (close the DNS-stall gap with a link to this change; add any gap from 1.1);
  - this change's coverage notes.

  Ratchet the coverage floors. Verify `make docs`, `make quality` and `make coverage` pass, and that every tier ran green once more on the finished change (Go unit, integration, e2e and the three UATs; `make console-check` as a regression check, since the console is unchanged).
