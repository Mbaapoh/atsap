## Purpose

Establishes how a caller proves who they are: credential storage that survives
a database disclosure, time-limited tokens that cannot be forged or
downgraded, machine credentials for non-interactive callers, and visibility
into failed attempts.

## Requirements

### Requirement: Credentials are never stored recoverably
The system SHALL NOT store, log, export, or display a password or an API key
in a form from which the original value can be recovered. A password SHALL be
stored only as a deliberately slow, salted hash; an API key SHALL be stored
only as a one-way digest.

#### Scenario: Stored password cannot be read back
- **WHEN** a principal's stored credential material is inspected directly in
  the database
- **THEN** the original password is not present and cannot be derived from
  what is stored

#### Scenario: API key is shown exactly once
- **WHEN** an API key is issued
- **THEN** its raw value is returned to the caller at that moment only, and
  every later read of that key returns no recoverable raw value

### Requirement: Password length is bounded, composition is not dictated
The system SHALL reject a password shorter than 8 characters, SHALL accept
passwords up to at least 64 characters, and SHALL NOT impose character
composition rules.

#### Scenario: Too-short password is refused
- **WHEN** a principal is provisioned with a 7-character password
- **THEN** provisioning is rejected and no principal is created

#### Scenario: Long passphrase is accepted unchanged
- **WHEN** a principal sets a 64-character passphrase containing only
  lowercase letters and spaces
- **THEN** it is accepted, and authenticating with that exact passphrase
  succeeds

### Requirement: Successful authentication yields a time-limited token
Authenticating with valid credentials SHALL yield a token that expires within
a bounded lifetime, carries the principal's tenant and role assignments, and
is accepted only by this system.

#### Scenario: Token is accepted while fresh
- **WHEN** a principal authenticates successfully and immediately presents the
  returned token
- **THEN** the token validates, yielding that principal's identity, tenant,
  and roles

#### Scenario: Token stops being accepted after expiry
- **WHEN** a token is presented after its expiry time has passed
- **THEN** validation fails, and the caller is treated as unauthenticated

### Requirement: A token asserting a different signing algorithm is rejected
Token validation SHALL determine the acceptable signing algorithm from the
system's own configuration, never from the presented token. A token declaring
any other algorithm — including declaring that it is unsigned — SHALL be
rejected before its signature is examined.

#### Scenario: Unsigned token is rejected
- **WHEN** a token is presented that declares itself unsigned
- **THEN** validation fails and no identity is derived from it

#### Scenario: Downgraded algorithm is rejected
- **WHEN** a token is presented that is well-formed but declares a signing
  algorithm the system does not issue
- **THEN** validation fails without the token's signature being treated as
  valid under any algorithm

### Requirement: Token validation reflects current account status
Validation SHALL check the live status of the principal and its tenant on
every use, not only the token's own contents. A token belonging to a disabled
principal or a suspended tenant SHALL be refused even while it is otherwise
unexpired.

#### Scenario: Disabling a principal stops the next request
- **WHEN** a principal is disabled after its token was issued and that token
  is then presented
- **THEN** validation fails, without waiting for the token to expire

### Requirement: API keys are revocable and expirable
An API key SHALL be usable for authentication until it is revoked or reaches
its expiry, and SHALL be refused immediately thereafter.

#### Scenario: Revoked key stops working
- **WHEN** an API key is revoked and then presented
- **THEN** authentication fails and no identity is derived from it

### Requirement: Failed authentication attempts are visible
Every failed authentication or token validation SHALL produce a warning-level
log entry identifying what was attempted, and SHALL NOT include the presented
password, key, or token in that entry.

#### Scenario: Failed login is logged without the credential
- **WHEN** authentication fails because of a wrong password
- **THEN** a warning-level entry records the attempt, and the attempted
  password appears nowhere in it
