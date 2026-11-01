## 1. Prove it and decide it

- [ ] 1.1 Reproduce the stall on the rig as a failing Go e2e test (`api/internal/pbx/e2e`). Tenant A has a trunk whose host is unresolvable (`locslhodt`); a call from tenant B (extension to extension, and carrier to DID) must reach its first ring within 1 s. Verify it fails today with roughly 5 s. In the same session, measure whether **placing** a call over the unresolvable trunk delays tenant B's calls, and record the numbers here. If it does, add a *High* gap to the design's Non-Goals and to PROGRESS, without widening this change.
- [ ] 1.2 Record D-75 in `docs/DECISIONS.md`: the platform resolves carrier names for identification; identify holds addresses only; the last good addresses are kept; unresolvable hosts are accepted and flagged. Cite the alternatives from design D1. Verify `make docs` passes.

## 2. Domain and probe

- [ ] 2.1 Add `ResolvedAddresses` and `ResolvedAt` to `domain.Trunk`, and the pure rule `ObserveAddresses`, plus `IdentifyMatch(addresses)` (the joined literals, or none), per design D2. Unit tests cover:
  - an IP host, IPv4 and IPv6;
  - a name resolved to a new set (sorted, deduplicated, replacing the old one);
  - a failed name keeping its current set;
  - an empty set giving no match.

  Verify `make test` passes, with `internal/pbx/domain` at 100%.
- [ ] 2.2 Make `sipprobe.Prober` return every resolved address in `Result.Addresses`, alongside the one it probes (still IPv4 first). Unit tests use a fake `Resolver`: a mixed A/AAAA answer, duplicates, and a failure giving no addresses with `HostNotFound`. Verify `make test` passes.

## 3. Store, projection, check

- [ ] 3.1 Migration 0013: `resolved_addresses inet[] NOT NULL DEFAULT '{}'` and `resolved_at timestamptz` on `trunks`, with IP hosts backfilled to `{host}`, and a down migration. The store reads and writes the columns, and `UpdateTrunk` clears them when the host changes. Integration tests on `atsapbx_test` cover the backfill, the round trip and the clear on host change. Verify `make test` with the integration tag passes, and that down and up runs cleanly.
- [ ] 3.2 `ProjectTrunk` writes identify from the addresses only, deleting the row when there are none. `DiffTrunk` compares with `IdentifyMatch(t.ResolvedAddresses)`. Integration tests:
  - an IP trunk is projected at once;
  - a name trunk with no addresses has no identify row and never a name;
  - a `match` hand-edited to a name is reported as drift, and repair restores the addresses.

  Verify the reconcile integration tests pass.
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
