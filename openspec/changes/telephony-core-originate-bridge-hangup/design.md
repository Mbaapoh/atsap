## Context

Nothing of this bounded context exists yet: today the repo has a bare ARI
client (`api/internal/ari`), a bare AMI client (`api/internal/ami`), and a
health-check HTTP server — no domain model, no persistence, no eventing,
no public API. Constraints already fixed and not reopened by this design
(see `docs/lld/LLD-01-telephony-core-walking-skeleton.md` for the full
detail this design draws from): ports & adapters with `telephony-core` as
the first Tier-0 context built (`docs/TRD.md` dependency graph), the
transactional-outbox pattern for domain events (`docs/hld/01-architecture.md`
§3.2), and `tenant_id` on every row from commit one (D-24).

## Goals / Non-Goals

**Goals:**
- Prove the real call path — a genuine PJSIP-to-PJSIP call through
  Asterisk — end to end through the actual domain model, ACL, Postgres
  persistence, NATS eventing, and one API method, per LLD-01 §8's
  Definition of Done.
- Establish the port boundaries (`LicenseManager`, `ComplianceEngine`)
  correctly now, even though only stub adapters exist behind them, so
  LLD-02 replaces the adapters without touching `telephony-core`.

**Non-Goals:** as listed in `proposal.md`'s "Explicitly out of scope"
section (hold, transfer, IVR, real licensing/compliance, recording,
WebRTC, emergency handling) — not restated here. Additionally, out of
scope for *this design specifically*: crash recovery of in-flight calls
(see Risks below), and any rollback tooling beyond a plain down-migration
(deployment automation is a later change).

## Decisions

**In-memory correlation registry, ChannelHistory persisted.** The
Asterisk-channel-to-Participant mapping lives in an in-process map, not a
durable store; only the resulting `ChannelHistory` rows are persisted.
*Alternative considered:* persist the live registry itself (e.g. in
Postgres or NATS KV) so a restarted process could recover in-flight
calls. Rejected for this change — it adds real complexity before the
happy path is even proven, and the platform already accepts "a call on a
lost node ends" as a known limitation (`docs/DECISIONS.md`, "Known and
accepted limitations"). Revisit only if call preservation is separately
funded, per that same note.

**Stub adapters behind real port interfaces, not skipped calls.**
`LicenseManager.ValidateCapacity` and `ComplianceEngine` are called for
real from the `Screening` state, against `alwaysPermit*` adapters.
*Alternative considered:* skip the calls entirely until LLD-02 lands.
Rejected — per LLD-01 §5.3, the walking skeleton is supposed to prove the
*shape* of the system including where these checks sit, not just the
media path in isolation.

**Relocate `ari`/`ami` under `telephony/acl` now, not later.** Moves
`api/internal/ari` → `api/internal/telephony/acl/ari` and
`api/internal/ami` → `api/internal/telephony/acl/ami` in this change,
rather than leaving them in place and only documenting the boundary rule.
*Alternative considered:* leave the packages where they are and rely on
code review to enforce "nothing outside the ACL imports these."
Rejected — the Dependency Invariant Test
(`docs/hld/01-architecture.md` §6) is meaningless if the packages it's
supposed to gate aren't actually under the path it checks.

**Schema managed by forward-only `golang-migrate` migrations from the
first migration.** *Alternative considered:* a single hand-run
`init.sql` for this first slice, formalizing migrations later. Rejected —
`docs/TOOLSET.md` (D-34/ADR-014) and the T-12 expand/contract discipline
are established project-wide decisions; starting with an ad hoc script
would mean redoing this migration properly later for no benefit now.

**Dev-tenant seed is a deletable migration, not a code path.** A second
migration (`0002_dev_tenant_seed`) inserts one fixed-UUID tenant, gated by
`ATSAPBX_SEED_DEV_TENANT`. *Alternative considered:* build minimal real
tenant creation now instead of seeding a fixed row. Rejected — `identity`
is a separate Tier-0 context with its own later change; duplicating a
sliver of it here would create two tenant-creation paths to reconcile.

**Dev-test endpoints as static `pjsip.conf` entries, not a temporary test
harness service.** *Alternative considered:* stand up a small SIP softphone
simulator as part of this change. Rejected as unnecessary moving parts —
two static PJSIP endpoints in the existing dev config are enough to drive
a real call through Asterisk, and they're explicitly temporary fixtures
(proposal.md), not the `pbx-core` Extension feature.

**Outbox worker runs as a narrow `BYPASSRLS` platform role.** The worker
must read rows across *all* tenants to publish each tenant's events, which
tenant-scoped RLS would forbid. A dedicated role (`atsap_outbox_worker`,
`BYPASSRLS`) is granted `SELECT`/`UPDATE` on the `outbox` table only —
never on tenant domain tables. Tenant-facing queries without
`app.tenant_id` keep failing closed; INV-10 governs tenant interfaces, and
the outbox worker is platform infrastructure (HLD `03-domain-model.md` §5
now states this exception). *Alternative considered:* per-tenant sweep
(`SET LOCAL app.tenant_id` per tenant) — rejected: needs the tenant list,
which `identity` (Tier-0) does not provide yet, and would serialise the
publisher on every row. *Alternative considered:* drop RLS from `outbox` —
rejected: contradicts the agreed DDL and invites copy-paste misuse.

**App uses its own `atsapbx` database and role, not Asterisk's `asterisk`
CDR/CEL database.** *Alternative considered:* share the `asterisk` database
the `cdr`/`cel` tables live in. Rejected — the app's domain tables are RLS
multi-tenant data owned by a scoped app role; mixing them into the
CDR-writer's database couples two very different privilege models and makes
the `cdr_pgsql` user's credentials a path toward tenant tables. Dev stack
provisions `atsapbx` alongside `asterisk`.

## Risks / Trade-offs

- **[Risk]** In-memory correlation registry loses ACL state for calls in
  flight on a process crash. → **Mitigation:** accepted platform-wide
  limitation, already documented; not addressed by this change.
- **[Risk]** Relocating `ari`/`ami` breaks the existing `ari-playground`
  CLI's imports silently. → **Mitigation:** update `ari-playground`'s
  imports as a task in this change; run it manually against a dev
  Asterisk as a smoke check before considering the change done.
- **[Risk]** Stub adapters get mistaken for "good enough" and never
  replaced. → **Mitigation:** each stub file carries an explicit comment
  naming LLD-02 as its replacement; `docs/lld/README.md`'s status table
  tracks this.
- **[Risk]** The dev-tenant seed migration gets applied somewhere it
  shouldn't. → **Mitigation:** off by default, requires an explicit env
  var; must be down-migrated and deleted before LLD-02's real tenant
  provisioning lands (tracked as a task in that later change, not this
  one).
- **[Risk]** The `BYPASSRLS` outbox-worker role gets copied to tenant
  tables. → **Mitigation:** the role is created with grants scoped to
  `outbox` only; a test asserts the worker role cannot read `calls`/
  `call_participants`; HLD `03-domain-model.md` §5 documents the exception
  as narrow and non-reusable.
- **[Risk]** ARI WebSocket drop mid-call misses events while the process
  lives. → **Mitigation:** out of scope for this change — the reconnect
  loop reattaches and the in-memory registry survives a WS drop; a
  re-adoption/resync slice is tracked for later `telephony-core` work. The
  "re-derive after a WS drop" wording in LLD-01 §4.1 is rationale for
  setting the channel var, not an in-scope resync mechanism.

**Findings from implementation (2.1-2.4), corrected in both this change
and the authoritative HLD DDL:**
- `ENABLE ROW LEVEL SECURITY` alone does nothing for the `atsapbx_app`
  role, because that role **owns** every table its own migrations create,
  and PostgreSQL exempts a table's owner from its own RLS policies unless
  `FORCE ROW LEVEL SECURITY` is also set. `TestRLSIsolation` caught this
  (tenant B could see tenant A's row) before it shipped. Fixed by adding
  `FORCE ROW LEVEL SECURITY` for every RLS table, in both
  `api/migrations/0001` and `docs/hld/03-domain-model.md` §5 (so later
  LLDs copying that DDL pattern don't repeat the gap).
- `SET LOCAL app.tenant_id = $1` is not valid PostgreSQL — `SET`/`SET
  LOCAL` do not accept bind parameters. `WithTenant` uses
  `SELECT set_config('app.tenant_id', $1, true)` instead (the
  parameterized equivalent; the third argument is `is_local`).
- The dev-tenant seed gate (`ATSAPBX_SEED_DEV_TENANT`) cannot live inside
  `0002_dev_tenant_seed.up.sql` itself — a migration file has no way to
  read an environment variable. The gate is implemented in
  `postgres.MigrateUp`'s `seedDevTenant` parameter, which calls
  `m.Migrate(1)` (schema only) instead of `m.Up()` when the seed is
  disabled.

**`ports.CallStore` has a fourth method beyond HLD 04 §1's exact shape.**
Task 4.1 says "matching docs/hld/04-bounded-contexts.md §1 exactly," but
`CallStore` here also declares `RecordUsageTicks` — HLD's `CallStore` has
only `SaveCall`/`GetCall`/`AddChannelHistory`; usage recording is HLD's
`reporting` context's `UsageRecorder.RecordUsageSecond` (04 §5). Deliberate
deviation, not an oversight: `reporting` is a later Tier-1 context (TRD
dependency graph) that doesn't exist yet, and D-24's usage-ticking seam
must work from this first walking skeleton, not wait for it. Noted here
so the "exactly" claim doesn't silently rot — when `reporting` lands,
either `RecordUsageTicks` moves to a `UsageRecorder` port there, or this
note is updated to say why it stayed. Don't refactor it as part of
finishing this change; that's its own later decision.

## Migration Plan

No live system exists yet, so this is schema bring-up, not a cutover:
1. Apply `0001_telephony_core.up.sql` (schema + RLS) to a fresh dev
   Postgres.
2. Optionally apply `0002_dev_tenant_seed.up.sql` for local testing.
3. Rollback path (`*.down.sql` for both) is exercised in this change's
   tests, since T-12's expand/contract discipline applies from the first
   migration even though full deployment automation is a later change.

## Open Questions

- Exact Asterisk bridge `type` string to pass to `CreateBridge`
  (`mixing` vs a more specific type) — an implementation detail that
  doesn't change the spec, the approach, or the task breakdown; resolved
  during implementation against the running Asterisk version in `core/`.
