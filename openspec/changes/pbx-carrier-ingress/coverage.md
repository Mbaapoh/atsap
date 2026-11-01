# Coverage — pbx-carrier-ingress

**Tiers:**
- **U**: unit (`pbx/domain` ingress rules, `pbx/application` attributor, `telephony/acl` event loop, `telephony/application` arriving-call path).
- **I**: integration on `atsapbx_test` (`internal/postgres` lookup function, `pbx/postgres` store across tenants, `pbx/acl/asterisk` ingress projection, `pbx/application` reconcile and prober).
- **E**: e2e on the rig (`ingress_spike_test.go`, `carrier_ingress_test.go`, plus every D-75/D-76 e2e rewritten to the ingress).
- **A**: the three phone UATs and the console UAT, as regression.

## pbx-core/inbound-routing

### A call a platform-owned endpoint presents is attributed to its tenant by configuration

| Scenario | Proven by |
|---|---|
| Caller-supplied headers cannot change the tenant | **E** `TestCarrierIngress_TwoTenantsOnOneCarrierEachReceiveTheirOwnNumbers`: B's number dialled with `X-Tenant: <A>` is B's call, never A's. **U** the attributor takes only the transport address (`CHANNEL(pjsip,remote_addr)`) and the number |
| Signalling from an unknown source is refused | **E** container carriers get 401 before any trunk recognises them (`TestCarrierAddresses_*`, `TestIngressSpike_*`) |
| Two tenants on one carrier each receive their own numbers | **E** the two-tenant test: each number is a call in its own tenant only. **I** `TestIngressStore_AttributesAcrossTenants`, `TestCarrierIngressMatch` |
| An ambiguous number is refused, not guessed | **E** the same test: 404, no call in either tenant, the operator log says "configured in several tenants" with no tenant id. **U** `TestIngressAttributor` (several: refused and logged with trunk ids). **I** `TestCarrierIngressMatch` (both rows returned for the caller to refuse) |
| A number of a tenant whose trunk does not recognise the source is not reached | **I** `TestCarrierIngressMatch` (C's number from A's carrier: none). **E** an unassigned number: 404 |

Telephony attributes before recording: **U** `TestCallArrived_TheCarrierIngressIsAttributedFirst` (attributed: recorded under the tenant and routed as `t_…`; refused, not ingress, or a failed lookup: rejected, nothing recorded, never routed) and `TestEventLoop_ATenantlessChannelCarriesItsSourceAddress`.

## pbx-core/trunk-routing

### Inbound signalling is accepted only from a trunk's carrier address

| Scenario | Proven by |
|---|---|
| Two trunks on one carrier address both receive calls | **E** the two-tenant test. **I** `TestProjectIngress_IsTheUnionAcrossTenants` |
| Earlier scenarios (D-75, D-76) | **E** `TestHostResolution_*` and `TestCarrierAddresses_*`, rewritten to read the ingress, pass. **I** the prober and trunk tests, rewritten likewise |

**The ingress:** **I** `TestProjectTrunk_RecognitionIsTheIngress`, `TestProjectIngress_LeftoverTrunkRowsAreRemoved`, `TestRemoveTrunk_StopsRecognisingItsCarrier` and `TestReconcile_CarrierIngressDriftIsReportedAndRepaired` (a hand-widened `0.0.0.0/0` is drift; repair restores it).

**Spike (D-25):** **E** `TestIngressSpike_*`: one tenantless endpoint takes a carrier's INVITE into Stasis; ARI reads `remote_addr` as the carrier's address and port; 500 ranges add no measurable time.

**Found and fixed on the way:**
- `ps_endpoint_id_ips.match` was `VARCHAR(80)` (migration 0015 makes it `TEXT`); a D-76 trunk with several ranges failed to create. Test: `TestTrunkInboundAddresses_TwentyLongRangesAreProjected`.
- An arriving call whose destination could not be reached was left ringing. It is now refused 480 (`RefusalUnreachable`). Tests: `TestCallArrived_AnUnreachableDestinationIsRefusedNotLeftRinging` and the gateway mapping test.

## Gaps

| # | Gap | Severity | Note |
|---|---|---|---|
| G1 | A number configured in two tenants that share a carrier rings in neither until the operator resolves it | Accepted | By design (D-77); logged with trunk ids |
| G2 | Behind NAT or a proxy, `remote_addr` is the proxy's address | Accepted | As identify already requires: the platform must see carriers' real addresses |
| G3 | The operator log is the only signal of an ambiguous number; no alert | Low | Folds into the BR-08 alerting work |
