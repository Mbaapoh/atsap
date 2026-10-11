## ADDED Requirements

### Requirement: A call can begin with a channel arriving at the engine

The system SHALL accept a channel that arrives at the engine without having been originated by the platform as the first participant of a new Call, and SHALL decide what the call is before any further leg is set up. Telephony SHALL NOT itself know how a dialled number is resolved: the decision comes from a routing port, and with no router configured every arriving call is refused as "not found".

#### Scenario: An arriving call that resolves to a destination connects

- **WHEN** a channel arrives and the router resolves it to a destination
- **THEN** the call passes Screening (unless the router marks it emergency), the destination leg is originated, the arriving caller hears ringing, and when the destination answers the caller is answered and both are bridged

#### Scenario: An arriving call the router cannot place is refused at the edge

- **WHEN** a channel arrives and the router refuses it
- **THEN** the arriving channel is refused with the standard response the router named, the Call is recorded Terminated with the reason, and no other leg is set up

#### Scenario: Screening refuses an arriving call with service unavailable

- **WHEN** a non-emergency arriving call is refused by capacity
- **THEN** the arriving channel receives "service unavailable" and no destination leg is set up

#### Scenario: The arriving caller hangs up before an answer

- **WHEN** the arriving caller hangs up while the destination is still ringing
- **THEN** the destination leg is released and the Call is recorded Terminated

#### Scenario: Telephony runs with no router

- **WHEN** the platform runs telephony with no routing implementation present
- **THEN** every arriving call is refused as "not found" and every originated call behaves exactly as before
