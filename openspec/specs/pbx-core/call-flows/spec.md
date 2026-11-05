# Call flows

### Requirement: A DID can route through a time-based flow

The system SHALL let a tenant attach a DID to a flow with an open and a
closed destination and a UTC business window. An inbound call SHALL ring
the open destination inside the window and the closed destination on a
holiday or outside it. A DID SHALL NOT be both a flow and a direct
inbound number.

#### Scenario: Open hours ring the open destination
- **WHEN** a DID enters a flow inside its business window and not on a holiday
- **THEN** the call rings the flow's open destination

#### Scenario: Closed hours and holidays use the closed destination
- **WHEN** the call arrives outside the window or on a holiday
- **THEN** the call rings the closed destination

#### Scenario: A closed flow with no destination refuses
- **WHEN** the closed branch has no destination
- **THEN** the call is refused rather than left silent

### Requirement: Flow revisions are published and rollbackable

The system SHALL keep published flow revisions immutable and SHALL let an
administrator publish a new revision or make an older one active. Every
publish and rollback SHALL be audited.

#### Scenario: Publishing a revision
- **WHEN** an administrator publishes a new configuration
- **THEN** a new immutable revision becomes active and the prior one remains available

#### Scenario: Rolling back
- **WHEN** an administrator activates an earlier revision
- **THEN** the same configuration routes new calls again
