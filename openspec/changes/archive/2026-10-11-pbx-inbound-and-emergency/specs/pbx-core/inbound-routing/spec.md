## Purpose

Decides what happens to every call the platform did not originate itself: a customer calling a number the tenant owns, or a user picking up a desk phone and dialling. Each such call reaches its destination or is refused with a standard signal, never silence.

## ADDED Requirements

### Requirement: A call to a tenant's number rings the extension it is assigned to

The system SHALL let an authorized caller assign the tenant's inbound numbers (DIDs) to the tenant's extensions through public endpoints, and SHALL connect a call a carrier presents to an assigned number to that extension. An inbound number SHALL belong to one tenant and point at an extension in the same tenant.

#### Scenario: A carrier call to an assigned number rings the extension

- **WHEN** a carrier presents a call on the tenant's trunk to a number assigned to an extension
- **THEN** the extension's phone rings, and when it answers the caller and the extension are connected with media both ways

#### Scenario: An unassigned number is refused as unallocated

- **WHEN** a carrier presents a call to a number the tenant has not assigned
- **THEN** the call is refused with a standard "not found" signal, so the carrier can treat the number as unallocated

#### Scenario: A number cannot point at another tenant's extension

- **WHEN** an inbound number is assigned to an extension belonging to another tenant
- **THEN** the assignment is refused with the same outcome as naming an extension that does not exist

#### Scenario: Inbound numbers are managed through the API

- **WHEN** an authorized caller creates, lists and deletes inbound numbers
- **THEN** each operation takes effect immediately and is audited, and another tenant's numbers are never visible

### Requirement: A call dialled from a desk phone follows the tenant's dial plan

The system SHALL resolve a number dialled on a registered phone with the same dial plan used for calls placed through the API: an emergency number first, then another extension in the tenant, then the tenant's outbound routes. The call SHALL reach its destination, or be refused with a standard signal.

#### Scenario: A desk phone dials an external number

- **WHEN** a registered phone dials a number matched by an outbound route
- **THEN** the call leaves through the selected trunk and, when answered, the parties are connected

#### Scenario: A desk phone dials another extension

- **WHEN** a registered phone dials another extension's number in the same tenant
- **THEN** that extension rings and the call stays internal

#### Scenario: A desk phone dials an unroutable number

- **WHEN** a registered phone dials a number that is neither an extension nor routable
- **THEN** the call is refused with a standard "not found" signal

#### Scenario: A full trunk is passed over for a desk-phone call

- **WHEN** a desk-phone call's preferred trunk is at its channel limit and another selected trunk has room
- **THEN** the call leaves through the trunk with room

### Requirement: A refused arriving call is signalled, never left silent

The system SHALL refuse an arriving call it cannot or may not connect with a standard SIP response the far end can act on: "not found" for an unallocated or unroutable number, "service unavailable" when capacity or trunk channels are exhausted, and "forbidden" when compliance refuses it. Where the engine allows it, "service unavailable" SHALL carry a retry-after interval.

#### Scenario: Capacity exhaustion is service unavailable

- **WHEN** a carrier presents a call while the installation's licensed capacity is exhausted
- **THEN** the call is refused with "service unavailable", so the carrier can route elsewhere, and the caller hears a normal unavailable treatment

#### Scenario: No arriving call is left silent

- **WHEN** any arriving call is refused for any reason
- **THEN** the far end receives a final SIP response within the setup budget, and no channel is left in the engine

### Requirement: A call a platform-owned endpoint presents is attributed to its tenant by configuration

The system SHALL determine which tenant an arriving call belongs to from the configuration of the endpoint it arrived on, never from anything the caller sends. A call arriving on no configured endpoint SHALL be refused.

#### Scenario: Caller-supplied headers cannot change the tenant

- **WHEN** an arriving call carries headers naming another tenant or extension
- **THEN** the call is attributed to the tenant of the endpoint it arrived on, and the headers are ignored

#### Scenario: Signalling from an unknown source is refused

- **WHEN** a call arrives from an address that matches no trunk and carries no valid extension credentials
- **THEN** the call is refused and never reaches the dial plan
