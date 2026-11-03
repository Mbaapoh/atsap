## Why

#8 of the pre-pilot list: browser calling (BRD FBR-R1-01, PRD EPIC-03). The
console can already place a call through the API by ringing a desk phone;
it cannot be the phone. This change adds a WebRTC softphone page so an
administrator can register a WEBRTC extension in the browser, dial, answer,
mute and hang up without installing anything. It also finishes the engine
projection that has been warning about a missing WebRTC transport since
`pbx-extensions-projection`.

## What Changes

- **WebRTC extension projection**: a `WEBRTC` extension is projected with a
  WebRTC transport and DTLS-SRTP media (and the narrow-band codecs); a `SIP`
  extension keeps exactly its current projection. Migration 0018 adds the
  engine's realtime columns the projection needs.
- **Engine WebRTC transport**: `core/conf` gains `transport-ws` for the dev
  rig, and the deployment documents `transport-wss` for production with
  mounted certificates. The compose stack publishes the WebRTC signalling
  port; the app is configured with `SIP_WEBRTC_TRANSPORT` and
  `SIP_WEBRTC_URL`.
- **Registrar settings**: `GetSipRegistrar` also returns the WebRTC
  signalling URL when one is configured. It is public configuration, never
  a secret.
- **Browser phone**: a new `/phone` page registers a WEBRTC extension with
  its SIP username and password (entered by the administrator, held in
  memory only), dials, receives and answers calls, mutes and hangs up. A
  top-bar indicator shows registration and an incoming call from any page.
  The call uses the same engine path as a desk phone, so routing, call
  history and the API call session see it unchanged.
- **Dependency**: SIP.js 0.21.2 (MIT), behind a small `Softphone` interface
  so the UI is testable without a browser or Asterisk.
- **Scope**: Phase A has no user↔extension ownership (D-65), so the
  administrator chooses a WEBRTC extension and enters its one-time SIP
  password. Hold, transfer, park and conference are Phase B. No TURN is
  included; production NAT/TURN is a deployment requirement, recorded as a
  gap.

## Capabilities

### New Capabilities

None. The console behaviour is documented in PRD, HLD-19 and
CONSOLE-DESIGN; the backend shape belongs to the existing capabilities.

### Modified Capabilities

- `pbx-core/endpoint-projection`: a WEBRTC extension is projected as a
  browser endpoint with a WebRTC transport and encrypted media; a SIP
  extension is unchanged.
- `pbx-core/extension-management`: the registrar settings also report the
  WebRTC signalling URL when configured.

## Impact

- **Dependency:** `sip.js` 0.21.2 (MIT) in the console.
- **Engine:** `core/conf/pjsip.conf` (transport-ws), `deploy/docker-compose.yml`
  (port 8088); production WSS certificates documented, not committed.
- **API/config:** `SIP_WEBRTC_URL` and `SIP_WEBRTC_TRANSPORT`; a
  `webrtc_url` field on `GetSipRegistrarResponse`; generated Go/TS clients.
- **Migration:** 0018 adds `ps_endpoints` WebRTC columns (engine-facing).
- **Console:** a `features/softphone` client/provider, a `/phone` page, a
  top-bar indicator, and a route visible to `call.place` holders.
- **Tests:** component tests with a fake softphone, a real browser e2e with
  two pages and fake media over the rig's WebSocket transport, and a UAT
  procedure. No TURN path is tested in Phase A.
- **Docs:** PRD, HLD-19, CONSOLE-DESIGN, API.md, LLD-03 §7.1/§7.4,
  DECISIONS (D-81), PROGRESS/HANDOFF on archive.
