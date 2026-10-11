## ADDED Requirements

### Requirement: A trunk's channel limit caps its concurrent placed calls

The system SHALL count the call legs it has placed through each trunk while their calls are live, and SHALL NOT place a new leg through a trunk already at its channel limit. A trunk at its limit SHALL be passed over for the next selected candidate; when no candidate has room the placement is refused. A leg stops counting as soon as its call has ended, however it ended. Concurrent placements SHALL NOT together exceed the limit.

#### Scenario: A full trunk is passed over

- **WHEN** the preferred trunk for a destination is at its channel limit and another selected trunk has room
- **THEN** the call leaves through the trunk with room

#### Scenario: No room anywhere refuses the call

- **WHEN** every trunk selected for a destination is at its channel limit
- **THEN** the placement is refused with a reason identifying trunk capacity, and no call is set up

#### Scenario: An ended call frees its channel

- **WHEN** a call placed through a trunk ends, whether hung up through the API or by either party
- **THEN** that trunk's count no longer includes it

#### Scenario: Racing placements cannot exceed the limit

- **WHEN** two placements race for a trunk's last free channel
- **THEN** at most one of them uses it
