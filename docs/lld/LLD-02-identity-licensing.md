# LLD-02 — Identity & Licensing

| | |
|---|---|
| **Bounded contexts** | `identity`, `licensing` (both Tier 0 — see [`04-bounded-contexts.md` §10](../hld/04-bounded-contexts.md#10-bounded-context-build--dependency-graph)) |
| **Status** | Draft — not yet proposed as an OpenSpec change |
| **Traces to** | BRD FBR-R1-04/FBR-R1-08/FBR-R1-12/§10; PRD EPIC-01 (US-01.1–01.7, AC-01.1–01.7), EPIC-06 (US-06.1–06.7, AC-06.1–06.10), PRD §11.2 (licence states), INV-01/INV-03/INV-10/INV-11; TRD Domain model, extensibility seams; HLD [01-architecture.md](../hld/01-architecture.md), [03-domain-model.md](../hld/03-domain-model.md) §5, [04-bounded-contexts.md](../hld/04-bounded-contexts.md) §§4/7, [05-security.md](../hld/05-security.md) §1, [11-decisions.md](../hld/11-decisions.md) (T-1–T-3); DECISIONS D-11–D-14, D-24 |
| **Why this is LLD-02** | LLD-01 proved the media path against stub adapters whose files say, verbatim, "until LLD-02 lands." Every Tier 1 context needs `identity`'s tenant context (§10.1), and without real licensing there is no sellable product — capacity enforcement, not dialing features, is what makes the platform commercial. D-26 (dependency-first within a release) puts both here, ahead of `pbx-core`. |

## 1. Scope

**In scope (this LLD only):**
- `identity`: tenant provisioning (replacing and **deleting** the `0002_dev_tenant_seed` migration — LLD-01: "deleted, not left in place"), principals with Argon2id password hashes, scoped RBAC (`AuthorizeAction`), API keys stored as SHA-256 hashes, JWT issue/validate, append-only audit log (AC-01.3), tenant-context middleware feeding RLS.
- `licensing`: the full HLD 04 §4 `LicenseManager` port (`ValidateCapacity`, `ReleaseCapacity`, `ApplyLicenseKey`, `VerifyDailyEntitlement`) — LLD-01 stubbed only `ValidateCapacity`; this LLD lands the other three methods on the same interface. Ed25519 licence tokens (T-1), atomic capacity counter + 10% burst allowance (T-2/T-3), weighted 3-of-5 hardware fingerprint with tolerance (D-13), 7-day offline grace with day-2 warnings and degrade-never-disable (D-12/D-14).
- Auth cutover for ConnectRPC: `Authorization: Bearer <JWT>` required on all RPCs; request `tenant_id` must equal the JWT tenant (mismatch → `InvalidArgument`). Proto unchanged — no `buf breaking` impact.
- Swapping both stubs behind their existing ports in `cmd/atsap-api` composition. **Zero `telephony-core` changes**: if this LLD requires editing anything under `internal/telephony/`, the seam failed and work stops.

**Explicitly out of scope (deferred to the LLD that owns it):**

| Deferred | Owning LLD | Why not now |
|---|---|---|
| Real DNC/hours/spend compliance | LLD-04 | `stub_compliance.go` stays permit-all; nothing consumes real verdicts yet |
| Inbound dial-in handling (incl. SIP `503`+`Retry-After` on capacity, emergency-number recognition/bypass) | LLD-03 (`pbx-core`) | No inbound path exists yet — every call today is originated by test/app, and originated calls are never emergency calls. INV-01 bypass is therefore untriggerable; LLD-03 owns both the recognition and the bypass wiring |
| Recording, RTCP-XR, `Degraded` transitions | LLD-04/07 | Unchanged from LLD-01 §1 |
| Webhooks, AI pipeline, dialer | LLD-05, LLD-06 | Tier 1/2 in the dependency graph |
| Partner-portal licence issuance UI | EPIC-10 surface (portal) | `ApplyLicenseKey` port + file/CLI application suffice here |

## 2. Go package layout

New bounded-context roots beside `telephony/` (HLD 01 §1.1), plus two shared-kernel IDs:

```
api/internal/
├── shared/
│   └── domain/          # ADD: PrincipalID, ApiKeyID (typed UUID wrappers, same pattern as TenantID)
├── identity/            # NEW bounded context root
│   ├── domain/          # NEW: Tenant (provisioning), Principal, RoleBinding, ApiKey, AuditEntry; pure password-policy helpers
│   ├── ports/           # NEW: IdentityService (exactly HLD 04 §7) + TenantContext carrier
│   └── application/     # NEW: orchestrator (provision/authenticate/authorize/audit), Argon2id hasher, JWT issuer/validator, ConnectRPC auth interceptor
├── licensing/           # NEW bounded context root
│   ├── domain/          # NEW: LicenseToken (Ed25519 verify, pure), HardwareFingerprint (weighted 3-of-5 match, pure), CapacityCounter (atomic + burst window), GraceTracker (pure state machine over timestamps)
│   ├── ports/           # NEW: full LicenseManager (all four HLD 04 §4 methods — supersedes LLD-01 §5.1's single-method stub port)
│   └── application/     # NEW: capacity monitor, daily entitlement worker, key-apply flow; DELETES telephony/application/stub_license.go by replacing its wiring
└── telephony/
    └── application/     # DELETE stub_license.go only (stub_compliance.go stays for LLD-04)
```

**Why two contexts, one LLD:** they are independently replaceable modules that share one build-order tier and one cutover (auth + entitlement activate together); splitting them into two LLDs would manufacture a fake dependency between Tier-0 peers.

## 3. Domain types

```go
// identity/domain
type Tenant struct {
    ID             shared.TenantID
    Name           string
    Status         TenantStatus // Active | Suspended
    ResidencyZone  string       // carried from LLD-01 tenants row, now enforced
}
type Principal struct {
    ID           PrincipalID
    TenantID     shared.TenantID
    Username     string        // UNIQUE(tenant_id, username) — AC-01.2 overlap lives here
    Email        string
    PasswordHash string        // Argon2id, never plaintext anywhere (INV-11)
    Role         string
    Status       PrincipalStatus // Active | Disabled
}
type RoleBinding struct {
    PrincipalID PrincipalID
    TenantID    shared.TenantID
    Role        string
    Scope       string          // e.g. "tenant", "department:<id>"
}
type ApiKey struct {
    ID          ApiKeyID
    TenantID    shared.TenantID
    PrincipalID PrincipalID
    KeyHash     string          // SHA-256 of the presented key; raw key shown once at creation
    ExpiresAt   *time.Time
    RevokedAt   *time.Time
}
type AuditEntry struct {
    ID         int64 // BIGSERIAL, append-only
    TenantID   shared.TenantID
    ActorID    string
    ActorType  string
    Action     string
    Resource   string
    BeforeJSON []byte // nullable
    AfterJSON  []byte // nullable
    CreatedAt  time.Time
}
```

```go
// licensing/domain — pure where possible (D-21's shape, applied to licensing)
type LicenseToken struct {
    Edition    string
    Capacity   int       // concurrent channels
    ExpiresAt  time.Time
    InstanceID string
    Fingerprint [5]string // expected weighted attributes
}
func VerifyToken(payload, signature []byte, pubKey ed25519.PublicKey) (LicenseToken, error) // pure, T-1

type HardwareFingerprint struct{ Attrs [5]string }
func (f HardwareFingerprint) Matches(expected [5]string) bool // >=3 of 5 equal, D-13

type CapacityVerdict struct {
    Permitted bool
    Reason    string // distinct telemetry reason code (BR-05)
}
// CapacityCounter is atomic across threads (T-2); burst allowance is a
// 10%-over-capacity, 15-minute sliding window (T-3). Both live behind the
// LicenseManager port; telephony-core only ever sees the verdict.
type GraceTracker struct {
    LastConfirmedAt time.Time
    WarnSinceDay2   bool
}
// GraceState is a pure function of (now, lastConfirmedAt): Valid →
// EntitlementUnverified (warnings from day 2) → Degraded (reduced
// capacity, never disabled — D-12). Emergency calls connect in every
// state; that bypass lives with the caller (LLD-03), never here.
func GraceState(now, lastConfirmedAt time.Time) GraceStatus
```

Password hashing uses `golang.org/x/crypto/argon2` (already in `go.mod`/TOOLSET.md) — no new dependency. Ed25519 uses stdlib `crypto/ed25519` — no new dependency.

## 4. Ports (exactly HLD 04 §§4/7, superseding the LLD-01 stub subset)

```go
// identity/ports — exactly HLD 04 §7
type IdentityService interface {
    AuthenticateUser(ctx context.Context, username, password string) (*AuthToken, error)
    ValidateToken(ctx context.Context, tokenString string) (*TenantContext, error)
    AuthorizeAction(ctx context.Context, principal PrincipalID, action string, resource string) error
    RecordAudit(ctx context.Context, entry AuditEntry) error
}
// TenantContext carries tenant_id + principal_id + roles downstream into
// RLS (SET LOCAL app.tenant_id) and audit. Produced only by ValidateToken
// or API-key validation — never hand-constructed by callers.

// licensing/ports — exactly HLD 04 §4 (all four methods; LLD-01 §5.1's
// single-method stub port is retired by this LLD, not extended beside it)
type LicenseManager interface {
    ValidateCapacity(ctx context.Context, requestedChannels int) (CapacityVerdict, error)
    ReleaseCapacity(ctx context.Context, channels int) error
    ApplyLicenseKey(ctx context.Context, signedPayload []byte) error
    VerifyDailyEntitlement(ctx context.Context) (EntitlementStatus, error)
}
```

Auth enforcement point: a ConnectRPC unary+streaming interceptor in
`identity/application` (wired in `cmd/atsap-api`), calling
`ValidateToken`, rejecting missing/invalid tokens (`Unauthenticated`)
and body-tenant/JWT-tenant mismatch (`InvalidArgument`). `rpc/handler.go`
reads the tenant from context (falls back to body field only when no
auth context exists — i.e. in handler unit tests, never in production).

## 5. Data model

### 5.1 Migration scope (`api/migrations/0003_identity.up.sql`, `golang-migrate`)

Exactly the HLD [03-domain-model.md §5](../hld/03-domain-model.md) tables
this LLD owns — **not a new schema**, the agreed one, same as LLD-01 §6.1:

- `principals` (as HLD DDL: `id`, `tenant_id`, `username`, `email`,
  `password_hash`, `role`, `status`, `UNIQUE(tenant_id, username)`)
- `role_bindings`, `api_keys` (new tables, same column conventions)
- `audit_logs` (as HLD DDL: BIGSERIAL, actor/action/resource,
  before/after JSONB — **no UPDATE/DELETE policy path by construction**;
  RLS enabled + `tenant_isolation_audit` policy per the HLD-03 rule that
  no RLS table ships without its policy)
- `licensing_state` (new, minimal: `instance_id` PK, `fingerprint`
  JSONB, `edition`, `capacity`, `entitlement` status, `last_confirmed_at`,
  `grace_started_at`) — the only durable licensing state; counters stay
  in memory
- RLS enabled + `FORCE ROW LEVEL SECURITY` + `tenant_isolation_*`
  policies on every new table (the LLD-01 RLS lesson, now the house rule)

### 5.2 Deletions and dev bootstrap

- **Delete** `api/migrations/0002_dev_tenant_seed.{up,down}.sql`
  (LLD-01: "deleted, not left in place"). No replacement seed: tests and
  the e2e create their own tenants (already the established pattern);
  local dev provisions via the new `AuthenticateUser` bootstrap flow
  documented in `docs/TESTING.md` §6 follow-up.
- Dev-only token minting for rig/test use (`ATSAPBX_DEV_TOKEN_*`,
  env-gated, never prod): lands here so existing UAT scripts and the e2e
  keep working after the auth cutover. The UAT runbook's `curl GetCall`
  examples gain the `Authorization: Bearer` header at that point.

## 6. ConnectRPC surface (this LLD: auth on existing methods + identity methods)

No `.proto` shape changes (no `buf breaking` impact — deliberate):
`GetCall` keeps its fields; auth arrives via metadata. New RPCs for
`AuthenticateUser` (username/password → short-lived JWT) and tenant
provisioning land here only if EPIC-01's stories need them over the wire
in R1.0 — otherwise identity stays a Go-port surface until the portal
slices (HLD 12) demand otherwise. (API-first (D-24) is satisfied either
way: every *capability* is reachable; the decision of wire-vs-port per
method is recorded in the proposing change, not smuggled in.)

## 7. Definition of Done

1. Tenant provision → principal → JWT → authenticated `GetCall` succeeds;
   bad password / expired token / unknown tenant all fail closed with
   distinct codes (AC-01.1–01.3).
2. Overlapping extension-number-style identity across tenants never
   collides: same username in two tenants are independent principals with
   independent sessions (AC-01.2 pattern).
3. Disabling a principal kills sessions/calls-within-10s behavior at the
   token-validation layer; suspending a tenant blocks new calls, leaves
   active calls running, fully reversible (AC-01.5/01.7 patterns).
4. Every admin mutation writes exactly one immutable `audit_logs` row
   (actor, timestamp, before/after); no role — including a second admin —
   can edit or delete it (AC-01.3/01.4).
5. Tampered licence payload rejected (`ApplyLicenseKey`); weighted
   fingerprint tolerates 2-of-5 drift, fails closed beyond it with
   warning-first behavior (AC-06.1/06.2/06.9).
6. Capacity counter exact under a 10× setup-rate race test, zero drift;
   burst allowance absorbs a 15-minute overage then rejects with the
   distinct telemetry reason; active calls never dropped at any point
   (AC-06.6/06.10, INV-03).
7. Grace state machine: unreachable entitlement service → full function
   with day-2+ warnings → degraded reduced capacity after day 7, never
   disabled (AC-06.4/06.5); daily worker covered by test with injected
   clock.
8. `go-arch-lint`/AST + `depguard` green with the two new contexts
   present; RLS isolation test extended to the new tables; `tenant_id`
   absent from no new row, event, or log (D-24 seam 1, D-39).

## 8. Known limitations of this LLD (carried, not introduced)

- Inbound dial-in handling does not exist yet, so SIP `503`+`Retry-After`
  rejection and emergency-number bypass have no path to trigger — both
  are LLD-03's ingress work reusing the verdicts built here.
- Single-node capacity counting: the atomic counter is per-process.
  Multi-node shared counting is a later scaling change, explicitly not
  this LLD (D-08: nothing over-built for year-three scale).
- `compliance` stays a stub (LLD-04 owns real DNC/hours/spend).

## 9. OpenSpec handoff

When ready to implement: `/opsx:propose "identity provisioning, auth, and
licensing capacity/grace"` (Claude Code) or `/opsx-propose` (OpenCode).
`openspec/config.yaml`'s `context:` already surfaces the docs baseline;
point the agent at this file as the design source. Expect several
changes from this LLD (e.g. `identity-provisioning-auth`,
`licensing-capacity-grace`, `auth-cutover-connectrpc`), each proposed,
applied, and archived independently per `docs/lld/README.md` granularity.
