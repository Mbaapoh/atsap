## Why

Two LLDs in, the platform can place a call between two extensions that were
hand-written into `core/conf/pjsip.conf` as dev fixtures. **No customer can
create an extension at all**, and PRD principle 4 requires that they never
touch a configuration file to do it. D-46 makes "install, licence, and make
and receive real calls, configured entirely in the UI" the definition of
Phase A, and nothing in Phase A can start until an extension can be created
through the API and a phone can register to it.

This is also the change that establishes the D-47 projection pattern —
domain row in, live Asterisk state out, no reload — which every later
`pbx-core` change reuses. Being wrong about it is cheapest here, where the
surface is one table.

## What Changes

- **New bounded context `pbx-core`** under `api/internal/pbx/`, its first
  slice: the `Extension` aggregate, its store, and its ConnectRPC surface.
- **Extension lifecycle over the wire**: `CreateExtension`, `ListExtensions`,
  `UpdateExtension`, `DeleteExtension` on a new `PbxService`, each authorized
  through `identity` and scoped by tenant.
- **Tenant-local extension numbers**: the same number in two tenants is two
  independent extensions with independent credentials.
- **Generated SIP credentials**: the platform generates the secret, stores
  only its MD5 HA1, and returns the plaintext exactly once at creation. A
  user never chooses an extension password.
- **The Asterisk projection** (`api/internal/pbx/acl/asterisk/`): each
  extension write also writes `ps_endpoints`/`ps_auths`/`ps_aors` **in the
  same transaction**, so a phone can register the moment the write commits
  and deprovisioning takes effect just as immediately. No file is generated
  and no reload is issued.
- **A restricted database role** for the engine: `asterisk_engine` gets
  `SELECT` on the three projection tables and no access to any domain table.
- **Registration status** surfaced as a domain-level state on the extension
  resource (`REGISTERED` / `NOT_REGISTERED`) — never an Asterisk one.
- **`atsap-api pbx reconcile`**: diffs projection against domain and reports,
  with `--fix` to repair. Guards against hand-edited engine state, which a
  transaction cannot.
- Migration `0004_pbx_core.up.sql` with RLS on the domain table, deliberately
  **no** RLS on the projection tables, and the engine role and its grants.
- **HLD correction**: `extensions.password_hash` is renamed `secret_digest`.
  The current name asserts something impossible — SIP digest authentication
  cannot use a one-way slow hash. Not a breaking change: the column has never
  been created.

Not in this change, and deliberately: trunks, routes, call placement,
inbound routing, emergency handling (the next three Phase A changes), and
everything Phase B (IVR, queues, voicemail).

## Capabilities

### New Capabilities

- `pbx-core/extension-management`: extensions as durable tenant-scoped
  records — creation, listing, update, deletion, tenant-local numbering,
  generated credentials shown once, and registration state as something the
  platform observes rather than something a user sets.
- `pbx-core/endpoint-projection`: the guarantee that a configured extension
  becomes live engine state atomically and immediately, that removing it
  deprovisions it just as immediately, that the engine's own identifiers
  never surface to any caller, and that the engine's database access is
  confined to the projection.

### Modified Capabilities

None. `identity` and `telephony-core` requirements are unchanged: this
change authorizes through `identity` as an existing consumer, and does not
touch `telephony-core` at all.

## Impact

- **New code**: `api/internal/pbx/{domain,ports,application,acl/asterisk,postgres,rpc}`,
  `api/proto/atsapbx/v1/pbx.proto`, migration `0004_pbx_core.{up,down}.sql`.
- **Changed code**: `cmd/atsap-api` composition (wire the new service);
  `api/internal/shared/domain` gains `ExtensionID`.
- **Unchanged, and asserted so**: `api/internal/telephony/**`. If this change
  requires editing it, the seam has failed and work stops — the same rule
  LLD-02 applied to itself.
- **Infrastructure**: `core/conf/` gains `sorcery.conf`, `extconfig.conf` and
  `res_pgsql.conf`. `sorcery.conf` must restate the config-file wizard
  alongside the realtime one, or the existing `pjsip.conf` fixtures 1000/1001
  disappear — observed during the D-47 spike.
- **Docs**: `docs/API.md` §1 gains the four RPCs (the docs gate fails
  otherwise); HLD `03-domain-model.md` §5 gains the column rename, the
  projection tables, and the engine role.
- **Dependencies**: none added. `pgx/v5`, ConnectRPC and `slog` are already
  approved and in use.
