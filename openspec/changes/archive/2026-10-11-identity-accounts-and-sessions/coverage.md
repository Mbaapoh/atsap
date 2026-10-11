# Coverage — identity-accounts-and-sessions

Every scenario in the seven delta specs, mapped to what proves it. **Tiers:** U = unit (real service over the in-memory store, which mirrors every database constraint), I = integration (real Postgres), W = wire (handlers, cookies, rendered responses), E = e2e (`internal/identity/e2e`, the running app, a real cookie jar, mail read from Mailpit). The phone path did not change; every existing e2e suite and `uat-auto`, `uat-placement`, `uat-inbound` re-ran green as regression.

Test files: `application/accounts_test.go` (U), `rpc/auth_test.go` (W), `postgres/accounts_integration_test.go` and `postgres/store_integration_test.go` (I), `internal/postgres/identity_accounts_migration_integration_test.go` (I, migration), `cmd/atsap-api/admin_integration_test.go` (I, CLI), `identity/e2e/accounts_test.go` (E).

## identity/user-accounts

| Scenario | Proven by |
|---|---|
| One person in two tenants signs in once | **E** two-tenant sign-in → tenant choice → calls in each tenant. **U** `TestValidate_SessionUserAndMembershipAreLive`. **I** `TestLive_OnePersonAcrossTenants`, `TestStore_OnePersonManyMemberships` |
| Email uniqueness ignores case | **U** `TestProvision_ExistingAndNewPersonLookAlike` (`" ANA@example.com "` is the existing person). **I** `TestStore_OnePersonManyMemberships` (`ErrEmailTaken`), `TestAccounts_UsersByEmailIgnoreCase`. Domain: `TestNormalizeEmail` |
| A second membership of the same tenant is refused | **U** `TestProvision_SecondMembershipRefused`. **I** `TestStore_OnePersonManyMemberships` (`ErrAlreadyMember` from the unique constraint) |
| Adding an existing person looks like creating one | **W** `TestProvisionPrincipal_WireResponseRevealsNoExistingAccount` (byte-identical apart from identifiers). **U** `TestProvision_ExistingAndNewPersonLookAlike` (no audit row in the tenant mentions the other tenant) |
| An existing person's password is not replaced | **U** `TestProvision_ExistingAndNewPersonLookAlike`. **I** `TestLive_OnePersonAcrossTenants` |
| Disabling a membership leaves other tenants untouched | **U** `TestValidate_SessionUserAndMembershipAreLive/a disabled membership loses only its tenant` |
| A tenant administrator cannot disable the person | **W** `TestSetUserStatus_PlatformOnly` (PERMISSION_DENIED). *Changing a person's email has no RPC at all* — G2 |
| A disabled account loses every tenant | **U** `…/a disabled person loses every tenant` (tokens, refresh and sign-in all refused). **W** `TestSetUserStatus_PlatformOnly` |
| An invited person sets a password and signs in | **E** invitation read from Mailpit → set → sign in. **U** `TestInvitation_SetsFirstPasswordOnceAndExpires` (single use) |
| An invitation link expires | **U** `TestInvitation_SetsFirstPasswordOnceAndExpires` (72 h + 1 s). **I** `TestAccounts_SessionsRevokeAndRedeemTicketAtomically` (expired ticket refused) |
| An existing administrator signs in after the upgrade | **I** `TestAccountsMigration_PrincipalsBecomeMembershipsIntact` (ids, bindings, keys and audit rows intact; the hash moves to the person). Every e2e suite and UAT ran on the migrated schema |
| A conflicting duplicate stops the upgrade | **I** `TestAccountsMigration_ConflictingDuplicatesAbortUnchanged` (both ids named; nothing created or changed). Round trip: `TestAccountsMigration_DownThenUpRoundTrips` |

## identity/sessions

| Scenario | Proven by |
|---|---|
| A person with one tenant is signed straight in | **W** `TestSignIn_CookieCarriesTheSecretAndNothingElseDoes` (`HttpOnly`, `Secure`, `SameSite=Strict`, path, `Max-Age`). **U** `TestRefresh_RotatesAndReuseRevokes` |
| A person with several tenants chooses one | **E** no access token until `RefreshSession{tenant}`. **U** `TestValidate_SessionUserAndMembershipAreLive` |
| The refresh secret cannot be read back | **I** sessions store only digests (`TestAccounts_ConcurrentRefreshesSerializeOnTheRow` writes and reads digests only). **W** the secret never appears in a body. **E** not in any body; not in the app log |
| Refresh keeps a person signed in | **U** `TestRefresh_RotatesAndReuseRevokes`. **W** `TestRefreshAndSignOut_OverTheWire`. **E** cookie rotated |
| A replayed secret revokes the session | **E** a stolen old cookie → 401; then the current cookie → 401; then the access token → 401. **U**, **W** same. **I** `TestAccounts_ConcurrentRefreshesSerializeOnTheRow` (deterministic; fault-injected: fails without `FOR UPDATE`). Domain: `TestSessionRefresh` |
| Switching to another tenant | **E** token A reads A and is refused for B; token B reads B. **U** `TestRefresh_SwitchingNeverWidens` |
| Switching to a tenant the person does not belong to | **U** `TestRefresh_SwitchingNeverWidens` (identical errors for "not a member" and "no such tenant"; the session stays usable) |
| An idle session expires | **U** `TestRefresh_IdleAndAbsoluteExpiry`. Domain: `TestSessionRefresh/idle expiry boundary` (exactly 30 min) |
| An active session still ends at 12 hours | **U** `TestRefresh_IdleAndAbsoluteExpiry`. Domain: `TestSessionRefresh/absolute expiry…` |
| Signing out ends access at once | **E** sign-out → cookie cleared → token 401. **U**, **W** same |
| Disabling an account ends its sessions | **U** `…/a disabled person loses every tenant` |

## identity/credential-recovery

| Scenario | Proven by |
|---|---|
| Changing the password ends other sessions | **U** `TestChangePassword_EndsOtherSessionsKeepsThisOne`. **I** `TestAccounts_SessionsRevokeAndRedeemTicketAtomically` (`RevokeUserSessions` keeps one) |
| A wrong current password changes nothing | **U** same test |
| A reset by email | **E** link read from Mailpit → new password works, old refused, link single-use. **U** `TestReset_ByEmail` (earlier sessions ended). **I** `RedeemTicket` atomic: password, every outstanding ticket and every session in one transaction |
| An unknown email gets the same answer | **W** `TestPasswordReset_OverTheWireAnswersAlike` (`proto.Equal`). **U** no mail sent. **E** same 200 |
| An expired or used link is refused | **U** `TestReset_ByEmail` (30 min; second use). **I** expired ticket. **W** constant "invalid or has expired" |
| A domain that requires SSO gets no reset link | **U** `TestDiscovery_AndEnforcedSSO` |
| A temporary password must be changed | **E** temp sign-in → `ListCalls` 403 → `ChangePassword` → 200. **W** `TestInterceptor_MustChangeTokenOnlyChangesItsPassword` (refused in IdentityService and TelephonyService; allowed only `ChangePassword`, `SignOut`). **U** `TestAdminReset_AuthorityAndForcedChange` |
| A shared person cannot be reset from one tenant | **U** `TestAdminReset_AuthorityAndForcedChange` (refused for the tenant admin; allowed with system scope). Domain: `TestCanResetPassword` |
| The reset is audited without the password | **U** same test (one audit row, password absent) |
| Recovering a locked-out administrator | **I** `TestAdminResetPassword` (cmd: printed once, never logged, must-change, sessions ended, one system audit row per tenant, unknown email exits 1). **U** `TestRecoverByEmail_OffNetwork` |
| Recovery is not reachable over the network | **W** `AuthPublicProcedures` lists exactly five procedures; no RPC calls `RecoverByEmail` (it is wired only in `cmd/atsap-api/admin.go`). By construction — G3 |
| Repeated failures are delayed | **E** five 401s, then 429 with `Retry-After`. **W** `TestSignIn_ThrottledIsResourceExhaustedWithDelay`. **U** `TestThrottle_SlowsWithoutLockingOut`. **I** `TestAccounts_ThrottleBlocksAndPrunes` (incl. runs >15 min apart never accumulate). Domain: `TestThrottlePolicy`, `…WindowStartsAFreshRun` |
| Unknown and known emails throttle alike | **U** `TestThrottle_UnknownAndKnownAlike` (identical sequence of answers) |
| The owner signs in after the delay | **U** `TestThrottle_SlowsWithoutLockingOut` (success clears the account count) |
| A failed sign-in is recorded without the password | **U** `TestAuthEvents_OnePerOutcomeAndNoSecrets` (one event per outcome; unknown email has no person; no password, secret or token in the rendered events). **I** `TestAccounts_AuthEventsAppendOnly` (no update or delete method) |

## identity/federation-readiness

| Scenario | Proven by |
|---|---|
| A password-less account cannot sign in with a password | **U** `TestDiscovery_AndEnforcedSSO`, `TestSignIn_FailuresIdenticalAndHashAlwaysRuns` (same error, Argon2id still runs) |
| Discovery offers a password today | **E** `DiscoverSignIn` → `SIGN_IN_METHOD_PASSWORD`. **W** `TestDiscoverSignIn_Wire` |
| Discovery is not an account oracle | **U** `TestDiscovery_AndEnforcedSSO` (known and unknown email in an enforced domain: equal answers) |
| A domain cannot be claimed twice | **I** `TestAccounts_ProvidersDomainsAndLinks` (primary key → `ErrDomainClaimed`). **U** same |
| An enforced provider disables password sign-in | **U** `TestDiscovery_AndEnforcedSSO`. **I** `TestAccounts_ProvidersDomainsAndLinks` (discovery reads the provider before any tenant is known) — G1 |
| An unverified claim changes nothing | **U**, **I** same tests |
| One external identity maps to one account | **I** `TestAccounts_ProvidersDomainsAndLinks` (`ErrAlreadyLinked`). **U** same |
| A matching email is not a link | By construction: `UserForExternalIdentity` resolves by (provider, subject) only, and no code path looks a person up by an asserted email. `FederatedAuthenticator`'s shape is pinned by `TestFederatedAuthenticatorShape` — G1 |

## identity/tenant-provisioning (modified)

| Scenario | Proven by |
|---|---|
| Principal is created under a tenant | **I** `TestStore_PrincipalRoundTrip` (tenant and person through the join) |
| Acting as a membership never reaches another tenant | **E** token A naming tenant B → 400. Existing interceptor tests |
| *Removed:* Usernames are unique per tenant | Replaced by `TestStore_OnePersonManyMemberships` |

## identity/authentication (modified and added)

| Scenario | Proven by |
|---|---|
| Stored password cannot be read back; API key shown once | Unchanged tests (`TestStore_*`, `TestRawApiKey_IssuedOnceNeverAgain`) |
| Reset and refresh secrets cannot be read back | **I** sessions and tickets store digests only. **E** none in a body or the app log |
| Token is accepted while fresh / stops after expiry | Unchanged token tests |
| Disabling a principal stops the next request | `TestValidateToken_DisablingTakesEffectImmediately`, `TestLive_DisablingInvalidatesAnExistingToken` |
| Ending the session stops the next request | **E** sign-out. **U** `TestValidate_SessionUserAndMembershipAreLive` |
| Failed login is logged without the credential | `TestFailedAuth_IsLoggedWithoutCredentials` (unchanged), plus auth events above. **E** no secret in the app log; the log is shown to be this run's |
| A token signed before rotation still works | **U** `TestTokenIssuer_RotationKeepsOldTokensValid` |
| A token naming an unknown key is refused | **U** `TestTokenIssuer_RejectsAnotherIssuersToken` (a counted verifier proves no signature work), `TestTokenIssuer_KnownKidWrongSignature` |

## identity/public-api (modified)

| Scenario | Proven by |
|---|---|
| Mutating and reading operations refuse an unauthenticated caller | Unchanged interceptor tests; `SignOut` and `ChangePassword` are absent from `AuthPublicProcedures` |
| A caller cannot name a tenant that is not their own | **E** 400 for a token naming another tenant. **W** `TestTenantID_CoversResetPrincipalPassword` |
| Obtaining a token needs no token | **E** `SignIn`; `AuthenticateUser` in every existing e2e suite |
| Recovering access needs no token | **E** `DiscoverSignIn`, `RefreshSession`, `RequestPasswordReset`, `CompletePasswordReset` all called without a bearer token |
| Every credential failure looks the same | **U** `TestSignIn_FailuresIdenticalAndHashAlwaysRuns`; existing `TestAuthenticateUser_SuccessAndIndistinguishableFailures` |
| An unknown email costs the same as a wrong password | **U** `TestSignIn_FailuresIdenticalAndHashAlwaysRuns` (timed; fault-injected: skipping Argon2id for unknown emails makes it fail, 34 ms vs 15 µs) |
| Reading another tenant's resource | Unchanged |

## Gaps

| # | Gap | Severity | Note |
|---|---|---|---|
| G1 | SSO enforcement and discovery are proven with providers inserted through the store; no provider can be configured over the API, and no protocol exists | Accepted | D-63: ready, not built. `identity-oidc` adds provider administration, the OIDC flow and live tests |
| G2 | No RPC changes a person's email or display name, so "a tenant administrator cannot change the person's email" holds vacuously | Low | A self-service profile RPC belongs with the console's account screen; it must be the person or system scope |
| G3 | "Recovery is not reachable over the network" is shown by construction (no RPC wires it), not by a test that enumerates every procedure | Low | `AuthPublicProcedures` and the generated service list are the reviewable surface |
| G4 | The per-address throttle key is shared by every person behind one address; a determined attacker on a shared network slows everyone there for up to 15 minutes per run | Accepted | The trade D-64 chose over hard lockout; runs reset after 15 quiet minutes, and the account key is separate |
| G5 | MFA is not built | **High before GA** | D-66: `identity-mfa` (TOTP and passkeys; required for administrators) before general availability |
| G6 | Sessions and throttling read Postgres per request; at multi-node, validity may be cached only with NATS-broadcast revocation | Planned | D-66 §5 |
