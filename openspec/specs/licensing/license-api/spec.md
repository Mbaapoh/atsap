# licensing/license-api Specification

## Purpose
Lets an administrator see the installation's licence and apply a new one through the public API, so the console's licence screen needs nothing but the API, and no operator ever has to edit a configuration file or restart the platform to licence it.

## Requirements

### Requirement: Licence status is readable by any authenticated caller

The system SHALL let any authenticated caller read the installation's licence status: its state (not activated, valid, unverified, or degraded), edition, licensed channels, channels currently in use, tenant and extension caps, expiry, when the entitlement was last confirmed, and the reason for any state other than valid. The status SHALL never include the licence token, its signature or any key material.

#### Scenario: A licensed installation reports its entitlement

- **WHEN** an authenticated caller reads the licence status of a licensed installation
- **THEN** the response carries the state, edition, licensed channels, channels in use and caps that are in force

#### Scenario: A never-activated installation says so

- **WHEN** the licence status is read on an installation that has never been licensed
- **THEN** the state is "not activated" with that reason, rather than an error or a zero-capacity licence

#### Scenario: An unauthenticated caller cannot read it

- **WHEN** a caller with no valid token reads the licence status
- **THEN** the request is refused as unauthenticated

#### Scenario: No licence material is returned

- **WHEN** the licence status is read
- **THEN** no field contains the token, its signature or any key

### Requirement: A licence is applied through the API by installation-wide authority

The system SHALL let a caller holding installation-wide (system-scope) authority apply a signed licence token. A token that verifies SHALL take effect immediately, without a restart and without interrupting any call in progress, and the application SHALL be audited without recording the token. A token that does not verify SHALL be refused, and the licence in force SHALL remain exactly as it was.

#### Scenario: A valid token takes effect without a restart

- **WHEN** a system-scope caller applies a valid token granting more channels
- **THEN** the licence status reports the new entitlement at once, and a call already in progress continues

#### Scenario: A forged or altered token is refused and changes nothing

- **WHEN** a token whose signature does not verify is applied
- **THEN** the request is refused with an invalid-argument reason, and the licence status is unchanged

#### Scenario: A tenant administrator cannot apply a licence

- **WHEN** a caller with authority over only their own tenant applies a token
- **THEN** the request is refused as permission denied, and the licence is unchanged

#### Scenario: Applying is audited without the token

- **WHEN** a licence is applied
- **THEN** an audit record names who applied it and the resulting edition and capacity, and contains no token material

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
