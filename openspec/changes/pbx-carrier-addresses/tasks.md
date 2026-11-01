## 1. Decide

- [x] 1.1 Record D-76 in `docs/DECISIONS.md`: a per-trunk list of inbound signalling addresses recognised alongside the host; the rule for what can be a carrier (private networks allowed); the "Address not allowed" reason. Cite design D1–D3 alternatives and BRD FBR-R1-03, VP-3 and OWASP A10. Verify `make docs` passes.

  *(done 2026-11-01: D-76 recorded with six rejected alternatives; `make docs` passes.)*

## 2. Domain and probe

- [x] 2.1 In `internal/pbx/domain`, add:
  - `CarrierAddress`;
  - `ValidateHost` refusing `localhost`, `*.localhost` and non-carrier literals;
  - `ValidateInboundAddresses` (parse, mask, at most 20, the /16 and /32 floors, non-carrier refused, the entry's position in the error);
  - `Trunk.InboundAddresses`;
  - `ObserveAddresses` and `IdentifyMatch` filtering non-carrier addresses;
  - `IdentifyMatch` taking the union of addresses and ranges;
  - the `ADDRESS_NOT_ALLOWED` reason with its probe mapping.

  Unit tests cover every spec example (`localhost`, `pbx.localhost`, `127.0.0.1`, `::1`, `169.254.1.1`, `0.0.0.0`, `224.0.0.1` refused; `10.20.0.5` and `192.168.10.0/24` accepted), the floors, 21 entries, and the union. Verify `make test` passes with `pbx/domain`'s new code at 100%.
- [x] 2.2 Make `sipprobe` drop non-carrier addresses from a lookup, and return `ProbeAddressNotAllowed` with no packet sent when none remain. Unit tests: a name resolving to `127.0.0.1` and `::1` only; mixed addresses, keeping only the carrier ones; an IP literal refused before sending. Verify `make test` passes.

  *(2.1 and 2.2 done 2026-11-01, one commit: the domain rule makes a loopback IP host give no address, so the probe must filter in the same step or it panics.
  - **Domain:**
    - `CarrierAddress`, `ValidateHost` (with `localhost`, `*.localhost` and a trailing dot refused), `ParseInboundAddresses` and `ValidateInboundAddresses`, `InboundStrings`;
    - `IdentifyMatch(addrs, ranges)` as a normalised union;
    - `ObserveAddresses` and `CarrierAddresses`, which drop non-carrier addresses wherever they are read;
    - `ReasonAddressNotAllowed` and `ProbeAddressNotAllowed`.

    Every new function is at 100%; the package is at 89.3% (`TrunkCredentialRef` was already uncovered).
  - **Probe:** gains `Permit` (nil means `domain.CarrierAddress`). An IP literal is no longer special-cased through the domain. A name or literal with no permitted address returns `ProbeAddressNotAllowed` with nothing sent: a responder on loopback receives zero requests.
  - **Tests:** the probe tests that use loopback responders widen `Permit`, with a comment saying so. The old test that accepted `localhost` as a host now uses `10.20.0.5`.
  - **Verified:** `make test` and `make lint` pass.)*

## 3. API, storage, projection

- [x] 3.1 Proto:
  - `Trunk.inbound_addresses`, `CreateTrunkRequest.inbound_addresses`, `UpdateTrunkRequest.inbound_addresses`;
  - `TRUNK_HEALTH_REASON_ADDRESS_NOT_ALLOWED`.

  Run `buf lint` and check for breaking changes (additive only). Regenerate Go and TypeScript clients.

  Migration 0014 adds `inbound_addresses CIDR[]` and strips non-carrier `resolved_addresses`, with a down migration. The store, RPC mapping and application create and update carry the list; errors name the field.

  Tests:
  - wire tests for mapping and refusals;
  - store and migration integration tests: round trip, update replaces, the strip, down and up;
  - an application integration test: create with ranges, update to none, `localhost` refused.

  Verify unit and integration tiers pass on `atsapbx_test`, and `make lint` is clean.

  *(done 2026-11-01.
  - **Proto:** three fields and the enum value, additive; `buf lint` and `buf breaking` against develop pass. The Go, TypeScript and OpenAPI outputs are regenerated.
  - **Migration 0014:**
    - adds `inbound_addresses CIDR[]`;
    - extends the `health_reason` check with `ADDRESS_NOT_ALLOWED`;
    - strips loopback, link-local, unspecified, "this network" and multicast addresses from `resolved_addresses`;
    - its down migration maps `ADDRESS_NOT_ALLOWED` to `NO_RESPONSE`.
  - **Application:** commands carry the lines as entered; the application parses them through the domain, so errors name the entry. Views return them in canonical form.
  - **Console:** the exhaustive health-copy map would not typecheck against the new client, so "Address not allowed" lands here.
  - **Tests:**
    - **Wire:** `TestTrunkInboundAddresses_OnTheWire`, `TestToProtoTrunk_AddressNotAllowed`.
    - **Store:** round trip and replace, and the new reason.
    - **Migration:** `TestTrunkInboundMigration_StripsNonCarrierAddresses`.
    - **Application:** five new refusals in `TestCreateTrunk_RefusesInvalidInputAndWritesNothing`, and `TestTrunkInboundAddresses_AreConfiguration`.
  - **Found and fixed:**
    - an empty list was written as NULL;
    - `TestProbe_RealPeers` reached the rig's Asterisk on host loopback; it now widens `Permit` for that peer and asserts the default refuses it.
  - **Verified:** unit tests, `make lint` 0, all 35 integration packages on `atsapbx_test`, console typecheck.)*
- [ ] 3.2 Identify is the union: `ProjectTrunk`, `ProjectTrunkIdentify` and `DiffTrunk` use `IdentifyMatch(resolved, inbound)`. The prober passes inbound ranges when it reprojects, and clears the host part for `AddressNotAllowed`.

  Integration tests:
  - ranges are projected at create;
  - removing one reprojects at once;
  - a name trunk with no addresses but ranges keeps a row of ranges only;
  - recorded loopback addresses are never projected;
  - reconcile reports and repairs each case.

  Verify the `pbx` integration tiers pass.

## 4. Console and live proof

- [ ] 4.1 Console:
  - the trunk dialog's *Inbound signalling addresses (optional)* field, one per line, with hint and server error at the field;
  - the trunks list shows the addresses;
  - "Address not allowed" health copy;
  - regenerated clients.

  Tests: component tests (create and edit send exactly the lines entered; blank lines dropped; refusal shown at the field), a mocked browser e2e, and a rig spec creating a trunk with a range. Verify the full `make console-check` passes and the floors hold.
- [ ] 4.2 Live, Go e2e on the rig:
  - a container carrier whose trunk host is a different name, recognised only through an inbound range (404 for an unassigned DID, 401 before the range is added and after it is removed);
  - `localhost` and `127.0.0.1` refused with the field named;
  - a name resolving to loopback reads `Unhealthy · Address not allowed`.

  Verify the full Go e2e tier, the three phone UATs and the console UAT pass after `make rig-restore`.

## 5. Docs and close

- [ ] 5.1 Update:
  - LLD-03 (§3 `Trunk`, §7.5 and §10.5, a document history row);
  - `docs/API.md`;
  - `docs/uat/trunk-host-resolution.md` (steps for the new field and the refusal, executed);
  - PROGRESS (FBR-R1-03 carriers with published ranges now connectable; the overlap gap kept for the next change);
  - `coverage.md`.

  Ratchet the floors. Verify `make docs`, `make quality` and `make console-check` pass.
