## 1. Engine projection

- [x] 1.1 Migration 0018 adds the realtime endpoint columns for browser media (`webrtc`, `media_encryption`, `dtls_auto_generate_cert`, `ice_support`, `use_avpf`, `rtcp_mux`), with down. Verify up/down on `atsapbx_test`.
- [x] 1.2 The projector writes the WEBRTC transport/media settings for a WEBRTC extension and leaves a SIP extension exactly as before; update reconcile/diff for the new fields. Unit and integration tests. Verify `make test`/the integration tier.

## 2. Engine transport and settings

- [x] 2.1 Add `transport-ws` (dev) to `core/conf/pjsip.conf`, publish 8088 in compose, and document the production `transport-wss` block with mounted certificates. Verify the engine starts and the port is reachable.
- [x] 2.2 Add `SIP_WEBRTC_URL` (validated `ws`/`wss`) and `SIP_WEBRTC_TRANSPORT` configuration; expose `webrtc_url` on `GetSipRegistrarResponse`, regenerate clients, and cover configured/empty in wire tests. Verify the wire tier and `make docs`.
- [x] 2.3 Set the rig's `SIP_WEBRTC_TRANSPORT=transport-ws` / `SIP_WEBRTC_URL=ws://localhost:8088/ws`; rebuild and confirm a WEBRTC extension projects the WS transport and the browser can open the URL.

## 3. Browser softphone

- [x] 3.1 Add `sip.js@0.21.2` (MIT) and a `Softphone` interface plus a SIP.js implementation: connect/register, call, incoming, answer, reject, mute, hangup, disconnect; errors in console words; no secret logging. Unit-test the interface contract with a fake.
- [x] 3.2 Add a softphone provider in the shell and a top-bar indicator (registered, incoming, in call) visible to `call.place` holders; survive navigation and clear the password on reload/disconnect/tenant change. Component tests.
- [x] 3.3 Add the `/phone` page: WEBRTC extension picker, password field (memory only), Connect/Disconnect, registration state, dialler, incoming panel (Answer/Reject), mute and hang up. Component tests for every state and failure.
- [x] 3.4 Add the route/navigation entry and ensure the access token and SIP password never leave memory or reach a URL/log/toast; the page works at 390 px and with the keyboard. Component/browser tests.
- [x] 3.5 Production build: import SIP.js only with the softphone route (code-split) and verify `make console-build` shows no regressions.

## 4. Live proof

- [x] 4.1 Browser e2e on the rig: create two WEBRTC extensions, open two console pages with fake media, register both over WS, call extension to extension, answer, assert both in call, hang up, and see the call in history. Verify the console integration/e2e tier.
- [x] 4.2 Error paths live: wrong password, a missing microphone, and a disconnect/reconnect; assert console copy and no secret leakage. Verify with the browser spec.
- [x] 4.3 Add a UAT procedure (`docs/uat/browser-calling.md`) with the LAN/WS rig and the production WSS/TURN caveats; record the executed proof. Verify `make docs`.

## 5. Docs and close

- [x] 5.1 Update PRD EPIC-03, HLD-19, CONSOLE-DESIGN, API.md, LLD-03 §7.1/§7.4, DECISIONS D-81, and the TURN/WSS deployment notes. Verify `make docs`.
- [x] 5.2 Update `coverage.md`, ratchet floors after green, run every tier (unit, race, integration, e2e, console-check, phone UATs), and record the result (the host `-race` workaround applies).
- [x] 5.3 Sync the delta specs, archive the change, update PROGRESS/HANDOFF, and verify `openspec validate --archived`.
