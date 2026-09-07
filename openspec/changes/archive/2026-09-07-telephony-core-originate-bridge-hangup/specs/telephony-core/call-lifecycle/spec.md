## Purpose

Establishes the Call/Participant aggregate and its lifecycle for a simple
two-party call placed and bridged through Asterisk, from origination
through to a terminated, immutable record with continuous per-second usage.

## ADDED Requirements

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

#### Scenario: Both verdicts permitted
- **WHEN** a call in Screening receives a permitted capacity verdict and a
  permitted compliance verdict
- **THEN** the call moves to Routing

#### Scenario: A verdict is not permitted
- **WHEN** a call in Screening receives a verdict that is not permitted,
  from either check
- **THEN** the call moves to Terminated, and the reason for termination
  identifies which verdict rejected it

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
A Call SHALL move to Terminating when its last remaining Connected
participant leaves, and SHALL move to Terminated once teardown completes.
A Terminated call's record SHALL become immutable: its final state,
timestamps, and billable durations SHALL NOT change afterward.

#### Scenario: Either party hangs up
- **WHEN** one of the two connected participants disconnects while the
  other has already left or disconnects at the same time
- **THEN** the Call moves to Terminating and then Terminated, and its
  record is thereafter immutable

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
telephony-engine-internal identifier for a Call or Participant.

#### Scenario: Querying a call's state
- **WHEN** a Call's current state and participants are queried through the
  public API
- **THEN** the response identifies participants only by their Participant
  identifiers, with no telephony-engine channel identifier present
  anywhere in the response
