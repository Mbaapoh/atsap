# Call records

### Requirement: Final call records carry participant attribution

The system SHALL return only terminated calls from the reporting service,
with the call's direction, numbers, start/answer/end, duration, outcome
and each participant's role, extension, owner and usage seconds. It SHALL
never return an engine endpoint or channel identifier.

#### Scenario: A terminated call is reported with its people
- **WHEN** an authorized caller lists records for a terminated call
- **THEN** the call appears once with its attributed participants and duration

#### Scenario: An in-flight call is not a record
- **WHEN** a call is not terminated
- **THEN** it does not appear in the reporting list

### Requirement: Agents see only their own records

The system SHALL narrow a caller without an administrator or supervisor
role to records where their membership is a participant. A filter SHALL
NOT widen that scope. Administrators and supervisors SHALL see the
tenant's records.

#### Scenario: An agent cannot see a colleague's call
- **WHEN** an agent lists records for a call they were not on
- **THEN** it is absent, even if they name the other person in the filter

### Requirement: Export is audited

The system SHALL export the same records as a bounded CSV and SHALL write
one audit entry naming the exporting person, range and row count. It SHALL
not log the CSV body.

#### Scenario: An export is attributable
- **WHEN** a person exports call records
- **THEN** exactly one `report.export` audit entry records who exported which range and how many rows
