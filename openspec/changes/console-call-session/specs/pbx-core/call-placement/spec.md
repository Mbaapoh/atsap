## MODIFIED Requirements

### Requirement: A call placed through the API can be ended through the API

The system SHALL let an authorized caller end a call in their tenant by its identifier at any point in flight, including while the calling extension is still ringing. Ending a call before its destination is dialled SHALL leave the destination unrung. Ending a call that has already ended SHALL succeed without effect, so a retried hang-up is harmless. The call's final state and recorded reason SHALL remain queryable after it ends.

#### Scenario: Hanging up ends the call

- **WHEN** an authorized caller hangs up a call in progress
- **THEN** every leg of the call is released and the call is recorded as ended

#### Scenario: Hanging up before the caller answers

- **WHEN** an authorized caller ends a call whose calling extension is still ringing and whose destination has not been dialled
- **THEN** the calling extension's leg is released, the destination is never rung, and the call is recorded as ended

#### Scenario: Hanging up twice is harmless

- **WHEN** a hang-up is requested for a call that has already ended
- **THEN** the request succeeds and changes nothing

#### Scenario: Another tenant's call cannot be ended

- **WHEN** a caller hangs up a call belonging to another tenant
- **THEN** the outcome is identical to naming a call that does not exist, and the call is unaffected

## ADDED Requirements

### Requirement: A placed call reports live progress and its final reason

The system SHALL let a caller observe a placed call from placement to termination through the public API: its current lifecycle state, when it started, when the calling extension answered, when the destination answered, when it ended, and the recorded termination reason. It SHALL NOT report an ended call as active, and it SHALL NOT report the destination as reached before the calling extension has answered.

#### Scenario: The caller's extension is ringing

- **WHEN** a call is placed and the calling extension has not answered
- **THEN** the call is reported as Presenting, with its start time, and no destination leg exists

#### Scenario: The destination is dialled only after the caller answers

- **WHEN** the calling extension answers
- **THEN** the call reports the calling participant as Connected and the destination is dialled, while the call remains Presenting until the destination answers

#### Scenario: A connected call reports its answer time

- **WHEN** the destination answers and the two parties are bridged
- **THEN** the call is reported as Active, with the time it was answered and a running duration

#### Scenario: An ended call reports its reason

- **WHEN** a placed call ends, by a hang-up or by the caller-timeout
- **THEN** it is reported as Terminated, with the time it ended and the recorded reason, and never as Active

#### Scenario: A caller who does not answer is reported as such

- **WHEN** the calling extension does not answer within the placement timeout
- **THEN** the call ends with the reason `caller_no_answer`, and the destination was never dialled
