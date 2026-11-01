## Context

See proposal.md, *Why*. Today `ProjectTrunk` (`internal/pbx/acl/asterisk/trunk.go`) writes the trunk's host into four places:

- `ps_aors.contact` (`sip:<host>:<port>`);
- `ps_endpoints.from_domain`;
- `ps_endpoint_id_ips.match`;
- `DiffTrunk`, which compares the `match` column with `t.Host`.

The identify rows are realtime rows (sorcery, `ps_endpoint_id_ips`). Asterisk therefore reads them, and resolves any host name among them, while it identifies an incoming request. Only `match` sits on the path that every inbound request takes. The contact and `from_domain` are used only when a call is sent to that trunk.

Since D-74 the trunk check (`application/trunk_prober.go`, `acl/sipprobe`) already resolves each trunk host name:

- every 30 seconds, and as soon as a trunk is created or its host or port changes;
- with a 2-second limit, preferring IPv4;
- any resolution failure is mapped to `HostNotFound`.

It records health with a compare-and-set on the host and port it checked (`SetCheckedHealth`), so a stale check cannot overwrite a newer one.

## Goals / Non-Goals

**Goals:**

- No name lookup on Asterisk's identify path, ever.
- A carrier configured by name is recognised within one check of creation, or of a change to its host.
- Recognition survives a brief DNS outage.
- Reconcile stays deterministic: no lookups during comparison or repair.

**Non-Goals:**

- **Refusing unresolvable hosts.** The owner decided on 2026-11-01 to accept and flag them, and health already flags them.
- **Two trunks on one carrier address** (inbound G3). That is the next pilot gap, and it is not made worse here: two trunks resolving to one IP already collide today.
- **Name lookups on the outbound path** (the contact and `from_domain`). They stay names, because carriers expect their domain in the Request-URI and From headers, and the lookup happens only for a call to that trunk. Task 1.1 measures whether an outbound call to an unresolvable host delays others. If it does, that becomes a recorded gap, not hidden scope.
- **Showing the resolved addresses in the API or the console.**

## Decisions

### D1. The platform resolves names; identify holds addresses only (D-75)

The check already resolves each name. It now returns **every** address the name resolved to (A and AAAA, sorted and deduplicated), not only the one it probed. The check records those addresses on the trunk (`resolved_addresses inet[]`, `resolved_at`). `ProjectTrunk` then writes them into `match` as a comma-separated list of IP literals. `res_pjsip_endpoint_identifier_ip` accepts a list there.

- For an IP host, the address set is the host itself, written at create or update time, so there is no gap.
- For a host name with no recorded addresses, the identify row is **deleted** rather than left holding the name.

Alternatives considered:

- **A caching resolver beside Asterisk** (dnsmasq or unbound). It hides the latency of names that resolve. Its first miss, and a name that times out, still block, and Docker's resolver answers a mistyped single-label name with a *temporary failure*, which is not negatively cached. It would also add a moving part to the engine image. Rejected.
- **Refusing unresolvable hosts at creation.** This catches typos, but a host that later stops resolving stalls everyone again, so it is not a fix on its own. The owner rejected it for this change (accept and flag).
- **Static `pjsip.conf` identify with `srv_lookups=no`.** Trunks are realtime by design (D-47, and the requirement "configuration takes effect without an apply step"). Rejected.
- **A separate resolver loop, outside the check.** A second timer and a second compare-and-set race, for no gain: the check already resolves at exactly the cadence wanted. Rejected.

### D2. The rule for keeping or replacing addresses (domain, pure)

`domain.ObserveAddresses(current []netip.Addr, host string, resolved []netip.Addr, ok bool) []netip.Addr`:

| Case | Result |
|---|---|
| IP host | the host itself |
| Name, resolved | the sorted, deduplicated set (replaces the old one) |
| Name, failed | `current`, unchanged: a brief DNS outage keeps inbound calls working; health still reports `Host not found` |

A host change clears the addresses in the same update that changes the host (the store's `UpdateTrunk`). The previous carrier is therefore never recognised under the new configuration. Recording addresses is part of the configuration record, not a repair: it is an observation, like health, made under the same compare-and-set on host and port.

### D3. Address recording and projection happen in one transaction

`SetCheckedHealth` grows into `SetCheckedObservation`. Under the existing guard (`WHERE host = $checkedHost AND port = $checkedPort`), it writes health, reason, time, and the addresses when the set changed. It reprojects the identify row in the same tenant transaction, as create and update already do. This holds to "configuration and its projection succeed or fail together".

- When the set is unchanged, the identify row is not touched, so a steady state costs no writes on the engine table.

### D4. Reconcile compares with the recorded addresses

`DiffTrunk` computes the expected `match` from `t.ResolvedAddresses` with the same function `ProjectTrunk` uses: the joined IP literals, or no row when there are none. It does not resolve anything. A `match` that holds a name is drift, by construction, and repair writes the recorded addresses.

### D5. Migration

Migration 0013 adds `resolved_addresses inet[] NOT NULL DEFAULT '{}'` and `resolved_at timestamptz` to `trunks`.

- IP hosts are backfilled to `{host}` in the migration.
- Name hosts start empty, and their **existing identify rows are left as they are** until the first check round, seconds after start-up, rewrites them. This avoids an inbound outage across the upgrade.
- `pbx reconcile` run before that first round would report those rows as drift, which is true.
- The down migration drops the columns. Identify rows written as addresses keep working with the old code, because the old code's `DiffTrunk` would only report them.

## Risks / Trade-offs

- **[Risk] A carrier rotates its addresses faster than the check interval.** Calls from a new address are refused for up to 30 seconds. → Carrier signalling addresses are long-lived in practice; the interval is configurable; the old set is replaced, not merged, which keeps the recognised set from growing stale.
- **[Risk] The platform resolves names on the app's network, which may differ from the engine's.** → This is the same trade-off D-74 accepted for the probe. Both containers use the same Docker DNS in the deployed compose.
- **[Risk] A name that never resolves recognises nothing.** That is intended, and it is visible as `Unhealthy · Host not found`.
- **[Trade-off] For seconds after creation, a name-configured trunk does not take inbound calls.** → The check runs at once on create. The console already reads `Not checked yet` for that window.
- **[Risk] The outbound path may still block on DNS** (Non-Goals). → Task 1.1 measures it. If it blocks other tenants, it is recorded as a *High* gap with the measurement.

## Migration Plan

1. Deploy: migration 0013 runs. IP-host trunks are unchanged in effect. Name-host identify rows are rewritten to addresses by the first check round.
2. Verify: `pbx reconcile` reports no divergence once one round has passed.
3. Rollback: run the down migration and deploy the previous image. Identify rows holding addresses still recognise the carriers; `pbx reconcile --repair` on the old version restores the names, if wanted.
