## MODIFIED Requirements

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
