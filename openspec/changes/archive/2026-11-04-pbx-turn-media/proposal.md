# TURN for browser media

## Why

A browser behind a symmetric NAT cannot connect to another browser or a
phone with host ICE candidates alone. The browser phone shipped with
host candidates only (D-81), recorded as a production gap. This change
adds the configured STUN/TURN servers the browser needs.

## What Changes

- The installation configures `TURN_URLS`, `TURN_USERNAME` and
  `TURN_CREDENTIAL` from its secret store.
- `GetSipRegistrar` returns the ICE servers alongside the registrar and
  WebRTC URL; signed-in tenants receive them because their browser must
  allocate a relay.
- The browser phone passes them to SIP.js's peer connection.
- The dev compose starts coturn on the host network; production runs its
  own TURN service.
- With no TURN URLs, behaviour is unchanged (host candidates only).

## Impact

- Proto `GetSipRegistrarResponse.ice_servers`; generated Go/TS/OpenAPI.
- Config and deployment docs; dev compose coturn service.
- No engine or database change.
