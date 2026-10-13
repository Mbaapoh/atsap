## MODIFIED Requirements

### Requirement: No infrastructure channel identifier is ever exposed
No public interface (API response, event payload, or log entry intended
for a partner) SHALL expose an Asterisk channel identifier or any other
telephony-engine-internal identifier for a Call or Participant. A
participant's engine endpoint address is such an identifier.

#### Scenario: Querying a call's state
- **WHEN** a Call's current state and participants are queried through the
  public API
- **THEN** the response identifies participants only by their Participant
  identifiers, with no telephony-engine channel identifier present
  anywhere in the response

#### Scenario: A participant's engine endpoint is not returned
- **WHEN** a Call whose participants were reached at engine endpoints is
  queried through the public API
- **THEN** no participant in the response carries an engine endpoint
  address, and the deprecated endpoint field is empty

### Requirement: A call can begin with a channel arriving at the engine

The system SHALL accept a channel that arrives at the engine without having been originated by the platform as the first participant of a new Call, and SHALL decide what the call is before any further leg is set up. Telephony SHALL NOT itself know how a dialled number is resolved: the decision comes from a routing port, and with no router configured every arriving call is refused as "not found". The Call SHALL record the direction and the caller number the router decides, and the destination leg SHALL be presented with that caller number as its caller ID.

#### Scenario: An arriving call that resolves to a destination connects

- **WHEN** a channel arrives and the router resolves it to a destination
- **THEN** the call passes Screening (unless the router marks it emergency), the destination leg is originated, the arriving caller hears ringing, and when the destination answers the caller is answered and both are bridged

#### Scenario: The router's direction and caller number are recorded

- **WHEN** the router resolves an arriving call with a direction and a caller number
- **THEN** the stored Call, as listed and as read through the public API, carries that direction and that caller number as its source, not the values known before routing

#### Scenario: The destination is shown who is calling

- **WHEN** the destination leg of an arriving call is originated
- **THEN** it is presented with the call's caller number as caller ID, and with the caller's display name when the router supplies one

#### Scenario: An arriving call the router cannot place is refused at the edge

- **WHEN** a channel arrives and the router refuses it
- **THEN** the arriving channel is refused with the standard response the router named, the Call is recorded Terminated with the reason, and no other leg is set up

#### Scenario: Screening refuses an arriving call with service unavailable

- **WHEN** a non-emergency arriving call is refused by capacity
- **THEN** the arriving channel receives "service unavailable" and no destination leg is set up

#### Scenario: The arriving caller hangs up before an answer

- **WHEN** the arriving caller hangs up while the destination is still ringing
- **THEN** the destination leg is released and the Call is recorded Terminated

#### Scenario: A leg leaving after its call ended is not an error

- **WHEN** a call's last leg leaves after the Call has already been recorded Terminated
- **THEN** nothing about the Call changes and no error is reported

#### Scenario: Telephony runs with no router

- **WHEN** the platform runs telephony with no routing implementation present
- **THEN** every arriving call is refused as "not found" and every originated call behaves exactly as before

## ADDED Requirements

### Requirement: Readiness reflects whether call events are received

The platform SHALL report itself not ready while it is not receiving call events from the engine: while its event connection is down, and after another process has taken over its engine application. A takeover SHALL be logged as an error naming the application. The platform SHALL NOT take the application back by itself.

#### Scenario: Ready while call events flow

- **WHEN** the event connection to the engine is established and the application is held by this instance
- **THEN** readiness reports the call-events check as passing

#### Scenario: Not ready while the event connection is down

- **WHEN** the event connection to the engine is lost
- **THEN** readiness fails, naming the call-events check, until the connection is re-established

#### Scenario: Not ready after another process takes over the application

- **WHEN** another process registers the same engine application and the engine reports that this instance's application was replaced
- **THEN** an error is logged naming the application, readiness fails naming the call-events check, and this instance does not re-register the application
