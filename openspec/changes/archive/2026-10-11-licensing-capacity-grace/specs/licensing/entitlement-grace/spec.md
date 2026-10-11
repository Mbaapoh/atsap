## Purpose

Keeps an installation fully functional through a loss of contact with the
entitlement service, warning the administrator as the outage lengthens
and degrading to a reduced capacity only after a defined grace period —
so that a network problem is never sold to a partner as an outage.

## ADDED Requirements

### Requirement: Entitlement is confirmed on a schedule, never in the call path

The system SHALL attempt to confirm its entitlement on a recurring
schedule, and SHALL record when confirmation last succeeded.

No entitlement confirmation SHALL be performed during call setup or call
teardown. A call SHALL never wait on, or fail because of, an entitlement
confirmation attempt.

#### Scenario: Confirmation runs on its own schedule

- **WHEN** the scheduled entitlement confirmation runs and succeeds
- **THEN** the time of last successful confirmation is updated
- **AND** the licence state is valid

#### Scenario: An unreachable entitlement service does not affect calls

- **GIVEN** the entitlement service is unreachable
- **WHEN** calls are set up and torn down
- **THEN** every call behaves exactly as it would with the service
  reachable

#### Scenario: A slow entitlement service does not affect calls

- **GIVEN** the entitlement service is reachable but not responding
- **WHEN** a call is set up
- **THEN** the call setup does not wait on the confirmation attempt

### Requirement: An installation operates fully during the grace period

For a defined grace period following the last successful confirmation,
the system SHALL operate with no functional restriction whatsoever:
full licensed capacity, all features, unchanged behaviour.

#### Scenario: Full function on the first day of an outage

- **GIVEN** confirmation last succeeded less than one day ago and the
  service is now unreachable
- **THEN** the licence state is unverified
- **AND** the full licensed entitlement remains available

#### Scenario: Full function throughout the grace period

- **GIVEN** confirmation last succeeded within the grace period
- **WHEN** call setups are screened up to the full licensed entitlement
- **THEN** all are permitted

### Requirement: Warnings escalate from the second day of an outage

From the second day without a successful confirmation, the system SHALL
produce administrator-visible warnings stating that entitlement is
unverified and what will change if the outage continues.

Warnings SHALL NOT be produced on the first day, so that a transient
failure does not alarm an administrator.

#### Scenario: No warning on the first day

- **GIVEN** confirmation last succeeded less than one day ago
- **THEN** no entitlement warning is produced

#### Scenario: Warnings from the second day

- **GIVEN** confirmation last succeeded more than one day ago and less
  than the grace period ago
- **THEN** an entitlement warning is produced
- **AND** it states that function is currently unrestricted

### Requirement: After the grace period the system degrades and is never disabled

Once the grace period elapses without a successful confirmation, the
system SHALL reduce capacity to a defined minimum.

It SHALL NOT be disabled. Calls in progress SHALL continue. Emergency
calls SHALL connect. Administration and the API SHALL remain available so
that the situation can be resolved from the degraded system itself.

#### Scenario: Capacity reduces once the grace period elapses

- **GIVEN** confirmation last succeeded longer ago than the grace period
- **THEN** the licence state is degraded
- **AND** the entitlement in force is the defined minimum

#### Scenario: Degradation never reaches zero

- **GIVEN** the installation has been degraded for an arbitrarily long
  time
- **WHEN** a call is placed within the defined minimum
- **THEN** it is permitted

#### Scenario: Degradation does not drop calls in progress

- **GIVEN** active calls holding more channels than the defined minimum
- **WHEN** the grace period elapses and the system degrades
- **THEN** no active call is terminated

#### Scenario: The system can be repaired from itself

- **GIVEN** the installation is degraded
- **THEN** administration and the API remain reachable and functional

### Requirement: Confirmation restores full capacity without interruption

A successful entitlement confirmation SHALL return the system to its
valid state and its full licensed entitlement, without a restart and
without interrupting any call in progress.

#### Scenario: Recovery from degraded

- **GIVEN** the installation is degraded
- **WHEN** an entitlement confirmation succeeds
- **THEN** the licence state is valid
- **AND** the full licensed entitlement is available again

#### Scenario: Recovery does not disturb active calls

- **GIVEN** the installation is degraded with calls in progress
- **WHEN** an entitlement confirmation succeeds
- **THEN** no call in progress is interrupted

### Requirement: Licence state is a function of elapsed time, evaluated identically everywhere

The licence state SHALL be derived from the time elapsed since the last
successful confirmation, so that the same elapsed time always yields the
same state.

State SHALL NOT depend on how many times confirmation was attempted, on
process uptime, or on the order in which components observe it.

#### Scenario: The same elapsed time yields the same state

- **WHEN** the state is evaluated twice for the same last-confirmed time
  and the same current time
- **THEN** both evaluations yield the same state

#### Scenario: Repeated failures do not accelerate degradation

- **GIVEN** confirmation has failed many times within the grace period
- **THEN** the state is determined solely by elapsed time since the last
  success, not by the number of failures

#### Scenario: A restart does not reset the grace period

- **GIVEN** confirmation last succeeded longer ago than the grace period
- **WHEN** the system restarts
- **THEN** it is degraded on start, not returned to valid
