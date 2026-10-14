## MODIFIED Requirements

### Requirement: A call dialled from a desk phone follows the tenant's dial plan

The system SHALL resolve a number dialled on a registered phone with the same dial plan used for calls placed through the API: an emergency number first, then another extension in the tenant, then the tenant's outbound routes. The call SHALL reach its destination, or be refused with a standard signal. The call SHALL be attributed to the dialling extension: its caller number is that extension's number and its caller name that extension's display name, never the phone's engine endpoint.

#### Scenario: A desk phone dials an external number

- **WHEN** a registered phone dials a number matched by an outbound route
- **THEN** the call leaves through the selected trunk and, when answered, the parties are connected, and the call is recorded OUTBOUND with the dialling extension's number as its source

#### Scenario: A desk phone dials another extension

- **WHEN** a registered phone dials another extension's number in the same tenant
- **THEN** that extension rings and the call stays internal: it is recorded INTERNAL with the dialling extension's number as its source, and the ringing phone is shown the dialling extension's number and name

#### Scenario: A desk phone dials an unroutable number

- **WHEN** a registered phone dials a number that is neither an extension nor routable
- **THEN** the call is refused with a standard "not found" signal

#### Scenario: A full trunk is passed over for a desk-phone call

- **WHEN** a desk-phone call's preferred trunk is at its channel limit and another selected trunk has room
- **THEN** the call leaves through the trunk with room
