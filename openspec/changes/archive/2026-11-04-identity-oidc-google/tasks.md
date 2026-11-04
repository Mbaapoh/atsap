# Tasks — Sign in with Google

## 1. Protocol and storage
- [x] 1.1 Add migration 0020 and persist federated links and one-time OAuth state
- [x] 1.2 Implement the OIDC adapter (discovery, PKCE, nonce, ID-token verification)
- [x] 1.3 Add the invitation ticket peek so a mismatch does not burn the ticket

## 2. Application and wire
- [x] 2.1 Implement sign-in, invitation acceptance and account linking flows
- [x] 2.2 Expose Google options, begin/complete sign-in and link RPCs; public procedure list
- [x] 2.3 Wire configuration and disable safely when discovery fails

## 3. Console
- [x] 3.1 Google button on sign-in and invitation; callback page
- [x] 3.2 Link Google from the account screen after re-entering the password

## 4. Proof
- [x] 4.1 Application, OIDC adapter, Postgres and RPC tests
- [x] 4.2 Console tests; docs, SCA and full tiers
