## Purpose

Carriers are how every external call leaves and enters the platform. This capability connects partner-owned SIP trunks, chooses one per outbound destination, and keeps that choice live in engine state — the BYOT promise made real.

## Requirements

### Requirement: A trunk is a tenant-scoped record created through the public API

The system SHALL create, list, update and delete carrier trunks through public endpoints, scoped to the calling tenant. A trunk carries a name, host, port, per-trunk channel limit and priority, and optionally carrier digest credentials: an authentication username and a secret, as SIP trunk configuration conventionally takes them. The username is configuration and is returned on reads. The secret is supplied at creation or as a replacement on update, used for engine projection, and never returned. A trunk with no credentials is an IP-authenticated carrier; a secret without a username is refused.

#### Scenario: A trunk is created and usable

- **WHEN** a partner engineer creates a trunk with a name, host and secret
- **THEN** the trunk is stored under their tenant and appears in the trunk list
- **AND** the secret is never returned by any later read

#### Scenario: An IP-authenticated trunk needs no credentials

- **WHEN** a trunk is created with no username and no secret
- **THEN** the trunk is stored and projected with no engine-facing auth state

#### Scenario: A secret is replaced, never read back

- **WHEN** a trunk is updated with a replacement secret
- **THEN** the engine-facing auth state carries the new secret
- **AND** an update that supplies no secret leaves the stored secret unchanged

#### Scenario: A trunk belonging to another tenant is indistinguishable from one that does not exist

- **WHEN** a caller reads, updates or deletes a trunk owned by another tenant
- **THEN** the outcome is identical to addressing a nonexistent trunk

#### Scenario: Trunk input is validated before it is stored

- **WHEN** a trunk is created with an unparseable host, an out-of-range port, a non-positive channel limit, or a secret without a username
- **THEN** creation is refused with a reason identifying the offending field

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

### Requirement: Routes are tenant-scoped records created through the public API

The system SHALL create, list and delete outbound routes through public endpoints, scoped to the calling tenant. A route carries a prefix pattern, a trunk in the same tenant, a stored cost and a priority. Overlapping prefixes are permitted: overlap is how least-cost preference is expressed.

#### Scenario: A route is created against a tenant-local trunk

- **WHEN** a partner engineer creates a route pointing at their own trunk
- **THEN** the route is stored and participates in selection

#### Scenario: A route cannot point at another tenant's trunk

- **WHEN** a route references a trunk owned by another tenant
- **THEN** creation is refused with the same outcome as referencing a nonexistent trunk

### Requirement: A deleted trunk stops serving new selections immediately

The system SHALL remove a trunk's engine-facing state in the same transaction as its configuration row, so a deleted trunk can never be selected again. Calls already in progress are untouched: selection-time state only.

#### Scenario: Deletion deprovisions atomically

- **WHEN** a trunk is deleted
- **THEN** no subsequent selection returns it
- **AND** a projector failure leaves neither the configuration row removed nor the engine row orphaned

### Requirement: Every trunk and route mutation is auditable

The system SHALL write one audit row with before and after state in the same transaction as every trunk or route mutation. Secrets and digests are redacted at construction and never reach the audit row.

#### Scenario: Mutations leave an audit trail without secrets

- **WHEN** a trunk is created, updated or deleted
- **THEN** an audit row records the change with no secret material in any field

### Requirement: A trunk's channel limit caps its concurrent placed calls

The system SHALL count the call legs it has placed through each trunk while their calls are live, and SHALL NOT place a new leg through a trunk already at its channel limit. A trunk at its limit SHALL be passed over for the next selected candidate; when no candidate has room the placement is refused. A leg stops counting as soon as its call has ended, however it ended. Concurrent placements SHALL NOT together exceed the limit.

#### Scenario: A full trunk is passed over

- **WHEN** the preferred trunk for a destination is at its channel limit and another selected trunk has room
- **THEN** the call leaves through the trunk with room

#### Scenario: No room anywhere refuses the call

- **WHEN** every trunk selected for a destination is at its channel limit
- **THEN** the placement is refused with a reason identifying trunk capacity, and no call is set up

#### Scenario: An ended call frees its channel

- **WHEN** a call placed through a trunk ends, whether hung up through the API or by either party
- **THEN** that trunk's count no longer includes it

#### Scenario: Racing placements cannot exceed the limit

- **WHEN** two placements race for a trunk's last free channel
- **THEN** at most one of them uses it

### Requirement: Inbound signalling is accepted only from a trunk's carrier address

The system SHALL recognise a call presented by a carrier as arriving on a trunk only when it comes from that trunk's configured host. The recognition SHALL follow the trunk's configuration immediately on create, update and delete, like the rest of its engine state.

#### Scenario: A carrier's call is recognised as its trunk

- **WHEN** a call arrives from a trunk's configured host
- **THEN** it is attributed to that trunk and its tenant

#### Scenario: A deleted trunk's carrier is no longer recognised

- **WHEN** a trunk is deleted and its former carrier presents a call
- **THEN** the call is refused as from an unknown source

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
