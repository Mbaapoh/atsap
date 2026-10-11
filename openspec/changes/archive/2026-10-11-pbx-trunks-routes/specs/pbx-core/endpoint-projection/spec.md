## MODIFIED Requirements

### Requirement: Credentials are never stored in a recoverable form

The system SHALL store extension credentials only in a form that cannot be read back as the original secret, including in the state made available to the media engine.

Carrier trunk secrets are the deliberate exception (LLD-03 §7.4): outbound registration requires the engine to authenticate *as us* to the carrier, and there is no arrangement in which it presents a one-way hash. The trunk secret SHALL rest in the engine-facing auth state readable by the engine identity, and SHALL appear nowhere else — never in an API response after creation, never in a log or audit row, and never in the domain table, which holds a reference only. The engine identity's read access stays confined to the engine-facing tables it already holds.

#### Scenario: Stored state contains no plaintext secret

- **WHEN** an extension has been created
- **THEN** no stored record — configuration or engine-facing — contains the generated secret in recoverable form

#### Scenario: Authentication still works against the stored form

- **WHEN** a device authenticates using the correct generated secret
- **THEN** registration succeeds

#### Scenario: A wrong secret is refused

- **WHEN** a device authenticates using an incorrect secret
- **THEN** registration is refused

#### Scenario: A trunk secret is engine-readable and nowhere else

- **WHEN** a trunk has been created with a carrier secret
- **THEN** the engine-facing auth state holds a form the engine can present
- **AND** no API response, log record or audit row contains the secret
- **AND** the trunk's configuration row contains a reference, not the secret

#### Scenario: The engine still cannot read tenant data

- **WHEN** the media engine's database identity attempts to read configuration, identity or call records
- **THEN** the attempt fails, exactly as before this change
