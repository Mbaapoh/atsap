## Why

On 2026-10-13 a tenant administrator created a trunk with the host `locslhodt`, a typo for `localhost`. The console showed it as **Healthy**. That was false, and a carrier status the console cannot back up is worse than none.

- **Healthy is a default, not an observation.** A new trunk starts `Healthy` by design (`trunk-routing`: "a new trunk starts healthy"), and health changes only when a call through the trunk is answered. A trunk that has carried no call is reported healthy without anyone having checked.
- **Asterisk does not check it either.** Trunks are realtime AORs with a static contact, and Asterisk 22 never qualifies those: with `qualify_frequency` set, the contacts stay `NonQual`, and `pjsip qualify` sends nothing. This was measured on the rig, 2026-10-14.

## What Changes

- **A real check.** The platform sends each trunk's carrier a SIP OPTIONS request at a fixed interval (30 s by default), and immediately when a trunk is created or its host or port changes.
  - Any SIP response means the carrier is up.
  - A host that does not resolve, or no response, means it is not.
  - A slow response degrades the trunk.
- **Health says what it knows.** A new trunk is `Not checked yet` (`TRUNK_HEALTH_UNKNOWN`) until its first check, never `Healthy`. Every health carries:
  - **a reason:** host not found, no response, carrier error, slow response, or answering;
  - **when it was last observed.**
- **The console shows both,** for example "Unhealthy · Host not found · checked 14:02:31", instead of a bare badge.
- **Route selection is unchanged in kind.** `Unhealthy` is skipped and `Degraded` is deprioritised, as before. A trunk not yet checked is selectable like a healthy one, so a new trunk works at once.
- **Call outcomes still count.** An answered or failed call through the trunk updates its health between checks, as today, now with its reason and time.
- **BREAKING (console wording only):** a newly created trunk no longer reads "Healthy". The API's existing values keep their meaning; `UNKNOWN` is added.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `pbx-core/trunk-routing`:
  - trunk health is checked actively, starts unknown, and carries a reason and the time of its last observation;
  - route selection treats unknown like healthy.

## Impact

- **API (Go):**
  - `internal/pbx/domain` (unknown state, probe mapping, reasons);
  - a new `internal/pbx/acl/sipprobe` (SIP OPTIONS over UDP, no new dependency);
  - `internal/pbx/application` (the prober loop and checks on change);
  - `internal/pbx/postgres` (the health columns);
  - `internal/pbx/rpc` and `proto` (`TRUNK_HEALTH_UNKNOWN`, `health_reason`, `health_observed_at`);
  - `cmd/atsap-api` (wiring and interval configuration).
- **Schema:** migration 0012 adds `UNKNOWN` to the health check, the default `UNKNOWN`, and the reason and observed-at columns. Existing trunks are set to `UNKNOWN`, because they were never really checked; the first check follows within seconds.
- **Console:** the trunks list and the overview's trunk-health table show the state, reason and time.
- **Decisions:** D-74 records that the platform probes carriers itself, not Asterisk, and the trade-off: the probe leaves from the app's network.
- **Tests:**
  - unit tests (the mapping, and the probe against a local UDP responder);
  - integration tests (health columns, checks on change);
  - e2e on the rig (a real carrier answering, an unresolvable host, an unroutable address);
  - console component, browser and UAT tests.
- **Dependencies:** none.
