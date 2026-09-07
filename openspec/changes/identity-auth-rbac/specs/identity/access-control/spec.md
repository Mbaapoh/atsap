## Purpose

Establishes what an authenticated caller is permitted to do: authorization that
refuses anything not explicitly granted, and the tenant boundary that keeps one
organisation's request from ever reaching another organisation's data.

## ADDED Requirements

### Requirement: Authorization denies anything not explicitly granted
The system SHALL permit an action only when the acting principal holds a role
binding matching that action and resource. Absence of a matching binding SHALL
result in denial, never in a default allowance.

#### Scenario: No matching binding denies the action
- **WHEN** a principal with no role binding for an action attempts it
- **THEN** the action is refused and produces no effect on any record

#### Scenario: Matching binding permits the action
- **WHEN** a principal holds a role binding covering the attempted action and
  resource
- **THEN** the action proceeds

### Requirement: Broad grants exist only as explicitly scoped administrative roles
The system SHALL support wildcard authority only in explicitly scoped
administrative forms bounded to a single tenant or to platform operation. It
SHALL NOT support an unscoped grant that matches every action across every
tenant.

#### Scenario: Tenant administrator is bounded to its own tenant
- **WHEN** a principal holding tenant-wide administrative authority attempts an
  action against another tenant's resource
- **THEN** the action is refused, exactly as it would be for any other
  principal of that tenant

### Requirement: A caller acts only within its own tenant
When a request names a tenant, the system SHALL require that tenant to match
the authenticated caller's tenant, and SHALL refuse the request otherwise.

#### Scenario: Request naming another tenant is refused
- **WHEN** an authenticated caller submits a request naming a tenant other
  than its own
- **THEN** the request is refused without performing the requested operation

### Requirement: Another tenant's resources are indistinguishable from absent ones
A request for a resource belonging to a different tenant SHALL produce the same
outcome as a request for a resource that does not exist. No response, error
message, or timing difference SHALL reveal that the resource exists elsewhere.

#### Scenario: Cross-tenant resource lookup looks like a miss
- **WHEN** an authenticated caller requests a resource identifier that exists
  but belongs to a different tenant
- **THEN** the response is the same not-found outcome returned for an
  identifier that exists nowhere, disclosing nothing about the real owner
