# Coverage — pbx-carrier-addresses

**Tiers:**
- **U**: unit (`pbx/domain`, `pbx/acl/sipprobe`, `pbx/rpc`, `internal/config`; console `carrierAddress.test.ts`, `carrierAddressDialog.test.tsx`).
- **I**: integration on `atsapbx_test` (`pbx/postgres`, `pbx/acl/asterisk`, `pbx/application`, `internal/postgres` migration 0014).
- **E**: e2e on the rig (`internal/pbx/e2e/carrier_addresses_test.go`; console `trunk-health.spec.ts` mocked, `telephony.rig.spec.ts` on the real API).
- **A**: UAT (the three phone UATs and the console A3–A5 UAT, as regression; `docs/uat/trunk-host-resolution.md` in the browser).

## pbx-core/trunk-routing

### A trunk is a tenant-scoped record created through the public API

| Scenario | Proven by |
|---|---|
| Trunk input is validated before it is stored (inbound: unparseable, too broad, more than 20) | **U** `TestParseInboundAddresses`, `TestValidateInboundAddresses` (the entry's position in the error). **I** `TestCreateTrunk_RefusesInvalidInputAndWritesNothing` (five new cases). **E** `TestCarrierAddresses_ANonCarrierHostIsRefused` (`10.0.0.0/8`). Console `checkInbound` names the line |
| A host that cannot be a carrier is refused | **U** `TestValidateHost_RefusesWhatCannotBeACarrier` (every spec example). **E** `TestCarrierAddresses_ANonCarrierHostIsRefused` (`localhost`, `127.0.0.1`, `::1`, `pbx.localhost`: 400 `invalid_argument`, "trunk host cannot be a carrier", nothing created). Console: refused at the field, nothing sent (component and rig) |
| A carrier on a private network is accepted | **U** `TestCarrierAddress`, `TestValidateInboundAddresses`. **I** `TestTrunkInboundAddresses_AreConfiguration` (host `10.20.0.5`, range `192.168.10.0/24`) |
| Inbound signalling addresses are configuration | **I** `TestTrunkInboundAddresses_AreConfiguration`, `TestTrunkStore_InboundAddressesRoundTripAndAreReplaced`. **U** wire `TestTrunkInboundAddresses_OnTheWire`. Console: an edit starts from and replaces the list; clearing sends none |

### Inbound signalling is accepted only from a trunk's carrier address

| Scenario | Proven by |
|---|---|
| A carrier is recognised from its published signalling ranges | **E** `TestCarrierAddresses_RecognisedFromAPublishedRange`: a container carrier gets 401 with no range and 404 (routed by the tenant's plan) once its address is a range of a trunk whose host never resolves. **I** `TestTrunkInboundAddresses_AreProjectedAtOnce`, `TestProjectTrunk_IdentifyIsTheUnionOfAddressesAndRanges` |
| A removed inbound address stops being recognised | **E** the same test: 401 again after removal, no identify row. **I** `TestTrunkInboundAddresses_AreProjectedAtOnce` |
| Earlier scenarios (D-75) | Unchanged; the `TestHostResolution_*` e2e tests and the phone UATs run as regression |

### Trunk health is checked, never set

| Scenario | Proven by |
|---|---|
| A name that resolves only to non-routable addresses is not a carrier | **E** `TestCarrierAddresses_ANameResolvingToLoopbackIsNotChecked` (`ip6-localhost`, Docker's `::1`: Unhealthy · Address not allowed, no identify row). **U** `TestProbe_NeverChecksAnAddressThatCannotBeACarrier` (a loopback responder receives zero requests), `TestProbe_KeepsOnlyCarrierAddresses`. **I** `TestTrunkProber_AddressNotAllowedKeepsOnlyTheRanges` |

**Migration 0014:** **I** `TestTrunkInboundMigration_StripsNonCarrierAddresses`. Loopback and link-local addresses are stripped from `resolved_addresses` and carrier addresses kept; the new reason is accepted; down maps it to `NO_RESPONSE`; up again.

**Regression found while testing:** `SIP_PUBLIC_HOST=localhost` (the rig's registrar address) was refused, and the app would not start. The registrar is now checked for syntax only. Proven by **U** `TestLoad_SIPPublicRegistrarMayBeLocal`.

## Gaps

| # | Gap | Severity | Note |
|---|---|---|---|
| G1 | Ranges overlapping another trunk's (another tenant's) are not detected | High, pilot | The shared-carrier-address change; refusing one would reveal another tenant's carrier (INV-10) |
| G2 | The console cannot show the server's reason for a refusal; it mirrors the rules instead | Accepted | Console rule 10: no raw server message on screen |
| G3 | Existing trunks on `localhost` stay stored, reading "Address not allowed"; an edit must fix the host | Accepted | By design: they never worked |
