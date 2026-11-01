## Why

A trunk today has one address: its host, used both to send calls to the carrier and to recognise the carrier's calls (D-75). That is not how carriers work, and the owner's own testing showed a second gap:

- **Carriers send from more addresses than they receive on.** Twilio, Telnyx, Vonage and most wholesale carriers publish a *list of signalling IP ranges* their inbound calls come from, separate from the host you send to (for example `sip.carrier.com`, while calls arrive from `54.172.60.0/30`, `54.244.51.0/30`, …). With only the host, those calls are refused as unknown, so connecting such a carrier is impossible. That contradicts BRD FBR-R1-03 ("connect to any standards-compliant carrier") and VP-3 (BYOT). Every comparable product takes such a list per trunk: 3CX "IP-based" trunks, FreePBX `match`/`permit`, and Kamailio and OpenSIPS address tables.
- **Hosts that cannot be a carrier are accepted.** The owner created trunks on `localhost` (2026-11-01). Inside the platform, that name means the platform's own container. The trunk can never work, the console says only "No response", and the platform's carrier check is aimed at its own internal services. Hosted platforms refuse loopback, link-local, unspecified and multicast addresses for trunk targets outright. That is both a configuration guard and a basic server-side request forgery guard (OWASP A10).

## What Changes

- **Inbound signalling addresses.** A trunk gains an optional list of up to 20 inbound signalling addresses: IPv4 or IPv6 addresses or CIDR ranges (no broader than /16 for IPv4 or /32 for IPv6).
  - The carrier's calls are recognised from the host's own addresses (as today) **and** from these.
  - The list is configuration: returned on reads and replaced on update, as the rest of a trunk is.
- **Non-routable addresses are refused.**
  - On create and update, the host is refused when it is `localhost` or a `.localhost` name (RFC 6761), or a loopback, link-local, unspecified or multicast IP literal. Inbound addresses are refused on the same grounds. The refusal names the field.
  - Private ranges (RFC 1918, ULA) stay allowed, because partners run carriers and SBCs on their own networks.
- **A name that resolves only to such addresses is not a carrier.** Those addresses are never checked and never recognised. The trunk reads `Unhealthy · Address not allowed` (a new health reason), not "No response".
- **Console:** the trunk dialog gets an *Inbound signalling addresses (optional)* field (one per line), and the trunks list shows them. Health shows the new reason.
- **Existing trunks** whose host is now refused (the owner's two `localhost` trunks on the rig) are kept and keep their configuration. They read `Unhealthy · Address not allowed` at the next check. An edit must fix the host before it can be saved.
- **BREAKING (API, minor):** `UpdateTrunk` replaces the trunk's inbound addresses, as it replaces every other field, so a client that omits them clears them. The console always sends them.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `pbx-core/trunk-routing`:
  - *A trunk is a tenant-scoped record…*: the inbound addresses, and the refusal of non-routable hosts and addresses.
  - *Inbound signalling is accepted only from a trunk's carrier address*: also from its inbound addresses.
  - *Trunk health is checked, never set*: a name resolving only to non-routable addresses reads `Address not allowed`.

## Impact

- **API (Go):**
  - `internal/pbx/domain`: validation of hosts and inbound addresses, the routable-address rule, and the identify match as the union of addresses and ranges;
  - `internal/pbx/acl/sipprobe`: never sends to a non-routable address;
  - `internal/pbx/postgres`: the column;
  - `internal/pbx/acl/asterisk`: identify and reconcile;
  - `internal/pbx/rpc`;
  - `api/proto` (`Trunk.inbound_addresses`, the create and update fields, `TRUNK_HEALTH_REASON_ADDRESS_NOT_ALLOWED`).
- **Schema:** migration 0014 adds `inbound_addresses CIDR[] NOT NULL DEFAULT '{}'` to `carrier_trunks`.
- **Console:** the trunk dialog, the trunks list, health copy and regenerated clients.
- **Decisions:** D-76 records the inbound address list, the refusal rule, and why private ranges stay allowed.
- **Docs:** LLD-03 §3 (`Trunk`), §7.5 and §10.5; `docs/API.md`; `docs/uat/trunk-host-resolution.md` (steps for the new field).
- **Not in this change:** two trunks (of different tenants) whose addresses overlap still cannot be told apart. That is the shared-carrier-address change that follows, which needs a spike first (D-25).
- **Dependencies:** none.
