## Purpose

Guarantees that configuration a customer makes in the interface is the
configuration the media engine is running: applied atomically, live
immediately, reversible immediately, and never leaking the engine's own
vocabulary to anyone who calls the API.

## ADDED Requirements

### Requirement: Configuration takes effect without an apply step
The system SHALL make a newly created extension usable by a registering
device as soon as the creating operation reports success. No further caller
action, no scheduled job, no restart, and no reload of the media engine SHALL
be required.

#### Scenario: A device registers immediately after creation
- **WHEN** an extension is created and a device presents its generated
  credential
- **THEN** the registration succeeds, with no intervening apply, publish or
  reload action of any kind

#### Scenario: Deletion takes effect immediately
- **WHEN** an extension is deleted
- **THEN** a device presenting its former credential can no longer register,
  with no intervening apply or reload action

### Requirement: Configuration and its projection succeed or fail together
The system SHALL apply the stored configuration record and the engine-facing
state as a single atomic unit. Neither SHALL be observable without the other.

#### Scenario: Engine projection fails
- **WHEN** the engine-facing write fails during extension creation
- **THEN** the extension does not exist, is absent from any listing, and the
  caller receives a failure

#### Scenario: Configuration write fails
- **WHEN** the configuration record write fails during extension creation
- **THEN** no engine-facing state for that extension exists, and no device can
  register against it

### Requirement: Credentials are never stored in a recoverable form
The system SHALL store extension credentials only in a form that cannot be
read back as the original secret, including in the state made available to the
media engine.

#### Scenario: Stored state contains no plaintext secret
- **WHEN** an extension has been created
- **THEN** no stored record — configuration or engine-facing — contains the
  generated secret in recoverable form

#### Scenario: Authentication still works against the stored form
- **WHEN** a device authenticates using the correct generated secret
- **THEN** registration succeeds

#### Scenario: A wrong secret is refused
- **WHEN** a device authenticates using an incorrect secret
- **THEN** registration is refused

### Requirement: The media engine's identifiers never reach a caller
The system SHALL NOT expose any identifier, name or object belonging to the
media engine through any API response, listed field, or error message. A
caller SHALL see only the extension number, display name and state that the
platform defines.

#### Scenario: API responses carry no engine identifiers
- **WHEN** a caller creates, reads or lists extensions
- **THEN** no field of any response contains an engine object identifier, an
  engine endpoint name, or an engine routing context

#### Scenario: Errors carry no engine identifiers
- **WHEN** an extension operation fails for any reason
- **THEN** the error returned to the caller contains no engine identifier or
  engine-specific terminology

### Requirement: Engine-facing identifiers are unique across all tenants
The system SHALL ensure that the identifier used for an extension in
engine-facing state is unique across every tenant, so that identically
numbered extensions in different tenants never collide or resolve to one
another.

#### Scenario: Identical numbers in two tenants remain separate
- **WHEN** two tenants each have an extension numbered 1000 and a device
  registers to one of them
- **THEN** only that tenant's extension becomes registered, and the other
  tenant's extension is unaffected

### Requirement: The media engine's data access is confined to what it reads
The system SHALL grant the media engine read access only to the engine-facing
state it requires, and SHALL grant it no access to configuration records,
identity records, call records or any other tenant data.

#### Scenario: The engine cannot read tenant data
- **WHEN** the media engine's database identity attempts to read configuration,
  identity or call records
- **THEN** the attempt fails

#### Scenario: The engine can read what it needs
- **WHEN** the media engine's database identity reads the engine-facing state
- **THEN** the read succeeds

### Requirement: Divergence between configuration and engine state is detectable and repairable
The system SHALL provide an operator-invoked means to compare engine-facing
state against the configuration records and report any divergence, and to
repair it by making the engine-facing state match the configuration. The
configuration records SHALL be treated as the sole source of truth. Repair
SHALL NOT run automatically on a schedule.

#### Scenario: Divergence is reported
- **WHEN** engine-facing state is altered outside the platform and the
  comparison is invoked
- **THEN** the divergence is reported, identifying what differs

#### Scenario: Repair restores agreement
- **WHEN** the operator invokes repair after a divergence is reported
- **THEN** the engine-facing state is made to match the configuration records,
  and a subsequent comparison reports no divergence

#### Scenario: No silent automatic repair
- **WHEN** a divergence exists and no operator invokes repair
- **THEN** the divergence persists and remains reportable, rather than being
  corrected without record
