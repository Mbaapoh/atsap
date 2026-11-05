# Coverage — IVR menu execution

- Domain: menu validation for digits, prompt-less menus, missing/zero
  destinations, timeout bounds; open/closed/holiday semantics.
- Store integration: menu JSON/timeout/fallbacks survive publish and read
  back from the immutable revision.
- Application/inbound: an open flow with a menu becomes an IVR decision;
  destinations/owners are resolved; closed still routes closed.
- Telephony: answer + playback + DTMF routes to the branch; invalid and
  no-input fallbacks; timeout; prompt failure; unknown channel; timer is
  cleared on termination. ARI `ChannelDtmfReceived` parsing.
- RPC/console: menu/prompt/timeout/fallback fields validated, round-trip
  and editable; console coverage thresholds pass.
- Full integration, full e2e, race, console-check and phone UATs; lint,
  SCA, image scan/SBOM and docs. Unit 79.7% >= 79.7%.

## Not automated

RTP DTMF from a real handset is the manual UAT (`docs/uat/ivr-menu.md`);
the rig's SIP UA/Playwright suites do not yet drive DTMF. The menu state
machine itself is covered with the real media-gateway contract fake.
