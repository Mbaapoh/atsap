## Why

LLD-01 shipped telephony-core against an unauthenticated API and permit-all
stubs: `GetCall` trusts a caller-supplied `tenant_id` with nothing verifying
the caller may act for that tenant (`rpc/handler.go:39`), and
`application/stub_license.go` says verbatim "until LLD-02 lands". Every
Tier-1 bounded context — `pbx-core`, `reporting`, `webhook-delivery`,
`ai-pipeline` — depends on `identity`'s tenant context (HLD
`04-bounded-contexts.md` §10.1), so none of them can be built for real until
tenants, principals, and authenticated requests exist. This is the first of
five changes decomposed from LLD-02 (§9), and the one everything else
integrates against.

## What Changes

- **New `identity` bounded context** (Tier 0, no dependencies on any other
  context) at `api/internal/identity/{domain,ports,application}`, implementing
  HLD 04 §7's `IdentityService` port.
- **Tenant and principal provisioning**: tenants become real records with an
  immutable `residency_zone`; principals carry Argon2id password hashes, a
  role, and a status. Usernames are unique per tenant, never globally.
- **Argon2id password hashing** at the parameters fixed in LLD-02 §5.3
  (19 MiB, 2 iterations, parallelism 1), with 8–64 character length bounds
  and no forced composition rules.
- **JWT issue and validation** built on stdlib `crypto/ed25519` — no new
  dependency — with the accepted algorithm pinned to `EdDSA` at the
  validator (never read from the token's own `alg` header), a 15-minute
  token lifetime, and no refresh-token flow.
- **API keys** issued as high-entropy tokens, stored only as SHA-256 hashes,
  shown to the caller exactly once, revocable and expirable.
- **RBAC `AuthorizeAction`**, deny-by-default: a principal may act only when
  a role binding matches the (principal, action, resource) tuple; wildcards
  exist only in the scoped `tenant:*` / `system:*` admin forms.
- **Append-only audit log**: every provisioning mutation writes exactly one
  immutable row with actor, action, resource, and before/after state, with
  secrets stripped at construction rather than at read time.
- **A ConnectRPC auth interceptor is built and tested but NOT cut over.**
  Enforcing `Authorization: Bearer` on live RPCs is the separate, last
  change in LLD-02's sequence (`auth-cutover-connectrpc`); wiring it here
  would break the running e2e suite and UAT scripts, which hold no tokens
  yet. This change delivers the interceptor and its tests; the switch stays
  off.
- **Migration `0003_identity`** creates `principals`, `role_bindings`,
  `api_keys`, and `audit_logs` per HLD `03-domain-model.md` §5, each with
  RLS enabled, `FORCE ROW LEVEL SECURITY`, and its `tenant_isolation_*`
  policy — and **deletes** `0002_dev_tenant_seed`, which LLD-01 said should
  be "deleted, not left in place" once real provisioning exists.
- **No `.proto` changes** (identity stays a Go-port surface in this change,
  per LLD-02 §6) and **no `telephony-core` changes** — if this change needs
  to edit anything under `internal/telephony/`, the port seam failed and
  work stops (LLD-02 §1).

Deliberate scope calls, recorded rather than smuggled in: **audit logging is
included** even though LLD-02 §9's row-1 scope column does not name it,
because provisioning is the first admin mutation in the system and shipping
it unaudited would violate AC-01.3 for exactly the mutations this change
introduces. **API-key issuance is included** because LLD-02 §5.1 scopes the
`api_keys` table to this migration, and a table with no code path is worse
than a small complete capability.

## Capabilities

### New Capabilities

- `identity/tenant-provisioning`: tenants and principals as durable, governed
  records — creation, suspension/disablement, per-tenant username uniqueness,
  and immutable residency zone.
- `identity/authentication`: proving who a caller is — password verification,
  token issue/validation with algorithm pinning and expiry, API keys, session
  invalidation against live principal/tenant status, and failed-attempt
  visibility.
- `identity/access-control`: deciding what an authenticated caller may do —
  deny-by-default authorization, and the tenant-scoping rule that keeps one
  tenant's request from ever reaching another tenant's data.
- `identity/audit-log`: the immutable record of administrative mutations —
  one row per mutation, editable by no one, with credentials redacted before
  they are ever written.

### Modified Capabilities

None. `telephony-core/call-lifecycle` is deliberately untouched: this change
swaps what sits behind existing ports, and changes no telephony behavior.

## Impact

- **New code**: `api/internal/identity/{domain,ports,application}`;
  `PrincipalID` and `ApiKeyID` added to `api/internal/shared/domain`.
- **New migration**: `api/migrations/0003_identity.{up,down}.sql`; deletion of
  `api/migrations/0002_dev_tenant_seed.{up,down}.sql`.
- **Configuration**: JWT signing key material enters `internal/config` with
  distinct dev and production fields so the two can never be confused
  (LLD-02 §5.2).
- **Unchanged**: `api/proto/**` (no `buf breaking` impact),
  `api/internal/telephony/**`, the ARI/AMI ACL, and every existing test —
  the e2e walking skeleton and UAT scripts keep passing without tokens
  because the interceptor is not yet enforced.
- **Dependencies**: none added. Argon2id comes from `golang.org/x/crypto`
  (already present); Ed25519 and JWT serialization are stdlib.
- **Follows on**: `licensing-capacity-grace`, `licensing-apply-key`,
  `identity-bootstrap`, and `auth-cutover-connectrpc` (LLD-02 §9) — the
  cutover change is what finally requires a token on every RPC.
