## ADDED Requirements

### Requirement: The public call query reports recorded timestamps and the outcome

The public call query SHALL report a call's current lifecycle state together with the timestamps and outcome it has recorded so far: the start time always; the time each participant answered when it did; the time the call ended and the termination reason when it ended. It SHALL leave the end time and reason unset while the call is still in flight, and SHALL NOT include an engine channel, bridge or endpoint identifier.

#### Scenario: Querying a call in flight

- **WHEN** a call is queried before it reaches Terminated
- **THEN** the response reports its current state and start time, and no end time or termination reason

#### Scenario: Querying an answered call

- **WHEN** a call that reached Active is queried
- **THEN** the response reports Active, the time it was answered, and the answered time of each participant that has answered

#### Scenario: Querying a terminated call

- **WHEN** a terminated call is queried
- **THEN** the response reports Terminated, the time it ended, and the recorded termination reason

#### Scenario: The query leaks no engine identifier

- **WHEN** a call is queried at any point in its lifecycle
- **THEN** no participant or call field contains an engine channel, bridge or endpoint identifier
