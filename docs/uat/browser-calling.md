# UAT — Browser phone (real browser to real extension)

User Acceptance Testing for `console-browser-phone` (D-81). The automated
tiers prove the projection, registration and a browser-to-browser call.
This pass is what an administrator sees: connect a WEBRTC extension in the
browser, call a real phone, answer an incoming call, mute and hang up.

**Traceability:** BRD FBR-R1-01, §16; PRD EPIC-03 (US-03.1, AC-03.1,
AC-03.12); HLD-12, HLD-19; specs `pbx-core/endpoint-projection`,
`pbx-core/extension-management`; `openspec/changes/console-browser-phone`.

## 1. Environment

| Item | Detail |
|---|---|
| Stack | Dev rig (`make tilt-up`, or the compose stack) and console at **http://localhost:5173** |
| Login | `dev@example.test` / `e2e-fixture-operator-password`, tenant **Dev Acme** |
| Licence | Rig licensed (`make dev-licence`) |
| Signalling | `SIP_WEBRTC_URL=ws://localhost:8088/ws` on the rig; production uses `wss://` |
| Phones | The owner's `baresip` 1001/1002, or a second browser window |
| Certificate | None on the rig (plain WS on localhost); production needs TLS |

## 2. Procedure

| # | Step | Expected |
|---|---|---|
| 1 | **Extensions → Add extension**: number e.g. `8701`, name `Browser UAT`, device **Browser (WebRTC)**. Save the SIP password shown once | The extension exists and can be given to the browser phone |
| 2 | Open **Browser phone** from the top bar (`/phone`). Choose `8701`, enter its SIP password, press **Connect** | The page says *Registered as 8701*; the top-bar indicator says *Phone 8701* |
| 3 | Enter `1002` (the other phone) and press **Call** | The browser call is placed; the other phone rings. No secret appears in the URL, a toast or the page |
| 4 | Answer on the other phone | The browser says *In call with 1002 · …*; audio flows both ways; **Mute** silences the browser microphone and **Unmute** restores it |
| 5 | Press **Hang up** | The call ends on both ends; the phone is released |
| 6 | From the other phone, dial `8701` | The browser page shows *Incoming call from …*; the top-bar indicator says *Incoming call* even on another page |
| 7 | Open the browser phone and press **Answer**, then **Hang up** | The call connects and ends normally |
| 8 | Disconnect, reload the page | The form returns and the password field is empty; no SIP password is in local or session storage |
| 9 | Sign in as a non-administrator | No Browser phone entry; `/phone` says the role cannot use it |

## 3. Executed record

| Date | 2026-11-03 |
|---|---|
| Who | Claude (automated two-browser e2e; a human pass is still to be recorded) |
| Result | **PASS** at the automated tier |

- The console rig spec opened two browser contexts, registered two WEBRTC
  extensions against `ws://localhost:8088/ws`, called extension to
  extension, answered, showed *In call* on both, hung up, and found the
  INTERNAL call in `ListCalls`.
- The console component tests cover connect, failure, incoming/answer/
  reject, mute, hangup, disconnect, a missing URL, no WEBRTC extension and
  an unknown remote (never an engine id).
- `make docs` passes; the production WSS/TURN caveats are in
  `docs/hld/06-deployment.md`.
- A human pass with the owner's phones is still to be recorded here.

## TURN (D-84)

1. Set `TURN_URLS` to the deployment's TURN URL(s), with `TURN_USERNAME`
   and `TURN_CREDENTIAL`, and restart the app.
2. Call from a browser on a network that blocks direct peer-to-peer
   media (for example a symmetric NAT or a mobile hotspot with client
   isolation).
3. Expect the call to connect and audio to flow; the browser's ICE
   candidate log shows a `relay` candidate. With TURN unset the same call
   fails on a symmetric NAT and the console does not claim otherwise.
