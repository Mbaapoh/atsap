# Coverage — TURN for browser media

- Config tests: comma-separated URLs are trimmed; empty means no server.
- RPC test: `GetSipRegistrar` carries the ICE servers (URLs, username,
  credential).
- Console: the registrar adapter maps single and multiple URLs and omits
  empty credentials; the browser phone hands them to the connection.
- Live: coturn runs in the dev compose and the two-browser rig call still
  registers, calls, answers and hangs up. Production TURN parameters are
  in `docs/uat/browser-calling.md`.
