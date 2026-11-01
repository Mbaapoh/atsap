## MODIFIED Requirements

### Requirement: A call a platform-owned endpoint presents is attributed to its tenant by configuration

The system SHALL determine which tenant an arriving call belongs to from configuration and from facts the caller cannot set through signalling content, never from headers. A call from a registered phone SHALL be attributed to the tenant of the extension it authenticated as. A call from a carrier SHALL be attributed from its transport source address together with the dialled number: among the trunks whose carrier addresses or inbound signalling ranges contain the source address, in any tenant, the call SHALL be attributed to the one trunk whose tenant has that number configured. When no trunk, or more than one, matches, the call SHALL be refused as unallocated, and the refusal SHALL NOT reveal any tenant. A call arriving from an address no trunk recognises, without valid extension credentials, SHALL be refused.

#### Scenario: Caller-supplied headers cannot change the tenant

- **WHEN** an arriving call carries headers naming another tenant or extension
- **THEN** the call is attributed from its source address, the dialled number and configuration, and the headers are ignored

#### Scenario: Signalling from an unknown source is refused

- **WHEN** a call arrives from an address that matches no trunk and carries no valid extension credentials
- **THEN** the call is refused and never reaches the dial plan

#### Scenario: Two tenants on one carrier each receive their own numbers

- **WHEN** two tenants each have a trunk recognising the same carrier address, and each has a different number assigned
- **AND** the carrier sends a call to the first tenant's number, then one to the second's
- **THEN** each call is attributed to the tenant that owns the number dialled, and rings that tenant's extension

#### Scenario: An ambiguous number is refused, not guessed

- **WHEN** two tenants whose trunks recognise the same carrier address have both configured the number dialled
- **THEN** the call is refused as unallocated
- **AND** the ambiguity is recorded for the operator without the carrier learning of either tenant

#### Scenario: A number of a tenant whose trunk does not recognise the source is not reached

- **WHEN** a carrier recognised only by tenant A's trunk dials a number configured only in tenant B
- **THEN** the call is refused as unallocated
