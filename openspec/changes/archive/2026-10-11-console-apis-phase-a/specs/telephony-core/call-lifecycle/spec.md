## ADDED Requirements

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
