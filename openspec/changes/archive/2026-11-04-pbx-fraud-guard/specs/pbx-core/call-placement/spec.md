## ADDED Requirements

### Requirement: Outbound calling is guarded per tenant

The system SHALL refuse a new trunk-bound, non-emergency call when the
tenant's outbound calling is suspended, when its destination matches a
blocked prefix, or when its daily or monthly rated spend cap is reached.
It SHALL never refuse an emergency call for any of these reasons, and
SHALL never interrupt a call already in progress.

#### Scenario: A suspended tenant cannot place an outbound call
- **WHEN** the tenant's outbound calling is suspended
- **THEN** a new trunk-bound call is refused before the engine, while calls in progress continue

#### Scenario: A blocked prefix is refused
- **WHEN** the dialled destination matches a configured blocked prefix
- **THEN** the call is refused before any channel is reserved

#### Scenario: A spend cap blocks the next call
- **WHEN** the tenant's rated spend has reached the daily or monthly cap
- **THEN** the next outbound call is refused and the administrators are alerted, while calls in progress continue

#### Scenario: Emergency is never blocked
- **WHEN** any extension dials an emergency number, whatever the policy or suspension
- **THEN** the call is placed

### Requirement: Rated spend is recorded from answered calls

The system SHALL rate an answered trunk call from the selected route's
stored cost per minute and its answered duration, and accrue it in a
per-tenant daily ledger. Unanswered calls SHALL NOT accrue spend.

#### Scenario: An unanswered call costs nothing
- **WHEN** a placed trunk call ends before the destination answers
- **THEN** no spend is added to the ledger

### Requirement: High-volume international calling is suspended automatically

The system SHALL detect when a tenant exceeds its configured number of
international attempts inside the window outside business hours, suspend
outbound calling for that tenant, and alert its administrators, all
within the configured window. It SHALL record each attempt under a
per-tenant lock so concurrent attempts cannot evade the threshold.

#### Scenario: A fraud pattern suspends the tenant
- **WHEN** the configured threshold of international calls is crossed outside business hours
- **THEN** the next attempt is refused, outbound is suspended, and the administrators are alerted

#### Scenario: Emergency still works while suspended
- **WHEN** outbound is suspended by the anomaly rule
- **THEN** an emergency call still connects
