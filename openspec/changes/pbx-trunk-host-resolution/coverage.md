# Coverage — pbx-trunk-host-resolution

**Tiers:**
- **U**: unit (`pbx/domain`, `pbx/acl/sipprobe`).
- **I**: integration (`pbx/postgres`, `pbx/acl/asterisk`, `pbx/application`, `internal/postgres` migration; real Postgres on `atsapbx_test`).
- **E**: e2e (`internal/pbx/e2e/host_resolution_test.go`, the running app and live Asterisk).
- **A**: UAT (`uat-auto`, `uat-placement`, `uat-inbound`, real baresip phones), run as a regression.

## pbx-core/trunk-routing

### Inbound signalling is accepted only from a trunk's carrier address

| Scenario | Proven by |
|---|---|
| A carrier's call is recognised as its trunk | **E** `TestInbound_CarrierCallToAssignedDIDRingsExtension` (an IP carrier). **A** `uat-inbound`. **I** `TestProjectTrunk_WritesTenantVariableAndIdentify` (the addresses, never the name) |
| A carrier configured by name is recognised once resolved | **E** `TestHostResolution_ACarrierConfiguredByNameIsRecognised`: a container carrier reached by DNS name gets 401 before, then 404 for an unassigned DID once the first check has resolved it. **I** `TestTrunkProber_RecordsWhatTheNameResolvedTo`. **U** `TestProbe_ReturnsEveryResolvedAddress`, `TestObserveAddresses` |
| An unresolvable host stalls no one | **E** `TestHostResolution_AnUnresolvableHostIsNotLookedUpWhileIdentifying`: zero engine lookups for another tenant's carrier INVITEs (failed against the previous version: twelve lookups for three INVITEs). **E** `TestHostResolution_DiallingAnUnresolvableTrunkDelaysNoOneElse`. **I** `TestTrunkProber_ANameIsNeverLeftForTheEngine` |
| A brief resolution failure keeps a carrier recognised | **I** `TestTrunkProber_RecordsWhatTheNameResolvedTo` (failure keeps the set, health says so). **U** `TestObserveAddresses` |
| A changed host stops recognising the old carrier | **E** the by-name test: moving the host to `192.0.2.1` makes the carrier get 401 again. **I** `TestTrunkStore_AddressesFollowTheHost`, `TestTrunkProber_AStaleCheckRecordsNoAddresses` |
| A deleted trunk's carrier is no longer recognised | **E** `TestInbound_CarrierCallToAssignedDIDRingsExtension` (unchanged). **I** `TestRemoveTrunk_RemovesIdentifyRow` |

## pbx-core/endpoint-projection

### Divergence between configuration and engine state is detectable and repairable

| Scenario | Proven by |
|---|---|
| A trunk's recognised addresses are compared with its recorded addresses | **I** `TestDiffTrunk_IdentifyIsComparedWithTheRecordedAddresses` (the row tampered to the name, a subset and a superset, each repaired; a row a trunk with no address should not have). **E** the by-name test runs `pbx reconcile` and finds nothing for the trunk |
| Divergence is reported, repair restores agreement, no silent repair | Unchanged; the existing reconcile integration tests pass |

**Migration 0013:** **I** `TestTrunkAddressesMigration_IPHostsAreBackfilled`. IPv4 and IPv6 hosts are backfilled; a name, `1.2.3` (a name, not an address) and `locslhodt` are left empty; down then up round-trips.

## Gaps

| # | Gap | Severity | Note |
|---|---|---|---|
| G1 | `uat-inbound`'s carrier is configured by IP, not by name | Low | That carrier is baresip on the host, and no name resolves to the host's bridge address from the app's network. The by-name proof is e2e, with a real container carrier |
| G2 | A carrier that moves to a new address is refused for up to one check interval (30 s) | Accepted | D-75 trade-off. The interval is configurable |
| G3 | Names are resolved on the app's network, not the engine's | Accepted | As for the D-74 probe; both use Docker's DNS in the deployed compose |
