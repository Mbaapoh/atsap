## MODIFIED Requirements

### Requirement: Tenant provisioning respects the installation's tenant cap

The system SHALL refuse to provision a tenant once the installation already holds as many tenants as its entitlement allows. The cap is supplied to the provisioning operation and never fetched from the licensing context, which this context must not depend on. A cap of zero SHALL mean unlimited and disable the check. The cap source SHALL also be able to report that no licence has been applied; an installation with no licence applied SHALL refuse tenant provisioning through the public API. The operator's command-line bootstrap of the first tenant is exempt, because a licence can only be applied once a platform administrator exists.

#### Scenario: A second tenant is refused on a single-tenant installation

- **WHEN** the installation's tenant cap is 1 and one tenant exists
- **THEN** provisioning another tenant is refused with `FAILED_PRECONDITION` and an entitlement reason identifying the licence, not the caller's roles

#### Scenario: Zero cap means unlimited

- **WHEN** a licence is applied and the installation's tenant cap is 0
- **THEN** tenant provisioning is never refused for entitlement, regardless of how many tenants exist

#### Scenario: No tenant before a licence is applied

- **WHEN** no licence has been applied and a tenant is provisioned through the public API
- **THEN** it is refused with `FAILED_PRECONDITION` and a reason saying no licence is applied, and no tenant is stored

#### Scenario: The first tenant can still be bootstrapped

- **WHEN** no licence has been applied and the operator bootstraps the installation from the command line
- **THEN** the first tenant and its platform administrator are created, so a licence can then be applied

#### Scenario: Reads are never gated by the cap

- **WHEN** an installation holds tenants at or above its cap
- **THEN** listing and reading those tenants still succeeds — the cap constrains provisioning, never visibility

#### Scenario: Existing tenants survive a lowered cap

- **WHEN** the entitlement in force allows fewer tenants than already exist
- **THEN** no tenant is removed or hidden, and only new provisioning is refused
