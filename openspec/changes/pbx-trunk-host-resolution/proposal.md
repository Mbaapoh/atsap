## Why

On 2026-10-13 a tenant's trunk host was mistyped as `locslhodt`. Call setup on the rig then stalled for about 5 seconds for **every** tenant, and one e2e run failed on it (`phase-a-hardening` task 6.1).

- **The cause:** each trunk's host name is projected verbatim into the engine's identify table, as the `match` column of `ps_endpoint_id_ips`. Those rows are realtime rows, so Asterisk resolves every host name in them while it works out which endpoint an incoming SIP request belongs to. One name that does not resolve therefore delays the identification of every request, whichever tenant it is for.
- **Since `pbx-trunk-health-probing` (D-74):** the trunk is now *visible* as `Unhealthy · Host not found`. The stall itself remains, and it is a *High* pilot gap in `docs/PROGRESS.md`: one tenant's typo must not degrade service for the others.

## What Changes

- **No DNS lookups during identification.** The engine's identify rows hold only IP addresses, never a host name.
  - The platform resolves a trunk's host name itself, within the trunk check it already runs.
  - It records the addresses it found on the trunk, then projects them.
  - A host given as an IP address is projected as it is, at once, as today.
- **A name is recognised once it has resolved.** A carrier configured by name is recognised for inbound calls as soon as the platform has resolved it. That happens in the check that runs right after the trunk is created or its host changes, so within seconds.
  - Until then, the trunk is not recognised for inbound calls.
  - It reads `Not checked yet`, and then either healthy or `Unhealthy · Host not found`.
- **The last good addresses are kept.** When a name that resolved before fails to resolve, the trunk keeps its last addresses. A brief DNS outage at the carrier's provider therefore does not drop inbound calls.
  - A changed address set replaces the old one.
  - A changed host clears the addresses until the new host resolves.
- **An unresolvable host is still accepted** (owner decision, 2026-11-01). It is saved, it is flagged by the existing health check, and it cannot stall anyone. No new API field or error is added.
- **Reconcile follows the addresses.** The operator's divergence check compares the identify rows with the addresses recorded on the trunk, and repair writes those addresses back.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `pbx-core/trunk-routing`:
  - *Inbound signalling is accepted only from a trunk's carrier address.* A carrier configured by host name is recognised by the addresses that name last resolved to. Recognition never waits on name resolution. One trunk's unresolvable host does not delay calls for any other trunk or tenant.
- `pbx-core/endpoint-projection`:
  - *Divergence … is detectable and repairable.* The configuration a trunk's identify state is compared with now includes the addresses recorded for its host.

## Impact

- **API (Go):**
  - `internal/pbx/acl/sipprobe`: the check returns every address the name resolved to, not only the one it checked.
  - `internal/pbx/domain`: the trunk's recorded addresses, and the rule that keeps or replaces them.
  - `internal/pbx/application/trunk_prober.go`: a check records the addresses and reprojects identify in the same transaction, guarded by the unchanged host.
  - `internal/pbx/acl/asterisk/trunk.go`: identify is projected from addresses only, and `DiffTrunk` compares against them.
  - `internal/pbx/postgres`: the new columns.
- **Schema:** migration 0013 adds `resolved_addresses` and `resolved_at` to `trunks`. On upgrade, existing trunks keep working:
  - IP hosts are backfilled to themselves.
  - Name hosts are resolved by the first check round at start-up, seconds after the upgrade. Their identify row keeps its old value until then.
- **No change** to the public API, the console or the protos.
- **Decisions:** D-75 records that the platform, not Asterisk, resolves carrier names for identification. It sets out the alternatives: a caching resolver beside Asterisk, refusing unresolvable hosts, and static configuration.
- **Docs:**
  - LLD-03 (§7, trunk projection);
  - `docs/PROGRESS.md`: the DNS-stall gap is closed.
- **Tests:**
  - **Unit:** the address rule, and the probe's address set.
  - **Integration:** projection holds addresses only; the last good set is kept; a host change clears it; reconcile.
  - **e2e on the rig:** with an unresolvable trunk host present, another tenant's call sets up without the 5 s stall (the regression that found this); a carrier configured by name is recognised for inbound calls.
  - **UAT:** `uat-inbound` with the carrier configured by its host name.
- **Dependencies:** none.
