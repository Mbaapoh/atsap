## 1. Shared kernel

- [x] 1.1 Create `api/internal/shared/domain` with `TenantID`, `CallID`, `ParticipantID` typed UUID wrappers and verify `go build ./...` succeeds
- [x] 1.2 Create `api/internal/shared/event` with a `DomainEvent` interface and envelope metadata (type, tenant ID, aggregate ID, timestamp) and verify a unit test constructs an event and reads its metadata back

## 2. Database schema

- [x] 2.1 Write `api/migrations/0001_telephony_core.up.sql`/`.down.sql` (`tenants` minimal, `calls`, `call_participants`, `channel_history`, `usage_seconds`, `outbox`, RLS enabled + policies per `docs/hld/03-domain-model.md` §5) and verify `migrate up` then `migrate down` both succeed against a fresh dev Postgres (verified via `TestMigrateUpDown`, integration-tagged; found and fixed a real gap: `ENABLE ROW LEVEL SECURITY` alone does not apply to the owning role, needed `FORCE ROW LEVEL SECURITY` too — see migration comment)
- [x] 2.2 Write `api/migrations/0002_dev_tenant_seed.up.sql`/`.down.sql`, gated by `ATSAPBX_SEED_DEV_TENANT`, and verify the seed row appears only when the env var is set and `.down.sql` removes it cleanly (gate implemented in `postgres.MigrateUp`'s `seedDevTenant` bool, not in SQL — `SET`/migration files can't read env vars; verified via `TestMigrateUp_DevTenantSeedGate`)
- [x] 2.3 Implement `api/internal/postgres` (`pgxpool` wiring, `SET LOCAL app.tenant_id` helper) and verify an integration test confirms a query issued without tenant context returns zero rows (RLS Isolation Test pattern, `docs/hld/03-domain-model.md` §6) (verified via `TestRLSIsolation`; helper uses `set_config(..., true)` since plain `SET LOCAL ... = $1` is not valid Postgres syntax)
- [x] 2.4 Create the `atsap_outbox_worker` `BYPASSRLS` platform role granted `SELECT`/`UPDATE` on `outbox` only (in the `0001` migration), and verify an integration test asserts the worker role can read rows across tenants while a query from that role against `calls`/`call_participants` returns zero rows (design.md "Outbox worker runs as a narrow BYPASSRLS platform role") (role *creation* moved to `deploy/postgres/init/02-atsapbx.sql`, run as superuser — granting `BYPASSRLS` requires superuser, which the `0001` migration's `atsapbx_app` role does not have and should not have; the migration only grants the narrow `SELECT`/`UPDATE` on `outbox` itself. Verified via `TestOutboxWorkerRole`)

## 3. Domain model (pure)

- [x] 3.1 Implement `Call`/`CallParticipant` structs and `CallState`/`ParticipantState` enums in `api/internal/telephony/domain` and verify unit tests cover every enum value
- [x] 3.2 Implement pure state-transition functions enforcing PRD §11.1 entry conditions (e.g. Active requires 2+ Connected participants) and verify unit tests cover the happy path and reject an invalid transition (e.g. Active with only 1 connected participant) (100% coverage; removed the `CallDegraded` branch from `DisconnectParticipant` as untestable dead code — nothing transitions into Degraded in this change)
- [x] 3.3 Implement per-second usage-tick generation (pure: given a connected interval, produce exactly one usage record per elapsed second) and verify a unit test asserts no duplicate and no missing second across a simulated multi-second interval

## 4. Ports

- [x] 4.1 Define `CallService`, `MediaGateway`, `CallStore`, `LicenseManager`, `ComplianceEngine`, `EventPublisher` interfaces in `api/internal/telephony/ports` matching `docs/hld/04-bounded-contexts.md` §1 exactly and verify the package compiles with no implementations yet
- [x] 4.2 Implement `alwaysPermitLicense` and `alwaysPermitCompliance` stub adapters in `api/internal/telephony/application`, each with a comment naming LLD-02 as its replacement, and verify unit tests confirm both always return a permitted verdict

## 5. Asterisk Anti-Corruption Layer

- [x] 5.1 Move `api/internal/ari` to `api/internal/telephony/acl/ari`, fix import paths in `ari-playground` and existing tests, and verify `go build ./...` and the existing `ari` package tests still pass at the new path (already done by commit 1b81c37 on this branch; verified `go build ./...` and `go test ./internal/telephony/acl/ari/...` pass)
- [x] 5.2 Move `api/internal/ami` to `api/internal/telephony/acl/ami` unchanged and verify existing `ami` package tests still pass at the new path (already done by commit 1b81c37; verified `go test ./internal/telephony/acl/ami/...` passes)
- [x] 5.3 Extend `acl/ari.Client.Originate` to accept `OriginateRequest{Endpoint, CallerID, ChannelID, Variables}` and inject `channelId`/variables on the ARI call, and verify a unit test (`httptest` server) asserts the outgoing request carries the expected `channelId` and variables (also updated the one existing caller, `cmd/ari-playground`)
- [x] 5.4 Add `CreateBridge`, `AddChannelToBridge`, `DestroyBridge`, `GetChannelVariable`, `SetChannelVariable` to `acl/ari.Client` and verify unit tests (`httptest` server) for each against the expected ARI REST path and method
- [x] 5.5 Implement the in-memory correlation registry (Asterisk channel ID → `{CallID, ParticipantID}`) with concurrent-safe access and verify a unit test run with `-race` exercises concurrent registration and lookup without a data race
- [ ] 5.6 Implement the ACL event-handling loop (`StasisStart`, `ChannelStateChange`, `ChannelDestroyed`/`ChannelHangupRequest` → domain effects, per design's event table) and verify an integration test replays a scripted ARI event sequence and asserts the resulting Call/Participant states match the expected happy-path progression
- [x] 5.7 Extend `OriginateRequest` with `Timeout` (alerting timeout, default 20s per `docs/hld/03-domain-model.md` §2.1) and inject the ARI `timeout` param, and verify a unit test (`httptest`) asserts the outgoing request carries it — this makes the spec's "Destination does not answer" scenario reachable (implemented as `TimeoutSeconds`, defaulted by the caller — the ACL/orchestrator, not the ARI client — since Asterisk's own default applies when zero)

## 6. Application layer (CallService orchestrator)

- [ ] 6.1 Implement the `CallService` orchestrator wiring domain transitions, ports, and the ACL, including Screening calling `LicenseManager` and `ComplianceEngine`, and verify a unit test drives `InitiateCall` through to `Active` using a fake `MediaGateway`/`CallStore` and the stub adapters
- [ ] 6.2 Implement `HangupCall` driving `Terminating` → `Terminated` with the record becoming immutable, and verify a unit test asserts a subsequent attempt to mutate a `Terminated` call is rejected
- [ ] 6.3 Implement per-second usage-tick scheduling for Connected participants, persisted via `CallStore`, and verify an integration test against a real dev Postgres shows continuous, non-duplicated `usage_seconds` rows across a multi-second simulated call

## 7. Outbox and NATS

- [ ] 7.1 Implement the outbox writer so each domain state change and its outbox row commit in the same transaction, and verify an integration test asserts both roll back together on a simulated failure after the domain write
- [ ] 7.2 Implement the outbox worker (`FOR UPDATE SKIP LOCKED`, batched, marks `published_at`) and the NATS JetStream publisher to `tenant.<tenant_id>.event.call.<action>` / `...event.participant.<action>`, and verify an integration test asserts a written outbox row is published to the expected subject and marked published

## 8. ConnectRPC API

- [ ] 8.1 Write `api/proto/atsapbx/v1/telephony.proto` (`GetCall` only, no `channel_id` field anywhere) and generate Go code via `buf`, and verify `buf generate` succeeds and the generated code compiles
- [ ] 8.2 Implement the `GetCall` handler returning `Call` + `Participants`, and verify an automated response scan (per `docs/hld/01-architecture.md` §6.2) asserts no Asterisk-channel-ID-shaped value appears anywhere in the response

## 9. Dev fixtures and wiring

- [ ] 9.1 Add two static PJSIP dev-test endpoints to `core/conf/pjsip.conf`, clearly commented as temporary fixtures (not the real `pbx-core` Extension feature), and verify Asterisk starts cleanly with `asterisk -rx "pjsip show endpoints"` listing both
- [ ] 9.2 Wire config, Postgres, NATS, and the ACL into `api/cmd/atsap-api/main.go`'s startup, and verify the binary starts against the local `docker compose` dev stack and still serves `/healthz`
- [ ] 9.3 Add the `nats` JetStream service to `deploy/docker-compose.yml`, and add `NATS_URL`/`DATABASE_URL` keys to `.env.example` and `config.Load` so the stack reaches Postgres and NATS (design.md "App uses its own atsapbx database")
- [x] 9.4 Provision the `atsapbx` app database + scoped app role in the dev `postgres` init (`deploy/postgres/init/`), alongside the existing `asterisk` CDR/CEL database, and verify `migrate up` against `atsapbx` creates the telephony-core schema there (`deploy/postgres/init/02-atsapbx.sql`; verified via `TestMigrateUpDown` and `TestOutboxWorkerRole` against a container bootstrapped with this exact script)

## 10. Walking-skeleton verification

- [ ] 10.1 Add the dependency-boundary lint/AST test asserting no package outside `internal/telephony/acl` imports `acl/ari` or `acl/ami`, and verify it fails on a deliberately introduced violation and passes once the violation is removed
- [ ] 10.2 Run the end-to-end walking-skeleton test: originate a call between the two dev PJSIP endpoints, verify it reaches `Active`, hang up either side, verify `Terminated` with a complete non-duplicated `usage_seconds` series and the expected NATS event sequence, and verify `GetCall` returns correct state with no channel ID present — this is LLD-01 §8's Definition of Done
