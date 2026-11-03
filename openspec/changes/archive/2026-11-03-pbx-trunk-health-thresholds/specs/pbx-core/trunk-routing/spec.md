## MODIFIED Requirements

### Requirement: Trunk health is checked, never set

The system SHALL report each trunk as `Unknown`, `Healthy`, `Degraded` or `Unhealthy`, with the reason and the time it was last observed. A new trunk SHALL be `Unknown` until its first observation. The system SHALL check every trunk with a SIP OPTIONS request from the platform every 10 seconds by default, and at once when a trunk is created or its host or port changes.

A check is **failed** when the carrier sends no final response, sends a server-error response, or its host does not resolve. A host name that resolves only to addresses that cannot be a carrier (loopback, link-local, unspecified or multicast) is **Unhealthy** at once with that reason, and no check is sent. A slow answer SHALL be `Degraded` at once. Any other final response SHALL be a healthy check, including a 403 or 404 to an unauthenticated OPTIONS.

The platform SHALL mark a trunk `Degraded` when it observes **3 consecutive failed checks**, or when its call-setup failure rate is **greater than 20 % over a rolling 60-second window with at least 5 call attempts**, whichever happens first. A call-setup failure is an attempt through the trunk that the platform could not originate; a successful setup counts in the window's denominator. A `Degraded` trunk is still selected, after every healthy candidate.

A trunk SHALL return to `Healthy` only after **5 continuous minutes of healthy checks**. A failed check or a call-setup failure SHALL reset that window. Health changes only through these observations, never through a caller-supplied value.

#### Scenario: A new trunk is not yet checked

- **WHEN** a trunk is created
- **THEN** its reported health is `Unknown`, with no observation time, until its first check completes

#### Scenario: A carrier that answers is healthy

- **WHEN** a trunk's carrier answers the platform's SIP OPTIONS request promptly with any non-server-error status
- **THEN** the trunk is reported `Healthy`, with the reason "answering" and the time of the check

#### Scenario: A slow carrier is degraded

- **WHEN** a carrier answers the check slower than the slow-response threshold
- **THEN** the trunk is reported `Degraded` at once, with the reason "slow response"

#### Scenario: Three failed checks degrade a carrier

- **WHEN** three consecutive checks of a trunk fail
- **THEN** the trunk is reported `Degraded`, with the reason of the failed check

#### Scenario: Two failed checks do not degrade a carrier

- **WHEN** two consecutive checks fail and the next check succeeds
- **THEN** the trunk remains `Healthy`

#### Scenario: A failed call window degrades a carrier

- **WHEN** at least five call setups have been attempted through a trunk in the last 60 seconds and more than 20 % of them failed
- **THEN** the trunk is reported `Degraded`, with the reason "call setup failures"

#### Scenario: A small or healthy call window changes nothing

- **WHEN** fewer than five call setups were attempted in the last 60 seconds, or the failure rate is 20 % or less
- **THEN** the trunk's health is unchanged by those calls

#### Scenario: A host that does not resolve is unhealthy

- **WHEN** a trunk's host name does not resolve
- **THEN** the trunk is reported `Unhealthy`, with the reason "host not found", on that check

#### Scenario: A name that resolves only to non-routable addresses is not a carrier

- **WHEN** a trunk's host name resolves only to loopback, link-local, unspecified or multicast addresses
- **THEN** no check is sent to them
- **AND** the trunk is reported `Unhealthy`, with the reason "address not allowed"

#### Scenario: A carrier that does not answer is unhealthy

- **WHEN** a carrier sends no response to one check
- **THEN** that check is failed, but the trunk is not yet `Unhealthy`; the three-consecutive-failure rule above decides, and a successful next check keeps it `Healthy`

#### Scenario: A degraded carrier recovers after five healthy minutes

- **WHEN** a `Degraded` trunk receives healthy checks continuously for five minutes
- **THEN** it is reported `Healthy` with the reason "answering"

#### Scenario: A failure during recovery restarts the window

- **WHEN** a trunk recovering from `Degraded` has a failed check or a call-setup failure before five healthy minutes have passed
- **THEN** it remains `Degraded`, and the five-minute window starts again at the next healthy check

#### Scenario: Changing the host is checked at once

- **WHEN** a trunk's host or port is changed
- **THEN** the trunk is checked again without waiting for the next interval, and its health reflects the new address

#### Scenario: Health cannot be set by configuration

- **WHEN** a trunk is created or updated with any health value
- **THEN** the supplied value is ignored and the observed state is unchanged

#### Scenario: Reported failure degrades future selection only

- **WHEN** an observation marks a trunk `Degraded` or `Unhealthy`
- **THEN** subsequent route selections prefer a healthy trunk and skip an unhealthy one
- **AND** no call already in progress is affected

## ADDED Requirements

### Requirement: A failing carrier is reported to the operator

On the transition of a trunk from `Healthy` or `Unknown` to `Degraded` or `Unhealthy`, the system SHALL send one email alert to the tenant's active tenant administrators and to any installation-level addresses configured for carrier alerts. The alert SHALL name the trunk, its host, the reason and the time, and SHALL NOT contain a credential, an engine identifier or a channel identifier. It SHALL be sent outside the call path and the check path, so a mail failure never changes health or a call. A trunk that remains failed SHALL NOT be alerted again, and a recovery SHALL NOT send an alert.

#### Scenario: A carrier failure alerts its tenants administrators

- **WHEN** a trunk transitions from `Healthy` to `Degraded` because of failed checks
- **THEN** its tenant's active tenant administrators receive one email naming the trunk, host, reason and time

#### Scenario: A configuration failure alerts at once

- **WHEN** a trunk is first checked and its host does not resolve
- **THEN** it becomes `Unhealthy` and one alert is sent

#### Scenario: No repeated alert while failed

- **WHEN** further checks keep a trunk `Degraded` or `Unhealthy`
- **THEN** no further alert is sent for that failure

#### Scenario: Recovery is silent

- **WHEN** a failed trunk returns to `Healthy` after the recovery window
- **THEN** no alert is sent

#### Scenario: A mail failure changes nothing

- **WHEN** the alert cannot be delivered
- **THEN** the failure is logged and the trunk's health, selection and any call are unaffected
