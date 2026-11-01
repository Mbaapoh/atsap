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
- [x] 3.3 The check records addresses and health in one guarded transaction (`SetCheckedObservation`), and reprojects identify only when the set changed (design D3). Integration tests:
  - a check of a resolving name writes its addresses to identify;
  - a later failure keeps them;
  - a check that raced a host change is dropped;
  - an unchanged set writes nothing to `ps_endpoint_id_ips`.

  Verify the prober integration tests pass.

  *(done 2026-11-01.
  - **Writes:** health is still written first, under the host-and-port guard. The new `SetResolvedAddresses` then writes the addresses under a host guard, and reports whether the set changed. It reads the old set under the row lock in the same statement, so a concurrent check cannot make that report wrong.
  - **Identify:** the prober reprojects identify (`ports.TrunkIdentifyProjector`, the same projector `main` gives the service) only on a change.
  - **A failed name with no addresses** has its row cleared. This covers rows written by the previous version that still hold the name, and is a no-op otherwise.
  - **IP hosts:** a check never touches them.
  - **Integration tests:**
    - `RecordsWhatTheNameResolvedTo`: none, then two addresses; an unchanged set leaves the row's `xmin` alone but moves `resolved_at`; a failure keeps the set; a new set replaces it; reconcile agrees.
    - `ANameIsNeverLeftForTheEngine`.
    - `AStaleCheckRecordsNoAddresses`.
    - `TrunkStore_ResolvedAddressesAreGuardedByHost`.
  - **Verified:** the whole Go integration tier passes on `atsapbx_test` (35 packages); `make test` and `make lint` pass.)*

## 4. Live proof

- [x] 4.1 Make the 1.1 e2e test pass. Add e2e tests on the rig:
  - a trunk configured by the carrier's **service name** (not its IP) has a carrier → DID call attributed to it once the first check completes;
  - changing that trunk's host stops recognising the old carrier;
  - `pbx reconcile` reports no divergence after a round.

  Verify `mise exec -- make test` with e2e (rig restored first) passes, together with `uat-inbound`, `uat-placement` and `uat-auto`, with the inbound UAT's carrier configured by name.

  *(done 2026-11-01, on the rig app rebuilt with this change. Migration 13 was applied; the owner's Dev Acme trunk `localhost` now reads `127.0.0.1,::1` in identify, the same thing Asterisk resolved it to before.
  - **The 1.1 reproduction passes:** zero lookups of `locslhodt`, and the other tenant's carrier INVITEs are answered in 22–33 ms (70–385 ms before).
  - **New: `TestHostResolution_ACarrierConfiguredByNameIsRecognised`.** The carrier is a throwaway container on the engine's network, reached by a unique DNS name, that sends a raw INVITE.
    - From an unknown source it gets 401.
    - Once the trunk is created by that name, its identify row becomes the container's address within one check, and the INVITE is routed to the tenant's plan: 404 for an unassigned DID.
    - `pbx reconcile` reports nothing for the trunk.
    - Moving the trunk's host to `192.0.2.1` projects that address at once, and the carrier is challenged (401) again.
  - **Full Go e2e tier:** every package passes (`pbx/e2e` 45 s, `telephony/e2e`, `identity/e2e`), after `make rig-restore`.
  - **UATs:** `uat-auto`, `uat-placement` and `uat-inbound` all PASS, with media both ways.
  - **Deviation:** `uat-inbound`'s carrier is still configured by IP. That carrier is baresip on the host, and no name resolves to the host's bridge address from the app's network (no `host.docker.internal`, and `host-gateway` maps to a different bridge). The by-name proof is the e2e test above, with a real container carrier resolved through Docker's DNS.
  - **Found and fixed:** the app binary is `/out/app` under Tilt but `/usr/local/bin/atsap-api` in the compose image. `scripts/console-soak.sh` hard-coded the Tilt path, and now runs whatever the container runs; so does the e2e test.
  - **Noted:** `pbx reconcile` exits 1 on this rig only because of the three throwaway `probe-*` rows from the D-74 measurement.
  - **Noted:** `make dev-app` also rebuilds the Asterisk image, because the app depends on it; that build was stopped, and the app restarted with `--no-deps`.)*

## 5. Docs and close

- [x] 5.1 Update:
  - LLD-03 §7 (trunk projection: addresses only, the keep rule, and a document history row);
  - `docs/PROGRESS.md` (close the DNS-stall gap with a link to this change; add any gap from 1.1);
  - this change's coverage notes.

  Ratchet the coverage floors. Verify `make docs`, `make quality` and `make coverage` pass, and that every tier ran green once more on the finished change (Go unit, integration, e2e and the three UATs; `make console-check` as a regression check, since the console is unchanged).

  *(done 2026-11-01.
  - **Docs:**
    - LLD-03 gains §7.5 (a carrier is recognised by address, never by name) and a document history row;
    - PROGRESS closes the DNS-stall gap and lists the change as done; no new gap came out of 1.1;
    - `coverage.md` maps every scenario to its tests;
    - the design and proposal name the table correctly (`carrier_trunks`).
  - **Floors ratcheted** (unit): total 77.3 → 77.4; `pbx/domain` 85 → 86.4. The merged all-tier floors were not remeasured: `coverage-all` needs the cover-instrumented Tilt image, and the rig ran the compose image.
  - **The quality gate failed first** on advisories published since 2026-10-14, none caused by this change. They were cleared in their own commit (`deps: …`):
    - Go 1.26.10 (the one reachable finding, `crypto/x509`);
    - `klauspost/compress` v1.18.7 and `x/crypto` v0.56.0;
    - a dated exception for the unfixed, unused `x/crypto/openpgp` advisory;
    - console `vitest` 4.1.12 and `undici` 7.30.1.
  - **Verified:**
    - `make quality` exit 0: lint 0 issues; unit coverage 77.4%; console 398/398; no vulnerabilities and 0 reachable; no leaks; audit 0.
    - The Go integration tier passes on the new dependencies (35 packages).
    - `make console-check`: component 398, e2e 29, rig integration 25, UAT 3, audit 0.
    - `make docs` passes.
    - Earlier in the change: Go e2e tier all green, and the three phone UATs PASS.
  - **Found and fixed:** `console/e2e/support/rig.ts` hard-coded the Tilt binary path (`/out/app`); it now runs whatever the container runs.
  - **Environment, not code:** this machine was under heavy load (load average about 20 on 8 cores, with an unrelated Rust package build). Several prebuilt Go tools crashed inside the Go runtime: OSV-Scanner (`s.allocCount > s.nelems`), govulncheck (`fatal error: fault`), golangci-lint (impossible SSA panics), plus one compiler panic. The kernel logged a segfault in that same Rust build. Each gate passed on rerun. Worth a memory test of the host.)*
