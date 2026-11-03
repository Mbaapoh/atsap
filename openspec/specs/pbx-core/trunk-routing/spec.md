## Purpose

Carriers are how every external call leaves and enters the platform. This capability connects partner-owned SIP trunks, chooses one per outbound destination, and keeps that choice live in engine state — the BYOT promise made real.

## Requirements

### Requirement: A trunk is a tenant-scoped record created through the public API

The system SHALL create, list, update and delete carrier trunks through public endpoints, scoped to the calling tenant. A trunk carries a name, host, port, per-trunk channel limit and priority, and optionally carrier digest credentials: an authentication username and a secret, as SIP trunk configuration conventionally takes them. The username is configuration and is returned on reads. The secret is supplied at creation or as a replacement on update, used for engine projection, and never returned. A trunk with no credentials is an IP-authenticated carrier; a secret without a username is refused. A trunk MAY carry up to 20 inbound signalling addresses, each an IPv4 or IPv6 address or a CIDR range no broader than /16 (IPv4) or /32 (IPv6); they are configuration, returned on reads and replaced on update like every other field. The system SHALL refuse a host that cannot be a carrier: `localhost` or a name under `.localhost`, or a loopback, link-local, unspecified or multicast IP literal; and SHALL refuse such an address or range among the inbound signalling addresses. Private-network addresses SHALL be accepted.

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

- **WHEN** a trunk is created with an unparseable host, an out-of-range port, a non-positive channel limit, a secret without a username, or an inbound signalling address that is not an address or range, is too broad, or is more than the twentieth
- **THEN** creation is refused with a reason identifying the offending field

#### Scenario: A host that cannot be a carrier is refused

- **WHEN** a trunk is created or updated with the host `localhost`, `pbx.localhost`, `127.0.0.1`, `::1`, `169.254.1.1`, `0.0.0.0` or `224.0.0.1`
- **THEN** the request is refused with a reason identifying the host
- **AND** the same addresses are refused among the inbound signalling addresses

#### Scenario: A carrier on a private network is accepted

- **WHEN** a trunk is created with the host `10.20.0.5` or an inbound signalling address `192.168.10.0/24`
- **THEN** the trunk is stored and projected

#### Scenario: Inbound signalling addresses are configuration

- **WHEN** a trunk is created with inbound signalling addresses
- **THEN** every later read returns them
- **AND** an update replaces them with exactly the addresses it supplies, none if it supplies none

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

The system SHALL recognise a call presented by a carrier as arriving on a trunk only when it comes from that trunk's carrier address: an address of its host, or one of its inbound signalling addresses or ranges. Inbound signalling addresses SHALL be recognised immediately on create and update, and SHALL stop being recognised immediately when removed. Addresses a host name resolves to SHALL be recognised only when they are routable: never a loopback, link-local, unspecified or multicast address. A trunk whose host is an IP address SHALL be recognised by that address immediately on create and update. A trunk whose host is a name SHALL be recognised by the addresses the platform last resolved that name to; the platform SHALL resolve it as soon as the trunk is created or its host changes, and again at every trunk check. Recognition SHALL NOT wait on name resolution: no trunk's host, resolvable or not, SHALL delay the recognition or setup of any call for another trunk or tenant. When a name that resolved before fails to resolve, the trunk SHALL keep its last resolved addresses; when it resolves to different addresses, those SHALL replace them; when the host changes, the previous host's addresses SHALL stop being recognised at once. A deleted trunk SHALL stop being recognised immediately.

#### Scenario: A carrier's call is recognised as its trunk

- **WHEN** a call arrives from a trunk's configured IP address
- **THEN** it is attributed to that trunk and its tenant

#### Scenario: A carrier configured by name is recognised once resolved

- **WHEN** a trunk is created with a host name that resolves
- **AND** its first check has completed
- **THEN** a call arriving from an address that name resolved to is attributed to that trunk and its tenant

#### Scenario: An unresolvable host stalls no one

- **WHEN** one tenant has a trunk whose host name does not resolve
- **THEN** that trunk is accepted and reported `Unhealthy` with the reason "host not found"
- **AND** it recognises no inbound call
- **AND** calls for every other trunk and tenant are recognised and set up without waiting on that name

#### Scenario: A brief resolution failure keeps a carrier recognised

- **WHEN** a trunk's host name resolved before and now fails to resolve
- **THEN** calls from its last resolved addresses are still attributed to the trunk
- **AND** its health reports the failure

#### Scenario: A changed host stops recognising the old carrier

- **WHEN** a trunk's host is changed
- **THEN** calls from the previous host's addresses are no longer attributed to the trunk
- **AND** the new host is recognised as soon as it is an IP address or has resolved

#### Scenario: A deleted trunk's carrier is no longer recognised

- **WHEN** a trunk is deleted and its former carrier presents a call
- **THEN** the call is refused as from an unknown source

#### Scenario: A carrier is recognised from its published signalling ranges

- **WHEN** a trunk's host is `sip.carrier.example` and its inbound signalling addresses include `203.0.113.0/28`
- **AND** a call arrives from `203.0.113.9`
- **THEN** it is attributed to that trunk and its tenant, without waiting for any name to resolve

#### Scenario: A removed inbound address stops being recognised

- **WHEN** a trunk is updated without an inbound signalling address it had
- **THEN** calls from that address are no longer attributed to the trunk, unless it is also an address of the trunk's host

#### Scenario: Two trunks on one carrier address both receive calls

- **WHEN** trunks in two tenants recognise the same carrier address
- **THEN** a call from that address is attributed to the trunk whose tenant owns the dialled number
- **AND** neither tenant's configuration is changed or refused because of the other

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
