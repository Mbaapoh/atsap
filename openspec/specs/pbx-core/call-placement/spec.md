## Purpose

Lets a tenant place and end a call through the public API: the platform resolves what was dialled, chooses how it leaves, and connects it. This is the path that makes a real call possible without touching the engine.

## Requirements

### Requirement: A call is placed from an extension to a dialled destination through the public API

The system SHALL let an authorized caller place a call from one of their tenant's extensions to a dialled destination, and SHALL return an identifier for the call. The system SHALL ring the calling extension first, and SHALL ring the destination only once the calling extension has answered; when the calling extension does not answer within the placement timeout, the call SHALL end and the destination SHALL NOT be rung. The platform, never the caller, decides how the destination is reached: the request carries the extension and the digits as dialled, and nothing that names an engine endpoint, trunk address or routing context.

#### Scenario: An outbound call is placed over a trunk

- **WHEN** an authorized caller places a call from their extension to a number matched by one of their outbound routes
- **THEN** the extension rings first, and only when it answers does a leg leave through the selected trunk
- **AND** when the destination answers, the two parties are connected with media flowing in both directions

#### Scenario: A call to another extension stays internal

- **WHEN** the dialled destination is the number of another extension in the caller's tenant
- **THEN** the call connects the two extensions without using any trunk

#### Scenario: An unroutable destination is refused

- **WHEN** the dialled destination is neither a tenant extension nor matched by any route with a usable trunk
- **THEN** the request is refused as a failed precondition with a reason identifying the destination as unroutable, and no call is set up

#### Scenario: Another tenant's extension cannot place a call

- **WHEN** a caller places a call from an extension that belongs to another tenant
- **THEN** the outcome is identical to naming an extension that does not exist

#### Scenario: An unauthorized caller cannot place a call

- **WHEN** a caller without permission to place calls in the tenant attempts one
- **THEN** the request is refused as permission denied and no call is set up

#### Scenario: The destination is never rung before the caller answers

- **WHEN** a call is placed and the calling extension has not answered
- **THEN** no leg to the destination exists

#### Scenario: A caller who does not answer rings no one

- **WHEN** the calling extension does not answer within the placement timeout
- **THEN** the call ends with a reason saying the caller did not answer, and the destination was never rung

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

### Requirement: An emergency destination is never refused by a commercial control

The system SHALL recognise an emergency destination on the digits as dialled, before any other evaluation, and SHALL place it with capacity, compliance and suspension checks bypassed (INV-01). The decision to bypass SHALL be made only by the platform's own match: no request field can request or influence it.

#### Scenario: An emergency call connects with capacity exhausted

- **WHEN** an extension dials an emergency number while the installation's licensed capacity is exhausted
- **THEN** the call is placed and connects

#### Scenario: A near-emergency number is an ordinary call

- **WHEN** the dialled digits only begin with an emergency number, or carry signalling characters around one
- **THEN** the call is treated as ordinary and is subject to every check

#### Scenario: The wire cannot request a bypass

- **WHEN** a place-call request is decoded with every field populated
- **THEN** the resulting call is exempt from checks only if the dialled destination matched the emergency set

### Requirement: A trunk's outcome on a placed call is observed as its health

The system SHALL report the observed outcome of each call leg it sends through a trunk as that trunk's health. A leg that is answered reports success. A leg that the trunk could not originate reports failure. Answer time is not used as a carrier signal: it includes the time a person takes to pick up. An outcome that cannot be attributed to the carrier changes nothing.

#### Scenario: An answered trunk leg keeps the trunk healthy

- **WHEN** a call leg through a trunk is answered, however long it rang
- **THEN** the trunk is reported healthy

#### Scenario: A trunk that fails to originate leaves selection

- **WHEN** a call leg cannot be originated through a trunk
- **THEN** the trunk is reported unhealthy and is skipped by subsequent placements
- **AND** no call already in progress is affected

#### Scenario: An unanswered call does not blame the carrier

- **WHEN** a trunk leg rings but is never answered and no carrier failure is recorded
- **THEN** the trunk's health is unchanged

### Requirement: No engine identifier reaches a caller on the placement path

The system SHALL NOT expose any engine endpoint name, trunk address string, routing context or channel identifier in any place-call or hang-up response or error.

#### Scenario: Responses and errors are engine-free

- **WHEN** calls are placed, refused and ended through the API, including on internal failure
- **THEN** no response or error contains an engine identifier

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
