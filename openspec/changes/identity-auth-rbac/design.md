## Context

See `proposal.md` — Why. The full design source is
[`docs/lld/LLD-02-identity-licensing.md`](../../../docs/lld/LLD-02-identity-licensing.md);
this document records only what is specific to this first change of LLD-02's
five, and the decisions taken at change level rather than LLD level.

Constraints inherited from the current codebase:

- `identity` is Tier 0 (HLD `04-bounded-contexts.md` §10.1): it may depend on
  no other bounded context, and nothing about this change may edit
  `internal/telephony/**` — LLD-02 §1's tripwire.
- The running e2e suite (`internal/telephony/e2e`) and OpenCode's baresip UAT
  scripts call `GetCall` with no credentials. They must keep passing.
- Migration `0002_dev_tenant_seed` is gated by `ATSAPBX_SEED_DEV_TENANT`, and
  `postgres.MigrateUp` implements that gate by stopping at version 1 when the
  seed is disabled. Dev databases therefore sit at version 2; CI and
  production sit at version 1.
- `docs/TESTING.md` §1: integration tests sharing the dev database run with
  `-p 1`.

## Goals / Non-Goals

**Goals:**

- A usable `identity` bounded context: provision tenants and principals,
  authenticate them, authorize their actions, and audit their mutations.
- Credential and token handling that holds up to the OWASP failure modes named
  in LLD-02 §10.9 — specifically A02 (crypto), A07 (auth), A09 (logging).
- Every new table tenant-isolated at the database level, proven by test, not by
  reading the migration.

**Non-Goals:**

- Enforcing authentication on live RPCs — that is `auth-cutover-connectrpc`,
  the last change in LLD-02 §9. This change builds and tests the interceptor
  and leaves it unwired.
- Anything in `licensing` — the `AlwaysPermit` license stub stays exactly as
  it is; `licensing-capacity-grace` replaces it next.
- Rate limiting and brute-force lockout (LLD-02 §10.5 — belongs at the
  connection boundary, in a later LLD).
- Refresh tokens, JWKS rotation, OIDC federation (LLD-02 §5.3 — no consumer
  exists; building them now is the over-building D-08 rules out).

## Decisions

### Four capabilities rather than one

LLD-01 produced a single capability (`telephony-core/call-lifecycle`) because
a call's lifecycle is one coherent behaviour. Identity is not: "who exists",
"who you are", "what you may do", and "what was done" have independent
requirements and independent readers. Delta specs become living specs at
archive, so the split is chosen for how `openspec/specs/identity/` should read
in a year, not for this change's convenience.

*Alternative considered:* one `identity/access` capability holding everything.
Rejected — it would put audit immutability and password length bounds in the
same document, and the next identity change would have to modify one large
spec instead of adding to a small one.

### `AuthenticateUser` takes a tenant, unlike HLD 04 §7's literal signature

HLD 04 §7 writes `AuthenticateUser(ctx, username, password)`. The same
HLD's §5 DDL declares `UNIQUE(tenant_id, username)`, and AC-01.2 requires
that two tenants may each have an `admin` as unrelated accounts. Those
cannot both hold: with usernames unique only per tenant, a login with no
tenant has no single principal to resolve to.

The port therefore takes `tenantID`. This is the HLD being
under-specified rather than wrong — every other part of it assumes
per-tenant usernames — but it is a deviation from the written signature
and is recorded here rather than quietly absorbed, the same way LLD-02 §4
records `ValidateCapacity` keeping its explicit `tenantID`.

*Alternative considered:* resolve the tenant from a subdomain or an
`Authorization` realm before reaching the port, keeping the HLD signature
intact. Rejected for now — it moves a lookup that belongs to identity out
into whichever transport happens to call it, and no transport with that
notion exists yet. Worth revisiting when the portal defines how a user
names their organisation at login.

### The interceptor ships unwired

The auth interceptor is written and unit-tested here, but `cmd/atsap-api` does
not install it. LLD-02 §9 sequences the cutover last precisely so that
providers exist before consumers are forced to use them; wiring it now would
break the e2e suite and the UAT rig in the same commit that introduces auth,
making a failure ambiguous between "auth is wrong" and "callers have no
tokens yet".

*Alternative considered:* install the interceptor in permissive mode (validate
if a token is present, allow if absent). Rejected — a permissive auth path that
exists in production wiring is exactly the configuration that survives longer
than intended. Off is unambiguous; the cutover change turns it on once.

### JWT is built on the standard library

`crypto/ed25519` + `encoding/json` + `encoding/base64`, no JWT library.
`docs/TOOLSET.md` vets no JWT dependency, and adding one would require the
TOOLSET §6 protocol for a component whose entire surface here is: encode two
JSON objects, sign the concatenation, verify it. Ed25519 is already the
primitive LLD-02 uses for licence tokens (T-1), so the same key handling
applies to both.

*Alternative considered:* `golang-jwt/jwt`. Rejected for this change — it
brings algorithm negotiation, a parser accepting many algorithms by default,
and a dependency-approval step, in exchange for code we do not need. If JWKS
rotation or federation ever lands, that is a new change with its own TOOLSET
§6 vetting.

### The validator never reads `alg` from the token

The acceptable algorithm is a fixed expectation held by the validator. A token
declaring `none`, or any algorithm other than the one this system issues, is
rejected before signature verification runs. This removes the "alg confusion"
class of attack structurally rather than by careful coding — there is no
negotiation to get wrong.

### Argon2id at fixed, stated parameters

19 MiB memory, 2 iterations, parallelism 1 — the current OWASP Password Storage
minimum, fixed in code rather than left to a library default that may change
under us. API keys stay SHA-256: they are high-entropy machine-generated
values, so the slow-hash defence against guessing a low-entropy secret does not
apply, and HLD 04 §7 already specifies SHA-256 for them.

### API keys carry their own tenant ID

Keys are formatted `atsa_<tenant-id>_<random>`. Found during
implementation, not designed up front: `api_keys` is RLS-protected, so a
lookup by digest alone returns zero rows whatever the digest — the
integration test failed exactly this way against the first design, which
assumed an unscoped lookup. RLS was right and the port contract was
wrong.

Embedding the tenant makes API-key authentication a tenant-scoped read
like every other one. The tenant ID is not a secret — it is the caller's
own and appears in their every request — and the unguessable part is
still the 256 random bits after it. Naming a tenant authenticates
nothing: a forged key naming a real tenant fails the digest comparison,
which is what actually proves possession, and there is a test asserting
precisely that.

*Alternative considered:* a narrow `BYPASSRLS` role for the key lookup,
following the outbox-worker precedent. Rejected — the outbox worker
bypasses RLS over an events table, whereas this would put a standing
cross-tenant read path over a credentials table, which is a materially
worse thing to own. *Also considered:* deferring API-key authentication
entirely; rejected because the table and generation are already in
scope, and a credential store nothing can authenticate against is
half a feature.

### Audit redaction happens at construction

The audit record is stripped of credential material when it is built, not when
it is read. A read-time filter can be bypassed by any new query path; a
constructor cannot. `actor_id` is a bare UUID rather than a foreign key to
`principals`, because system-initiated mutations have no principal — those use
a well-known sentinel with `actor_type` distinguishing them.

### `0002_dev_tenant_seed` is tombstoned, not deleted outright

LLD-01 said the seed should be "deleted, not left in place". Deleting both
files breaks `golang-migrate` on any database already at version 2 — every
developer's local stack — because the library cannot resolve what follows a
version whose files are absent. The seed's *behaviour* is removed (the up
migration becomes empty, and `0003` drops any dev tenant row it created), while
an empty version-2 tombstone preserves chain integrity.

*Alternative considered:* delete both files and require every dev database to
be dropped and recreated. Rejected as a worse trade: it breaks the rig for
everyone, mid-sequence, to save one empty file — and the UAT rig's Postgres
volume also carries the `asterisk` CDR/CEL database, so dropping it is not
free.

### Boundary enforcement needs no new rules

`internal/archtest` and `.golangci.yml`'s `depguard` both police imports of
`acl/ari` and `acl/ami`. `identity` imports neither, so no rule changes. The
architecture test's whole-module scan already covers new packages
automatically — `identity/**` is inside `internal/` and gets scanned the day
it exists, with no registration step.

## Risks / Trade-offs

- **Migration chain breakage on existing dev databases** → tombstoned version
  2 (above), plus a task that runs `migrate up` from *both* a version-1 and a
  version-2 starting state, since only the latter reproduces the failure.
- **`config.SeedDevTenant` and `MigrateUp`'s gate become vestigial** → they are
  removed in this change. This touches `internal/config`, `internal/postgres`,
  and `cmd/atsap-api`, and — found during implementation — **one line under
  `internal/telephony`**: `postgres/callstore_integration_test.go` calls the
  shared `MigrateUp` and must match its new signature. Surfaced and approved
  rather than absorbed: LLD-02 §1's tripwire exists to catch the *port seam*
  failing — identity forcing changes on telephony's domain, application, or
  ports — not a test call site of shared infrastructure that belongs to
  neither context. Task 9.3 verifies zero *production* telephony files
  changed, and names this single test-file exception explicitly.
- **Removing the gate is a prerequisite, not a cleanup** → `MigrateUp` pinned
  to version 1 whenever seeding was off, so migration 0003 could never be
  applied in the normal path while the gate existed. Task 8.1 therefore runs
  before 1.3, out of numeric order.
- **Four new RLS tables, four chances to ship one without a policy** → LLD-01's
  lesson was that `ENABLE ROW LEVEL SECURITY` alone silently does nothing for
  the owning role. The RLS isolation test is extended to every new table, and a
  table with RLS enabled but no policy fails that test by returning zero rows
  for its own tenant.
- **Audit redaction depends on fields being tagged** → an untagged new
  sensitive field would be recorded. Mitigated by testing the constructor
  against a struct carrying known-sensitive fields, and by keeping the
  before/after capture on explicitly-listed fields rather than whole-struct
  reflection where practical.
- **A 15-minute token with no refresh may be short for a future portal** →
  accepted deliberately: no human-facing session exists yet, and the lifetime
  is one configuration constant to revisit when the portal (HLD 12) defines
  its own session UX.

## Migration Plan

1. `0002_dev_tenant_seed.up.sql` becomes an empty tombstone; its `.down.sql`
   likewise.
2. `0003_identity.up.sql` creates `principals`, `role_bindings`, `api_keys`,
   `audit_logs` — each with `ENABLE` + `FORCE ROW LEVEL SECURITY` and its
   `tenant_isolation_*` policy — and removes any dev tenant row the old seed
   created.
3. `0003_identity.down.sql` drops those four tables, returning the database to
   the telephony-core schema.
4. Rollback is `migrate down` one step; because this change installs no
   interceptor, nothing in the running system depends on the identity tables
   existing, so rollback carries no runtime consequence.

## Open Questions

- The exact role vocabulary beyond the `AGENT` default in HLD 03 §5's
  `principals` DDL (supervisor, tenant-admin, platform-admin naming) can be
  settled when `pbx-core` gives roles something to authorize against; this
  change needs only that role bindings match exactly and deny otherwise, which
  the specs already state without depending on the vocabulary.
