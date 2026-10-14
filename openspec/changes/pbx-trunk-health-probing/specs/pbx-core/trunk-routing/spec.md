## ADDED Requirements

### Requirement: Trunk health is checked, never set

The system SHALL report each trunk as `Unknown`, `Healthy`, `Degraded` or `Unhealthy`, together with the reason for that state and the time it was last observed. A new trunk SHALL be `Unknown` until it is first observed. The system SHALL observe each trunk actively by sending its carrier a SIP OPTIONS request from the platform at a configured interval, and as soon as a trunk is created or its host or port changes. Any SIP response SHALL count as the carrier answering: `Healthy`, or `Degraded` when the response is slow; a server error response SHALL count as `Unhealthy`; a host that does not resolve, or no response within the timeout, SHALL count as `Unhealthy` with that reason. Outcomes of calls through the trunk SHALL also update its health. Health changes only through these observations, never through a caller-supplied value.

#### Scenario: A new trunk is not yet checked

- **WHEN** a trunk is created
- **THEN** its reported health is `Unknown`, with no observation time, until its first check completes

#### Scenario: A carrier that answers is healthy

- **WHEN** a trunk's carrier answers the platform's SIP OPTIONS request promptly, with any non-server-error status
- **THEN** the trunk is reported `Healthy`, with the reason "answering" and the time of the check

#### Scenario: A slow carrier is degraded

- **WHEN** a trunk's carrier answers the platform's SIP OPTIONS request, but slower than the slow-response threshold
- **THEN** the trunk is reported `Degraded`, with the reason "slow response"

#### Scenario: A host that does not resolve is unhealthy

- **WHEN** a trunk's host name does not resolve
- **THEN** the trunk is reported `Unhealthy`, with the reason "host not found", within one check of its creation or change

#### Scenario: A carrier that does not answer is unhealthy

- **WHEN** a trunk's carrier sends no response to the platform's SIP OPTIONS request within the timeout
- **THEN** the trunk is reported `Unhealthy`, with the reason "no response"

#### Scenario: Changing the host is checked at once

- **WHEN** a trunk's host or port is changed
- **THEN** the trunk is checked again without waiting for the next interval, and its health reflects the new address

#### Scenario: Health cannot be set by configuration

- **WHEN** a trunk is created or updated with any health value
- **THEN** the supplied value is ignored and the observed state is unchanged

#### Scenario: Reported failure degrades future selection only

- **WHEN** an observation marks a trunk `Unhealthy`
- **THEN** subsequent route selections skip it while another trunk can carry the call
- **AND** no call already in progress is affected

## MODIFIED Requirements

### Requirement: Route selection is deterministic and explainable

The system SHALL select outbound routes by longest prefix match, breaking ties by priority then by stored cost, skipping `Unhealthy` trunks while any other matching trunk can carry the call and deprioritizing `Degraded` ones. When every matching trunk is `Unhealthy`, they SHALL be tried in the same order as the last resort, so a wrong observation costs preference, never service. A trunk whose health is `Unknown` SHALL be selected exactly as a `Healthy` one, so a trunk is usable as soon as it is created. The same configured set SHALL always yield the same order. Each selection SHALL identify the matched rule so the call record can show which rule matched and why.

#### Scenario: Longest prefix wins

- **WHEN** two routes cover a dialled destination and one prefix is longer
- **THEN** the longer-prefix route is selected regardless of priority

#### Scenario: Overlapping prefixes resolve deterministically

- **WHEN** routes overlap on prefix, priority and cost
- **THEN** repeated selections over the same configured set return the same winner every time

#### Scenario: An unhealthy trunk is the last resort

- **WHEN** every trunk whose routes match a destination is `Unhealthy`
- **THEN** those trunks are tried, in the usual order, rather than the call being refused as unroutable

#### Scenario: A trunk not yet checked is selectable

- **WHEN** the only trunk for a destination has not been checked yet
- **THEN** it is selected as a healthy trunk would be

#### Scenario: The matched rule travels with the selection

- **WHEN** a route is selected for a destination
- **THEN** the selection names the matched route and trunk, suitable for recording on the call record

## REMOVED Requirements

### Requirement: Trunk health is observed, never set

**Reason**: It made a new trunk `Healthy` by default and changed health only on call outcomes, so a trunk that had never been checked — even one whose host did not exist — was reported healthy. Replaced by "Trunk health is checked, never set", which starts a trunk `Unknown` and checks it actively.

**Migration**: Existing trunks are set to `Unknown` and checked within seconds of the change being deployed. API clients see the added `TRUNK_HEALTH_UNKNOWN` value; the meaning of `HEALTHY`, `DEGRADED` and `UNHEALTHY` is unchanged.
