# Scenario coverage (task 10.2)

Every scenario in the two delta specs, mapped to what verifies it. An
unmapped scenario is a gap to state, not a formality to wave through — so
the gaps are at the bottom, named.

Legend: **unit** (no I/O) · **integration** (real Postgres, `-tags
integration`) · **live** (run by hand against the dev stack, recorded in
the commit) · **structural** (asserted against the API's shape, so the
guarantee cannot be quietly removed).

## `pbx-core/extension-management`

| Scenario | Verified by | Kind |
|---|---|---|
| An administrator creates an extension | `TestCreateExtension_WritesDomainProjectionAndAudit` | integration |
| Deleting an extension removes it | `TestDeleteExtension_RemovesDomainAndProjection` | integration |
| An unauthorized caller is refused | `TestCreateExtension_RefusedWithoutPermission`, `TestEveryMethod_RequiresAuthentication` | integration, unit |
| Same number in two tenants | `TestExtensionStore_NumberUniquePerTenantNotGlobally` | integration |
| Duplicate number within one tenant | `TestExtensionStore_NumberUniquePerTenantNotGlobally` | integration |
| Reading another tenant's extension | `TestExtensionStore_CrossTenantReadsFindNothing`, `TestNotFound_IsIdenticalForForeignAndNonexistent` | integration, unit |
| Secret is returned once at creation | `TestCreateExtension_Success`, `TestOnlyMintingResponsesCarryTheSecret` | unit, structural |
| A caller-supplied secret is not honoured | `TestRequests_CannotCarryASecret` | structural |
| Regeneration replaces the credential | `TestRegenerateSecret_ReplacesTheCredential` | integration |
| Username differs from the number | `TestEndpointIdentifier_IsNotDerivedFromTheNumber` | unit |
| A newly created extension is not registered | `TestCreateExtension_WritesDomainProjectionAndAudit` | integration |
| State reflects a real registration | `TestListExtensions_AttachesRegistrationStatus`; live 9.1 | integration, live |
| Registration state cannot be written | `TestRequests_CannotSetRegistrationState` | structural |
| A malformed extension number is rejected | `TestValidateNumber`, `TestCreateExtension_RejectsInvalidNumber` | unit, integration |
| Creation is audited | `TestCreateExtension_WritesDomainProjectionAndAudit` | integration |
| Audit entries carry no credential material | `TestAuditRows_NeverContainCredentialMaterial` | integration |

## `pbx-core/endpoint-projection`

| Scenario | Verified by | Kind |
|---|---|---|
| A device registers immediately after creation | live 9.1 — **gap G1** | live only |
| Deletion takes effect immediately | live 9.2 — **gap G1** | live only |
| Engine projection fails | `TestCreateExtension_ProjectionFailureLeavesNoDomainRow` | integration |
| Configuration write fails | `TestCreateExtension_DomainFailureLeavesNoProjection` | integration |
| Stored state contains no plaintext secret | `TestProjectExtension_WritesAllThreeRows` (asserts `password` NULL) | integration |
| Authentication still works against the stored form | `TestHA1_MatchesTheSchemeAsteriskAccepted`; live 9.1 | unit, live |
| A wrong secret is refused | live 9.3 — **gap G1** | live only |
| API responses carry no engine identifiers | `TestNoEngineIdentifierCrossesTheWire`, `TestListResponse_CarriesNoEngineVocabulary` | unit |
| Errors carry no engine identifiers | `TestInternalError_DoesNotLeakTheUnderlyingMessage` | unit |
| Identical numbers in two tenants remain separate | `TestExtensionStore_NumberUniquePerTenantNotGlobally` + `TestEndpointIdentifier_IsNotDerivedFromTheNumber` — **gap G2** | integration, unit |
| The engine cannot read tenant data | `TestPbxSchema_EngineRoleReadsOnlyTheProjection` (asserts SQLSTATE 42501) | integration |
| The engine can read what it needs | `TestPbxSchema_EngineRoleReadsOnlyTheProjection` | integration |
| Divergence is reported | `TestReconcile_DetectsHandEditedRow`, `_DetectsMissingProjection`, `_DetectsOrphanedProjection` | integration |
| Repair restores agreement | `TestRepair_RestoresAgreementAndIsOptIn`, `TestRepair_RemovesOrphanedProjections` | integration |
| No silent automatic repair | `TestRepair_RestoresAgreementAndIsOptIn`, `TestReconcile_ReportsButNeverRepairsUnattributableRows` | integration |

## Gaps

### G1 — three scenarios are verified live but not automated

*A device registers immediately after creation*, *deletion takes effect
immediately*, and *a wrong secret is refused* were each proven against
the running engine and recorded in commit `6b87c89`:

- create via API → `pjsip show endpoints` lists it with no reload
- real `REGISTER` → `200 OK`, **and the contact confirmed bound in
  `ps_contacts`**, not inferred from the 200
- status read back through the public API as `REGISTERED`
- wrong secret → `401`
- delete via API → correct secret now `401`, no reload

**No automated test covers them.** They are the change's headline claims,
and they are currently verified by a human doing it once. A regression
would not be caught by `mise run test`.

Closing this needs a `pbx` e2e tier alongside `internal/telephony/e2e`:
the API server, a SIP client, and the dev stack. `telephony-core` already
has that shape — an auto-answering sidecar (`cmd/sip-ua`) plus a tagged
e2e suite — so the pattern exists to copy rather than invent.

**Recommended as the first task of the next `pbx-core` change**, not
deferred silently. Until then, these three scenarios rest on a manual
check.

### G2 — cross-tenant separation is proven at the database, not at the engine

*Identical numbers in two tenants remain separate* is covered by an
integration test (the same number in two tenants yields two independent
extensions under real RLS) and a unit test (projected identifiers derive
from the extension UUID, never the number, and never collide).

What is **not** staged is a live registration against two tenants'
extension 1000 at once. A second tenant cannot be bootstrapped through
the API — `bootstrap` refuses once a tenant exists, and `ProvisionTenant`
needs platform scope — so the live version needs fixture work the change
did not carry.

The property under test is a database and derivation one, and both halves
are tested. The residual risk is that Asterisk resolves two
UUID-derived identifiers to each other, which is not a plausible failure
mode. Recorded rather than left implied.

## Not a gap, stated for the reader

Three scenarios are satisfied by the **shape** of the API rather than by
logic: a caller cannot supply a secret, cannot write registration state,
and cannot read a secret back through any response. Those are asserted
against the proto descriptors (`TestRequests_CannotCarryASecret`,
`TestRequests_CannotSetRegistrationState`,
`TestOnlyMintingResponsesCarryTheSecret`) rather than left as "impossible
by construction", because a construction only holds while nobody changes
it. The assertions were fault-injected to confirm they fail when the
shape changes.
