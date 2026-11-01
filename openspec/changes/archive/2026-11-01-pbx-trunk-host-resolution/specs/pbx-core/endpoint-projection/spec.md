## MODIFIED Requirements

### Requirement: Divergence between configuration and engine state is detectable and repairable
The system SHALL provide an operator-invoked means to compare engine-facing
state against the configuration records and report any divergence, and to
repair it by making the engine-facing state match the configuration. The
configuration records SHALL be treated as the sole source of truth; for a
trunk they include the carrier addresses last resolved for its host, so the
comparison and the repair SHALL NOT resolve names themselves. Repair SHALL
NOT run automatically on a schedule.

#### Scenario: Divergence is reported
- **WHEN** engine-facing state is altered outside the platform and the
  comparison is invoked
- **THEN** the divergence is reported, identifying what differs

#### Scenario: Repair restores agreement
- **WHEN** the operator invokes repair after a divergence is reported
- **THEN** the engine-facing state is made to match the configuration records,
  and a subsequent comparison reports no divergence

#### Scenario: A trunk's recognised addresses are compared with its recorded addresses
- **WHEN** a trunk's engine-facing recognition holds a host name, or
  addresses other than those recorded for the trunk
- **THEN** the comparison reports the divergence
- **AND** repair restores the recorded addresses, without a name lookup

#### Scenario: No silent automatic repair
- **WHEN** a divergence exists and no operator invokes repair
- **THEN** the divergence persists and remains reportable, rather than being
  corrected without record
