## Why

Phase A exists to make a real call through the public API (BRD §16, flow A5). Extensions register and trunks and routes project into the engine, but nothing places a call over them. The only origination path is `telephony-core`'s `InitiateCall`, which takes raw engine strings and has no RPC. LLD-03 §12 sequences this slice third, after `pbx-trunks-routes`: there are now routes to route to.

A second problem blocks proving any of it. The running dev app **cannot be licensed**: its image is built without the development verification key and no token is supplied, so it boots in Setup with no call path (D-52). Every existing call test sidesteps this by building services in-process with its own licence, so no test has ever placed a call through the running app. This change closes that gap, because a call path that is only proven in-process has not been proven.

## What Changes

- **`PlaceCall` and `HangupCall` on `PbxService`** (LLD-03 §6). `PlaceCall(extension_id, destination)` resolves the destination with the platform's own dial plan, never the caller's. A tenant extension number connects internally. A number matching an outbound route leaves through the selected trunk. Anything else is refused as unroutable. The call is originated through `telephony-core`'s existing `CallService.InitiateCall`, so `telephony-core` gets **no change** (LLD-03 §4.1).
- **Engine addresses stay in the ACL.** A new pbx-owned port, implemented in `pbx/acl/asterisk`, turns a domain `Destination` into the endpoint string `InitiateCall` takes. No engine vocabulary appears in `pbx/application`, `pbx/rpc`, or any response or error.
- **Per-trunk `channel_limit` is enforced.** The limit has been stored since `pbx-trunks-routes`. Each placed trunk leg is now recorded and counted while its call is still live, under the same advisory-lock pattern as the extension cap. A trunk at its limit is skipped in favour of the next candidate, and if no candidate has room the call is refused.
- **The first trunk-health reporter.** The outcome of each trunk leg, as recorded on the call (answered, how fast, or failed to originate), feeds `ReportRouteHealth`. That closes the loop `pbx-trunks-routes` left port-only.
- **Emergency destinations bypass Screening (INV-01).** `PlaceCall` matches the destination against a built-in default emergency set in `pbx/domain` and sets `InitiateCallCommand.BypassScreening`. That is the only place it is set. A wire-decoding test proves no request field can reach the flag (LLD-03 DoD 9). The per-tenant `emergency_numbers` table replaces the built-in set in `pbx-inbound-and-emergency`.
- **Dev-rig activation, nothing committed.** A `make dev-licence` target generates a throwaway Ed25519 keypair locally. The dev image is built with its public key through the existing `-ldflags` hook, and a dev-signed token goes into the gitignored `.env`. Production builds are unchanged and still trust only the vendor key (D-54, D-39).
- **Proof through every tier.** Unit and integration tests; an e2e test that places calls through the running app's public API against live Asterisk, with a scripted carrier answering on the trunk; and a new `make uat-placement` beside `make uat-auto`, where a real baresip phone places a call over a trunk to a real baresip "carrier" with RTP asserted in both directions. `make uat-auto` must stay green too.
- **Out of scope:** outbound-class toll-fraud enforcement (deferred to its own change: telling national from international needs a tenant country the platform does not hold). Inbound calls and the `InboundRouter` port, the tenant emergency table, and SIP `503` + `Retry-After` (all `pbx-inbound-and-emergency`). Mid-setup failover across trunks (LLD-03 §9). SIP cause-code capture (it needs a `telephony-core` change).

## Capabilities

### New Capabilities

- `pbx-core/call-placement`: placing and hanging up calls through the public API. This covers dial-plan resolution (internal, outbound, unroutable), emergency bypass, per-trunk channel limits at placement, outcome-observed trunk health, and the guarantee that no engine identifier reaches a caller.

### Modified Capabilities

- `pbx-core/trunk-routing`: the per-trunk channel limit changes from stored-only to enforced at placement. Trunk health gains its first observer, placed-call outcomes.

## Impact

- **New:** `pbx/application` call placement and the outcome observer; a `pbx/acl/asterisk` endpoint addresser; a pbx-owned `placed_call_legs` table (migration `0008`, RLS like every pbx table); `PlaceCall`/`HangupCall` proto plus regenerated stubs; a `make dev-licence` target and a dev-image build argument; e2e and UAT additions; `docs/API.md` rows.
- **Changed:** `pbx/ports` (call-placement port, addresser, leg store, and a read-only call-status port that pbx owns), `cmd/atsap-api` wiring (pbx gains `telephony-core`'s `CallService` and a call-status adapter over `CallStore`, which is the legal Tier 1 → Tier 0 direction).
- **Not touched:** `internal/licensing/`, `internal/identity/`. No production key material, and no committed private key or token.
- **`internal/telephony/` — touched once, deliberately (revised at apply time).** The UAT found that `telephony-core`'s `HangupCall` destroyed the bridge and recorded the call Terminated, but **never hung up the legs**: both real phones stayed on the line with media flowing until killed. Every earlier test ended calls from the phone side, so no test had seen it. Fixing it was approved in-flight. `HangupCall` now destroys each live participant's channel after finalizing, using the existing `MediaGateway.DestroyChannel` with no port change. Two e2e tests that had compensated by hanging channels up themselves now assert none remain. The rule "no telephony change" narrows to "only `HangupCall`'s channel teardown".
- **Sequencing:** after `pbx-trunks-routes` (archived). Before `pbx-inbound-and-emergency`, which replaces the built-in emergency set and adds the `telephony-core` changes this slice avoids.
