## Purpose

Establishes the extension as the unit a customer actually configures: a
tenant-scoped record with a number they choose, credentials the platform
generates, and a registration state the platform observes — all reachable
through the public API, with no configuration file anywhere in the path.

## Requirements

### Requirement: An extension is a tenant-scoped record created through the public API
The system SHALL allow an authorized caller to create, list, update and delete
extensions for their own tenant through the public API. Every operation SHALL
be authorized before it takes effect, and SHALL act only on the caller's own
tenant. No operation SHALL require editing a file, running a command on the
server, or any knowledge of the underlying media engine.

#### Scenario: An administrator creates an extension
- **WHEN** an authorized administrator creates an extension with a number and
  a display name
- **THEN** the extension exists, is returned to the caller, and appears in a
  subsequent listing for that tenant

#### Scenario: Deleting an extension removes it
- **WHEN** an authorized administrator deletes an existing extension
- **THEN** it no longer appears in a listing, and a subsequent read of it
  reports that it does not exist

#### Scenario: An unauthorized caller is refused
- **WHEN** a caller without permission to manage extensions attempts any
  extension operation
- **THEN** the operation is refused and no extension is created, changed or
  removed

### Requirement: Extension numbers are unique per tenant, not globally
The system SHALL reject a second extension with the same number within one
tenant, and SHALL accept the same number in a different tenant as an entirely
independent extension with independent credentials.

#### Scenario: Same number in two tenants
- **WHEN** two different tenants each create an extension numbered 1000
- **THEN** both are created successfully, each with its own credentials, and
  neither tenant can see, reach or infer the other's

#### Scenario: Duplicate number within one tenant
- **WHEN** a tenant already has an extension with a given number and another
  is created with the same number
- **THEN** the second creation is rejected and no second extension exists

### Requirement: An extension belonging to another tenant is indistinguishable from one that does not exist
The system SHALL respond to any operation naming an extension outside the
caller's tenant exactly as it responds to one naming an extension that does
not exist. No response, error message or timing SHALL reveal that the
extension exists elsewhere.

#### Scenario: Reading another tenant's extension
- **WHEN** a caller requests an extension that exists but belongs to a
  different tenant
- **THEN** the response is the same "not found" outcome returned for an
  identifier that exists nowhere

### Requirement: The platform generates extension credentials and reveals them once
The system SHALL generate the authentication credential for every extension
from a cryptographically secure source. A caller SHALL NOT be able to choose
or supply it. The generated secret SHALL be returned to the caller exactly
once, in the response to the operation that created or regenerated it, and
SHALL NOT be retrievable by any later operation. The system SHALL NOT store
the secret in a form from which the original can be read back.

#### Scenario: Secret is returned once at creation
- **WHEN** an extension is created
- **THEN** the response contains the generated secret, and no subsequent read
  of that extension returns it

#### Scenario: A caller-supplied secret is not honoured
- **WHEN** a caller attempts to set an extension's secret directly
- **THEN** the request is rejected, or the supplied value is ignored and a
  generated secret is used instead

#### Scenario: Regeneration replaces the credential
- **WHEN** an administrator regenerates an extension's credential
- **THEN** a new secret is returned once, the previous secret no longer
  authenticates, and the extension is otherwise unchanged

### Requirement: The authentication identity is not the extension number
The system SHALL NOT derive an extension's authentication username from its
extension number. Knowledge of a tenant's extension numbering SHALL NOT reveal
what an attacker would need to present in order to authenticate.

#### Scenario: Username differs from the number
- **WHEN** an extension numbered 1000 is created
- **THEN** its authentication username is not "1000" and is not derivable from
  the number alone

### Requirement: Registration state is observed, never set
The system SHALL report whether a device is currently registered to an
extension. This state SHALL be derived from the live state of the platform,
and SHALL NOT be settable through any API operation.

#### Scenario: A newly created extension is not registered
- **WHEN** an extension is created and no device has registered to it
- **THEN** its reported state is "not registered"

#### Scenario: State reflects a real registration
- **WHEN** a device successfully registers to an extension
- **THEN** the extension's reported state becomes "registered" without any
  further API call

#### Scenario: Registration state cannot be written
- **WHEN** a caller attempts to set an extension's registration state
- **THEN** the request is rejected and the reported state continues to reflect
  reality

### Requirement: The public API reports where devices register

The system SHALL let an authorized caller who may read their tenant's extensions obtain the SIP registrar address that devices register to: its host, port and transport. When the installation has not configured a public registrar host, the system SHALL report that no address is configured instead of returning a guessed or internal address. The address SHALL be the installation's public one, never an internal container or engine address.

#### Scenario: An administrator reads the registrar address

- **WHEN** an authorized administrator asks for the registrar address on an installation that has configured one
- **THEN** the response carries the configured host, port and transport, and marks the address as configured

#### Scenario: No public host is configured

- **WHEN** an authorized administrator asks for the registrar address on an installation with no public host configured
- **THEN** the response marks the address as not configured and carries no host

#### Scenario: Reading the address requires the extension-read permission

- **WHEN** a caller without permission to read the tenant's extensions asks for the registrar address
- **THEN** the request is refused as not permitted, and no address is returned

#### Scenario: Another tenant cannot be named

- **WHEN** a caller asks for the registrar address naming a tenant other than their own
- **THEN** the request is refused as not permitted

### Requirement: Extension input is validated before it is stored
The system SHALL validate every caller-supplied extension field against an
explicit permitted form before storing it. An extension number SHALL consist
only of characters valid for a dialable number.

#### Scenario: A malformed extension number is rejected
- **WHEN** a caller supplies an extension number containing characters outside
  the permitted form
- **THEN** the request is rejected and no extension is created

### Requirement: Every extension mutation is auditable
The system SHALL record one audit entry for each successful extension
creation, update or deletion, identifying the actor, the affected extension,
and the before and after state. No audit entry SHALL contain the extension's
secret or any value from which it could be recovered.

#### Scenario: Creation is audited
- **WHEN** an extension is created
- **THEN** exactly one audit entry records the actor and the resulting state

#### Scenario: Audit entries carry no credential material
- **WHEN** any extension mutation is audited
- **THEN** the recorded before and after state contain no secret and no stored
  credential value

### Requirement: Extension provisioning respects the installation's extension cap

The system SHALL refuse to create an extension once the installation already holds as many extensions as its entitlement allows. The cap is supplied to the creation operation through a port this context owns and never fetched from the licensing context. A cap of zero SHALL mean unlimited and disable the check. Reads, updates, deletions and registration state are never gated by the cap.

#### Scenario: An eleventh extension is refused on a ten-extension installation

- **WHEN** the installation's extension cap is 10 and ten extensions exist
- **THEN** creating another extension is refused with `FAILED_PRECONDITION` and an entitlement reason identifying the licence, not the caller's roles

#### Scenario: Zero cap means unlimited

- **WHEN** the installation's extension cap is 0
- **THEN** extension creation is never refused for entitlement, regardless of how many extensions exist

#### Scenario: Existing extensions survive a lowered cap

- **WHEN** the entitlement in force allows fewer extensions than already exist
- **THEN** no extension is removed, hidden or deprovisioned from the engine, and only new creation is refused
