## Context

Trunk health today:
- A trunk is created `HEALTHY`: the schema default, set again by `CreateTrunk`.
- `ObserveHealth` maps a call outcome onto a state.
- The only outcome ever reported is an answered inbound or placed leg (`watchLeg`, `watchInboundLeg`). Unanswered calls carry no SIP cause (call-placement G1, G2).

So health means "no answered call has said otherwise". A trunk whose host does not exist is `HEALTHY` until a call fails, and a failed call does not count either.

**Asterisk will not check for us.** Measured on the rig, 2026-10-14:
- Throwaway realtime AORs were given `qualify_frequency=5`, with matching endpoints. Their contacts were `127.0.0.1`, the TEST-NET address `192.0.2.1`, and `locslhodt`.
- All three stayed `NonQual`, and `pjsip qualify <endpoint>` sent no OPTIONS.
- Asterisk 22 qualifies only AORs held in its options state, and realtime AORs without a full backend cache never enter it.

## Goals / Non-Goals

**Goals:**
- Health is an observation with a reason and a time.
- A new trunk is `Unknown` until checked, and is checked within seconds of creation or of an address change.
- The check needs no new dependency and does not disturb registration.

**Non-Goals:**
- Taking the DNS lookup off Asterisk's identify path. That is the separate *High* gap: an unresolvable trunk host stalls SIP identification for every tenant. This change makes such a trunk visible as `Unhealthy · Host not found`; it does not fix the stall.
- Mid-call failover.
- Probing over TCP or TLS. Every trunk uses the installation's UDP transport today.

## Decisions

### D1. The platform sends SIP OPTIONS itself (D-74)

`internal/pbx/acl/sipprobe` sends one OPTIONS request over UDP. It is RFC 3261 §11 shaped, with a fresh branch, tag and Call-ID, and `rport`.

- **Resolve:** the host is resolved with a 2-second limit. Any failure to resolve is `HostNotFound`.
  - A mistyped name often fails as a temporary error, not a clean NXDOMAIN. Docker's DNS does this for a single-label name such as `localhosts2`, found on the rig.
  - "No response" would send someone looking for a dead carrier when the name is the problem.
- **Send:** to the first resolved address, IPv4 preferred. Retransmit at 500 ms, doubling, until a 4-second budget is spent.
- **Read:** 1xx responses and packets whose Call-ID is not ours are ignored. The first final response gives the status and the round-trip time.

It is about 150 lines of Go on `net`. No SIP library is added.

**Alternative rejected:** Asterisk's qualify behind a sorcery memory cache (`full_backend_cache`). The cache would also hold every extension's AOR. A new extension could then fail to register until the cache refreshed, unless every projection also expired the cache over AMI. That risks registration, which works, to fix health.

**Trade-off, recorded in D-74:** the request leaves from the app's network, not Asterisk's.
- On the single-host deployment both egress through the host's address, which is what a carrier's allowlist sees.
- A host name can still differ between the two: on the dev rig, `localhost` is a different container for each.
- A carrier that refuses the app's address still sends a response (403 is common), and any response counts as answering.

### D2. Mapping a check onto health

`domain.ObserveProbe(outcome, status, rtt)` returns `(health, reason)`:

| Observation | Health | Reason |
|---|---|---|
| Final response 2xx–4xx, RTT ≤ 1 s | Healthy | `ANSWERING` |
| Final response 2xx–4xx, RTT > 1 s | Degraded | `SLOW_RESPONSE` |
| Final response 5xx or 6xx | Unhealthy | `CARRIER_ERROR` |
| No final response in 4 s | Unhealthy | `NO_RESPONSE` |
| Host does not resolve | Unhealthy | `HOST_NOT_FOUND` |

A 4xx counts as answering because a carrier that is up but declines an unauthenticated OPTIONS request is up. `ObserveHealth`, for call outcomes, gains the same reasons:
- 2xx gives `ANSWERING`, or `SLOW_RESPONSE` beyond the existing 2-second answer threshold;
- 0 and 408 give `NO_RESPONSE`;
- 5xx gives `CARRIER_ERROR`.

### D3. Storage: unknown, reason, observed-at

Migration 0012 changes `carrier_trunks`:
- `health_status` admits `UNKNOWN` and defaults to it;
- it adds `health_reason` (nullable) and `health_observed_at` (nullable);
- existing rows are set to `UNKNOWN`.

`SetHealth` writes all three, and only while the trunk still has the host and port that were checked (`WHERE id = $1 AND host = $2 AND port = $3`). A result for an address that has since changed, or for a deleted trunk, is dropped.

Route selection already penalises only `DEGRADED` and skips only `UNHEALTHY`, so `UNKNOWN` is selected like `HEALTHY` with no code change. A test pins that.

### D4. When checks run

- **On a schedule:** `TrunkProber.Run` checks every trunk of every tenant at `ATSAPBX_TRUNK_PROBE_INTERVAL` (default 30 s; `0` disables), at most 8 at a time. It lists tenants as the reconciler does, then each tenant's trunks under its own RLS scope.
- **On change:** `CreateTrunk`, and `UpdateTrunk` when the host or port changed, call `CheckTrunk` once the transaction commits. The check runs in the background with its own timeout and never delays the RPC.
- **In tests:** the service holds the checker as a port, so integration tests can substitute a fake probe.

### D5. API

Additive only:
- `TrunkHealth` gains `TRUNK_HEALTH_UNKNOWN = 4`;
- `Trunk` gains `TrunkHealthReason health_reason = 10` and `google.protobuf.Timestamp health_observed_at = 11`;
- `TrunkHealthReason` lists the five reasons plus `UNSPECIFIED`.

`buf breaking` passes. Nothing here is an engine identifier.

### D6. Console

- A trunk's health reads "Not checked yet" (neutral) until observed.
- Otherwise the badge is followed by the reason and the local time of the observation, for example "Unhealthy · Host not found · checked 14:02:31".
- This applies on the Trunks & routes page and in the overview's trunk-health table.
- The words are console copy keyed by enum, never server text.

### D7. An unhealthy trunk is the last resort, not a dead end

Active checks can be wrong:
- some carriers ignore OPTIONS from an address they do not know;
- a carrier that was down when checked stays marked until the next round.

Before this change only a failed call could take a trunk out of service. Now a check can, so selection skips an `Unhealthy` trunk only while another matching trunk can carry the call. When every match is `Unhealthy`, they are tried in the usual order.

A wrong observation then costs preference, never service, and a call that succeeds marks the trunk healthy again.

This was found by the A3–A5 UAT on the rig. Its carrier phone starts after the trunk is created, so the first check found nobody, and the next call was refused as unroutable.

## Risks / Trade-offs

- **A carrier that rate-limits OPTIONS:** 30 s is the common qualify interval and is configurable.
- **The app's network is not Asterisk's:** see D1 and D-74. A trunk Asterisk can reach but the app cannot would read `Unhealthy`. That is visible and explainable, unlike a silent `Healthy`.
- **Busy installations:** checks are bounded to 8 concurrent. With 4 s worst-case each, 100 dead trunks take about 50 s per round, longer than the interval. The next round starts when the previous one ends, so rounds never overlap.

## Migration Plan

Migration 0012 is applied by the app on start, as every migration is. Existing trunks read "Not checked yet" for the seconds until their first check. Down-migration restores the three-state check and the `HEALTHY` default, mapping `UNKNOWN` back to `HEALTHY`.
