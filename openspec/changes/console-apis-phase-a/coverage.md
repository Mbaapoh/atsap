# Coverage — console-apis-phase-a

Every scenario in the three delta specs, mapped to what proves it. **Tiers:** U = unit, I = integration (real Postgres), W = wire (handlers over the real service, response rendered), E = e2e (running licensed app, public API, live Asterisk; `internal/pbx/e2e/console_apis_test.go`). No phone path changed, so the UATs (`uat-auto`, `uat-placement`, `uat-inbound`) re-ran unchanged as regression only — all green, `pbx reconcile` clean.

## licensing/license-api

### Licence status is readable by any authenticated caller

| Scenario | Proven by |
|---|---|
| A licensed installation reports its entitlement | **E** `TestConsoleAPIs_Licence` (tenant admin reads Valid, 16 channels). **U** `TestStatus_ValidReportsEntitlementUsageAndDates`, `TestGetLicenseStatus_AnyAuthenticatedCaller` |
| A never-activated installation says so | **U** `TestStatus_SetupIsAStatusNotAnError`, `TestGetLicenseStatus_SetupIsNotActivated` (NOT_ACTIVATED with reason) |
| An unauthenticated caller cannot read it | **U** `TestGetLicenseStatus_Unauthenticated`. Live: the auth interceptor guards every procedure except `AuthenticateUser` |
| No licence material is returned | **U** `TestStatus_CarriesNoLicenceMaterial` (rendered status holds neither half of the token). Unverified/Degraded reasons: `TestStatus_UnverifiedAndDegradedAsTimePasses` |

### A licence is applied through the API by installation-wide authority

| Scenario | Proven by |
|---|---|
| A valid token takes effect without a restart | **E** `TestConsoleAPIs_Licence` (dev-key 8-channel token → every caller sees 8 at once; original re-applied in cleanup). **U** `TestApplyLicenseKey_Outcomes/system scope…`. "A call in progress continues" — G1 |
| A forged or altered token is refused and changes nothing | **E** same test (**400** "does not verify", token not echoed, still 16). **U** `TestApplyLicenseKey_Outcomes/forged token…` |
| A tenant administrator cannot apply a licence | **E** same test (**403**, still 16). **U** `TestApplyLicenseKey_Outcomes/tenant admin…` |
| Applying is audited without the token | **E** same test (one `licence.apply` audit row, token absent). **U** the auditor receives edition and channels only; `…/an internal failure does not leak its text` |

## identity/public-api

### A tenant's users and their roles are listable through the API

| Scenario | Proven by |
|---|---|
| An administrator lists their tenant's users | **E** `TestConsoleAPIs_UsersAndRoles` (operator + new supervisor, nobody else). **I** `TestListPrincipals_PagedIsolatedAndHashless` (RLS isolation, paging, bindings). **W** `TestListPrincipals` |
| A caller without permission cannot list users | **W** `TestListPrincipals` (AGENT → PERMISSION_DENIED). **E** a revoked supervisor gets **403** |
| No credential material is listed | **E** no `argon2` in the response. **I** hash column not selected. **W** no hash or "password" in the rendered response |

### A granted role can be revoked through the API

| Scenario | Proven by |
|---|---|
| A revoked role stops authorizing | **E** `TestConsoleAPIs_UsersAndRoles` (supervisor lists, `RevokeRole`, next request **403**, one `principal.role.revoke` audit row). **W** `TestRevokeRole` |
| The last tenant administrator cannot be removed | **I** `TestRevokeRoleBinding_LastAdministratorIsKept`, `TestRevokeRoleBinding_SerializesOnTheTenantLock` (deterministic, fault-injected: fails without the lock). **W** `TestRevokeRole` (FAILED_PRECONDITION) |
| A caller cannot revoke beyond their authority | **W** `TestRevokeRole` (tenant admin vs platform system-scope role). Another tenant's binding: the interceptor's tenant matcher, as for `GrantRole` |

## telephony-core/call-lifecycle

### A tenant's calls are listable through the API

| Scenario | Proven by |
|---|---|
| A placed call appears in the list | **E** `TestConsoleAPIs_CallHistory` (`PlaceCall` → Active → hang-up; first entry, OUTBOUND, `+442071234567`). **I** `TestListCalls_IsolationFiltersPaging` (newest first, duration) |
| Another tenant's calls never appear | **I** `TestListCalls_IsolationFiltersPaging` (tenant B never listed). **E** naming another tenant → **400** "does not match the authenticated tenant" (the interceptor's existing contract, same as `GetCall`). **U** `TestTenantID_CoversListCalls` |
| Filters narrow the list | **I** `TestListCalls_IsolationFiltersPaging` (state, direction, `[from, to)`, paging; invalid state → INVALID_ARGUMENT) |
| No engine identifier is listed | **W** `TestListCalls_WireCarriesNoEngineIdentifier` (participants hold `PJSIP/…`; no `PJSIP/`, `atsa-part-`, bridge or `[et]_`+hex rendered; includes a Screening-refused call; fault-injected). **E** no `PJSIP/` in the live response |

## Gaps

| # | Gap | Severity | Note |
|---|---|---|---|
| G1 | "A call in progress continues" while a licence is applied is not proven live; the e2e applies with no call up | Low | `ApplyLicenseKey` swaps the cached entitlement only and touches no channel; `licensing-capacity-grace` proved live that no licence transition drops a call (`TestNoActiveCallIsDroppedByAnyLicenceTransition`) |
| G2 | `ListCalls` and `GetCall` have no role check: any authenticated user of the tenant can read its calls | Medium | Design D5, recorded rather than widened; arrives with LLD-09 |
| G3 | `GetCall` returns `Participant.endpoint_uri` (`PJSIP/e_…`), an engine identifier | Medium | Found in task 3.2, out of scope (existing RPC); fix before the console shows call detail |
| G4 | A tenant-mismatched request is refused as INVALID_ARGUMENT (400), not PERMISSION_DENIED | Accepted | The interceptor's existing contract for every tenant-scoped RPC; the spec requires only "refused" |
