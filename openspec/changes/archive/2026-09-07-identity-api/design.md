## Context

See `proposal.md` — Why. Design source is
[`docs/lld/LLD-02-identity-licensing.md`](../../../docs/lld/LLD-02-identity-licensing.md)
§6, as corrected by D-43.

Constraints from what already exists:

- `identity-auth-rbac` shipped the whole service layer and an
  `AuthInterceptor` that is built, tested, and deliberately not installed.
  This change installs it for one service.
- `TelephonyService` must keep working unauthenticated: the e2e walking
  skeleton and OpenCode's baresip UAT both call `GetCall` with no token.
- `mise run proto` now runs `buf lint` and `buf breaking`, so the new proto
  must satisfy `STANDARD` lint rules on the first commit.
- `check-docs.sh` check 4 fails if a proto RPC is missing from
  `docs/API.md`.

## Goals / Non-Goals

**Goals:**

- Every identity capability with a named consumer reachable over HTTP.
- Mutating operations authenticated from the moment they exist.
- A published OpenAPI document generated from the protos.
- Each endpoint proven over real HTTP, authenticated and unauthenticated.

**Non-Goals:**

- Enforcing auth on `TelephonyService` — that is
  `auth-cutover-connectrpc`, and its risk is breaking existing callers.
- Licensing RPCs — `licensing` does not exist yet.
- Pagination, filtering, or bulk operations. `ListAudit` takes a limit and
  nothing more; a partner needing cursors will say so, and inventing them
  now guesses at a shape (D-25).
- A hand-written API reference. Generated or nothing.

## Decisions

### Auth is enforced on this service now, not at the cutover

LLD-02 §9 framed enforcement as the cutover's job. Applied literally that
would ship `ProvisionTenant` and `GrantRole` reachable with no credentials —
anyone able to reach the port could create a tenant and make themselves its
administrator. The interceptor already exists and is tested; wiring it for
`IdentityService` costs one line at the composition root.

The cutover stays a separate change because its risk is different in kind:
it breaks callers that work today (the e2e suite, the UAT rig, any partner
script written in the meantime), and that needs staging. Adding auth to
endpoints that have no callers yet breaks nothing.

*Alternative considered:* ship the endpoints unauthenticated and enforce
everything at the cutover, as written. Rejected — it creates a window in
which the identity API is an open door, and windows like that are usually
found by someone else.

### `AuthenticateUser` is exempt, by necessity not by exception

A caller cannot hold a token before obtaining one. The interceptor therefore
skips exactly one method, matched by its full procedure name rather than a
prefix or pattern — an exemption list that can accidentally widen is worse
than no list.

### The first administrator comes from a CLI subcommand

Provisioning needs a token; a token needs a principal; a principal needs
provisioning. Something outside that cycle must create the first pair.

`atsap-api bootstrap` does it, running against the database with the
operator's own credentials. It works in production, which matters: the
dev-token path from `identity-bootstrap` is `//go:build dev` and refuses to
run outside `ATSAPBX_ENV=development`, so it can never serve this purpose.
The command refuses when any tenant already exists, so it cannot be used a
second time to mint another administrator.

*Alternatives considered:* a seeded default admin (rejected — a shipped
default credential is a vulnerability, and D-24's seed is exactly what
`identity-auth-rbac` just removed); an unauthenticated
`ProvisionFirstTenant` RPC guarded by an "is the database empty" check
(rejected — an unauthenticated mutating endpoint whose safety depends on
current data is a race, and it would remain reachable forever for the
benefit of one use).

### One `IdentityService`, not one service per aggregate

Tenants, principals, roles, keys and audit are all administered by the same
actors through the same console. Splitting them across four services would
multiply the auth wiring and the client stubs for no consumer benefit. If
audit later needs a different scaling or retention story, it can move — the
package path is versioned and a service can be added without reshaping the
existing one.

### OpenAPI is generated, and the generator understands Connect

A `buf` remote plugin emits OpenAPI v3 into `docs/api/`. Remote means no
local toolchain install, matching how `protocolbuffers/go` and
`connectrpc/go` already work.

The plugin must describe Connect's actual HTTP surface —
`POST /<package>.<Service>/<Method>`, Connect's error model — rather than a
grpc-gateway URL scheme this server does not serve. A document that
describes routes we do not expose is worse than none: it sends partners to
endpoints that 404.

*Alternative considered:* hand-write the OpenAPI file. Rejected — it drifts
from the contract within one change, which is the failure mode
`check-docs.sh` check 4 exists to prevent.

## Risks / Trade-offs

- **Wiring the interceptor touches the composition root, where the e2e rig
  lives** → `TelephonyService` is mounted without it, and a test asserts an
  unauthenticated `GetCall` still succeeds. If that test fails, the cutover
  happened by accident.
- **A bootstrap command is a privileged path** → it requires database
  credentials (not an API call), refuses once a tenant exists, and writes an
  audit record attributing the bootstrap to the system actor.
- **The OpenAPI plugin is a new dependency** → remote, pinned, permissive
  licence, recorded per TOOLSET §6. If no plugin describes Connect's scheme
  faithfully, publishing nothing beats publishing a wrong map, and the task
  says so.
- **Nine endpoints is a lot of surface to get right at once** → each is
  verified over real HTTP in both authenticated and unauthenticated states,
  and the spec's disclosure requirements are asserted against actual
  responses rather than reasoned about.

## Migration Plan

Additive. A new proto file and a new service mount; no existing message or
RPC changes, so `buf breaking` has nothing to report and existing callers
are unaffected. Rollback is unmounting the service — nothing depends on it
yet.
