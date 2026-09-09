## Purpose

Carriers are how every external call leaves and enters the platform. This capability connects partner-owned SIP trunks, chooses one per outbound destination, and keeps that choice live in engine state — the BYOT promise made real.

## ADDED Requirements

### Requirement: A trunk is a tenant-scoped record created through the public API

The system SHALL create, list, update and delete carrier trunks through public endpoints, scoped to the calling tenant. A trunk carries a name, host, port, per-trunk channel limit and priority. The carrier secret is supplied at creation, used for engine projection, and never returned afterwards.

#### Scenario: A trunk is created and usable

- **WHEN** a partner engineer creates a trunk with a name, host and secret
- **THEN** the trunk is stored under their tenant and appears in the trunk list
- **AND** the secret is never returned by any later read

#### Scenario: A trunk belonging to another tenant is indistinguishable from one that does not exist

- **WHEN** a caller reads, updates or deletes a trunk owned by another tenant
- **THEN** the outcome is identical to addressing a nonexistent trunk

#### Scenario: Trunk input is validated before it is stored

- **WHEN** a trunk is created with an unparseable host, an out-of-range port, or a non-positive channel limit
- **THEN** creation is refused with a reason identifying the offending field

### Requirement: Trunk health is observed, never set

The system SHALL report each trunk as `Healthy`, `Degraded` or `Unhealthy`, defaulting to `Healthy` at creation. Health changes only through observed outcome reports, never through a caller-supplied value.

#### Scenario: A new trunk starts healthy

- **WHEN** a trunk is created
- **THEN** its reported health is `Healthy`

#### Scenario: Health cannot be set by configuration

- **WHEN** a trunk is created or updated with any health value
- **THEN** the supplied value is ignored and the observed state is unchanged

#### Scenario: Reported failure degrades future selection only

- **WHEN** outcome reports mark a trunk `Unhealthy`
- **THEN** subsequent route selections skip it
- **AND** no call already in progress is affected

### Requirement: Route selection is deterministic and explainable

The system SHALL select outbound routes by longest prefix match, breaking ties by priority then by stored cost, skipping `Unhealthy` trunks and deprioritizing `Degraded` ones. The same configured set SHALL always yield the same order. Each selection SHALL identify the matched rule so the call record can show which rule matched and why.

#### Scenario: Longest prefix wins

- **WHEN** two routes cover a dialled destination and one prefix is longer
- **THEN** the longer-prefix route is selected regardless of priority

#### Scenario: Overlapping prefixes resolve deterministically

- **WHEN** routes overlap on prefix, priority and cost
- **THEN** repeated selections over the same configured set return the same winner every time

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
