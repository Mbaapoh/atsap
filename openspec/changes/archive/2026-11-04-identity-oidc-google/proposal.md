# Sign in with Google

## Why

Many target businesses run on Gmail rather than a company identity
provider. D-67 chooses Google as the first external sign-in method,
delivered on generic OpenID Connect so another provider is mostly
configuration. It must never create a person: people are added by
administrators, as today.

## What Changes

- One installation-wide Google OAuth client, configured from the
  deployment's secret store. Discovery shows "Continue with Google" to
  everyone when it is configured.
- An authorization request uses server-side one-time state, PKCE and a
  nonce; the callback verifies the ID token's issuer, audience, nonce and
  `email_verified` before using it.
- Sign-in finds a person by Google's stable `sub`, never by matching
  email. An unlinked Google account is refused like a wrong password.
- Linking happens in two places only: accepting an invitation from the
  emailed link (Google's verified email must equal the invited email),
  or from the account screen after re-entering the current password.
- Consumer mailbox domains (`gmail.com`, `outlook.com`, …) can never be
  claimed as a tenant provider domain.

## Impact

- New `federated_identities` and `oauth_login_states` tables
  (migration 0020).
- Five AuthService RPCs; `DiscoverSignIn` gains `google_enabled`.
- New Go dependencies `coreos/go-oidc/v3` and `golang.org/x/oauth2`
  (named by D-67; pinned).
- Console: Google button on sign-in, invitation and account; a callback
  page; no self-signup.

## Out of scope

- Per-tenant Google switches (D-67 leaves that for later).
- Company-domain SSO (`identity-oidc`) and MFA (`identity-mfa`).
