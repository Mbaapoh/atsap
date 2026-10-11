## Context

The design of record is **LLD-02 revision 2** (§3.1, §4, §5.2, §5.3, §5.4, §6, §10.2, §10.5, §10.10, §11.2) and decisions **D-61 to D-64**, with **D-66** refinements 1–3. This document records how the change implements them, not the reasons for them.

Current state:
- `principals` holds username, email, password hash, role and status per tenant.
- `AuthenticateUser(tenant, username, password)` returns a 15-minute EdDSA JWT.
- `ValidateToken` re-reads the principal and tenant under RLS on every request.
- There are no sessions, throttling, recovery or mail.
- `TenantContext` (tenant, principal, roles, scopes, residency) is the only thing other contexts read.
- `archtest/enginetables.go` already confines a set of tables to one package. The same mechanism confines identity's installation-wide tables here.

## Goals / Non-Goals

**Goals:**
- One person, one account, many memberships. No other context changes: `PrincipalID` and `TenantContext` keep their meaning.
- A console session that survives past 15 minutes, can be withdrawn on the next request, and detects a copied refresh secret.
- Password change, email reset, administrator reset and invitation, with throttling and no account-existence oracle.
- A model and ports that let OIDC land as an adapter.
- The wire stays additive: `buf breaking` is clean.

**Non-Goals:**
- OIDC or SAML flows, provider-administration RPCs, JIT provisioning and domain-verification UX. These belong to the `identity-oidc` change. This change creates providers and domains through the store only, in tests.
- MFA.
- Moving platform authority onto the user (D-61 keeps it as a binding).
- General per-RPC rate limiting.
- Listing a person's sessions or auth history over the API. No console screen consumes either yet (D-43).
- The console itself.

## Decisions

**D1 — Domain first; invariants are pure functions in `identity/domain`.**
- `NormalizeEmail` (trim, lower-case, one `@`, non-empty domain).
- `Session.Refresh(presentedDigest, now)` returns `Rotated`, `Reused`, `Expired` or `Revoked`, and computes the new expiries. It is pure and has no clock of its own.
- `ThrottlePolicy.Delay(failures)` returns 0 below 5 failures, then 30 s doubling per failure, capped at 15 min.
- `CanResetPassword(actorAdminTenants, memberTenants, actorHasSystemScope)` is true with system scope, or when every tenant of the person is one the actor administers.
- `ProviderMode` and `VerifiedDomain` carry the enforcement rule: only a verified domain with an `Enforced` provider disables passwords.

Unit tests cover each function exhaustively. Application services orchestrate the stores and ports and hold no rules of their own. *Rejected:* rules in handlers or SQL, which put them where they cannot be unit-tested.

**D2 — Migration `0010_identity_accounts`, in one transaction.**
1. Create the new tables (LLD-02 §5.4, HLD 03 §5).
2. A `DO` block finds normalized emails shared by principals with **different** `password_hash` values, and raises an exception listing the principal ids. The migration aborts with nothing changed.
3. Insert one `users` row per normalized email, and one `user_credentials` row (`kind = 'password'`) per user from the shared hash.
4. Add `principals.user_id` and backfill it. Set it `NOT NULL`, and add `UNIQUE (tenant_id, user_id)`.
5. Drop `username`, `email` and `password_hash` from `principals`.

The down migration re-adds the columns. It restores `email` from `users`, `username` from the email's local part (suffixed if needed, to stay unique per tenant), and `password_hash` from the user's password credential, or an unusable marker when there is none. It is documented as lossy for invited users. The migration test seeds duplicates, both conflicting and compatible, and asserts both outcomes.

**D3 — Installation-wide tables are confined by archtest.**
A new rule in `archtest`, modelled on `enginetables.go`, lists `users`, `user_credentials`, `sessions`, `password_reset_tickets`, `identity_provider_domains`, `external_identities`, `login_throttle` and `auth_events`. It fails if any package other than `internal/identity/postgres` (and archtest itself) names one in a string literal. It is fault-injected. These tables have no RLS (LLD-02 §5.4). `principals` and `identity_providers` keep or gain RLS.

**D4 — The token gains `sid` and `uid`; validation is one transaction.**
`Claims` gains `SessionID` and `UserID`. Both are absent on deprecated `AuthenticateUser` and API-key paths. `ValidateToken` reads, in one transaction:
- the principal and tenant, under `WithTenant`, as today;
- the user's status;
- when `sid` is present, the session's `revoked_at` and both expiries.

Any failure is reported as the same `ErrInvalidCredentials`. `TenantContext` gains `UserID`, `SessionID` and `PasswordChangeOnly`. No other context reads them, which the archtest boundary rules already ensure.

**D5 — `AuthService`, and how cookies cross ConnectRPC.**
- New `auth.proto` in `atsapbx.v1` with `AuthService`: `DiscoverSignIn`, `SignIn`, `RefreshSession`, `SignOut`, `ChangePassword`, `RequestPasswordReset` and `CompletePasswordReset`. It lives in `identity/rpc`, as a second handler beside `IdentityService`.
- **Cookie:** `atsa_refresh`, `HttpOnly; SameSite=Strict; Path=/atsapbx.v1.AuthService/`, `Secure` unless `ATSAPBX_COOKIE_INSECURE=true` (dev rig only; startup logs a WARN when it is set). Handlers read it from `req.Header().Get("Cookie")` and write it through `res.Header().Add("Set-Cookie", …)`. The secret never appears in a response body. `Max-Age` equals the absolute remaining lifetime, and sign-out sends `Max-Age=0`.
- **Interceptor:** `AuthService` procedures except `SignOut` and `ChangePassword` are exempt from bearer auth, as `AuthenticateUser` already is. `RefreshSession` authenticates by the cookie inside the handler. A `PasswordChangeOnly` context is refused `PERMISSION_DENIED` by the interceptor for every procedure except `ChangePassword` and `SignOut`.

**D6 — Sign-in, refresh and switching.**
- **`SignIn`:** throttle check, then the user by email. Argon2id runs against the user's hash, or against a dummy hash computed at startup with the §5.3 parameters when the user is absent or has no password. Then live checks, then the active memberships. It creates the session, choosing the membership when there is exactly one; otherwise the access token is withheld until a refresh names a tenant. It records an auth event.
- **`RefreshSession(tenant_id?)`:** runs `Session.Refresh` under `SELECT … FOR UPDATE`, so two concurrent refreshes cannot both rotate. `Reused` revokes the session (reason `reuse`) and records an auth event. A named tenant must be an active membership, and the error is identical for "not a member" and "no such tenant".
- **Response:** `memberships` (tenant id, tenant name, roles) are returned only to the person themselves. That is the one place a person's tenants are listed.

**D7 — Throttling (`LoginThrottle`, Postgres).**
- **Keys:** `SHA-256("account:" + normalized email)` and `SHA-256("source:" + address)`.
- **Order:** `Check` runs before any user lookup and refuses while either key is blocked, with `RESOURCE_EXHAUSTED` and a `retry-after-seconds` detail. `Record` happens after verification: a failure increments both keys and sets `blocked_until` from D1's policy; a success clears the account key only.
- **Source address:** the connection peer. `X-Forwarded-For` is honoured only when the peer is in `ATSAPBX_TRUSTED_PROXIES`, which is empty by default.
- **Runs, not totals:** a failure more than 15 minutes after the key's previous failure starts a fresh count (`ThrottlePolicy.Window`), so a shared address never accumulates a day of typos into a block. *Found while designing the e2e suite: without it the source key never reset.*
- **Housekeeping:** rows not updated for 24 hours are deleted on write, so no background job is needed.
- **Scope:** `SignIn`, `AuthenticateUser`, `RequestPasswordReset` (source key only, plus a per-account limit of 3 per hour) and `CompletePasswordReset` (source key).

**D8 — Recovery and invitation.**
- **Tickets:** `password_reset_tickets` with `purpose` reset (30 min) or invite (72 h). The 256-bit secret goes in the link `ATSAPBX_CONSOLE_URL + "/reset#" + secret`. The fragment keeps it out of server logs and Referer headers. Only the digest is stored.
- **Completing:** `CompletePasswordReset` marks the ticket used, sets the password, clears `must_change`, and revokes all sessions, in one transaction.
- **Silent cases:** no ticket is issued for a user with no password whose domain is SSO-enforced, or for an unknown email. The response is identical in every case.
- **Mail after commit:** mail is sent after the transaction commits. A send failure is logged at ERROR with the user id and never changes the response.
- **Administrator reset:** `ResetPrincipalPassword` loads the person's memberships through an installation-wide store query, then applies `CanResetPassword` against the actor's TENANT_ADMIN tenants and system scope. It sets `must_change`, revokes sessions, and writes an audit row (`principal.password.reset`, added to `IdentityActions`) and an auth event.

**D9 — `ProvisionPrincipal` and `ListPrincipals`, additive on the wire.**
- `ProvisionPrincipalRequest` gains `display_name`, and `username` is marked `deprecated = true` and ignored.
- `Principal` gains `user_id` and `display_name`, and `username` is deprecated and filled with the email.
- **New email:** create the user, plus a password if one was given, otherwise an invite ticket.
- **Existing email:** create the membership only, and ignore the password.

The response is built identically in both cases, which a wire test asserts byte for byte apart from the ids. `AuthenticateUser` reads `username` as the email, and still requires the `tenant_id` membership.

**D10 — Mail: an identity-owned port, an SMTP adapter, Mailpit in dev.**
- `ports.Mailer` has `SendPasswordReset` and `SendInvitation`. Identity composes the plain-text body; the adapter only sends.
- `internal/mail` uses stdlib `net/smtp`, with STARTTLS when offered and required unless `ATSAPBX_SMTP_INSECURE=true`. There is no new Go dependency.
- Configuration: `ATSAPBX_SMTP_ADDR`, `ATSAPBX_SMTP_FROM`, `ATSAPBX_SMTP_USER` and `ATSAPBX_SMTP_PASSWORD`. When `ATSAPBX_SMTP_ADDR` is unset, a logging mailer records that a mail **would** be sent, without the link, so an unconfigured install still runs.
- The dev rig adds `axllent/mailpit` (pinned by digest, dev-only, vetted under TOOLSET §6 and recorded there). e2e reads messages from Mailpit's HTTP API.

**D11 — Federation readiness is store and domain only.**
- `IdentityProviderStore` gives `ProviderForDomain(domain)`, returning only a verified domain's provider, and `ClaimDomain`, which fails on a duplicate by primary key. `ExternalIdentityStore.Link` fails on a duplicate `(provider, subject)`. `ports.FederatedAuthenticator` is declared with no implementation, and a compile-time test pins its shape.
- **Discovery:** `DiscoverSignIn` returns `PASSWORD`, or `SSO{provider_id, display_name}` when the domain's verified provider is `Enforced`. It depends only on the domain, never on the account.
- **Enforcement:** password sign-in for an enforced domain fails as invalid credentials, after Argon2id.

The enforcement and discovery scenarios are proven at integration level, with providers inserted through the store.

**D12 — Bootstrap, fixtures and harnesses.**
- `atsap-api bootstrap` takes `--email` and `--display-name`. `--username` is removed, and passing it fails with a message.
- The e2e and UAT fixtures (`projection_test.go`, `walkingskeleton_test.go`, `console_apis_test.go` and the UAT scripts) seed users and memberships, and sign in by email with `SignIn` and a cookie jar, or with `AuthenticateUser` where a harness only needs a token.

**D13 — Credential kinds (D-66 §1).**
- `user_credentials (id, user_id, kind, secret, must_change, created_at, changed_at)` with a partial unique index allowing one `password` per user. The domain exposes `User.Password *PasswordCredential`.
- The store reads and writes the `password` kind only. Other kinds are rejected by a `CHECK (kind IN ('password'))` until `identity-mfa` widens it, so nothing half-supported can be written.

**D14 — Signing key set (D-66 §2).**
- `TokenIssuer` holds a current key and zero or more previous keys, indexed by `kid` = hex(SHA-256(pub)[:8]).
- **Issue:** sets `kid` in the header and signs with the current key.
- **Validate:** after the `alg` pin, looks the `kid` up and refuses an unknown one (`ErrTokenUnknownKey`) before any signature work. A missing `kid` is checked against the current key only.
- **Config:** `ATSAPBX_JWT_PREVIOUS_KEYS` holds comma-separated hex seeds. A malformed entry fails startup, and so does an entry equal to the current key.
- Unit tests cover: a token from the previous key still validates after rotation; an unknown `kid` is refused without calling verify (counted); a kid-less token validates only against the current key; issue always uses the current key.

**D15 — Off-network recovery CLI (D-66 §3).**
- `atsap-api admin reset-password --email <e>` is a subcommand beside `bootstrap` and `pbx reconcile`.
- It generates a 20-character temporary password from crypto/rand, prints it once to stdout and never logs it, and calls the same application method as the administrator reset with the system actor and no authority check, because shell access to the server is the authority.
- It sets `must_change`, revokes every session, writes an auth event and one system audit row per tenant the person belongs to, and exits non-zero for an unknown email with a generic message.
- An integration test drives the command function directly.

## Risks / Trade-offs

- [**Installation-wide tables bypass RLS**] Mitigated by D3's archtest confinement and by exposing tenant-visible facts only through RLS-scoped memberships. Any new query on these tables also needs review against AC-01.1.
- [**Adding an existing person silently ignores the supplied password**] The administrator may expect the password to work. The console states that existing people keep their own password, and the API documentation says so. This is the price of not revealing account existence (D-61).
- [**`ValidateToken` does more work per request**] It is one transaction, at most two extra indexed lookups by primary key. That is within D-42's accepted ceiling, and the e2e suite's timing is watched for regressions.
- [**The migration aborts on conflicting duplicates**] Pre-release, these can exist only on dev or test rigs. The message names the principals, and the operator deletes or re-emails one.
- [**Email delivery is outside our control**] An unconfigured SMTP setting falls back to the logging mailer, and administrator reset works without mail. Invitation then needs an administrator to set an initial password instead.
- [**Throttling by source can penalize a shared NAT**] The per-source threshold applies the same backoff, which never blocks permanently, and a correct password still works once the delay passes. Trusted-proxy handling avoids treating the proxy as one source.
- [**A cookie on the API origin**] The console must be served from the same site as the API (HLD 12 §5). A split-origin deployment would need `SameSite=None` plus CORS credentials, which is out of scope and recorded.

## Migration Plan

1. Deploy the migration. It aborts with nothing changed if conflicting duplicates exist.
2. Deploy the app. Existing API-key and `AuthenticateUser` clients keep working.
3. Re-run bootstrap documentation and fixtures with email.

Rollback is the down migration, lossy only for invited users without a password, plus the previous binary.

## Open Questions

None blocking. Recorded for `identity-oidc`:
- the JIT provisioning policy (auto-create a membership for a verified-domain user, or require an invitation);
- how a tenant proves domain ownership (a DNS TXT record is the likely answer).
