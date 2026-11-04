## ADDED Requirements

### Requirement: The registrar reports the browser's ICE servers

The system SHALL return the installation's configured STUN/TURN servers
with the SIP registrar, in the shape a browser's peer connection takes,
to a caller who may read the tenant's extensions. It SHALL return an
empty list when none is configured, and SHALL NOT invent a public STUN
server.

#### Scenario: Configured TURN reaches the browser phone
- **WHEN** the installation has TURN URLs and a credential
- **THEN** the registrar response carries them and the browser phone passes them to its peer connection

#### Scenario: Unconfigured TURN changes nothing
- **WHEN** no TURN URL is configured
- **THEN** the ICE server list is empty and browser calling still works with host candidates on a normal NAT
