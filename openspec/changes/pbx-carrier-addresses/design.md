## Context

See proposal.md, *Why*. The current state, since D-75:

- `domain.Trunk` has `Host` and `ResolvedAddresses`. `IdentifyMatch(addrs)` joins the addresses into the engine's identify `match` (`ps_endpoint_id_ips`).
- The trunk check (`application/trunk_prober.go` with `acl/sipprobe`) resolves names, records the addresses, and reprojects identify when they change.
- `domain.ValidateHost` accepts any IP literal or DNS-shaped name.

Asterisk's IP identifier accepts a comma-separated list of addresses **and CIDR ranges** in `match`. A trunk can therefore be recognised from a published range with no new engine mechanism.

On the rig on 2026-11-01, the owner's `localhost` trunks recorded `127.0.0.1,::1`. Those are the app container's own loopback addresses, now projected as the carrier's.

## Goals / Non-Goals

**Goals:**

- A carrier is recognised from its published signalling addresses and ranges, immediately, with no name lookup.
- No trunk targets or recognises a loopback, link-local, unspecified or multicast address, whether written as a literal or reached through a name.
- Existing trunks keep working; the ones that never could are flagged.

**Non-Goals:**

- **Overlapping addresses across trunks or tenants.** The next change, shared carrier address, needs a spike first (D-25). Refusing an overlap with another tenant's trunk would also reveal that trunk exists (INV-10).
- **A shared, installation-wide carrier address book** (3CX's global list). Per-trunk lists match how a partner configures each tenant's carrier. A shared list is a later convenience.
- **SRV/NAPTR lookups** (RFC 3263) for the host. That is a separate refinement.

## Decisions

### D1. A per-trunk list of inbound signalling addresses, recognised alongside the host (D-76)

`Trunk.InboundAddresses []netip.Prefix`: up to 20 entries, each an address (stored as a /32 or /128) or a CIDR range. The identify match becomes the union of the host's routable addresses and these ranges, normalised (masked, sorted, deduplicated) so that equal configurations project identically.

- The list is configuration. It is written by create and update in the same transaction as the trunk and its projection, so it is recognised at once. The trunk check never touches it.

Alternatives considered:

- **Replacing the host's addresses with the list when one is given.** Rejected: a carrier that also signals from its host would silently stop being recognised.
- **Asterisk `permit`/`deny` ACLs.** Rejected: they filter, they do not attribute a call to a trunk and tenant. Identify is the attribution mechanism, so the ranges belong there.
- **Accepting any range.** Rejected: `0.0.0.0/0` would capture every unidentified request on the engine for one tenant. The floor is /16 for IPv4 and /32 for IPv6. The broadest published carrier ranges, such as Twilio's, are /30 to /24.

### D2. "Can be a carrier" is one rule, in the domain

`domain.CarrierAddress(a netip.Addr) bool` returns false for:

- loopback (`127.0.0.0/8`, `::1`);
- link-local (`169.254.0.0/16`, `fe80::/10`);
- unspecified (`0.0.0.0`, `::`) and IPv4 "this network" (`0.0.0.0/8`);
- multicast (`224.0.0.0/4`, `ff00::/8`);
- IPv4-mapped IPv6 written as a range.

Private ranges (RFC 1918, `fc00::/7`) and CGNAT space are allowed, because partners run SBCs and carriers on their own networks (BRD: deployed on the partner's infrastructure).

It is applied in four places:

- **`ValidateHost`:** refuses such a literal, and the names `localhost` and `*.localhost` (RFC 6761 §6.3), case-insensitive, with or without a trailing dot.
- **Inbound address validation:** a range is refused when its first address fails the rule, or when it is `0.0.0.0/8`.
- **Resolution:** the probe drops such addresses from what a name resolves to.
- **`IdentifyMatch`:** filters them out as well, so addresses recorded before this change (the rig's `127.0.0.1,::1`) are never projected again.

Alternatives considered:

- **Refusing private ranges too, as hosted platforms do.** Rejected: AtsaPBX runs on the partner's network, not ours.
- **Resolving names at creation to refuse loopback targets.** Rejected: names can change later. The check-time filter covers every case, and D-75 already decided that names are not resolved on the request path.

### D3. A name that resolves only to forbidden addresses reads "Address not allowed"

The probe gains the outcome `ProbeAddressNotAllowed` and the domain the reason `ADDRESS_NOT_ALLOWED`, mapped to `Unhealthy`. No packet is sent.

- For the address rule (D-75 D2), the lookup counts as a failure that keeps no forbidden address: `ObserveAddresses` filters `current` as well. Such a trunk therefore ends with no recognised host address.
- Its inbound ranges, if any, still apply.

Alternatives considered: reusing "Host not found" (rejected: it is false, the host was found) and "No response" (rejected: the owner saw exactly that, and it misled).

### D4. Storage and API

- **Storage:** migration 0014 adds `inbound_addresses CIDR[] NOT NULL DEFAULT '{}'`. It also removes non-carrier addresses from the `resolved_addresses` recorded by D-75, so the `localhost` trunks stop being recognised from the app's own loopback addresses.
- **Proto:**
  - `Trunk.inbound_addresses` (12), `CreateTrunkRequest.inbound_addresses` (9) and `UpdateTrunkRequest.inbound_addresses` (10), each `repeated string` in canonical CIDR form;
  - `TRUNK_HEALTH_REASON_ADDRESS_NOT_ALLOWED` (6).
- **Errors:** invalid input is refused with `invalid_argument`, naming `host` or `inbound_addresses` and the entry's position, never echoing engine terms.

### D5. Console

- **Trunk dialog:** an *Inbound signalling addresses (optional)* textarea, one address or range per line, with a hint ("The addresses your carrier sends calls from, if they differ from the host. Example: 203.0.113.0/28"). The server's refusal is shown at the field.
- **Trunks list:** shows the addresses under the host.
- **Health copy:** "Address not allowed".

## Risks / Trade-offs

- **[Risk] A partner enters a range that overlaps another tenant's.** → Out of scope (Non-Goals). The engine's identify picks one, as it does today for equal hosts. Recorded as the gap the next change closes.
- **[Risk] Existing `localhost` trunks stop matching their loopback addresses.** → They never could work: those addresses are the platform's own. The console says why.
- **[Trade-off] Twenty entries.** Large carriers publish around 10 to 15 ranges per region. The limit is a constant, raised by decision if a partner needs more.

## Migration Plan

1. Migration 0014 adds the column and strips non-carrier addresses from `resolved_addresses`.
2. The app version that projects the union deploys. On upgrade, existing identify rows still hold what D-75 projected. `pbx reconcile` reports the rows of trunks whose recorded addresses were stripped, and `--fix` or the next check rewrites them.
3. Rollback: run the down migration, which drops the column. The previous version projects the host's addresses only.
