## Why

`TelephonyService.GetCall` is reachable with **no token at all**, and it
takes the tenant to read from the request body. Anyone who can reach the
port can read any tenant's call by supplying that tenant's id.

This was a deliberate, documented concession in LLD-02: cutting auth over
would have broken the e2e suite and the UAT rig, which existed before a
wire-level `AuthenticateUser` existed to mint them a token. That reason
has expired. `identity-api` shipped `AuthenticateUser`, and
`pbx-e2e-projection` demonstrated the fixture path a test uses to obtain
a real token.

The concession has now outlived two changes, and every change that lands
makes it harder to remove. `docs/API.md` still records it as
"temporary"; this change is what makes that true.

## What Changes

- **`TelephonyService` is mounted with the auth interceptor.** Every
  method requires `Authorization: Bearer <JWT>`; an unauthenticated call
  is refused before it reaches a handler.
- **The tenant comes from the token.** `GetCall`'s `tenant_id` field is
  retained and must **match** the authenticated tenant — the same rule
  `IdentityService` and `PbxService` already enforce. A mismatch is
  refused; a system-scoped caller is exempt, as elsewhere.
- **The e2e suite and the server test authenticate.** They obtain a real
  token rather than being exempted, so nothing keeps a back door open for
  test convenience.
- **`docs/API.md` §1 loses the "TelephonyService remains unauthenticated"
  concession** and records the API-wide posture instead.

**Not in scope:** `PbxService` and `IdentityService` already enforce
auth, so nothing changes for them. The `AuthInterceptor` and its
`TenantMatcher` already exist and are unchanged — this change applies
them, it does not build them.

**No proto change.** Removing `tenant_id` from `GetCallRequest` would be
a breaking change for a field every existing caller sends, and the
established rule is match-the-token rather than omit-the-field. `buf
breaking` therefore stays green.

## Capabilities

### New Capabilities
None.

### Modified Capabilities

- `identity/public-api`: the requirement *"Only obtaining a token may be
  done unauthenticated"* is scoped to identity operations today. It
  becomes the posture of the **whole public API** — obtaining a token is
  the only unauthenticated operation anywhere, not merely the only one in
  `IdentityService`. This is the requirement that currently permits an
  unauthenticated `GetCall`, so it is the one that must change.

`telephony-core/call-lifecycle` is deliberately **not** modified: it
describes what a call does, and carries no requirement about who may read
one. Putting the API's auth posture there would split one rule across two
capabilities.

## Impact

- **Changed**: `api/cmd/atsap-api/main.go` (mount `TelephonyService` with
  the interceptor and a telephony tenant matcher); `docs/API.md`.
- **Changed tests**: `api/internal/telephony/e2e/walkingskeleton_test.go`
  and `api/internal/server/server_test.go` obtain a token.
- **Behaviour change for callers**: an unauthenticated `GetCall` that
  returns `200` today returns `401` after this change. There are no
  external consumers yet — the console does not exist and no partner has
  been onboarded — so this is the last moment it is free.
- **Dependencies**: none added.
- **Follow-on**: LLD-02 §9 lists `identity-bootstrap` ("dev-token minting
  for the rig") as a prerequisite. It is not needed — the fixture path in
  `internal/pbx/e2e` obtains a real token through `AuthenticateUser`, and
  a second token-minting path would be a second thing to keep secure.
  This change records that and closes it.
