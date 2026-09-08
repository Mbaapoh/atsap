## Context

See `proposal.md` — Why. The design source is
[`docs/lld/LLD-03-pbx-core.md`](../../../docs/lld/LLD-03-pbx-core.md);
this document records only what that LLD leaves to the change, plus the
decisions taken here.

Constraints that shape everything below:

- **D-47 settled the mechanism**, empirically: PJSIP Realtime, tested on our
  own Asterisk 22.8.2. ARI cannot create a PJSIP endpoint
  (`403 "Cannot create sorcery objects of type 'endpoint'"`), so the engine
  reads projected rows directly.
- **HLD 04 §10.1**: `pbx-core` may depend on `telephony-core` and `identity`.
  This change depends on `identity` only; it does not touch `telephony-core`
  at all.
- **HLD 01 §1.2**: `domain` imports nothing, `application` imports only
  `domain` and `ports`, engine vocabulary lives behind an ACL (D-41).
- **PRD principle 4**: no engine identifier reaches any caller.

## Goals / Non-Goals

**Goals:**

- Establish the projection pattern — one transaction, domain row plus engine
  rows — that the three following Phase A changes reuse unchanged.
- Prove the pattern against a real registering device, not a mock.
- Keep the engine's blast radius small enough that being wrong about the
  projection cannot expose tenant data.

**Non-Goals:**

- Performance work on registration status. One query per listed page is
  accepted; a presence cache is Phase B (AC-03.3's 3-second requirement is
  not attempted here).
- Any form of routing. An extension created by this change can register and
  be dialled by the existing walking-skeleton path; deciding *where* a call
  goes is the next two changes.
- Multi-node registration visibility. `ps_contacts` stays in node-local
  `astdb` (D-08, D-47).

## Decisions

### D1: The projector takes the caller's transaction

`EndpointProjector` methods accept a `pgx.Tx` rather than opening their own
connection, so the domain write and the engine write cannot commit
independently.

*Alternative — projector owns its own transaction, called after the domain
commit.* Rejected: it reintroduces exactly the split-brain that choosing
Realtime over generate-and-reload was meant to eliminate. A crash between the
two commits leaves an extension the console shows and no phone can use, and
the window is invisible.

*Alternative — outbox row, projected asynchronously by a worker.* Rejected
for this table: the outbox exists for events crossing a process boundary
(INV-06), not for state the same database already holds. It would add latency
to a write that has no reason to be eventual, and reintroduce the apply
window the whole decision removes.

### D2: Projected identifiers are `e_<hex uuid>`, derived not stored

`ps_endpoints.id = "e_" + hex(extensions.id)`. Derived rather than stored as a
column, because a stored mapping is a second source of truth that can drift
from the first.

The prefix distinguishes extension endpoints from the trunk endpoints the next
change adds, and makes engine-side state legible to an operator reading it
directly. It is never returned to a caller — asserted by test over the whole
RPC surface, the way LLD-01 asserts it for channel IDs.

*Alternative — use the extension number.* Rejected outright: `ps_*` is one
flat namespace shared by every tenant, so two tenants' extension 1000 would
collide and one tenant's phone could register against the other's endpoint.
This is the single most important correctness constraint in the change.

### D3: Credentials are stored as the MD5 HA1, not plaintext and not Argon2id

SIP digest authentication requires the server to hold the plaintext or
`MD5(username:realm:password)`. A one-way slow hash cannot answer a digest
challenge, so `extensions.password_hash` as HLD 03 §5 names it is not
implementable as named — hence the rename to `secret_digest`.

Verified on Asterisk 22.8.2 on 2026-09-07, with `ps_auths.password` empty and
only `md5_cred` set: a real `REGISTER` returned `401` then **`200 OK`**, and a
wrong password returned `401`.

Three consequences, all requirements rather than notes:

1. **The realm is pinned in configuration.** HA1 is computed over it, so
   changing it invalidates every stored credential. Changing it is a
   migration, not a config edit.
2. **Secrets are generated, never chosen** (`crypto/rand`). An HA1 is
   password-equivalent and offline-crackable; a user-chosen password would be
   the weak link, and a generated one gives an attacker nothing reusable.
3. **The plaintext is returned exactly once**, the same contract as
   `IssueApiKey`.

*Alternative — `auth_type=userpass` with plaintext, protected by SIP/TLS.*
Rejected: transport encryption does not protect data at rest, and it would put
recoverable extension passwords in a table a second database role can read.

### D4: The engine gets its own database role with three grants

`asterisk_engine` receives `SELECT` on `ps_endpoints`, `ps_auths`, `ps_aors`
and nothing else. This replaces RLS for those tables, which cannot apply:
Asterisk connects as itself and cannot set a tenant context, so a policy would
hide every row from the one reader that needs them.

**Corrected during apply.** This decision originally said migration `0004`
creates the role. It cannot: `atsapbx_app`, the role migrations run as, is
deliberately `NOCREATEROLE` — `deploy/postgres/init/02-atsapbx.sql` says
"it must never itself bypass the RLS policies its migrations create" — and
attempting it fails with `permission denied to create role (SQLSTATE 42501)`.

The repository already solves this exact problem for `atsap_outbox_worker`,
whose `BYPASSRLS` also requires a superuser: the **bootstrap script creates
the role**, and the **migration grants narrowly once the tables exist**.
`asterisk_engine` follows that precedent unchanged.

*Alternative — grant `CREATEROLE` to `atsapbx_app`.* Rejected: it reverses a
deliberate security decision so that one migration is self-contained, and a
role that can create roles can create one more privileged than itself.

The role is created `NOBYPASSRLS` on purpose. It must never see a domain
table, so it has no reason to bypass a policy; its isolation from tenant data
is the *absence of any grant*, not a policy exemption.

Isolation for `ps_*` is therefore *by construction* (D2's globally unique
identifiers) and the grant is the enforcement. Because that inverts the usual
control, it gets a direct test: connect as `asterisk_engine` and prove
`SELECT` fails on `extensions`, `principals` and `calls`.

### D5: `sorcery.conf` restates the config-file wizard

Adding a realtime wizard *replaces* the default config-file wizard rather than
supplementing it. Observed during the D-47 spike: the `pjsip.conf` fixtures
1000/1001 vanished until both wizards were listed. Both are listed, and the
regression is covered by the e2e suite continuing to pass — those fixtures are
what it dials.

### D6: Reconciliation is operator-invoked, never scheduled

`atsap-api pbx reconcile` reports divergence; `--fix` repairs it, treating the
domain as truth.

*Alternative — a periodic reconciler.* Rejected: a transaction already
prevents drift caused by our own code, so any drift that appears is either a
bug or a human editing the database. Silently repairing on a timer hides both.
Reporting loudly and repairing on demand keeps the signal.

### D7: Registration status is a batch read

`RegistrationStatus` takes a slice of extension IDs and returns a map. A
per-row call would make one screen N queries against engine state. Status is
read at request time from the engine's own view, not cached and not stored on
the domain row — storing it would create a second thing that can be stale.

## Risks / Trade-offs

- **Trunk and extension secrets are readable by `asterisk_engine`** →
  unavoidable for SIP: the engine must present credentials. Mitigated by D4's
  three grants, by D3 (HA1 rather than plaintext), and by never returning or
  logging the value after creation. Recorded as residual risk, not solved.
- **`res_config_pgsql` is Asterisk *extended* support, not core** → if it
  disappoints, switching to `res_config_odbc` (core) needs one Debian package
  in `core/Dockerfile` and a DSN. No schema change, no code change, no
  decision reopened.
- **A pinned realm is a one-way door** → changing it later invalidates every
  credential. Mitigated by fixing it in configuration now and documenting the
  consequence; it is a migration if it ever changes.
- **Registration status costs a query per page** → accepted for Phase A, sized
  by the page, and superseded by the Phase B presence work.
- **The projection pattern is new and three later changes copy it** → which is
  precisely why it is first, and why DoD-level tests here cover both failure
  directions (D1) rather than only the happy path.

## Migration Plan

1. **Prerequisite**: the `asterisk_engine` role must exist. Fresh installs get
   it from `deploy/postgres/init/02-atsapbx.sql` at bootstrap. **An existing
   database needs it created once, out of band** — the same one-time step
   `atsap_outbox_worker` required, and a dev stack whose volume predates this
   change needs it before `0004` will apply (D4).
2. `0004_pbx_core.up.sql` creates `extensions` (RLS enabled **and** forced,
   tenant isolation policy), the three `ps_*` tables (no RLS, no `tenant_id`,
   deliberately), and grants `USAGE` plus `SELECT` on those three tables to
   `asterisk_engine`.
3. `core/conf/` gains `sorcery.conf`, `extconfig.conf`, `res_pgsql.conf`; the
   Asterisk image is rebuilt. Both sorcery wizards are listed (D5).
4. Deploy is ordered: migration first, then the engine config, then the app.
   The engine reading empty projection tables is harmless; the app writing
   projection rows the engine is not yet reading is not.
5. **Rollback**: `0004_pbx_core.down.sql` revokes the grants and drops the
   tables. It does **not** drop the role, which is the bootstrap script's to
   own — the same division `0001` keeps for `atsap_outbox_worker`. Reverting
   `core/conf/` and rebuilding restores file-only sorcery. The existing
   `pjsip.conf` fixtures are untouched throughout, so `telephony-core`'s e2e
   remains the rollback smoke test.

## Open Questions

- What the engine's `asterisk_engine` password is managed by in production —
  environment, file, or secret store — is a deployment concern that does not
  change the schema, the grants, or any task here. It is settled with the
  Phase A deployment work.
