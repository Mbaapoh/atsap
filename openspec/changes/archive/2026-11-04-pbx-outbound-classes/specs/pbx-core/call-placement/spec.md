## ADDED Requirements

### Requirement: An outbound call is refused outside the caller's class

The system SHALL classify a trunk-bound destination from its dialled
form: a leading `+` is INTERNATIONAL, otherwise NATIONAL. It SHALL refuse
the call before reserving a channel or originating anything when the
calling extension's class does not cover the destination. Internal
destinations SHALL always be allowed, and an emergency destination SHALL
never be refused by this rule (AC-08.3).

#### Scenario: A national extension cannot dial international form
- **WHEN** an extension whose class is NATIONAL dials a destination beginning with `+`
- **THEN** the request is refused as permission denied, and no leg is reserved and no call is set up

#### Scenario: A national extension dials national form
- **WHEN** an extension whose class is NATIONAL dials the same number without `+`
- **THEN** the call is placed through the trunk as usual

#### Scenario: An internal extension reaches only extensions
- **WHEN** an extension whose class is INTERNAL dials a trunk-bound destination
- **THEN** the request is refused before the engine is touched

#### Scenario: Emergency is never blocked by class
- **WHEN** an extension of any class dials an emergency number
- **THEN** the call is placed with the class check bypassed
