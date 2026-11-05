## ADDED Requirements

### Requirement: A flow can play a prompt and route on a digit

The system SHALL let a published flow revision define a prompt, up to ten
DTMF digit branches, a timeout and no-input/invalid destinations. On an
open, non-holiday call it SHALL answer the caller, play the prompt and
route to the branch for the first digit. It SHALL stop the prompt and
timeout when a valid digit arrives.

#### Scenario: A digit routes the caller
- **WHEN** a caller in an open flow presses a configured digit
- **THEN** the prompt stops and the call rings that branch's extension

#### Scenario: No input or invalid input uses its fallback
- **WHEN** the timeout expires without a digit, or the digit has no branch
- **THEN** the call routes to the configured no-input/invalid destination, or ends with a stable reason when none is set

#### Scenario: Closed/holiday never enters the menu
- **WHEN** the flow is closed or the day is a holiday
- **THEN** the call uses the closed destination and no prompt is played

### Requirement: Menu revisions are validated before publish

The system SHALL refuse a menu with a digit outside 0–9, a destination
that does not exist, or a prompt-less menu. A published revision SHALL be
immutable and a live call SHALL keep the revision it started on.

#### Scenario: A bad destination is refused
- **WHEN** an administrator publishes a menu branch to a missing extension
- **THEN** the publish is refused and the previous revision remains active
