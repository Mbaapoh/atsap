# Design — Sign in with Google

## D1. Installation-wide, not per tenant

D-67 chooses one OAuth client for the installation. The existing
per-tenant provider model remains for company SSO later. Google links
live in `federated_identities`, keyed by `(issuer, subject)`, so the
code already fits another issuer without a provider-administration UI.

## D2. Configuration lives in the deployment secret store

The client ID, secret and redirect URL come from
`GOOGLE_OIDC_CLIENT_ID`, `GOOGLE_OIDC_CLIENT_SECRET` and
`GOOGLE_OIDC_REDIRECT_URL`. The secret is not stored in the database:
it is deployment configuration, held by the platform's secret store and
never logged (D-83). With any of them missing, Google is disabled and
password sign-in is unchanged. If discovery fails at startup, the app
logs the error and runs with Google disabled rather than taking every
sign-in down.

## D3. Bound authorization requests

`Begin` creates 256-bit random state, PKCE verifier and nonce. Only the
state digest is queried; the verifier and nonce stay in the row. The
state row is single-use and expires in 10 minutes. `Exchange` sends the
verifier and verifies the ID token through go-oidc (issuer, audience,
expiry, signature); the nonce must equal the request's. A consumed state
cannot be replayed.

## D4. Linking is explicit, never by email at sign-in

- *Invitation:* the invite fragment secret is sent with `Begin`; the
  state stores its digest. At the callback the invited person is read
  **without consuming the ticket**, Google's verified email must equal
  the invited email, and only then is the ticket consumed and the link
  stored. A wrong Google account does not burn the invitation.
- *Account screen:* the signed-in person re-enters the current password
  before `BeginGoogleLink`; the state names that person. The callback
  requires the same authenticated person.
- *Normal sign-in:* the person is found by `(issuer, subject)`. No link
  means `ErrInvalidCredentials`, the same answer as a wrong password.

## D5. The console never sees tokens

Google tokens and the PKCE verifier stay server-side. The browser gets
the authorization redirect and, at the callback, the normal HttpOnly
session cookie. The account link callback does not start a second
session.

## Risks

- A real Google client cannot be exercised in CI. The adapter is proven
  against a local OIDC issuer (discovery, JWKS, signed ID token, nonce);
  a live Google smoke test is a release checklist item.
- A user who links Google and has no password cannot link again or
  change a password; administrator reset remains their recovery path.
