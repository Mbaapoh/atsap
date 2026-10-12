## ADDED Requirements

### Requirement: Licence status reports the PRD condition, decided by the server

The system SHALL report, with the licence status, the installation's licence and capacity condition as one of Setup, Free Community, Valid, Capacity Warning, Capacity Reached, Entitlement Unverified or Degraded. It SHALL also report the facts that condition is built from: the tier (Free Community or Commercial) and the capacity level (Normal, Warning or Reached; none in Setup).

Capacity SHALL be Warning when channels in use reach 80% of the channels in force, and Reached when they reach 100%. Calls admitted into the burst allowance SHALL still count as Reached.

When more than one condition applies, the reported condition SHALL be the most urgent, in this order: Setup, Degraded, Capacity Reached, Entitlement Unverified, Capacity Warning, Free Community, Valid. The condition SHALL be computed by the server; a client SHALL be able to show it without calculating anything.

#### Scenario: A Free Community installation says so

- **WHEN** the licence in force is the Free Community tier and usage is below 80% of its channels
- **THEN** the condition is Free Community and the tier is Free Community, not Valid

#### Scenario: Usage at 80% is a warning

- **WHEN** channels in use reach 80% of the channels in force, and stay below 100%
- **THEN** the condition is Capacity Warning and the capacity level is Warning

#### Scenario: Usage at 100% is reached, including the burst allowance

- **WHEN** channels in use reach the channels in force, or exceed them within the burst allowance
- **THEN** the condition is Capacity Reached and the capacity level is Reached

#### Scenario: The most urgent condition wins and the other facts remain

- **WHEN** an installation is Entitlement Unverified and at full capacity at the same time
- **THEN** the condition is Capacity Reached, the state still reports Entitlement Unverified, and the capacity level reports Reached

#### Scenario: Degradation explains itself before capacity

- **WHEN** an installation is Degraded and its reduced capacity is full
- **THEN** the condition is Degraded with its reason, and the capacity level reports Reached

#### Scenario: Setup has no capacity

- **WHEN** the installation has never been licensed
- **THEN** the condition is Setup and no capacity level is reported
