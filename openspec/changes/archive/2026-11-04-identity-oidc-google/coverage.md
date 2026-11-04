# Coverage — Sign in with Google

## Automated (ran green 2026-11-04)

- OIDC adapter against a local issuer: discovery, JWKS, signed ID token,
  audience/issuer verification, nonce mismatch refused, incomplete
  configuration and unreachable discovery refused.
- Application over the in-memory store: linked sign-in, unlinked refusal,
  single-use state, invitation with matching/mismatching email (the
  mismatch leaves the invitation usable), account linking with the
  current password, duplicate link refused, linked-email read, disabled
  Google, unsafe `return_to`, unverified email and exchange failure.
- Postgres integration (migration 0020 up/down): links by
  issuer/subject, one link per person and account, OAuth state single
  use and expiry; full integration suite on `atsapbx_test`.
- RPC wire: options true/false, public procedure list, disabled refusal,
  begin/complete sign-in sets the normal cookie, and begin/complete link.
- Console: Google button on sign-in (both steps), callback success,
  incomplete/cancelled/provider-error/unlinked/failed-link callbacks,
  invitation Google button, account link success and both failure kinds,
  options failure and redirect-start failure.
- `make console-check`, `make docs`, `make sca`, the unit gate (79.4%)
  and the container race tier. Full Go e2e and the three phone UATs pass.

## Not automated

- A live Google account and client, because the secret and account are
  not in CI: `docs/uat/google-sign-in.md`.
