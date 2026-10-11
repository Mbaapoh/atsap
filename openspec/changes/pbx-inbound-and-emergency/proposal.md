## Why

Any call the platform did not originate itself is silently dropped. When a channel enters Stasis without a correlation, `telephony-core`'s event loop logs "StasisStart for unregistered channel" and returns, and the caller sits in silence. That covers both halves of a phone system that `PlaceCall` does not:

- **Carrier inbound:** a customer calls a DID and nothing rings.
- **A registered desk phone dialling**, flow A5 (HLD-18): a user picks up a phone and dials.

The second includes **a desk phone dialling 112**, the case INV-01 exists for. Today it gets silence. This is the fourth and last Phase A slice of LLD-03 §12, and the only one that edits `telephony-core`, so it is isolated on purpose.

## What Changes

- **`telephony-core` gains the `InboundRouter` port** (LLD-03 §4.2) and handles a StasisStart it did not originate. It creates the Call with the arriving channel as its first participant, asks the router what the call is, and then:
  - screens it, unless the router says bypass;
  - originates the destination leg, rings the caller (180), answers the caller when the far end answers, and bridges;
  - or refuses it at the SIP edge.

  A default `NoInboundRouter` ships with the port, so `telephony-core` runs with no `pbx-core` in the binary (LLD-03 DoD 15).
- **Refusals are standard SIP responses**, never silence (PRD §12.4):
  - an unallocated DID gets **404**;
  - an unroutable dialled number gets **404**;
  - capacity or a full trunk gets **503**, with **`Retry-After`** if the engine allows the header (a spike decides; the fallback is a plain 503, recorded as a gap);
  - a compliance refusal gets **403**.
- **`pbx-core` implements the router.**
  - A call from a **trunk** resolves the DID through a new per-tenant **inbound numbers** table (DID → extension), with `CreateInboundNumber`/`ListInboundNumbers`/`DeleteInboundNumber` RPCs.
  - A call from an **extension** runs the same dial plan `PlaceCall` uses: emergency first, then internal, then routes, with the trunk's channel limit reserved and the health reporter attached.
- **Whose call it is comes from the engine, not a cross-tenant lookup.** Every projected endpoint gains `set_var=ATSA_TENANT_ID=<tenant>`, so an arriving channel carries its tenant. The variable is set by endpoint configuration, so no SIP header can spoof it. pbx never needs an RLS-bypassing read.
- **Carriers are recognised by address.** Trunk projection writes an identify-by-IP row (`ps_endpoint_id_ips`) from the trunk's host. That is one new engine-read table and grant, a deliberate widening approved for this change. Signalling from any other source stays anonymous and refused (AC-08.4).
- **Emergency numbers by country** (the platform is not US-only). Each tenant sets an emergency country. Its emergency set is 112 (always), plus the main national numbers a built-in, source-verified catalogue lists for that country, plus numbers the tenant adds. The catalogue covers Cameroon and Central Africa, wider Africa, Europe and the UK, North America, and selected Asian markets, and is deliberately not exhaustive: tenants elsewhere enter their numbers when configuring the platform. 112 and catalogue numbers cannot be deleted, while the tenant's own additions can. With no country set the default is 112/911/999. The effective set replaces the built-in-only set at every origination path (API, desk phone).
- **Proof through every tier:**
  - unit and integration tests;
  - e2e through the running licensed app against live Asterisk: a scripted carrier sends INVITEs to a DID, and a scripted phone dials out, dials 112 under exhausted capacity, and dials internally;
  - a new `make uat-inbound`: a real baresip carrier calls a DID that rings a real baresip phone, and the phone dials out over the trunk, both with RTP asserted in both directions. `uat-auto` and `uat-placement` must stay green.
- **Out of scope:** IVR/auto-attendant/business hours (Phase B), and outbound-class toll fraud (its own change). Also SIP cause-code capture beyond the refusals this slice issues, and the ERROR-log noise after API hang-up (G3, a separate follow-up).

## Capabilities

### New Capabilities

- `pbx-core/inbound-routing`: calls arriving at the engine. It covers DID-to-extension resolution for carrier calls, the dial plan for desk-phone calls, and the standard refusals.
- `pbx-core/emergency-numbers`: the tenant's emergency set by country — 112 everywhere, the country's catalogue numbers, the tenant's own additions — and its management through the API.

### Modified Capabilities

- `telephony-core/call-lifecycle`: a call can begin with an arriving channel rather than two originations. Refusals of an arriving call are signalled at the SIP edge.
- `pbx-core/trunk-routing`: inbound signalling is accepted only from a trunk's carrier address.
- `pbx-core/endpoint-projection`: the credentials requirement no longer freezes the engine to the tables it held before. Engine read access stays confined to engine-facing state.

## Impact

- **New:**
  - in `telephony/ports`: `InboundRouter`, `InboundDecision` and `NoInboundRouter`;
  - in `telephony/application`: the arriving-call path;
  - in `telephony/acl`: unregistered-StasisStart dispatch, and channel ring/answer/reject in `MediaGateway`;
  - in pbx: the inbound router plus the inbound-numbers and emergency-numbers stores, services and RPCs;
  - migration `0009` (incl. `emergency_settings`), the country catalogue with sources, the inbound and emergency e2e, `scripts/uat-inbound.sh`, and `docs/API.md` rows.
- **Changed:** the trunk and extension projection (`set_var` on both, an identify row for trunks), `PlaceCall`'s emergency set, and `cmd/atsap-api` wiring. Plus the `asterisk_engine` grant: `SELECT` on `ps_endpoint_id_ips`, and the `set_var` column.
- **Not touched:** `internal/licensing/` and `internal/identity/`. Licensing is reached only through telephony's existing Screening.
- **Risk:** this is the change a reviewer must look at hardest (LLD-03 §12). Every earlier telephony behaviour must still pass. The walking-skeleton and licence-transition e2e suites run unchanged against the default router.
