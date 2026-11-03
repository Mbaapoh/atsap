## Context

See `proposal.md` — Why. The engine projection already branches on
`DeviceType`, but `core/conf` has only a UDP transport and the projector
sets no WebRTC media fields; its own comment warns that projecting a
WEBRTC extension points at a transport that does not exist. The console
has no SIP stack. `GetSipRegistrar` reports the phone registrar address
only. Phase A has no user↔extension ownership (D-65), and an extension's
SIP secret is shown once and never stored recoverably.

## Goals / Non-Goals

**Goals:**

- An administrator can register a WEBRTC extension in the console and
  make, answer and end a real browser call, audio over DTLS-SRTP.
- The browser call is an ordinary extension call: the same dial plan,
  routing, call records and call session.
- The projection is correct before any UI offers WebRTC; a SIP extension
  is untouched.
- Production uses WSS with the operator's certificate; the dev rig uses a
  plain WS transport on localhost only.
- The softphone UI is testable without a browser or an engine.

**Non-Goals:**

- Hold, transfer, park, conference (Phase B).
- User↔extension ownership / "my phone" (D-65; Phase B).
- TURN/coturn. Phase A offers host candidates only; production NAT/TURN is
  a deployment gap, documented.
- Inbound carrier calls to a browser extension are not a new path: the
  existing DID routing rings it like any extension.
- A second SIP library or a custom SIP stack.

## Decisions

### D1. SIP.js 0.21.2 (MIT), behind a `Softphone` interface

Use `sip.js` — MIT, browser-native WebRTC, maintained, and it handles
registration, INVITE/BYE, re-INVITE and DTLS-SRTP SDP. It is a new runtime
dependency, so D-68 is amended and its licence checked against D-35
(MIT is allowed). The feature depends on a small `Softphone` interface
(`connect`, `disconnect`, `call`, `answer`, `reject`, `hangup`, `mute`,
events), so the page and provider are tested with a fake and only one
module knows SIP.js.

**Alternative rejected:** JsSIP — also MIT, but SIP.js has a clearer
TypeScript API and session-description handling for browser media.
**Alternative rejected:** a hand-written SIP stack — out of scope and
unsafe.

### D2. WS on the dev rig, WSS in production

`pjsip.conf` gains `[transport-ws]` on port 8088 (plain, localhost only).
`SIP_WEBRTC_TRANSPORT` selects the transport the projector writes;
`SIP_WEBRTC_URL` is the URL the console opens (for example
`ws://localhost:8088/ws` on the rig, `wss://pbx.example.com/ws` in
production (TLS at the reverse proxy)). The deployment documentation gives the `[transport-wss]`
block and certificate mount; the repository commits no certificate or
private key. A production deployment MUST use `wss://`; the e2e rig is
the only place `ws://` is expected.

**Alternative rejected:** a committed self-signed certificate trusted by
nothing — it fails in a real browser and is a bad example.

### D3. Projection: migration 0018 adds the engine columns

The realtime `ps_endpoints` table lacks `webrtc`, `media_encryption`,
`dtls_auto_generate_cert`, `ice_support`, `use_avpf` and `rtcp_mux`.
Migration 0018 adds them. For a WEBRTC extension the projector writes:
`transport = <WebRTCTransport>`, `allow = ulaw,alaw`, `webrtc=yes`,
`media_encryption=dtls`, `dtls_auto_generate_cert=yes`, `ice_support=yes`,
`use_avpf=yes`, `rtcp_mux=yes`. A SIP extension writes the previous values
(the new columns NULL/false). The engine generates its DTLS certificate;
the browser validates it through the SDP fingerprint.

### D4. The registrar settings also publish the WebRTC URL

`GetSipRegistrarResponse` gains `webrtc_url` (empty when not configured),
next to the phone host/port/transport. It is configuration, not a secret,
and is read with the extension-read permission. This is simpler than a
second settings RPC and keeps one place that says where devices register.

### D5. Credentials: administrator-chosen extension, memory-only password

No ownership exists in Phase A, so the browser phone asks the
administrator to choose one of the tenant's WEBRTC extensions and enter
its SIP password. The password is held in React state only (never
localStorage, sessionStorage, a URL or a log) and is cleared on reload,
disconnect or tenant change. The secret cannot be read back from the API
by design; the console's existing create/rotate flows are where it is
shown once.

### D6. A `/phone` page and a small shell indicator

The softphone lives on `/phone` (visible to `call.place` holders) and is
owned by a provider mounted in the shell, so a call survives navigation.
The page shows: connection (extension select, password, Connect), the
registration state, a dialler, a call/incoming panel with Answer, Reject,
Mute and Hang up. The top bar shows "Phone: registered" / "Incoming call"
/ "In call" as a link to `/phone`. An incoming call while elsewhere is
visible and answerable by opening the page; there is no auto-answer.

Mute is implemented by disabling the local audio sender; it is not a
server-side hold (Phase B).

### D7. No TURN in Phase A

The browser and the engine exchange host/ICE candidates on the same host
or LAN. A browser behind a symmetric NAT cannot connect without a relay;
that is a deployment gap, not a hidden fallback. Production must add
coturn or equivalent and configure Asterisk's ICE; the docs list it
plainly. This keeps the change honest and avoids shipping a half-built
TURN stack.

### D8. Tests: fake client for UI, real WS for e2e

Component tests use the `Softphone` interface to walk every state
(connect, failure, incoming, answer, reject, mute, hangup). A browser e2e
opens two console pages with fake media, registers two WEBRTC extensions
over the rig's WS transport, calls extension to extension, answers, and
hangs up; it asserts both SIP sessions and the call history row. The
phone UATs are regression (a browser call never disturbs the media path
they prove). A real TURN path is not tested because Phase A does not ship
one.

## Risks / Trade-offs

- **[Risk] SIP.js bundle size** → It is imported only by the softphone
  route; Vite code-splitting is checked in the production build.
- **[Risk] A browser without `getUserMedia` or a blocked microphone** →
  Connection and call actions show a fixed error and offer retry; no
  secret or engine detail is shown.
- **[Risk] Plain WS on the dev rig** → It is bound to the dev compose port
  and documented as dev-only; production requires WSS.
- **[Risk] One registered contact per extension** (AOR `max_contacts=1`) →
  Use a dedicated WEBRTC extension; the docs say so, and the console marks
  the extension type.
- **[Trade-off] Password entry** → Secure (the secret is not readable),
  but less convenient than ownership. Phase B fixes it with
  `pbx-extension-assignment`.
- **[Trade-off] No TURN** → Calls work on the LAN/rig and fail behind a
  symmetric NAT until the operator adds a relay.

## Migration Plan

Migration 0018 is additive. Existing WEBRTC extensions (if any) receive
the new engine columns on the next projection/reconcile. `SIP_WEBRTC_URL`
defaults to the rig URL for development; production sets it explicitly.
Rollback drops the columns; existing SIP extensions are unaffected.

## Open Questions

None that change the approach. TURN is a deployment decision recorded as a
gap, not a code unknown.
