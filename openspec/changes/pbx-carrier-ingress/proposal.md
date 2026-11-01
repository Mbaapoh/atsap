## Why

Two tenants that buy from the same carrier receive its calls from the same signalling addresses. Today each trunk projects its own identify row, and the engine attributes every request from an address to whichever row it finds first. One tenant's calls can then reach another tenant's plan. That breaks INV-10 ("tenant isolation is absolute"), and it is the *High* pilot gap in `docs/PROGRESS.md` (inbound G3, widened by D-76 to overlapping ranges).

- **Asterisk cannot express the fix.** On 2026-11-01 the rig confirmed that Asterisk 22's identify can match by address, header or Request-URI, but when several criteria are set it matches on *any*, not all. "This carrier address **and** this tenant's number" cannot be written as engine configuration.
- **The owner chose platform routing by number** (2026-11-01). That is how SBC-fronted platforms (Kamailio or OpenSIPS in front of Asterisk, carrier-grade hosted PBXs) attribute shared carrier traffic.

## What Changes

- **One carrier ingress.** The engine gets one platform-owned endpoint for all carrier traffic, `carrier-ingress`. Its identify row recognises every carrier address and range of every trunk, and it carries no tenant.
  - Trunks keep their own endpoints, used **outbound** only: contact, credentials and From domain.
  - Per-trunk identify rows go away.
- **The platform attributes each carrier call** from two facts. Neither can be set by a header:
  - **the packet's source address**, read from the transport (`CHANNEL(pjsip,remote_addr)`);
  - **the dialled number** (the Request-URI user).

  It finds the trunks whose carrier addresses contain the source address, across tenants, and among their tenants the one that owns the dialled number. **Exactly one** match routes the call as that tenant's trunk, as today: DID lookup, channel reservation and health watcher. **None or several** refuse it `404`, and an ambiguity is logged for the operator, without naming either tenant to the carrier.
- **Unchanged:** registered phones are still identified by username, and their tenant comes from their endpoint, as today.
- **Attribution is read through one narrow privileged lookup** (a `SECURITY DEFINER` function). It returns only the trunk and tenant for an (address, number) pair. Nothing else crosses tenants.
- **No API or console change.** Partners configure trunks and numbers exactly as before. Shared addresses simply work.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `pbx-core/inbound-routing`, *A call a platform-owned endpoint presents is attributed to its tenant by configuration*: a carrier call is attributed by its transport source address together with the tenant's configured numbers; ambiguity is refused.
- `pbx-core/trunk-routing`, *Inbound signalling is accepted only from a trunk's carrier address*: two trunks, in two tenants, on one carrier address each receive their own numbers' calls.

## Impact

- **API (Go):**
  - `internal/pbx/acl/asterisk`: the ingress endpoint and identify, the trunk projection without identify, reconcile;
  - `internal/pbx/application`: attribution in `ResolveInbound`;
  - `internal/pbx/postgres`: the lookup function;
  - `internal/telephony`: the arriving-call path reads the source address and, for the ingress, asks pbx-core for the tenant **before** recording the call;
  - `internal/telephony/acl`: reads `CHANNEL(pjsip,remote_addr)`.
- **Schema:** migration 0015, a `SECURITY DEFINER` function `carrier_ingress_match(source inet, number text)` returning at most the matching (tenant, trunk) pairs, executable by the app role only.
- **Engine:** one new realtime endpoint, AOR and identify row (`carrier-ingress`). Per-trunk `ps_endpoint_id_ips` rows are removed on upgrade, and reconcile reports any left.
- **Decisions:** D-77 (carrier traffic enters through one ingress and is attributed by the platform). It amends D-75 and D-76: addresses are recognised by the ingress, no longer per trunk.
- **Docs:** LLD-03 §4.2, §7.5–7.6 and §11.1 (sequence); PROGRESS closes the High gap.
- **Tests:**
  - unit: attribution rules;
  - integration: the lookup function, the projection, reconcile and cross-tenant isolation;
  - e2e: two tenants' trunks on one container carrier, each receiving its own number's call; an ambiguous number refused; a header naming the other tenant ignored;
  - UAT: `uat-inbound`, as regression.
- **Dependencies:** none.
