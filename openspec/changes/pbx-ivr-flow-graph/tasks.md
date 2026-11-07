# Tasks — IVR flow graph

One commit per task. Run every relevant tier before ticking (HANDOFF §3
and §4). Integration runs on `atsapbx_test` only.

## 1. Domain (pure)

- [ ] 1.1 `Destination` value type and kind registry (extension, flow, external, hangup; queue/ring_group/voicemail reserved); unit tests
- [ ] 1.2 Revision graph types (schedule, menu, play, goto, hangup) and graph validation (D6 rules except cross-aggregate checks); unit tests
- [ ] 1.3 `Schedule` with IANA zone, weekday ranges, cross-midnight, local-date holidays and override states; unit tests including DST
- [ ] 1.4 Pure interpreter `Step` (D3), with re-prompt and direct-dial semantics (D8); table-driven unit tests
- [ ] 1.5 v1 → v2 conversion on read (D5); a test for every v1 shape proving identical routing

## 2. Persistence and application

- [ ] 2.1 Migration 0026 (`schema_version`, `graph`, `ivr_schedules` with RLS, `tenants.time_zone`) with down migration; store read/write; integration tests
- [ ] 2.2 Publish/activate validation with cross-aggregate checks (extensions, flows, outbound policy, schedules); audited; integration tests
- [ ] 2.3 Schedule CRUD and the audited manual override; integration tests
- [ ] 2.4 `DeleteExtension` guard (D7); integration test

## 3. Telephony execution

- [ ] 3.1 The IVR session steps the interpreter and executes commands over ARI; replace the D-90 single-menu path; unit tests with the media-gateway fake
- [ ] 3.2 Routing to `flow`, `external` (through placement policy) and `hangup` destinations; integration tests
- [ ] 3.3 E2e: a carrier call through a two-level flow with a schedule on the rig

## 4. API and console

- [ ] 4.1 Proto/OpenAPI/TS: graph, destination, schedule and override messages; v1 fields deprecated and translated; wire tests; `docs/API.md`
- [ ] 4.2 Console: tenant time zone, Schedules screen with override
- [ ] 4.3 Console: Call flows tree editor (menu → option → node, destination picker by kind); component and browser tests on the real API

## 5. Proof and docs

- [ ] 5.1 Extend `docs/uat/ivr-menu.md` (two levels, direct dial, re-prompt, weekend closed) and run it with a real handset
- [ ] 5.2 Docs: LLD-03 §3.2.1 marked implemented, PROGRESS, HANDOFF, coverage notes, ratcheted floors; then `/opsx:archive`
