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

That is the whole wire surface at present. It is thin on purpose for
LLD-01 (a walking skeleton proves a path before specifying it — D-25),
and **too thin from LLD-02 onward**, which is what D-43 corrects.

### Authentication

Not yet enforced. `auth-cutover-connectrpc` (LLD-02 §9) makes
`Authorization: Bearer <JWT>` mandatory on every RPC. Until then calls
are unauthenticated and `GetCall` accepts a caller-supplied `tenant_id`
— a documented, temporary walking-skeleton concession, not the intended
end state.

After the cutover: a token comes from `AuthenticateUser` (§3), the
request's `tenant_id` must match the token's, and a resource belonging
to another tenant returns `not_found` rather than anything that reveals
it exists (INV-10).

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

### LLD-02 — identity & licensing

Change `identity-api`, sequenced **before** `auth-cutover-connectrpc`
and required by it: without a wire-level `AuthenticateUser` there is no
way to obtain a token in production, so enforcing JWTs would lock every
caller out.

| RPC | Service | Consumer |
|---|---|---|
| `AuthenticateUser` | `IdentityService` | Portal login; partner developers; the UAT harness after cutover |
| `ProvisionTenant`, `ProvisionPrincipal` | `IdentityService` | EPIC-01 admin stories; the portal's first screens |
| `SetTenantStatus`, `SetPrincipalStatus` | `IdentityService` | AC-01.5/01.7 — suspend and disable are console actions |
| `GrantRole` | `IdentityService` | EPIC-01 RBAC management |
| `IssueAPIKey`, `RevokeAPIKey` | `IdentityService` | Partner key self-service (US-05.1) |
| `ListAudit` | `IdentityService` | Audit review and compliance export (AC-01.3) |
| `ApplyLicenseKey`, `GetLicenseStatus` | `LicensingService` | Partner licence application and entitlement display (EPIC-06); `licensing-apply-key` |

### LLD-03 — pbx-core

Extensions, trunks, routing, and IVR flows are all console-managed, so
each needs CRUD on the wire (US-05.1). Expected: `Extension*`,
`CarrierTrunk*`, `CarrierRoute*`, `IvrFlow*` (including publish/version),
plus whatever call control survives LLD-03's reshaping — this is where
`InitiateCall` and `HangupCall` are decided.

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

## 4. Compatibility

- `buf breaking` runs against every proto change. Once an RPC has a
  consumer, its request/response shape is a contract.
- Versioning is in the package path (`atsapbx.v1`). A breaking change
  means `v2` alongside `v1`, never a silent reshape of `v1`.
- No infrastructure identifier — Asterisk channel IDs above all — appears
  in any response, ever. `telephony-core`'s spec makes this a
  requirement with an automated scan behind it, not a convention.
