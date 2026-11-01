## Context

See proposal.md, *Why*.

**Today:**
- every trunk projects an identify row (D-75, D-76) on its own endpoint, `t_<id>`;
- the endpoint carries `ATSA_TENANT_ID` in `set_var`;
- telephony's arriving-call path reads that variable (`handleArrivingChannel`) and records the call under that tenant *before* asking pbx-core's `ResolveInbound`;
- phones are identified by username first (`endpoint_identifier_order = username,ip,anonymous`).

**The spike** (task 1.1) must prove two things on the rig before anything else is built:
- that ARI can read the transport source address of an arriving channel (`CHANNEL(pjsip,remote_addr)`);
- that a realtime endpoint with an identify row covering several ranges takes the calls from all of them.

## Goals / Non-Goals

**Goals:**
- Any number of trunks, in any tenants, may recognise one carrier address. Each tenant receives its own numbers.
- Attribution uses only the transport source and the dialled number, checked against configuration. Ambiguity is refused, never guessed.
- One narrow privileged lookup is the only cross-tenant read.

**Non-Goals:**
- Refusing duplicate numbers across tenants at configuration time. That would reveal another tenant's number (INV-10). Ambiguity is refused at call time and logged for the operator.
- Carrier-specific header matching, and outbound changes. Trunk endpoints still send.

## Decisions

### D1. One `carrier-ingress` endpoint takes all carrier traffic (D-77)

The platform projects:
- one realtime endpoint, AOR and identify row, id `carrier-ingress`;
- its identify match is the normalised union of every trunk's carrier addresses and inbound ranges, across tenants;
- the endpoint has no `set_var` tenant, and the same context, codecs and transport as a trunk endpoint.

It is rewritten whenever a trunk is created, changed or deleted, or a check changes the addresses, inside that transaction. Per-trunk identify rows are no longer projected, and upgrade removes them.

**Alternatives considered:**
- Per-trunk rows with `match_request_uri`. Rejected: criteria are OR-ed (spike evidence, Asterisk 22.11 help text).
- Per-carrier-address ingress endpoints. Rejected: overlapping ranges make "the address set" ill-defined, and one endpoint is simpler to reconcile.
- A separate SBC such as Kamailio. Rejected for now: a new moving part. The design is the same at the platform level and can move into one later.

### D2. Attribution: source address plus number, exactly one

`carrier_ingress_match(source inet, number text)` is a `SECURITY DEFINER` function, owned by the migration role and executable by `atsapbx_app` only. It returns the (tenant_id, trunk_id) pairs where:
- the trunk's resolved addresses or inbound ranges contain `source`, through `inet <<=`;
- the trunk's tenant has an inbound number equal to `number`, normalised as `NormalizeDialled` does.

pbx-core's `ResolveInbound`, for the ingress source, routes on exactly one row. With zero or several it refuses `NotFound`, and on several it logs a warning naming the trunk ids, operator-only, never sent on the wire. The chosen trunk then goes through the existing `resolveTrunkCall` in its tenant: DID, extension, channel reservation, health watcher.

### D3. Telephony asks pbx-core for the tenant before recording an ingress call

On a channel from `carrier-ingress`, `handleArrivingChannel` also reads `CHANNEL(pjsip,remote_addr)` and passes it on.

`CallArrived` asks a new port, `InboundAttributor.Attribute(source endpoint, source address, dialled)`, for (tenant, trunk) when the endpoint is the ingress:
- On a refusal, the channel is rejected `404`. No call row is written, because there is no tenant to write it under.
- Otherwise the call is recorded under that tenant, and the router receives the attributed trunk as the source.

Phones and the other endpoints are unchanged.

### D4. Reconcile

`DiffTrunk` no longer expects a per-trunk identify row, and reports a leftover one as drift. A new `DiffIngress` compares the ingress identify with the union. `--fix` rewrites both.

## Risks / Trade-offs

- **[Risk] One identify row with many entries.** Asterisk re-reads the realtime row per carrier request, and parses CIDRs, not names (D-75). A few hundred entries is cheap; there are 20 per trunk at most. → Measured in the spike with 500 entries.
- **[Risk] The ingress row is rewritten by every trunk change in every tenant.** → It is one row and an `UPDATE`, inside the writer's transaction. Concurrent writers serialise on that row, which is acceptable for configuration rates.
- **[Risk] `remote_addr` behind NAT or a proxy is the proxy's address.** → The same is true of identify today. Documented: the platform must see carriers' real addresses.
- **[Trade-off]** A number configured in two tenants that share a carrier stops ringing in both, until the operator resolves it. This is visible in the log and fails safe.

## Migration Plan

1. Migration 0016 adds the function.
2. The app version that projects the ingress deploys. On startup the projector writes the ingress row and deletes per-trunk identify rows; reconcile confirms.
3. Rollback: the previous version re-projects per-trunk identify rows on the next trunk write, or `pbx reconcile --fix`. Run the down migration to drop the function.
