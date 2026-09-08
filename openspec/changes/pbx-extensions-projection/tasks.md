## 1. Schema and engine access

- [x] 1.1 Write `api/migrations/0004_pbx_core.up.sql` creating `extensions` per HLD 03 §5 with the `password_hash` column renamed `secret_digest`, plus `ENABLE` and `FORCE ROW LEVEL SECURITY` and a `tenant_isolation_extensions` policy; verify by running `MigrateUp` against a scratch database and confirming the table, both RLS flags and the policy exist
- [x] 1.2 Add `ps_endpoints`, `ps_auths`, `ps_aors` to the same migration with **no** `tenant_id` and **no** RLS; verify with a test asserting each has no `tenant_id` column and `relrowsecurity` is false — the deliberate exception, asserted the way `licensing_state`'s is (D-24, D-39)
- [x] 1.3 Create the `asterisk_engine` role in `deploy/postgres/init/02-atsapbx.sql` (migrations run as `atsapbx_app`, which is deliberately `NOCREATEROLE` — see design D4) and grant it `USAGE` plus `SELECT` on exactly the three `ps_*` tables from the migration; verify by connecting as that role and asserting the three reads succeed
- [x] 1.4 Write `api/migrations/0004_pbx_core.down.sql` revoking the grants and dropping the tables and policy — **not** the role, which the bootstrap script owns (the division `0001` keeps for `atsap_outbox_worker`); verify a full up → down → up cycle leaves no residue and no error
- [x] 1.5 Update HLD `03-domain-model.md` §5 with the column rename, the projection tables and the engine role; verify `mise run docs` passes

## 2. Domain (pure, no I/O)

- [x] 2.1 Add `ExtensionID` to `api/internal/shared/domain` following the existing `TenantID` pattern; verify it compiles and round-trips through its string form in a unit test
- [x] 2.2 Implement `pbx/domain.Extension` and its validation (number form, display name, device type) with an explicit permitted-character allowlist for the number; verify unit tests cover accepted and rejected forms including empty, over-length and non-dialable characters
- [x] 2.3 Implement `NewExtensionCredential(username, realm string, rand io.Reader) (plaintext, digest string, err error)` producing a high-entropy secret and its MD5 HA1; verify a unit test with a fixed reader produces the documented HA1 for known inputs, and that two calls with `crypto/rand` never repeat
- [x] 2.4 Implement generation of an authentication username that is **not** derived from the extension number; verify a unit test asserts the username for extension "1000" is neither "1000" nor derivable from it

## 3. Ports and store

- [x] 3.1 Define `pbx/ports`: `ConfigService`, `ExtensionStore`, `EndpointProjector` (every method taking `pgx.Tx`), and the view/result types; verify the package compiles with no import of any adapter (`go build ./...`)
- [x] 3.2 Implement `pbx/postgres.ExtensionStore` with parameterized queries only, operating within a caller-supplied transaction; verify an integration test creates, reads, updates and deletes an extension under a tenant context
- [x] 3.3 Add the unique constraint behaviour for `(tenant_id, extension_number)`; verify an integration test proves a duplicate within one tenant is rejected and the same number in a second tenant succeeds
- [x] 3.4 Extend the RLS isolation test to `extensions`; verify a row written under tenant A is invisible under tenant B's context

## 4. The Asterisk ACL projector

- [x] 4.1 Implement `pbx/acl/asterisk` with the `e_<hex uuid>` identifier derivation; verify a unit test proves the identifier is derived from the extension UUID and never from the extension number
- [x] 4.2 Implement `ProjectExtension` writing `ps_endpoints`, `ps_auths` (`auth_type=md5`, `md5_cred`, no `password`) and `ps_aors` inside the caller's transaction; verify an integration test finds all three rows after commit and asserts the `password` column is empty
- [x] 4.3 Implement `RemoveExtension` deleting all three rows in the caller's transaction; verify an integration test finds none of them after commit
- [x] 4.4 Implement `RegistrationStatus` as a batch read returning a map for a slice of IDs; verify an integration test issues one query for several extensions and returns the correct state for each
- [x] 4.5 Add a `depguard` rule confining `ps_endpoints`/`ps_auths`/`ps_aors` to `internal/pbx/acl/asterisk`, and forbidding any import between `internal/telephony` and `internal/pbx`; verify `mise run lint` fails when the rule is deliberately violated and passes when it is not

## 5. Application layer

- [x] 5.1 Implement `pbx/application` extension creation: authorize via `identity`, validate, generate the credential, then write the domain row, the projection and the audit row in one transaction; verify an integration test confirms all three exist after success
- [x] 5.2 Prove atomicity in both directions with injected failures; verify a test asserts a failing projection leaves no `extensions` row, and a failing domain write leaves no `ps_*` row
- [x] 5.3 Implement update, delete and credential regeneration with the same transactional shape; verify integration tests cover each, including that regeneration invalidates the previous secret
- [x] 5.4 Implement list with registration status attached from the batch read; verify an integration test returns the correct state per extension
- [x] 5.5 Redact `secret_digest` and the generated plaintext at the audit-entry constructor; verify a test asserts no audit row for any extension mutation contains either value

## 6. ConnectRPC surface

- [x] 6.1 Write `api/proto/atsapbx/v1/pbx.proto` defining `PbxService` with `CreateExtension`, `ListExtensions`, `UpdateExtension`, `DeleteExtension`; the generated secret appears **only** in the create/regenerate response; verify `mise run proto` passes lint and breaking checks
- [x] 6.2 Implement `pbx/rpc` handlers with authorization on every method and tenant matching against the token; verify tests cover authorized success, unauthorized refusal, and cross-tenant access returning the same "not found" outcome as a nonexistent identifier (INV-10)
- [x] 6.3 Assert no engine identifier crosses the wire; verify a test walks every field of every `PbxService` response and error for an `e_` prefixed value, an engine endpoint name or a routing context, the way LLD-01 asserts it for channel IDs
- [x] 6.4 Wire `PbxService` into `cmd/atsap-api` composition; verify the server starts and the four methods are reachable over HTTP+JSON
- [x] 6.5 Add the four RPCs to `docs/API.md` §1; verify `mise run docs` passes — check 4 fails if a proto RPC is undocumented

## 7. Engine configuration

- [x] 7.1 Add `core/conf/res_pgsql.conf` and `core/conf/extconfig.conf` mapping the three `ps_*` tables; verify `realtime show pgsql status` reports a connection after rebuild
- [x] 7.2 Add `core/conf/sorcery.conf` listing **both** the config-file wizard and the realtime wizard for endpoint, auth and aor; verify `pjsip show endpoints` still lists the 1000/1001 fixtures after restart — omitting the config wizard removes them (design D5)
- [x] 7.3 Pin the SIP realm in configuration and thread it into credential generation; verify a test asserts the realm used for HA1 generation matches the configured value

## 8. Reconcile command

- [x] 8.1 Implement `atsap-api pbx reconcile` diffing `ps_*` against `extensions` and reporting divergence; verify a test hand-edits a projection row and confirms the divergence is reported
- [x] 8.2 Implement `--fix` repairing the projection from the domain; verify the same test repairs and reports no divergence afterwards, and that without `--fix` nothing is changed

## 9. End-to-end verification

- [x] 9.1 Create an extension through the public API and register a real SIP client against it with no reload; verify the registration returns `200 OK` and the extension reports `REGISTERED`
- [x] 9.2 Delete that extension and verify the same client can no longer register, with no reload issued
- [x] 9.3 Verify a wrong secret is refused with `401` and the extension stays `NOT_REGISTERED`
- [x] 9.4 Create extension 1000 in two tenants, register to one, and verify only that tenant's extension becomes registered (design D2's correctness constraint)
- [x] 9.5 Connect as `asterisk_engine` and verify `SELECT` fails on `extensions`, `principals` and `calls` while succeeding on the three `ps_*` tables (design D4)
- [x] 9.6 Verify `telephony-core`'s existing walking-skeleton e2e still passes unchanged, and that `git diff --stat api/internal/telephony/` is empty — the seam check this change must not break

## 10. Close-out

- [ ] 10.1 Run the full local gate: `mise run docs`, `mise run lint`, `mise run proto`, `mise run test` (unit, integration and e2e tiers) and `mise run diagrams`; verify all pass
- [ ] 10.2 Verify every scenario in both delta specs has a corresponding passing test, and record any that do not with the reason — an unmapped scenario is a gap, not a formality
