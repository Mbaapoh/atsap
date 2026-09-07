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
Every principal SHALL belong to exactly one tenant. The system SHALL NOT
permit a principal to exist without a tenant, nor to act for any tenant other
than its own.

#### Scenario: Principal is created under a tenant
- **WHEN** a principal is provisioned for a tenant
- **THEN** the principal is associated with exactly that tenant, and its
  identity is meaningful only within that tenant

### Requirement: Usernames are unique per tenant, not globally
The system SHALL reject a second principal with the same username within one
tenant, and SHALL accept the same username in a different tenant as an
entirely independent principal.

#### Scenario: Same username in two tenants
- **WHEN** two different tenants each provision a principal with the username
  "admin"
- **THEN** both are created successfully as independent principals, and
  authenticating as one never yields access to the other's tenant

#### Scenario: Duplicate username within one tenant
- **WHEN** a tenant already has a principal with a given username and another
  is provisioned with the same username
- **THEN** the second provisioning is rejected and no second principal exists

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
