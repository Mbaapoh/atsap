## Purpose

Establishes tenants and principals as durable, governed records: who exists in
the system, which organisation they belong to, and how an organisation or an
individual account is suspended and restored without destroying either.

## Requirements

### Requirement: A tenant is a durable record with an immutable residency zone
The system SHALL represent each partner organisation as a tenant record
carrying a name, a status, and a data residency zone. Once a tenant is
provisioned, its residency zone SHALL NOT change by any operation.

#### Scenario: Residency zone is fixed at provisioning
- **WHEN** a tenant is provisioned with a residency zone
- **THEN** that zone is readable on the tenant thereafter, and any attempt to
  change or blank it is rejected

### Requirement: A principal belongs to exactly one tenant
Every principal SHALL be one person's membership of exactly one tenant: it
SHALL belong to exactly one tenant and to exactly one user account. The
system SHALL NOT permit a principal to exist without a tenant or without a
user, nor to act for any tenant other than its own. Identifiers of
principals SHALL remain stable, so that every record naming a principal
continues to name the same membership.

#### Scenario: Principal is created under a tenant
- **WHEN** a principal is provisioned for a tenant
- **THEN** the principal is associated with exactly that tenant and one
  user account, and its identity is meaningful only within that tenant

#### Scenario: Acting as a membership never reaches another tenant
- **WHEN** a person who is a member of two tenants acts with an access
  token for one of them and names the other in a request
- **THEN** the request is refused, exactly as it would be for a person
  belonging only to the first tenant

### Requirement: Suspension and disablement are reversible and non-destructive
Suspending a tenant or disabling a principal SHALL block new authenticated
access for the affected scope while leaving the underlying records intact, and
SHALL be fully reversible by restoring the prior status. Neither action SHALL
terminate a call that is already active.

#### Scenario: Suspended tenant blocks new access
- **WHEN** a tenant is suspended and one of its principals then presents
  otherwise-valid credentials
- **THEN** access is refused, and no data belonging to that tenant is returned

#### Scenario: Restoring a suspended tenant
- **WHEN** a suspended tenant is returned to active status
- **THEN** its principals authenticate successfully again, with the same
  identities and role assignments they had before suspension

#### Scenario: Active calls survive suspension
- **WHEN** a tenant is suspended while one of its calls is Active
- **THEN** that call continues to completion and is never terminated by the
  suspension

### Requirement: Provisioning mutations are recorded
Every tenant or principal creation, status change, or role change SHALL
produce exactly one audit record attributing the change to the actor that made
it.

#### Scenario: Provisioning writes an audit record
- **WHEN** an administrator provisions a new principal
- **THEN** exactly one audit record exists for that action, naming the acting
  administrator and the principal that was created

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
