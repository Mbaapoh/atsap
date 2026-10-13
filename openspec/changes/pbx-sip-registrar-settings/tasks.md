## 1. Configuration

- [x] 1.1 Add `SIP_PUBLIC_HOST`, `SIP_PUBLIC_PORT` (default 5060) and `SIP_PUBLIC_TRANSPORT` (default `udp`) to `config.Config`, validated at load: the host as a bare hostname or IP, the port 1–65535, the transport `udp`, `tcp` or `tls`. Verify with config unit tests for defaults and for each invalid value. *(done 2026-10-13: `config.Config` gains SIPPublicHost, SIPPublicPort and SIPPublicTransport. Validation at load reuses pbx-core's `ValidateHost`, so trunk and registrar hosts follow one rule. Tests cover defaults, overrides, and seven invalid values, each refused with a message naming the setting. Unit suite and lint (0 issues) pass.)*

## 2. API

- [x] 2.1 Add `GetSipRegistrar` to `pbx.proto` (request: `tenant_id`; response: `configured`, `host`, `port`, `transport`), regenerate the Go and console clients, and keep `buf lint` and `buf breaking` clean. Verify the console typechecks with the regenerated clients. *(done 2026-10-13: the RPC and messages are added; `buf lint` is clean; `buf breaking` against develop is clean (additive); the Go stubs, console TypeScript and `docs/api/openapi.yaml` are regenerated. The console typechecks and its 185 tests pass; the public-procedure list picks up the new method from the descriptors.)*
- [ ] 2.2 Implement the read in pbx-core: a `ConfigService.SipRegistrar` authorized with the extension-read action for the caller's tenant, and the RPC handler mapping it. Verify with application tests (authorized, refused, not configured) and wire tests (another tenant refused, every field mapped).

## 3. Wiring and proof

- [ ] 3.1 Wire the settings into the composition root, set `SIP_PUBLIC_HOST=localhost` for the rig in compose, and document all three in `.env.example`. Verify with an e2e test against the running app: the configured address is returned, and a caller without permission is refused.
- [ ] 3.2 Update API.md and LLD-03, run every backend tier (unit, integration, e2e, UATs), and record the results. Verify `make ci` and `make quality` pass.
