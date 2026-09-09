## Purpose

Establishes that a licence is only ever believed when it is
cryptographically proven to be ours and unaltered, so that entitlement
cannot be granted to an installation by editing a payload.

## ADDED Requirements

### Requirement: A licence payload is verified before it is interpreted

The system SHALL verify a licence payload's signature before reading any
field from it. A payload whose signature does not verify SHALL be
rejected, and none of its contents SHALL be parsed, stored, logged, or
acted upon.

#### Scenario: A valid payload is accepted

- **WHEN** a licence payload with a valid signature is presented
- **THEN** it is accepted
- **AND** its edition, capacity, expiry and instance identity become
  available

#### Scenario: An altered payload is rejected before parsing

- **GIVEN** a validly signed payload whose contents have since been
  altered
- **WHEN** it is presented
- **THEN** it is rejected as tampered
- **AND** no field from it is parsed or stored

#### Scenario: A payload with a corrupt signature is rejected

- **WHEN** a payload is presented whose signature is malformed or
  truncated
- **THEN** it is rejected
- **AND** the rejection does not distinguish a malformed signature from a
  wrong one in any externally observable way

#### Scenario: An empty or truncated payload is rejected

- **WHEN** an empty or truncated payload is presented
- **THEN** it is rejected
- **AND** no parsing is attempted

### Requirement: The verifying keys are fixed in the artefact, and verification is never skipped

The keys used to verify licence payloads SHALL be fixed in the deployed
artefact. They SHALL NOT be read from configuration, a database, an
environment variable, or any network location, so that there is no supply
point at which an attacker can substitute a key of their own.

**There SHALL be no configuration value, environment variable, build
option, or code path that skips verification or accepts an entitlement
without it.** A released artefact SHALL trust exactly one key. An
artefact built for development MAY additionally trust a development key,
and a payload signed by that key SHALL be rejected by a released
artefact exactly as a forgery is.

#### Scenario: Verification uses only keys fixed in the artefact

- **WHEN** a licence payload is verified
- **THEN** the verifying keys are those fixed in the artefact
- **AND** no configuration, database, or network lookup is performed to
  obtain them

#### Scenario: A payload signed by an untrusted key is rejected

- **WHEN** a payload correctly signed by a key the artefact does not
  trust is presented
- **THEN** it is rejected as tampered

#### Scenario: A released artefact trusts exactly one key

- **WHEN** an artefact built with no development options is inspected
- **THEN** it trusts exactly one verifying key

#### Scenario: A development-signed entitlement is inert in a release

- **GIVEN** an entitlement signed by the development key
- **WHEN** it is presented to a released artefact
- **THEN** it is rejected, and no capacity is granted

#### Scenario: No configuration can disable verification

- **WHEN** any combination of configuration values is supplied
- **THEN** verification still runs, and no entitlement is accepted
  without it

### Requirement: An entitlement is supplied as a signed token, by any route

An entitlement SHALL be applicable by supplying its signed token, and the
system SHALL treat every route by which a token arrives identically —
the same verification, the same result, the same refusal.

Applying a valid token SHALL take effect **without a restart**.

#### Scenario: A token supplied by configuration activates the installation

- **GIVEN** an installation with no entitlement on record
- **WHEN** a valid signed token is supplied through configuration
- **THEN** the entitlement takes effect and calls are permitted within it

#### Scenario: Applying a token requires no restart

- **GIVEN** a running installation
- **WHEN** a valid token granting greater capacity is applied
- **THEN** the new capacity is in force for the next call setup
- **AND** no call in progress is interrupted

#### Scenario: An invalid token leaves the current entitlement untouched

- **GIVEN** a running installation with a valid entitlement
- **WHEN** a tampered or untrusted token is supplied
- **THEN** it is rejected
- **AND** the entitlement already in force is unchanged

### Requirement: The stored entitlement is the signed token, re-verified when loaded

The system SHALL store the signed payload and its signature as the
entitlement of record, and SHALL re-verify them when loading the
entitlement. Any human-readable copy of the claims SHALL be treated as a
display convenience and SHALL NOT be read to decide what the
installation may do.

Editing stored claim values directly SHALL change no entitlement
decision. A stored entitlement that fails verification SHALL be treated
as tampering: it degrades to the defined floor, is reported, and is never
honoured.

Re-verification SHALL NOT occur during call setup.

#### Scenario: Editing a stored claim grants nothing

- **GIVEN** an installation with a valid entitlement
- **WHEN** the stored capacity, tenant cap or edition values are altered
  directly in storage
- **THEN** the entitlement in force is unchanged
- **AND** call setups are permitted or refused exactly as before

#### Scenario: A stored entitlement that fails verification is reported, not honoured

- **GIVEN** stored entitlement data whose signature no longer verifies
- **WHEN** the entitlement is loaded
- **THEN** the installation degrades to the defined floor
- **AND** the state is reported as tampering
- **AND** nothing the altered data claimed is granted

#### Scenario: Tampering degrades but never disables

- **GIVEN** a stored entitlement that fails verification
- **WHEN** a call is placed within the defined floor
- **THEN** it is permitted

#### Scenario: Verification stays out of the call path

- **WHEN** call setups are screened repeatedly
- **THEN** no signature verification is performed during any of them

### Requirement: Verification is deterministic and side-effect free

Verification SHALL depend only on the payload, its signature, and the
embedded key. It SHALL NOT perform input or output, and SHALL yield the
same result for the same inputs on every call.

#### Scenario: The same payload always verifies the same way

- **WHEN** the same payload and signature are verified repeatedly
- **THEN** every result is identical

#### Scenario: Verification works with no network

- **GIVEN** no network is reachable
- **WHEN** a valid licence payload is verified
- **THEN** it is accepted

### Requirement: An expired licence degrades rather than disables

A licence whose expiry has passed SHALL place the installation in a
degraded state at the defined minimum capacity. It SHALL NOT disable the
installation, and it SHALL NOT prevent an emergency call.

#### Scenario: Expiry degrades

- **GIVEN** a validly signed licence whose expiry has passed
- **THEN** the licence state is degraded
- **AND** the entitlement in force is the defined minimum

#### Scenario: Expiry never disables

- **GIVEN** an expired licence
- **WHEN** a call is placed within the defined minimum
- **THEN** it is permitted

#### Scenario: Expiry never blocks an emergency call

- **GIVEN** an expired licence
- **WHEN** an emergency call is placed
- **THEN** it proceeds

### Requirement: Licence material never appears in logs, errors, or telemetry

Licence payloads, signatures, and key material SHALL NOT be written to
logs, included in error messages returned to a caller, or emitted in
telemetry.

#### Scenario: A rejection reveals nothing

- **WHEN** a tampered payload is rejected
- **THEN** the error states that the licence was rejected
- **AND** it contains no part of the payload, the signature, or the key

#### Scenario: Acceptance reveals nothing

- **WHEN** a valid payload is accepted
- **THEN** no part of the payload or signature is written to any log
