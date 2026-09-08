# API Surface

The living inventory of what AtsaPBX exposes over the wire, what it
deliberately does not, and what each upcoming LLD will add.

**This file is updated by the change that changes the surface — not
afterwards.** `scripts/check-docs.sh` fails if an RPC exists in
`api/proto/**` and is not listed in §1 here, so the two cannot drift.

**The rule (D-43):** a capability gets an RPC in the change that builds
it when it has a named R1.0 consumer — the partner portal (EPIC-10), a
partner developer (US-05.1), or a test harness. Otherwise it stays a Go
port and the change records why. "Reachable in principle" is not
API-first; in-process Go calls are not an API.

Transport is ConnectRPC over HTTP (D-33), so every RPC below is callable
as gRPC, gRPC-Web, or plain HTTP+JSON at
`POST /<package>.<Service>/<Method>`.

---

## 1. Exposed today

| RPC | Service | Landed in | Consumer |
|---|---|---|---|
| `GetCall` | `atsapbx.v1.TelephonyService` | `telephony-core-originate-bridge-hangup` (LLD-01) | e2e harness; the portal's future call view |
| `AuthenticateUser` | `atsapbx.v1.IdentityService` | `identity-api` | Portal login; partner developers; the UAT harness after cutover |
| `ProvisionTenant` | `atsapbx.v1.IdentityService` | `identity-api` | EPIC-01 admin stories; the portal's first screens |
| `ProvisionPrincipal` | `atsapbx.v1.IdentityService` | `identity-api` | EPIC-01 admin stories; the portal's first screens |
| `SetTenantStatus` | `atsapbx.v1.IdentityService` | `identity-api` | AC-01.7 — suspend is a console action |
| `SetPrincipalStatus` | `atsapbx.v1.IdentityService` | `identity-api` | AC-01.5 — disable is a console action |
| `GrantRole` | `atsapbx.v1.IdentityService` | `identity-api` | EPIC-01 RBAC management |
| `IssueApiKey` | `atsapbx.v1.IdentityService` | `identity-api` | Partner key self-service (US-05.1) |
| `RevokeApiKey` | `atsapbx.v1.IdentityService` | `identity-api` | Partner key self-service (US-05.1) |
| `ListAudit` | `atsapbx.v1.IdentityService` | `identity-api` | Audit review and compliance export (AC-01.3) |
| `CreateExtension` | `atsapbx.v1.PbxService` | `pbx-extensions-projection` (LLD-03) | Console extensions screen; the only response carrying a generated secret |
| `GetExtension` | `atsapbx.v1.PbxService` | `pbx-extensions-projection` (LLD-03) | Console extension detail, with live registration state |
| `ListExtensions` | `atsapbx.v1.PbxService` | `pbx-extensions-projection` (LLD-03) | Console extensions list (flow A3) |
| `UpdateExtension` | `atsapbx.v1.PbxService` | `pbx-extensions-projection` (LLD-03) | Console rename and device-type change |
| `DeleteExtension` | `atsapbx.v1.PbxService` | `pbx-extensions-projection` (LLD-03) | Console deprovisioning |
| `RegenerateSecret` | `atsapbx.v1.PbxService` | `pbx-extensions-projection` (LLD-03) | Credential rotation; the second and last place a secret appears |

### Authentication

**Two services, two policies** (identity-api design):

- **`IdentityService` is authenticated from the moment it exists** —
  every RPC except `AuthenticateUser` requires
  `Authorization: Bearer <JWT>`. That one method is exempt by its exact
  procedure name, because a caller cannot hold a token before obtaining
  one. `ProvisionTenant` additionally requires **platform (system)
  scope**, so a customer's own administrator cannot create tenants.
- **`PbxService` is authenticated on every method.** All six mutate or
  read tenant configuration, so none is reachable without a token, and
  the tenant named in a request body must equal the token's — there is no
  system-scoped path, because a platform operator has no legitimate
  reason to create an extension inside a customer's tenant. Verified
  live: an unauthenticated call returns `401`, and a call naming another
  tenant returns `403` before the application layer is reached.
- **`TelephonyService` is authenticated too**, since
  `auth-cutover-connectrpc`. It previously accepted a caller-supplied
  `tenant_id` with no token, so anyone who could reach the port could
  read any tenant's call by naming it — a documented concession from
  LLD-02, made when no wire-level `AuthenticateUser` existed to mint the
  e2e suite a token. That is closed: `GetCall` requires a token and the
  body's tenant must match it.

**The posture is now uniform across the API.** Obtaining a token is the
only operation reachable without one, in any service. No operation
derives the tenant it acts on from the request alone — a caller-supplied
tenant never checked against a token is indistinguishable from no
tenancy at all.

For an authenticated request, the body's `tenant_id` must match the
token's (system-scoped callers excepted), and a resource belonging to
another tenant returns `not_found` rather than anything that reveals it
exists (INV-10).

Two standing guards, both proven by fault injection: an unauthenticated
`GetCall` returns `401`, and a valid token naming another tenant is
refused with `400` rather than a `404` — a `404` would mean the request
reached the store and merely found nothing.

The first tenant and its platform administrator are created off the
network by `atsap-api bootstrap` (identity-api 3.3), never by a seeded
default credential.

---

## 2. Implemented but deliberately not exposed

Capabilities that exist and work in Go, kept off the wire on purpose.
Each needs a reason, not merely an absence of demand.

| Capability | Context | Why it stays a port |
|---|---|---|
| `ValidateToken` | identity | Mechanism, not capability — it is what the auth interceptor does *for* a caller. Exposing it hands out a token-validity oracle |
| `AuthorizeAction` | identity | Same: the interceptor and handlers call it; a client asking "may I?" separately from doing the thing invites TOCTOU |
| `AuthenticateAPIKey` | identity | A key is presented as a header on the real request, never exchanged at an endpoint |
| `RecordAudit` | identity | A write path used by provisioning. Partners *read* audit; they do not author it |
| `InitiateCall` | telephony-core | Works and is e2e-proven, but `pbx-core` (LLD-03) reshapes what origination means once routing and extensions exist. Freezing the contract now is what D-25 warns against |
| `HangupCall` | telephony-core | Same reasoning; the likeliest early addition once the portal has a call view |
| `AnswerParticipant`, `HoldParticipant`, `TransferParticipant` | telephony-core | Not implemented at all — `ErrNotImplemented`, out of LLD-01 scope |

---

## 3. Pending, by LLD

What each LLD is expected to put on the wire. This is a plan, not a
promise: a capability arrives here only when it has a named consumer,
and the owning change is where that is decided and recorded.

### LLD-02 — licensing (identity landed)

`identity-api` shipped the identity surface (§1) ahead of
`auth-cutover-connectrpc`, which required it: without a wire-level
`AuthenticateUser` there was no way to obtain a token in production, so
enforcing JWTs would have locked every caller out. **Both have since
landed** — the cutover is complete and the whole API is authenticated
(§1). Licensing RPCs remain pending until the `licensing` context
exists, and are the only part of LLD-02 still outstanding.

| RPC | Service | Consumer |
|---|---|---|
| `ApplyLicenseKey`, `GetLicenseStatus` | `LicensingService` | Partner licence application and entitlement display (EPIC-06); `licensing-apply-key` |

### LLD-03 — pbx-core

Specified in [`lld/LLD-03-pbx-core.md`](lld/LLD-03-pbx-core.md) §6. All
of it lands on a new **`PbxService`**, not on `TelephonyService`:
`PlaceCall` needs routing before it needs a channel, and putting it on
`TelephonyService` would force `internal/telephony/rpc` to import
`pbx`, which the dependency graph forbids.

| RPC | Phase | Consumer |
|---|---|---|
| `CreateExtension`, `ListExtensions`, `UpdateExtension`, `DeleteExtension` | A | Console extensions screen |
| `CreateTrunk`, `ListTrunks`, `UpdateTrunk`, `DeleteTrunk` | A | Console trunks screen |
| `CreateRoute`, `ListRoutes`, `DeleteRoute` | A | Console outbound routing |
| `PlaceCall`, `HangupCall` | A | The UAT harness, which BRD §16 requires to demonstrate Phase A through the public API; the Phase B agent UI second |
| `IvrFlow*` (including publish, version, rollback) | B | Visual IVR builder |

**This resolves the `InitiateCall`/`HangupCall` question §2 left open**:
they stay ports on `telephony-core`, and `PbxService.PlaceCall` becomes
the wire surface, because a caller dials a number — resolving it to an
endpoint is the platform's job, not the client's.

Staying off the wire, with reasons: `SelectOutboundRoute` and
`ReportRouteHealth` (mechanism — asking which trunk would be picked,
separately from placing the call, is an oracle whose answer can change
between the two calls), `ResolveInbound` (called by `telephony-core`,
never by a client), and `IsEmergency` (INV-01 is not delegable to a
caller).

`TelephonyService.GetCall` stays where it is. Splitting the call read and
the call write across two services is awkward; it is recorded as a known
cost, and consolidation is a Phase B decision taken when the agent UI
shows which grouping a real client wants.

### LLD-04 — compliance & reporting

`ExportTenantBilling` and usage/CDR queries: US-05.4 is explicitly "pull
usage records granular enough to rate and invoice my own customers",
which is a partner-facing API by definition, not a report screen.

### LLD-05 — webhook-delivery & ai-pipeline

Webhook subscription management (US-05.2) and the live telemetry stream
(US-05.3). US-05.3 is a *stream*, so it is the first surface that needs
ConnectRPC server-streaming rather than unary.

### LLD-06 — dialer (R2)

Campaign and list management, pacing controls, agent state. R2-gated.

---

## 3a. Conventions

Fixed once, here, so that delivering the API one LLD at a time still
produces one coherent API. Every LLD from LLD-03 onward follows these;
where one must deviate, the owning change records why.

They exist because of a specific R1.0 gate (BRD §16): *"every function
demonstrated through the public API with published documentation, and the
administration console proven to use only those same endpoints."* The
console is not a privileged client with a private back door — it is a
consumer of this API like any partner's own software. In practice **all
configuration is performed through the portal, which translates it into
Asterisk configuration**: administration, call flows, IVR, contact-centre
setup, SIP trunks. So every one of those is an API operation, subject to
the same authorization as any other, and none of them is a hand-edited
file on the server.

### Naming

- `Verb` + `Noun`: `ProvisionTenant`, `ListAudit`, `RevokeApiKey`. Use
  `Get` for one, `List` for many, `Set` for a status transition, and a
  domain verb where one exists (`Revoke`, `Grant`, `Provision`) rather
  than a generic `Update`.
- Request and response messages are `<Rpc>Request` / `<Rpc>Response`,
  which `buf lint`'s STANDARD rules enforce anyway.
- Field names are `snake_case` in proto and stay singular unless the
  field is repeated.

### Identifiers and timestamps

- Every identifier crossing the wire is a UUID rendered as a string.
  Never an integer, never a database sequence — those leak volume and
  ordering.
- Every timestamp is `google.protobuf.Timestamp` in UTC, **set by the
  server and returned as stored**. Relying on a column default and
  echoing the pre-insert value produced `0001-01-01` responses once
  already; the value returned must be the value persisted.
- `audit_logs.id` is the one exception (BIGSERIAL), because ordering is
  its purpose.

### Errors

Connect codes, mapped consistently:

| Cause | Code |
|---|---|
| No or invalid credentials | `unauthenticated` |
| Authenticated but not permitted | `permission_denied` |
| Absent, or belonging to another tenant | `not_found` |
| Malformed input, or a body tenant that is not the caller's | `invalid_argument` |
| Uniqueness violated (username, extension number) | `already_exists` |
| Refused because of current state (suspended tenant, revoked key) | `failed_precondition` |
| Capacity or licence exhausted | `resource_exhausted` |

Messages are safe to show a user and disclose nothing about other
tenants. Every authentication failure returns the same code *and the same
message* whatever its cause — the distinction lives in logs (INV-10).

### Pagination

`List*` returning an unbounded collection takes `page_size` and
`page_token`, and returns `next_page_token` (empty when exhausted).
Opaque token, not an offset: offsets skip or repeat rows when the
underlying set changes between calls.

`ListAudit` currently takes only `limit` — the first `List*` written, and
before this convention existed. It gains the standard shape when a
consumer needs more than the newest page; noted here rather than left as
an inconsistency to be discovered.

### Mutation semantics

- A `Set*` carries the complete new value of what it names, so there is
  no "was this field omitted or cleared" ambiguity. Avoid partial
  updates; if one becomes unavoidable, use an explicit field mask and say
  so in the change.
- Creates are not idempotent by default. Where a retry must be safe, the
  request carries a caller-supplied idempotency key — decided per
  endpoint, never assumed.
- A mutation that succeeds writes exactly one audit record; a mutation
  that is refused writes none.

### Tenancy and permissions

- A request naming a tenant must name the caller's own, unless the caller
  holds `system` scope. The interceptor enforces this before the handler
  runs.
- **Every operation authorizes, not merely authenticates.** A valid token
  is identity, never permission — the handler calls `AuthorizeAction`
  (or `AuthorizeSystem` for installation-level operations such as
  creating a tenant). This applies to configuration endpoints exactly as
  it does to identity ones: who may edit a trunk, publish an IVR flow, or
  change a queue is a permission question with the same shape.
- Installation-level operations require `system` scope specifically. A
  tenant administrator's wildcard must never reach them.

### Configuration endpoints translate to Asterisk

Configuration written through the API becomes live Asterisk state —
endpoints, trunks, routes, IVR execution. Two consequences for the
contract:

- The API models the **domain**, not Asterisk. A partner configures an
  `Extension` and a `CarrierTrunk`; they never see `ps_endpoints`, a
  dialplan context, or a channel identifier. Same rule as
  `telephony-core`'s: no infrastructure identifier crosses the wire.
- Applying configuration is **not assumed to be instantaneous**. Where
  activation is asynchronous, the response says what was accepted and the
  resource carries its own applied//pending state, rather than a success
  that silently means "written to a table, not yet live."

> **Settled by D-47.** A configuration row becomes live Asterisk state
> through **PJSIP Realtime**: the ACL projects the domain row into
> ACL-owned `ps_*` tables that Asterisk reads directly, with no file
> generation and no reload. The mapping is owned by the ACL —
> `extensions` and `carrier_trunks` (HLD 03 §5) stay the source of truth
> and keep their own columns; `ps_*` is a read model for the engine.
> None of this reaches the API: a partner still configures an
> `Extension`, and the two rules above are unaffected. Because there is
> no reload, activation for these objects is in fact immediate — but the
> contract deliberately does **not** promise that, since other
> configuration may not be.

## 4. Compatibility

- `mise run proto` runs `buf lint` and `buf breaking` (against `main`).
  Once an RPC has a consumer, its request/response shape is a contract,
  and the gate is what keeps that true.
- Versioning is in the package path (`atsapbx.v1`). A breaking change
  means `v2` alongside `v1`, never a silent reshape of `v1`.
- No infrastructure identifier — Asterisk channel IDs above all — appears
  in any response, ever. `telephony-core`'s spec makes this a
  requirement with an automated scan behind it, not a convention.

## 5. Machine-readable specification

The `.proto` files under `api/proto/` **are** the specification: for a
gRPC/Connect API they are the design-first contract in the same way an
OpenAPI document is for REST, and they are what generates the server
interfaces, the clients, and the breaking-change gate.

A generated **OpenAPI v3 document is shipped with `identity-api`** —
**generated from the protos, never hand-written**, so it cannot drift
from the contract. It lives at [`docs/api/openapi.yaml`](api/openapi.yaml)
and is produced by `buf generate` via the pinned
`sudorandom-connect-openapi` remote plugin, which understands Connect's
HTTP semantics (`POST /<package>.<Service>/<Method>`, Connect's error
model) rather than grpc-gateway routes this server does not serve.
`TelephonyService` (`GetCall`) is not in the published document yet — it
has no external consumer today, and adding it would document a surface
nothing outside this repo calls.
