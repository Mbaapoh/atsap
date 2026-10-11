## Context

Today `telephony-core` only handles calls it originates: both legs are originated over ARI with a deterministic `atsa-part-<participant>` channel id and correlated in the registry. A channel arriving in Stasis by any other route — a carrier INVITE, or a registered phone dialling into the `stasis-in` context — reaches `handleStasisStart`, finds no correlation, is logged ("unregistered channel") and is ignored, leaving the caller in silence.

`pbx-core` already has the dial plan (`ResolveDestination`: emergency, then internal, then routes), trunk-channel reservation under a per-trunk lock (`placed_call_legs`), the health watcher, and the built-in emergency set.

On the engine side, `ps_endpoints` has no `set_var` column, and there is no `ps_endpoint_id_ips` table. Sorcery maps only endpoint, auth, aor and contact to realtime, although `res_pjsip_endpoint_identifier_ip` is loaded.

The ARI client already has `AnswerChannel` and `HangupChannel(reason)`. A hangup reason of `unallocated` yields 404, `congestion` 503 and `rejected` 403 on an unanswered inbound channel. Whether `Retry-After` can ride on that 503 is unproven.

## Goals / Non-Goals

**Goals:**

- Carrier → DID → extension, and desk phone → dial plan, both connect with media, proven at e2e and with real phones.
- Every refused arriving call gets a final SIP response, never silence.
- An emergency number reaches its route from every path, under exhausted capacity, with the floor no tenant can remove.
- `telephony-core` gains one port and one path, runs with no `pbx-core` (DoD 15), and every existing telephony test passes unchanged.

**Non-Goals:**

- IVR, business hours, queues (Phase B).
- Toll-fraud outbound class (its own change).
- Caller-ID presentation policy (AC-02.4).
- Quieting G3's log noise.

## Decisions

**D1 — The tenant arrives on the channel, set by endpoint configuration.**
Every projected endpoint gets `set_var=ATSA_TENANT_ID=<tenant uuid>`, for extensions and trunks alike. A channel created on that endpoint carries the variable, and the ACL reads it on StasisStart. Configuration sets it, so no SIP header can forge it. A channel without it (a static fixture such as 1000/1001, or anything unconfigured) has no tenant and is refused 404.
*Alternatives rejected:*
- Look up `e_`/`t_` across tenants: that needs an RLS-bypassing role, the exact widening D-24 forbids.
- A tenant in the dialled number: caller-controlled.

**D2 — `InboundRouter` port, owned by telephony, implemented by pbx.**
```go
// telephony/ports
type InboundRequest struct {
    TenantID       shareddomain.TenantID
    CallID         shareddomain.CallID
    SourceEndpoint string // opaque: the arriving channel's endpoint
    Dialled        string
    CallerNumber   string
}
type InboundDecision struct {
    Refuse          RefusalCause // "" to connect; NotFound | Unavailable | Forbidden
    DestinationURI  string       // opaque to telephony
    Direction       domain.Direction
    BypassScreening bool         // assigned only in pbx/application (archtest)
}
type InboundRouter interface { ResolveInbound(ctx, InboundRequest) (InboundDecision, error) }
```
This widens LLD-03 §4.2's `ResolveInbound(tenant, dialled)` with a source, so pbx can tell trunk from extension, and with a call id, so pbx can reserve a trunk channel keyed by it. LLD-03 is amended in the same change. `NoInboundRouter` returns `Refuse: NotFound`. The decision field keeps the name `BypassScreening`, so the existing provenance scanner covers the new assignment, which lives in pbx/application.

**D3 — The arriving-call path in telephony.**
On a StasisStart with no correlation:
1. The ACL reads `ATSA_TENANT_ID`, the endpoint name from the channel name, the dialled extension from `dialplan.exten`, and the caller number. It calls `Service.CallArrived(...)`.
2. The service creates the Call: caller participant = the arriving channel, correlated under its own channel id.
3. It calls the router.
4. A refusal: reject the channel with the mapped cause and terminate with the reason.
5. Otherwise Screening, unless bypassed. A capacity refusal gets 503 and a compliance refusal 403.
6. Then Presenting: ring the arriving channel (180), originate the destination leg exactly as today, and ensure the bridge.
7. When the destination answers, its StasisStart arrives with its correlation. The service answers the arriving channel and adds both to the bridge, and the call becomes Active by the existing two-connected rule.
8. Caller hang-up before the answer reaches the existing ParticipantLeft and terminates the call. `HangupCall`'s teardown releases the ringing leg.

`MediaGateway` gains `RingChannel`, `AnswerChannel` and `RejectChannel(ref, cause)`. The ARI client gains `RingChannel`.

**D4 — pbx's router reuses the dial plan.**
- *Source is a trunk (`t_`):* the dialled DID is looked up in `inbound_numbers` (tenant from D1, under RLS). An assigned DID connects internally to its extension (`Direction: Inbound`). An unassigned DID is refused NotFound.
- *Source is an extension (`e_`):* `ResolveDestination` with the tenant's emergency set, exactly as `PlaceCall`. For a trunk destination, the leg is reserved per trunk under the lock with the call id already known, the next candidate is tried when a trunk is full, and all-full is refused Unavailable. The health watcher is attached on connect.
- An emergency match sets `BypassScreening` and ignores the trunk limit.
- An unknown source (neither prefix) is refused NotFound.

Translating the source endpoint string to a trunk or extension identity happens in `pbx/acl/asterisk` (a new `EndpointAddresser.Identify`), not in pbx/application.

**D5 — Identify-by-IP for trunks.**
`ProjectTrunk` writes `ps_endpoint_id_ips (id=t_…, endpoint=t_…, match=<host>)`, and `RemoveTrunk` deletes it. `sorcery.conf` and `extconfig.conf` map `identify` to realtime. The `asterisk_engine` role gets `SELECT` on the new table, its first new grant since `0004`, approved for this change; the endpoint-projection delta records it. Asterisk resolves hostname `match` values itself.

**D6 — Emergency set by country: 112 ∪ catalogue[country] ∪ tenant additions.** *(Revised before apply: the platform is not US-only.)*
A fixed global floor of 112/911/999 is wrong in both directions outside North America and the UK. It misses national numbers: Cameroon's 117/118/119, France's 15/17/18, India's 100–108, Japan's 110/119. Until a tenant thinks to add them, such a call is *refused* under exhausted capacity, which is exactly INV-01's failure. And it forces 911 and 999, which can be ordinary short codes elsewhere, to bypass licence and compliance.

So, following FreePBX/3CX practice, emergency numbers are a **country profile**:
- `emergency_settings (tenant_id PK, country_code CHAR(2))`: the tenant's emergency country, set and read through the API and audited.
- A **built-in catalogue** in `pbx/domain` maps an ISO 3166-1 code to its main national emergency numbers. It is code, not configuration, so no deployment can empty it. It is deliberately **not exhaustive**. By owner decision it ships Cameroon (113, 117, 118, 119) and the United States (911) only, and anything else is entered by the tenant at configuration. Adding a country later is a data-only map entry. Each entry carries its source, and an entry whose number cannot be verified against the national regulator or the ITU-T emergency-number list does not ship. A wrong "emergency" number is a licence bypass, and a missing one is an INV-01 failure, so guessing is worse than leaving the tenant to enter it.
- `emergency_numbers (tenant_id, number)` holds only the tenant's **own** additions. They are freely added and removed.
- **Effective set** = {112} ∪ catalogue[country] ∪ additions. 112 is the floor everywhere, because it is the international GSM number and many networks route it even where it is not the local number. With no country set, the effective set is {112, 911, 999} ∪ additions, a safe default rather than an empty one, and the profile reports "no country set" so the console can prompt for it. A country outside the catalogue gives {112} ∪ additions, and the response says the tenant must enter its numbers.
- Deleting 112 or a catalogue number for the tenant's country is refused before storage. Additions are validated like destinations.
- `PlaceCall` and the desk-phone router both read the effective set per call (one indexed read).

*Rejected:*
- A fully editable list: one mistake makes a real emergency number refusable.
- Seeding rows per residency zone: residency is coarse ("EU"), lives in identity, and would need a cross-context hook at tenant creation.

**D7 — Inbound numbers.**
`inbound_numbers (id, tenant_id, number, extension_id, created_at)`, with `UNIQUE (tenant_id, number)` and a composite FK `(tenant_id, extension_id) → extensions(tenant_id, id)`. That needs a new `UNIQUE (tenant_id, id)` on `extensions`, the same cross-tenant guard as `carrier_routes`. Numbers are validated like destinations (`+?[0-9]{1,32}`) and matched with a leading `+` ignored on both sides. The RPCs are `CreateInboundNumber`, `ListInboundNumbers` and `DeleteInboundNumber`, authorized and audited like trunks. Deleting an extension cascades its inbound numbers in the same transaction, each audited, mirroring trunk/route deletion.

**D8 — `Retry-After` is a spike, with a recorded fallback.**
Task 1.x tests on the rig whether a header can be set on the 503 for an unanswered inbound channel (e.g. a `PJSIP_HEADER`-family function via ARI channel variable, or `PJSIP_RESPONSE_HEADERS`). If it can, `RejectChannel` sets `Retry-After: 30` for Unavailable. If not, a plain 503 ships and the gap is recorded. The spec says "where the engine allows it" for exactly this reason.

**Outcome (task 1.1, Asterisk 22.11.0):** the engine does not allow it. An authenticated INVITE was sent into Stasis from a projected extension. Setting `PJSIP_HEADER(add,Retry-After)` and `PJSIP_RESPONSE_HEADER(add,Retry-After)` on the inbound channel over ARI both returned 204, but neither reached the wire. The `congestion` hangup produced exactly `SIP/2.0 503 Service Unavailable` with `Reason: Q.850;cause=34` and no `Retry-After`. That matches the function documentation: `PJSIP_HEADER` writes only on outbound channels, and the response-header functions read only. So a plain 503 with the Q.850 reason ships, and the missing `Retry-After` is a recorded gap. Adding it would need a custom PJSIP module or an upstream change.

**D9 — Proving it at every tier.** The ports are fixed by the host firewall (5070/5071, RTP 18000–18039), as in `pbx-call-placement`.
- *Integration:* the router against real Postgres (DIDs, desk-phone dial plan, emergency set, limits). Telephony's arriving-call path against fake gateway and router (connect, every refusal cause, caller hang-up while ringing, bypass).
- *e2e*, against the running licensed app and live Asterisk. The scripted carrier on 5071 sends an INVITE from the trunk's host to a DID, and the scripted phone on 5070 rings and answers, reaching Active. Also:
  - an unassigned DID gets 404;
  - capacity exhausted gets 503;
  - a deleted trunk's carrier is refused;
  - the scripted phone dials out (the carrier answers), dials another extension, dials an unroutable number (404), and dials 112 with capacity exhausted (connects).
- *UAT* (`make uat-inbound`): real baresip carrier → DID → real baresip phone, RTP both ways. Then the real phone dials out over the trunk to the carrier, RTP both ways. Both are released by hang-up. `uat-auto` and `uat-placement` run back to back with it.

**D10 — A two-party call ends when either party leaves.** *(Added at apply time, user-approved.)*
The e2e found that an Active call ended only when its LAST connected participant left. When one party of a two-party call hung up, the other stayed connected to a dead bridge, in silence, with per-second usage still accruing, on every call path, inbound and outbound. `DisconnectParticipant` now moves an Active call to Terminating when fewer than two remain connected. `ParticipantLeft` records the remaining participants' usage, disconnects them, finalizes, and then releases their legs, using a `legRef` helper that resolves an arrived caller's real channel *before* finalizing. The same helper fixes `HangupCall`, which had resolved it after `removeCall` deleted the record and so missed an arrived caller entirely. Conferences, where a call can outlive a departure, are Phase B and revisit the rule.

## Risks / Trade-offs

- [This is the riskiest slice] → the arriving path is a new branch only reached for uncorrelated StasisStarts. The originated path is unchanged and its suites run unmodified, and DoD 15 runs telephony with `NoInboundRouter`.
- [Realtime identify scans] → Asterisk may query identify rows broadly for each unknown INVITE. Acceptable at Phase A trunk counts; a sorcery memory cache is the known remedy.
- [Static fixtures 1000/1001 dialling] → they carry no `ATSA_TENANT_ID` and so are refused 404. The walking-skeleton never dials from them (it originates), so nothing regresses. Stated rather than special-cased.
- [Answer timing] → the caller is answered only when the far end answers, so the caller hears ringing (180), not early media. Early media from a carrier is not passed through in Phase A.
- [`Retry-After` may be impossible] → D8's fallback is a plain 503, recorded.

## Migration Plan

Migration `0009`:
- `inbound_numbers` and `emergency_numbers`, with RLS forced and policies;
- `UNIQUE (tenant_id, id)` on `extensions`;
- `ps_endpoints.set_var`;
- `ps_endpoint_id_ips` with `GRANT SELECT` to `asterisk_engine`.

The down migration drops them in reverse. Existing projections have no `set_var` until re-projected, so `pbx reconcile` reports the drift and `--fix` repairs it (DiffExtension/DiffTrunk gain the field). Engine config: `sorcery.conf` and `extconfig.conf` gain the identify mapping, which needs an Asterisk restart (`make rig-restore` does it).
