## 1. Configuration

- [ ] 1.1 Add `SIP_PUBLIC_HOST`, `SIP_PUBLIC_PORT` (default 5060) and `SIP_PUBLIC_TRANSPORT` (default `udp`) to `config.Config`, validated at load: the host as a bare hostname or IP, the port 1–65535, the transport `udp`, `tcp` or `tls`. Verify with config unit tests for defaults and for each invalid value.

## 2. API

- [ ] 2.1 Add `GetSipRegistrar` to `pbx.proto` (request: `tenant_id`; response: `configured`, `host`, `port`, `transport`), regenerate the Go and console clients, and keep `buf lint` and `buf breaking` clean. Verify the console typechecks with the regenerated clients.
- [ ] 2.2 Implement the read in pbx-core: a `ConfigService.SipRegistrar` authorized with the extension-read action for the caller's tenant, and the RPC handler mapping it. Verify with application tests (authorized, refused, not configured) and wire tests (another tenant refused, every field mapped).

## 3. Wiring and proof

- [ ] 3.1 Wire the settings into the composition root, set `SIP_PUBLIC_HOST=localhost` for the rig in compose, and document all three in `.env.example`. Verify with an e2e test against the running app: the configured address is returned, and a caller without permission is refused.
- [ ] 3.2 Update API.md and LLD-03, run every backend tier (unit, integration, e2e, UATs), and record the results. Verify `make ci` and `make quality` pass.
