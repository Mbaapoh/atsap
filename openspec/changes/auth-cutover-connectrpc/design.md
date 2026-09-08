## Context

See `proposal.md` — Why. What matters for the approach is that **the
mechanism already exists**: `identity/application.AuthInterceptor`
authenticates a bearer token, loads a live `TenantContext`, and — through
a `TenantMatcher` — refuses a request whose body names a different
tenant, exempting system scope. `IdentityService` and `PbxService` are
both already mounted with it.

`TelephonyService` is mounted without it, with a comment saying so. This
change is therefore mostly a wiring change plus the test updates it
forces, not new security machinery.

## Goals / Non-Goals

**Goals:**

- No operation in the public API is reachable without a token, except
  `AuthenticateUser`.
- No handler derives its tenant from the request body alone.
- The change is provable by test rather than by inspection of `main.go`.

**Non-Goals:**

- Reworking the interceptor. If this change needs to modify
  `AuthInterceptor`, that is a signal the abstraction was wrong and
  should be surfaced, not absorbed.
- Rate limiting, token revocation lists, or refresh tokens. LLD-02 §10.5
  records rate limiting as deferred and documents where a limiter wraps
  the interceptor; that stays true and stays deferred.
- Any change to how tokens are issued or validated.

## Decisions

### D1: Keep `tenant_id` in the request and require it to match

`GetCallRequest.tenant_id` stays. The interceptor's existing
`checkBodyTenant` refuses a mismatch.

*Alternative — remove the field and take the tenant only from the token.*
Rejected on two grounds. It is a breaking proto change to a field every
existing caller populates, so `buf breaking` fails and every client
updates in lockstep. And it diverges from `IdentityService` and
`PbxService`, which both keep the field and match it — one API with two
conventions for the same thing is worse than a slightly redundant field.

The redundancy is not free of value either: a caller that sends the wrong
tenant gets a clear refusal rather than silently reading a different
tenant's data than it asked for.

### D2: The e2e suite authenticates rather than being exempted

`ExemptProcedure` exists and takes exactly one procedure
(`AuthenticateUser`). It would be trivial to exempt `GetCall` "for
tests".

Rejected: an exemption added for test convenience is indistinguishable
in production from an exemption added by mistake, and it is the exact
shape of the concession this change exists to remove. The e2e suite
obtains a real token by the fixture path `internal/pbx/e2e` already
uses — seed a tenant and an operator, call `AuthenticateUser`. That path
is proven and needs no new mechanism.

### D3: Prove the cutover with a test that fails if the interceptor is unmounted

Mounting is one line in `main.go`, and a revert or a bad merge silently
un-mounts it. The guard is a test asserting that an unauthenticated
`GetCall` is refused — the same guard `pbx-e2e-projection` added for
`PbxService`, which exists precisely because "it's wired correctly" is
not a property source review reliably catches.

### D4: `identity-bootstrap` is not a prerequisite and is closed

LLD-02 §9 sequenced `identity-bootstrap` ("dev-token minting for the
rig") before this cutover, on the assumption the rig would need a
token-minting path of its own.

It does not. The fixture path obtains a real token through the real RPC,
which is a better test anyway — a minted dev token proves the rig can
reach the API, not that a caller can. A second token-minting path would
also be a second thing to keep secure and a second thing that can be left
enabled in production by accident.

## Risks / Trade-offs

- **A caller outside this repository breaks** → there are none. The
  console does not exist and no partner has been onboarded, which is
  exactly why this is the last cheap moment. Recorded so that if that
  stops being true before this lands, the change is reconsidered rather
  than pushed through.
- **The UAT rig may call `GetCall` unauthenticated** → checked:
  `scripts/` contains no such call, and the walking-skeleton e2e reaches
  `GetCall` through the running app. If a rig script is found during
  apply that does, it is updated the same way the e2e suite is, not
  exempted.
- **Tightening auth can mask a functional failure as an auth failure**
  during apply → the e2e suite is run before and after the cutover, so a
  test that changes from pass to `401` is distinguishable from one that
  was already failing.

## Migration Plan

1. Add a `TenantMatcher` for telephony requests, alongside the existing
   `identityrpc.TenantID` matcher.
2. Mount `TelephonyService` with the interceptor in `cmd/atsap-api`,
   deleting the comment that documents the concession.
3. Update `internal/server/server_test.go` and the telephony e2e suite to
   authenticate.
4. Update `docs/API.md` §1: remove the concession, state the API-wide
   posture.
5. **Rollback**: revert the mount. The interceptor is unchanged, so
   nothing else has to move.

Deploy order does not matter — there is no client to update ahead of the
server.
