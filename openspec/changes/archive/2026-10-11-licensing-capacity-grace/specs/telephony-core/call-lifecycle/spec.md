## MODIFIED Requirements

### Requirement: Screening evaluates capacity and compliance before Routing
Every call SHALL receive a capacity verdict and a compliance verdict
before leaving the Screening state. A call whose capacity or compliance
verdict is not permitted SHALL move to Terminated without reaching
Routing.

A permitted capacity verdict reserves a channel against the
installation's entitlement. Every call that reserves a channel SHALL
release it when the call terminates, by every route to termination —
normal hangup, screening rejection, and failure during setup — so that
the reserved count returns to its pre-call value exactly once.

#### Scenario: Both verdicts permitted
- **WHEN** a call in Screening receives a permitted capacity verdict and a
  permitted compliance verdict
- **THEN** the call moves to Routing

#### Scenario: A verdict is not permitted
- **WHEN** a call in Screening receives a verdict that is not permitted,
  from either check
- **THEN** the call moves to Terminated, and the reason for termination
  identifies which verdict rejected it

#### Scenario: A completed call releases its reserved channel
- **GIVEN** a call that received a permitted capacity verdict and reached
  Active
- **WHEN** the call reaches Terminated
- **THEN** the reserved channel is released exactly once

#### Scenario: A screened-out call releases nothing it did not reserve
- **WHEN** a call in Screening is terminated by a verdict that is not
  permitted
- **THEN** no channel release is performed for that call

#### Scenario: A call failing after Screening releases its channel
- **GIVEN** a call that received a permitted capacity verdict and left
  Screening
- **WHEN** the call terminates without ever reaching Active
- **THEN** the reserved channel is released exactly once

#### Scenario: Repeated termination does not release twice
- **GIVEN** a call that has already terminated and released its channel
- **WHEN** a further termination signal arrives for the same call
- **THEN** no further release is performed

#### Scenario: Release is identified by the call, not counted blindly
- **GIVEN** a call that has released its reserved channel
- **WHEN** a release is requested again for that same call, by any route
- **THEN** the reserved count is unchanged
- **AND** the outcome is the same as if the release had never been
  requested, rather than an error the caller must handle
