## MODIFIED Requirements

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
