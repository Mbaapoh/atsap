## Purpose

Establishes how a caller proves who they are: credential storage that survives
a database disclosure, time-limited tokens that cannot be forged or
downgraded, machine credentials for non-interactive callers, and visibility
into failed attempts.

## Requirements

### Requirement: Credentials are never stored recoverably
The system SHALL NOT store, log, export, or display a password, an API key,
a session refresh secret, or a password-reset or invitation secret in a form
from which the original value can be recovered. A password SHALL be stored
only as a deliberately slow, salted hash; an API key and every other secret
SHALL be stored only as a one-way digest.

#### Scenario: Stored password cannot be read back
- **WHEN** a person's stored credential material is inspected directly in
  the database
- **THEN** the original password is not present and cannot be derived from
  what is stored

#### Scenario: API key is shown exactly once
- **WHEN** an API key is issued
- **THEN** its raw value is returned to the caller at that moment only, and
  every later read of that key returns no recoverable raw value

#### Scenario: Reset and refresh secrets cannot be read back
- **WHEN** stored sessions and password-reset records are inspected
  directly in the database
- **THEN** no refresh secret or emailed link secret is present or derivable
  from what is stored

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
a bounded lifetime, carries the tenant and membership it acts for and that
membership's role assignments, identifies the session it was issued under
when it was issued to a signed-in person, and is accepted only by this
system.

#### Scenario: Token is accepted while fresh
- **WHEN** a person authenticates successfully and immediately presents the
  returned token
- **THEN** the token validates, yielding that membership's identity, tenant,
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
Validation SHALL check, on every use and not only from the token's own
contents, the live status of the membership the token acts for, of that
membership's tenant, of the person's account, and of the session the token
was issued under, if any. A token SHALL be refused while otherwise unexpired
if its membership is disabled, its tenant suspended, its account disabled,
or its session ended.

#### Scenario: Disabling a principal stops the next request
- **WHEN** a principal is disabled after its token was issued and that token
  is then presented
- **THEN** validation fails, without waiting for the token to expire

#### Scenario: Ending the session stops the next request
- **WHEN** the session a token was issued under is ended, by signing out or
  by a password change, and the token is then presented
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
password, key, secret or token in that entry. Every failed password sign-in
SHALL additionally be recorded in the person's authentication history, or
without a person when the email has no account.

#### Scenario: Failed login is logged without the credential
- **WHEN** authentication fails because of a wrong password
- **THEN** a warning-level entry records the attempt, a history entry records
  it against the person, and the attempted password appears in neither

### Requirement: Token signing keys can be rotated without ending sessions
The system SHALL sign every token with its current signing key and identify that key in the token. It SHALL accept tokens signed by any configured previous key until they expire, and SHALL refuse a token naming a key it does not hold before examining the signature.

#### Scenario: A token signed before rotation still works
- **WHEN** the signing key is rotated, and the old key is kept as a previous key, while a token signed with the old key is still unexpired
- **THEN** that token is accepted, and every newly issued token is signed with the new key

#### Scenario: A token naming an unknown key is refused
- **WHEN** a token is presented that names a signing key the system does not hold
- **THEN** validation fails without the signature being treated as valid under any key
