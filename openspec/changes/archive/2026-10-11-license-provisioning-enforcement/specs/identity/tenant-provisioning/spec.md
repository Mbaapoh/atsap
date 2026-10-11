## ADDED Requirements

### Requirement: Tenant provisioning respects the installation's tenant cap

The system SHALL refuse to provision a tenant once the installation already holds as many tenants as its entitlement allows. The cap is supplied to the provisioning operation and never fetched from the licensing context, which this context must not depend on. A cap of zero SHALL mean unlimited and disable the check.

#### Scenario: A second tenant is refused on a single-tenant installation

- **WHEN** the installation's tenant cap is 1 and one tenant exists
- **THEN** provisioning another tenant is refused with `FAILED_PRECONDITION` and an entitlement reason identifying the licence, not the caller's roles

#### Scenario: Zero cap means unlimited

- **WHEN** the installation's tenant cap is 0
- **THEN** tenant provisioning is never refused for entitlement, regardless of how many tenants exist

#### Scenario: Reads are never gated by the cap

- **WHEN** an installation holds tenants at or above its cap
- **THEN** listing and reading those tenants still succeeds — the cap constrains provisioning, never visibility

#### Scenario: Existing tenants survive a lowered cap

- **WHEN** the entitlement in force allows fewer tenants than already exist
- **THEN** no tenant is removed or hidden, and only new provisioning is refused
