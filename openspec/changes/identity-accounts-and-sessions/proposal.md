## Why

Console slice A is the first thing built for people rather than machines, and identity was built for machines (LLD-02 revision 1). Before the console's sign-in and users screens can exist:
- a person is logged out every 15 minutes, because there is no refresh;
- sign-in needs a tenant id the person does not know;
- nothing slows password guessing;
- a forgotten password cannot be recovered;
- an engineer who manages twenty customer tenants needs twenty accounts.

The last point also blocks SSO: an identity provider asserts a person, and there is no "person" in the model to link it to. The owner's direction (2026-10-11) is to fix the account model and the sign-in shape now, ready for SSO and an IdP, without building the protocols yet. The design is LLD-02 revision 2, decisions D-61 to D-64.

## What Changes

- **Users and memberships (D-61).**
  - A global `User` represents one person. It is keyed by a normalized email that is unique installation-wide.
  - The existing principal becomes that user's **membership of one tenant**. The table name and every id are unchanged, so audit rows, `TenantContext`, role bindings and API keys stay valid. Nothing outside identity changes.
  - Credentials move to the user. **BREAKING:** the per-tenant username is retired; people sign in with their email.
  - A tenant administrator governs the membership, never the person. Adding an email that already has an account looks the same as creating one, so the administrator never learns about the person's other tenants.
- **Console sessions (D-62).**
  - A new `AuthService` with `DiscoverSignIn`, `SignIn`, `RefreshSession` (which also chooses the tenant) and `SignOut`.
  - Each refresh rotates the secret, which lives in an HttpOnly, SameSite=Strict cookie. A reused secret revokes the session. Sessions expire after 30 minutes idle or 12 hours absolute.
  - Access tokens stay 15-minute JWTs and now name their session. Every request checks the session, so signing out, a password change or a disabled user ends access on the next request.
- **Credential recovery and throttling (D-64).**
  - Self-service `ChangePassword`, which revokes the user's other sessions.
  - An email reset link (`RequestPasswordReset` and `CompletePasswordReset`): single use, valid 30 minutes, and the same answer for every email.
  - An administrator reset (`ResetPrincipalPassword`), forcing a change at next sign-in. A tenant administrator may only reset a person who belongs to no tenant outside their authority.
  - Invitation by email when an administrator adds a new person without a password.
  - Login throttling per account and per source, with exponential backoff and no hard lockout. This supersedes LLD-02 §10.5's "edge proxy" stance.
  - An unknown email now costs the same Argon2id work as a wrong password, closing a timing oracle.
  - An append-only `auth_events` history per user.
- **SSO-ready, not built (D-63).**
  - The password becomes optional.
  - Per-tenant `IdentityProvider` records with verified email domains (one provider per domain).
  - External identities linked by `(provider, subject)`, never by email.
  - Sign-in discovery that will route a domain's members to their IdP. While no provider is enforced, it answers "password".
  - A `FederatedAuthenticator` port with no adapter yet. OIDC is the next identity change, and MFA (TOTP) is recorded for later.
- **Mail.** A `Mailer` port owned by identity, an SMTP adapter in `internal/mail`, and Mailpit in the dev rig to capture mail for tests.
- **Deprecated, kept:** `AuthenticateUser`, for machines and harnesses. Its `username` field is read as the email, and it creates no session. `ProvisionPrincipal.username` is ignored.
- **Bootstrap** takes an email instead of a username.

## Capabilities

### New Capabilities

- `identity/user-accounts`: one person across tenants, memberships, what an administrator may and may not change about a person, invitations, and email uniqueness.
- `identity/sessions`: signing in, choosing a tenant, refreshing, signing out, rotation and reuse detection, expiry, and revocation taking effect on the next request.
- `identity/credential-recovery`: password change, email reset, administrator reset with forced change, and login throttling.
- `identity/federation-readiness`: password-less accounts, sign-in method discovery, provider domains, and linking external identities by issuer and subject.

### Modified Capabilities

- `identity/tenant-provisioning`:
  - "A principal belongs to exactly one tenant" now describes a membership of a user.
  - "Usernames are unique per tenant" is removed; emails are unique installation-wide.
- `identity/authentication`:
  - tokens carry a session;
  - validation also checks the user and the session;
  - secrets at rest include refresh and reset secrets;
  - failed attempts are recorded as auth events as well as logs.
- `identity/public-api`:
  - the set of operations callable without a bearer token grows (discovery, sign-in, refresh, reset);
  - failures disclose nothing, including through timing or the reset and discovery answers.

## Impact

- **Code:** all inside `internal/identity` (domain, ports, application, postgres, rpc, bootstrap), plus a new `internal/mail` adapter, `cmd/atsap-api` wiring (the `AuthService` mount, cookie handling, mailer, config) and a new `auth.proto`. No change to telephony, pbx or licensing. `TenantContext` keeps every field those contexts read.
- **Schema:** migration `0010_identity_accounts`:
  - new tables `users`, `user_passwords`, `sessions`, `password_reset_tickets`, `identity_providers`, `identity_provider_domains`, `external_identities`, `login_throttle` and `auth_events`;
  - `principals` gains `user_id` and drops `username`, `email` and `password_hash` after copying them.
  - The migration refuses to merge two principals with one email and different hashes.
- **API:** a new `AuthService` (7 RPCs) and `IdentityService.ResetPrincipalPassword`. The wire change is **additive**, and `buf breaking` stays clean: `Principal` gains `user_id` and `display_name`, and `ProvisionPrincipalRequest` gains `display_name`. The `username` fields are marked deprecated but kept: requests ignore them and responses fill them with the email. The **BREAKING** change is behavioural only: a username no longer identifies anyone.
- **Config:** SMTP settings, the console's public base URL (for links), and the cookie's Secure flag (off only for the HTTP dev rig).
- **Dev rig:** a Mailpit service in compose.
- **Tests:** unit, integration, wire, and e2e against the running app. That covers sign-in to refresh to tenant switch to sign-out, reuse revocation, throttling, a reset by email read from Mailpit, administrator reset with forced change, and invitation. Existing e2e and UAT fixtures move from username to email. No phone path changes, so the UATs re-run as regression.
- **Docs (already updated with this proposal):** DECISIONS D-61 to D-64; LLD-02 revision 2; HLD 03, 04, 05, 12, 17 and 18; API.md §3; PROGRESS.md.
- **Order:** apply after `console-apis-phase-a` is archived, because both change `identity/public-api`.
