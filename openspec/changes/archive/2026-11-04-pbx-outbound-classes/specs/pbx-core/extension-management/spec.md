## ADDED Requirements

### Requirement: An extension holds an outbound destination class

The system SHALL store one outbound class on every extension — INTERNAL,
NATIONAL or INTERNATIONAL — chosen by an authorized caller and returned
on every read. When the class is omitted on creation the system SHALL
store NATIONAL. The system SHALL refuse an unknown class. The class is
configuration, never observed state, and changing it SHALL be auditable
like every other extension mutation.

#### Scenario: A new extension defaults to national
- **WHEN** an administrator creates an extension without naming a class
- **THEN** the extension is stored and returned as NATIONAL

#### Scenario: An administrator chooses a class
- **WHEN** an administrator creates or edits an extension with INTERNAL, NATIONAL or INTERNATIONAL
- **THEN** every later read returns that class

#### Scenario: An unknown class is refused
- **WHEN** a caller supplies a class outside the three
- **THEN** the request is refused as invalid input and nothing is stored
