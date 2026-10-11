# Tasks — identity-accounts-and-sessions

Implements LLD-02 revision 2 (D-61–D-64). Apply after `console-apis-phase-a` is archived. Every task ends with a verification. Nothing outside `internal/identity`, `internal/mail`, `internal/archtest`, `cmd/atsap-api`, protos, migrations, fixtures and docs may change.

## 1. Domain

- [ ] 1.1 Add `User`, `PasswordCredential`, `Session`, `IdentityProvider`, `VerifiedDomain`, `ExternalIdentity`, `PasswordResetTicket` and `AuthEvent` with typed ids to `identity/domain`. Reshape `Principal` (adds `UserID`; drops `Username`, `Email` and `PasswordHash`). Verify `go build ./...` and that `identity/domain` imports nothing outside `shared`
- [ ] 1.2 Implement the pure rules (design D1): `NormalizeEmail`, `Session.Refresh` (Rotated, Reused, Expired, Revoked; idle 30 min and absolute 12 h), `ThrottlePolicy.Delay` (0 below 5 failures, then 30 s doubling, capped at 15 min), `CanResetPassword` and provider enforcement. Verify table-driven unit tests cover every outcome and boundary, including exactly 30 min, exactly 12 h, the 5th and 6th failure, and the cap

## 2. Schema

- [ ] 2.1 Write `0010_identity_accounts.{up,down}.sql` per design D2. Verify integration tests: a fresh database migrates; existing principals become users and memberships with ids, bindings, API keys and audit rows intact; compatible duplicates merge into one user; conflicting duplicates abort with both ids named and nothing changed; and down then up round-trips
- [ ] 2.2 Add the archtest rule confining the installation-wide tables to `identity/postgres` (design D3). Verify it passes, and fails when one table name is added to a string in another package (fault-injected)

## 3. Stores and ports

- [ ] 3.1 Declare the revision 2 ports in `identity/ports`: `UserStore`, `SessionStore`, `ResetTicketStore`, `IdentityProviderStore`, `ExternalIdentityStore`, `AuthEventStore`, `LoginThrottle`, `Mailer` and `FederatedAuthenticator` (no adapter). Extend `TenantContext` with `UserID`, `SessionID` and `PasswordChangeOnly`. Verify `go build`, and a compile-time test pinning `FederatedAuthenticator`'s shape
- [ ] 3.2 Implement the Postgres adapters. Verify integration tests:
  - email lookup ignores case;
  - the session row is locked during rotation, so two concurrent refreshes cannot both rotate (deterministic, fault-injected);
  - `ClaimDomain` refuses a second provider;
  - `ProviderForDomain` ignores unverified domains;
  - `Link` refuses a duplicate `(provider, subject)`;
  - `auth_events` has no update or delete path;
  - throttle rows older than 24 h are pruned on write;
  - memberships list across tenants only through the installation-wide query

## 4. Application

- [ ] 4.1 Rework provisioning: `ProvisionPrincipal` by email and display name (design D9). A new person gets a password or an invitation; an existing person gets a membership only, with the password ignored. Disabling acts on the membership; a new platform-scope `SetUserStatus` acts on the person. Verify unit tests: a duplicate membership is refused; the existing-person and new-person responses are identical apart from ids; an existing person's password is unchanged; a tenant administrator cannot change a person's status or email
- [ ] 4.2 Implement sign-in and validation (design D4, D6): throttle, then Argon2id always (a dummy hash when there is no account or no password), then live checks, then a session; `ValidateToken` checks the user and session in one transaction; `AuthenticateUser` reads `username` as the email and creates no session. Verify unit and integration tests:
  - every failure cause gives an identical error;
  - the hasher is invoked for unknown emails (asserted on a counting hasher);
  - a revoked session's token is refused on its next use;
  - a disabled user loses every tenant;
  - a disabled membership loses only its tenant
- [ ] 4.3 Implement refresh, switch and sign-out: rotation, reuse revoking the session, idle and absolute expiry, a tenant that must be an active membership (the same error for "no such tenant"), and sign-out revoking the session. Verify unit tests through the real service with a fake clock
- [ ] 4.4 Implement throttling (design D7), wired into `SignIn`, `AuthenticateUser` and both reset RPCs, with trusted-proxy source resolution. Verify unit tests: 5 failures pass and the 6th is refused even with the right password; unknown and known emails throttle alike; success clears the account key; `X-Forwarded-For` is ignored unless the peer is trusted
- [ ] 4.5 Implement `ChangePassword` (revokes other sessions, keeps the current one), `RequestPasswordReset` and `CompletePasswordReset` (design D8; identical responses; SSO-enforced domains and unknown emails get no ticket; mail is sent after commit), and `ResetPrincipalPassword`, with the `CanResetPassword` authority rule, `must_change`, session revocation and an audit row; add `principal.password.reset` to `IdentityActions`. Verify unit and integration tests for every credential-recovery scenario, including a shared person refused to a tenant administrator and allowed to platform scope
- [ ] 4.6 Record auth events for sign-in success and failure, throttling, change, reset request and completion, and revocation. Verify tests: one event per outcome; an unknown email is recorded with no user; no event contains a password, a secret or a token (asserted on the rendered rows)
- [ ] 4.7 Implement `DiscoverSignIn` and SSO enforcement (design D11). Verify integration tests with providers inserted through the store: discovery returns password by default, and SSO only for a verified enforced domain; an unknown email gets the same answer as a known one in the same domain; password sign-in for an enforced domain fails like a wrong password; a password-less account fails like a wrong password

## 5. Wire

- [ ] 5.1 Add `auth.proto` (`AuthService`) and make the additive `identity.proto` changes (design D9), regenerated with the pinned plugins. Verify `buf lint`, and that `buf breaking` against develop is clean
- [ ] 5.2 Implement the `AuthService` handlers with cookie handling (design D5): the secret only in `Set-Cookie`, with `HttpOnly`, `SameSite=Strict`, the path and `Secure` unless the dev flag is set; sign-out sends `Max-Age=0`; the interceptor exempts the unauthenticated procedures and confines `PasswordChangeOnly` contexts to `ChangePassword` and `SignOut`. Verify wire tests:
  - no response body contains a refresh or reset secret;
  - the cookie attributes are exact;
  - throttling maps to `RESOURCE_EXHAUSTED` with a retry delay;
  - a must-change token is refused everywhere else;
  - responses carry no hash or secret
- [ ] 5.3 Update the `IdentityService` handlers: `ProvisionPrincipal` and `ListPrincipals` carry the email, display name and user id; `ResetPrincipalPassword`; the tenant matcher covers the new requests. Verify wire tests, including the byte-identical existing-person and new-person responses apart from ids

## 6. Mail, config and composition

- [ ] 6.1 Add `internal/mail` (stdlib `net/smtp`, STARTTLS) and a logging fallback mailer, and wire `Mailer`, `AuthService`, the throttle and config (`ATSAPBX_SMTP_*`, `ATSAPBX_CONSOLE_URL`, `ATSAPBX_COOKIE_INSECURE`, `ATSAPBX_TRUSTED_PROXIES`) in `cmd/atsap-api`. Verify `go build`; `make lint` (depguard: identity never imports `internal/mail`); and a unit test that the fallback never logs a link
- [ ] 6.2 Add Mailpit to `deploy/docker-compose.yml` and the Tiltfile (dev only, pinned by digest, recorded in TOOLSET.md), and set the rig's SMTP and console URL in `.env.example`. Verify `make dev` brings it up, and a test mail sent through the adapter appears in Mailpit's API
- [ ] 6.3 Change bootstrap to `--email` and `--display-name`. Verify the bootstrap integration test, and that `--username` fails with a message

## 7. Docs and gates

- [ ] 7.1 Move the identity revision 2 rows in `docs/API.md` from §3 to §1. Regenerate the OpenAPI document, and update `.env.example` documentation, TOOLSET.md (Mailpit) and the LLD-02 status and §9 rows. Verify `make docs` passes and the proto-vs-API.md inventory matches every service
- [ ] 7.2 Run `make lint`, `go build`, `go vet` under every tag, unit tests with `-race` and integration tests with `-race -p 1`, all green. Verify there is no change under `internal/telephony`, `internal/pbx` (apart from test fixtures) or `internal/licensing`

## 8. End to end and regression

- [ ] 8.1 Move every e2e and UAT fixture from username to email (design D12). Verify all existing e2e suites and `make uat-auto`, `make uat-placement` and `make uat-inbound` pass back to back, with `pbx reconcile` clean
- [ ] 8.2 Add `internal/identity/e2e` against the running app, with a cookie jar and Mailpit, covering:
  - discovery, then sign-in as a two-tenant person, then a tenant choice, then an authorized call in each tenant, with the other tenant refused;
  - a refresh rotating the cookie, and a replayed old cookie revoking the session, so the current token is refused next;
  - sign-out, after which the access token is refused at once;
  - six wrong passwords giving `RESOURCE_EXHAUSTED`;
  - a reset email read from Mailpit, the new password working, the old one refused, and the link single-use;
  - an administrator reset forcing a change, with every other call refused until it is made;
  - an invitation email setting a first password;
  - no secret in any response body or log line captured by the test.

  Verify each assertion in the test
- [ ] 8.3 Write `coverage.md` mapping every scenario in the seven delta specs to the test that proves it, with gaps in the G-series convention (at least: SSO enforcement is proven at integration level only, and no protocol exists yet)
