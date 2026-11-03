# Coverage — console-browser-phone

**Tiers:**
- **U**: projection unit/integration tests, console component and provider tests.
- **I**: Postgres projection integration on `atsapbx_test`; the console rig spec with two real browser SIP sessions.
- **E**: full Go e2e (desk-phone and browser media paths do not regress), console mocked/browser tiers.
- **A**: the three phone UATs as regression; the browser UAT procedure.

## pbx-core/endpoint-projection — a WEBRTC extension is projected for browser media

| Scenario | Proven by |
|---|---|
| A WEBRTC extension uses the WebRTC transport | **I** `TestProjectExtension_IsIdempotentAndUpdatesInPlace` asserts `transport-wss` (the test config) and the browser fields |
| Browser media is encrypted | **I** the same test asserts `webrtc=yes`, `media_encryption=dtls`, `dtls_auto_generate_cert=yes`, `ice_support=yes`, `use_avpf=yes`, `rtcp_mux=yes`, `allow=ulaw,alaw` |
| A SIP extension is unchanged | **I** the existing projector suite; the same test starts as `SIP` before switching |
| The transport is configuration, not a guess | **I** the projection writes the configured transport; `pbx reconcile` reports divergence |
| The From domain is configuration | **I** the same test asserts `from_domain=pbx.example.test`, not the engine hostname |

## pbx-core/extension-management — registrar settings

| Scenario | Proven by |
|---|---|
| The registrar address is read | **W** `TestGetSipRegistrar_MapsEveryField`; existing integration |
| The WebRTC URL is returned when configured | **W** the same test asserts `webrtc_url`; the rig e2e uses it to register |
| No WebRTC URL is configured | **W** the default/empty response; **U** config validation |
| No public host / permissions / another tenant | **W** the existing registrar tests |

**Console:** component tests cover the softphone page and provider with a
fake SIP client (connect, failure, incoming/answer/reject, mute, hangup,
disconnect, validation, missing URL, no WEBRTC extension, unknown remote);
the two-browser rig spec proves real registration and a real call over the
rig's WS transport. `make console-check` and the coverage floors are green.

**Executed 2026-11-03:** Go lint 0; unit race+cover in the Go container; full `pbx/application`, `pbx/postgres`, `pbx/acl/asterisk` and `internal/postgres` integration on `atsapbx_test` (migration 0018 up/down); full Go e2e; `make console-check` including the two-browser rig spec; all three phone UATs; `make docs`; SCA. Unit coverage 79.3% >= 79.2%; console floors ratcheted to 96.6 / 89.2 / 98.6 / 97.9.

**Found and fixed on the way:**
- Asterisk's WS transport must share the HTTP server port (8088); a
  separate 8089 raw listener closed the browser handshake.
- A `default_from_domain` line is not a PJSIP global option and made the
  whole global object fail, so realtime endpoints stopped loading. The
  projector now sets `from_domain` per extension.
- The engine image has no `codec_opus`; WEBRTC endpoints use ulaw/alaw.
- Replacing the Call button with Hang up inside its own click re-fired the
  new button; the action now starts on the next tick.

## Gaps

| # | Gap | Severity | Note |
|---|---|---|---|
| G1 | No TURN; symmetric-NAT browsers cannot connect | **High** for production | Phase A host candidates only; add coturn before a NAT pilot |
| G2 | The administrator enters the SIP password each session; no ownership link | Accepted | D-65 Phase B `pbx-extension-assignment` gives "my phone" |
| G3 | Hold, transfer, park, conference are not in this change | Accepted | Phase B |
| G4 | No codec_opus; browsers use PCMU/PCMA | Accepted | Add codec_opus to the image later if desired |
| G5 | Plain WS on the dev rig | Accepted | localhost only; production must use WSS |
