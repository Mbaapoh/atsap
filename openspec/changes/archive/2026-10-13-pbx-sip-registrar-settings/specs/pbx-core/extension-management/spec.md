## ADDED Requirements

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
