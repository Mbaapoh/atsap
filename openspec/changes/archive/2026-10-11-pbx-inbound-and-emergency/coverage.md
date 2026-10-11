# Coverage — pbx-inbound-and-emergency

Every scenario in the five delta specs, mapped to what proves it. **Tiers:** U = unit, I = integration (real Postgres), W = wire (handlers over the real service), E = e2e (running licensed app, public API, live Asterisk, scripted SIP agents), A = UAT (`make uat-inbound`, real baresip phones, RTP measured).

## pbx-core/inbound-routing

### A call to a tenant's number rings the extension it is assigned to

| Scenario | Proven by |
|---|---|
| A carrier call to an assigned number rings the extension | **A** carrier → DID → phone: phone tx 298 / rx 299, carrier 298/299, errors 0. **E** `TestInbound_CarrierCallToAssignedDIDRingsExtension` (Active). **I** `TestRouter_TrunkCallToAssignedDIDRingsExtension` |
| An unassigned number is refused as unallocated | **E** same test: an unassigned `+44…` DID gets **404**. **I** `TestRouter_UnassignedDIDAndUnknownSourceAreNotFound` |
| A number cannot point at another tenant's extension | **I** `TestInboundNumbers_ValidationAndIsolation`. Schema: `TestPbxSchema_InboundNumberCannotReferenceAnotherTenantsExtension` (23503) |
| Inbound numbers are managed through the API | **I** `TestInboundNumbers_CRUD`, `TestDeleteExtension_CascadesInboundNumbers`. **W** task 4.1 wire tests |

### A call dialled from a desk phone follows the tenant's dial plan

| Scenario | Proven by |
|---|---|
| A desk phone dials an external number | **A** phone → trunk → carrier: phone 298/299, carrier 300/298. **E** `TestDeskPhone_DialsOutInternalUnroutableAndEmergency`. **I** `TestRouter_DeskPhoneDialPlan` |
| A desk phone dials another extension | **E** same test (internal, Active). **I** `TestRouter_DeskPhoneDialPlan` |
| A desk phone dials an unroutable number | **E** same test (**404**). **I** `TestRouter_DeskPhoneDialPlan` |
| A full trunk is passed over for a desk-phone call | **I** `TestRouter_FullTrunkIsPassedOver`, `TestRouter_AllTrunksFullIsUnavailable`. **Not live** — G4 |

### A refused arriving call is signalled, never left silent

| Scenario | Proven by |
|---|---|
| Capacity exhaustion is service unavailable | **E** `TestInbound_CapacityExhaustedIs503` (**503**, `Reason: Q.850;cause=34`). **E** desk-phone 911 under exhaustion (503). **U** `TestCallArrived_ScreeningRefusals`. No `Retry-After` — G1 |
| No arriving call is left silent | **U** `TestCallArrived_Refusals` (every cause rejects the channel with the mapped response; no other leg), `TestCallArrived_RouterErrorRefuses`, `TestCallArrived_NoTenantVariableRefusesWithoutRouter`. **E** every refusal above receives a final response |

### A call a platform-owned endpoint presents is attributed to its tenant by configuration

| Scenario | Proven by |
|---|---|
| Caller-supplied headers cannot change the tenant | **I** `TestProjectExtension_WritesTenantVariable`, `TestProjectTrunk_WritesTenantVariableAndIdentify` (tenant set by endpoint configuration). **U** `TestEventLoop_UncorrelatedStasisStart_RoutesToCallArrived` (tenant read from the channel variable, never from the request) |
| Signalling from an unknown source is refused | **E** a deleted trunk's carrier is challenged (**401**) and never routed. **U** `TestCallArrived_NoTenantVariableRefusesWithoutRouter`. **I** unknown source → NotFound |

## pbx-core/emergency-numbers

| Scenario | Proven by |
|---|---|
| A country's own numbers are emergency | **I** `TestEmergencyProfile_CountryCatalogueAndDefaults` (CM: 112 + 113/117/118/119). **U** `TestCountryCatalogue`, `TestEffectiveEmergencySet` |
| Another country's numbers are ordinary | **E** CM tenant: **911 refused 503** under exhaustion (routable, so capacity is what refuses it). **I** `TestEmergencyProfile_CountryCatalogueAndDefaults`, `TestRouter_NearEmergencyIsOrdinary` |
| Changing the country takes effect on the next call | **I** `TestEmergencyProfile_CountryCatalogueAndDefaults`, `TestPlaceCall_UsesEffectiveEmergencySet` |
| A country outside the catalogue still has a floor | **I** `TestEmergencyProfile_CountryCatalogueAndDefaults` (112 + additions; "not in catalogue" flag) |
| No country set is safe, not empty | **I** `TestEmergencyProfile_CountryCatalogueAndDefaults` (112/911/999; "no country set") |
| A tenant adds a local number the catalogue lacks | **I** `TestEmergencyNumbers_AdditionsAddAndRemove`, `TestPlaceCall_UsesEffectiveEmergencySet` |
| A built-in number cannot be deleted | **I** `TestEmergencyNumbers_BuiltInCannotBeDeleted`. **W** FAILED_PRECONDITION |
| A tenant's own number can be removed | **I** `TestEmergencyNumbers_AdditionsAddAndRemove` |
| Another tenant's numbers do not apply | **I** `TestEmergencyProfile_OtherTenantUnaffected` |
| A desk phone reaches a national emergency number with capacity exhausted | **E** CM tenant dials **117** with licensed capacity really exhausted by ringing calls: Active. **I** `TestRouter_EmergencyIgnoresTheLimit`. **U** `TestCallArrived_BypassSkipsScreeningUnderExhaustion` |
| A near-emergency number from a desk phone is ordinary | **I** `TestRouter_NearEmergencyIsOrdinary`. **U** `TestIsEmergency` |

## pbx-core/trunk-routing (delta)

| Scenario | Proven by |
|---|---|
| A carrier's call is recognised as its trunk | **A** the carrier authenticates with nothing, so it is recognised by its address alone. **E** inbound tests. **I** `TestProjectTrunk_WritesTenantVariableAndIdentify` |
| A deleted trunk's carrier is no longer recognised | **E** deleted trunk's carrier gets **401**. **I** `TestRemoveTrunk_RemovesIdentifyRow` |

## pbx-core/endpoint-projection (delta)

| Scenario | Proven by |
|---|---|
| Stored state contains no plaintext secret / auth works / wrong secret refused / trunk secret engine-readable only | Unchanged; the existing projector and e2e tests pass (`TestG1_*`, `TestProjectTrunk_SecretAppearsNowhereElse`) |
| The engine still cannot read tenant data | `TestPbxSchema_EngineRoleReadsOnlyTheProjection`: reads `ps_endpoint_id_ips`; 42501 on `inbound_numbers`, `emergency_numbers`, `emergency_settings` and every earlier domain table |

## telephony-core/call-lifecycle (delta)

| Scenario | Proven by |
|---|---|
| An arriving call that resolves to a destination connects | **U** `TestCallArrived_ConnectsToActive` (ring, originate, answer caller on far-end answer, bridge). **E** and **A** inbound and desk-phone calls |
| An arriving call the router cannot place is refused at the edge | **U** `TestCallArrived_Refusals`. **E** 404s |
| Screening refuses an arriving call with service unavailable | **U** `TestCallArrived_ScreeningRefusals`. **E** `TestInbound_CapacityExhaustedIs503` |
| The arriving caller hangs up before an answer | **U** `TestCallArrived_CallerHangupWhileRingingReleasesDestination` |
| Telephony runs with no router | **U** `TestCallArrived_DefaultRouterRefusesNotFound`. **E** telephony suites (walking skeleton, licence transitions, emergency) pass unmodified with no pbx in their composition (DoD 15) |

**Defects found and fixed on the way** (each regression-tested):
- Ending an established call: it used to end only when the LAST participant left, leaving the other party on a dead bridge accruing usage. It now ends when fewer than two remain connected, and the remaining leg is released. Proven by `TestParticipantLeft_LastConnectedTerminates`, `TestParticipantLeft_DestinationLeavingReleasesArrivedCaller`, `TestParticipantLeft_ArrivedCallerLeavingReleasesDestination`, and in E/A, "one hang-up released the other".
- `HangupCall` and arrived callers: it missed an arrived caller's real channel. Proven by `TestHangupCall_ArrivedReleasesTheCallersOwnChannel` (fault-injected).
- `+DID` dialplan, identify order and destination persistence (task 6.1 note).

## Gaps (G-series)

- **G1 — No `Retry-After` on 503.** Asterisk 22.11.0 cannot add a header to the final response of an inbound INVITE (task 1.1 spike). A plain 503 ships, with `Reason: Q.850;cause=34`.
- **G2 — Emergency catalogue is CM and US only, by owner decision.** It ships Cameroon (113/117/118/119) and the US (911). Every other country is tenant-entered via the profile RPCs. A new country is a data-only addition with a verified source per number.
- **G3 — Two trunks on one carrier address are indistinguishable.** Identify-by-IP cannot attribute a call when two trunks, in the same or different tenants, share a signalling address, and many tenants bringing the same carrier is common in BYOT. Seen live: a stale trunk from a killed UAT run captured the next run's calls. A resolution (refuse a duplicate host, or match on Request-URI/DID across tenants) is a design decision for a follow-up.
- **G4 — No live test of a full trunk being passed over** for a desk-phone call. It needs a second trunk on a second carrier address, and this host's firewall admits one. Proven at integration.
- **G5 — Static fixtures 1000/1001 cannot dial.** They carry no tenant variable and are refused 404 by design (design D1 risk); the walking skeleton only originates.
- **G6 — ERROR log noise after hang-up** (`participant-left handler failed: call not found`) is unchanged; it is the previous change's G3.
