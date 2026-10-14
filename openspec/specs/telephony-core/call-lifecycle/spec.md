## Purpose

Establishes the Call/Participant aggregate and its lifecycle for a simple
two-party call placed and bridged through Asterisk, from origination
through to a terminated, immutable record with continuous per-second usage.

## Requirements

### Requirement: Call is an aggregate of participants, not fixed roles
A Call SHALL be represented as one Call record associated with a
collection of Participants. The system SHALL NOT model a Call using fixed
"caller" and "agent" fields.

#### Scenario: Two-party call has two participant records
- **WHEN** a call is originated between two parties
- **THEN** the system records one Call and exactly two Participants
  associated with it, each independently identifiable

### Requirement: Call lifecycle for a normal two-party call
A Call SHALL progress through the states Initiated, Screening, Routing,
Presenting, Active, Terminating, Terminated, in that order, for a call
that is answered normally. The system SHALL NOT skip Screening or Routing
for a non-emergency call.

#### Scenario: Full happy-path progression
- **WHEN** a call is originated to a reachable destination and is answered
- **THEN** the call is observed to pass through Initiated, Screening,
  Routing, Presenting, and Active, in that order, before reaching
  Terminated

#### Scenario: Destination does not answer
- **WHEN** a call reaches Presenting and the destination does not answer
  within the configured alerting timeout
- **THEN** the call moves to Terminated without ever reaching Active, and
  no usage is recorded for that attempt

### Requirement: No billing or media exchange before Active
A Call SHALL NOT accrue billable usage, and SHALL NOT bridge external
media between participants, before it reaches the Active state.

#### Scenario: No usage before answer
- **WHEN** a call is in Initiated, Screening, Routing, or Presenting
- **THEN** no usage record exists yet for any of its participants

### Requirement: Screening evaluates capacity and compliance before Routing
Every call SHALL receive a capacity verdict and a compliance verdict
before leaving the Screening state. A call whose capacity or compliance
verdict is not permitted SHALL move to Terminated without reaching
Routing.

A permitted capacity verdict reserves a channel against the
installation's entitlement. Every call that reserves a channel SHALL
release it when the call terminates, by every route to termination —
normal hangup, screening rejection, and failure during setup — so that
the reserved count returns to its pre-call value exactly once.

#### Scenario: Both verdicts permitted
- **WHEN** a call in Screening receives a permitted capacity verdict and a
  permitted compliance verdict
- **THEN** the call moves to Routing

#### Scenario: A verdict is not permitted
- **WHEN** a call in Screening receives a verdict that is not permitted,
  from either check
- **THEN** the call moves to Terminated, and the reason for termination
  identifies which verdict rejected it

#### Scenario: A completed call releases its reserved channel
- **GIVEN** a call that received a permitted capacity verdict and reached
  Active
- **WHEN** the call reaches Terminated
- **THEN** the reserved channel is released exactly once

#### Scenario: A screened-out call releases nothing it did not reserve
- **WHEN** a call in Screening is terminated by a verdict that is not
  permitted
- **THEN** no channel release is performed for that call

#### Scenario: A call failing after Screening releases its channel
- **GIVEN** a call that received a permitted capacity verdict and left
  Screening
- **WHEN** the call terminates without ever reaching Active
- **THEN** the reserved channel is released exactly once

#### Scenario: Repeated termination does not release twice
- **GIVEN** a call that has already terminated and released its channel
- **WHEN** a further termination signal arrives for the same call
- **THEN** no further release is performed

#### Scenario: Release is identified by the call, not counted blindly
- **GIVEN** a call that has released its reserved channel
- **WHEN** a release is requested again for that same call, by any route
- **THEN** the reserved count is unchanged
- **AND** the outcome is the same as if the release had never been
  requested, rather than an error the caller must handle

### Requirement: Active requires two or more connected participants
A Call SHALL reach the Active state only when at least two of its
Participants are simultaneously in the Connected state.

#### Scenario: Second participant connects
- **WHEN** the first participant is already Connected and a second
  participant becomes Connected
- **THEN** the Call transitions to Active

### Requirement: Per-participant, per-second usage while connected
While a Participant is Connected, the system SHALL record exactly one
usage entry per elapsed second for that participant, with no duplicate
and no missing second, until the participant disconnects.

#### Scenario: Continuous ticking with no duplication
- **WHEN** a participant remains Connected for a period of several seconds
- **THEN** exactly one usage entry exists for that participant for each
  elapsed second in that period, and no second has more than one entry

### Requirement: Call ends when the last connected participant leaves

An Active Call SHALL move to Terminating as soon as fewer than two of its participants remain Connected, because no one is left to be connected to anyone. It SHALL release every remaining participant's leg on the engine, and SHALL move to Terminated once teardown completes. A Terminated call's record SHALL become immutable: its final state, timestamps, and billable durations SHALL NOT change afterward.

#### Scenario: Either party hangs up

- **WHEN** one of the two connected participants disconnects while the other has already left or disconnects at the same time
- **THEN** the Call moves to Terminating and then Terminated, and its record is thereafter immutable

#### Scenario: One party hanging up releases the other

- **WHEN** one of the two connected participants of an Active call disconnects and the other is still connected
- **THEN** the Call ends, the other participant's leg is released on the engine, their usage stops at that moment, and neither party is left connected to no one

### Requirement: Lifecycle transitions are published as domain events
Each Call state transition and each Participant join or leave SHALL be
published as a domain event at least once, even if the publishing system
is briefly unavailable at the moment of the transition.

#### Scenario: Event survives a transient publish failure
- **WHEN** a Call transitions to Active while the event-publishing system
  is temporarily unreachable
- **THEN** the corresponding event is still published once the
  event-publishing system becomes reachable again, without being lost

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

### Requirement: A tenant's calls are listable through the API

The system SHALL let an authorized caller list the calls of their own tenant, newest first, page by page, optionally filtered by state, direction and a time range. Each entry SHALL carry only platform-defined fields — identifier, direction, state, source and destination numbers, start, answer and end times, duration and termination reason — and SHALL never carry a channel, bridge or other engine identifier.

#### Scenario: A placed call appears in the list

- **WHEN** a call has been placed in the tenant and the caller lists calls
- **THEN** that call appears first, with its direction, numbers, state and timestamps

#### Scenario: Another tenant's calls never appear

- **WHEN** a caller lists calls
- **THEN** no call of any other tenant is returned, and naming another tenant in the request is refused

#### Scenario: Filters narrow the list

- **WHEN** a caller lists calls filtered by state, direction or time range
- **THEN** only calls matching every filter are returned

#### Scenario: No engine identifier is listed

- **WHEN** calls are listed, including calls that failed during setup
- **THEN** no field contains a channel, bridge or engine endpoint identifier

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
