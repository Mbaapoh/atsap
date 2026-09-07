## Purpose

Defines the externally reachable contract for identity: which operations a
portal or partner may invoke over the network, what authentication each
requires, and what a failure is allowed to disclose.

## ADDED Requirements

### Requirement: Identity operations are reachable over the network
Every identity capability with an external consumer SHALL be invocable over
the network by a client that holds valid credentials: obtaining a token,
provisioning tenants and principals, changing their status, granting roles,
issuing and revoking API keys, and reading audit history.

#### Scenario: A client provisions and signs in without in-process access
- **WHEN** a client with valid administrator credentials, running outside the
  server process, provisions a principal and then authenticates as it
- **THEN** both operations succeed over the network, and the resulting token
  is accepted on subsequent calls

### Requirement: Only obtaining a token may be done unauthenticated
The system SHALL require valid authentication on every identity operation
except the one that exchanges credentials for a token. That one SHALL be
reachable without prior authentication, because a caller cannot hold a token
before obtaining one.

#### Scenario: Mutating operations refuse an unauthenticated caller
- **WHEN** a caller with no credentials attempts to provision a tenant,
  provision a principal, change a status, grant a role, issue a key, or read
  audit history
- **THEN** each attempt is refused as unauthenticated, and no record is
  created, changed, or returned

#### Scenario: Obtaining a token needs no token
- **WHEN** a caller presents a valid username and password with no other
  credentials
- **THEN** a token is returned

### Requirement: A first administrator can be created without an existing one
The system SHALL provide a means, available to an operator of the
installation and not over the public network, to create an initial tenant and
administrator. Without it, provisioning could never begin: every provisioning
operation requires a token, and every token requires a principal that
provisioning would have to create.

#### Scenario: Bootstrapping an empty installation
- **WHEN** an operator bootstraps an installation that has no tenants
- **THEN** a tenant and an administrator principal exist, and that
  administrator can authenticate over the network and provision others

#### Scenario: Bootstrapping is refused once an administrator exists
- **WHEN** an operator attempts to bootstrap an installation that already has
  a tenant
- **THEN** the attempt is refused, so the path cannot be used to grant
  additional administrators

### Requirement: Failures disclose nothing about other tenants or accounts
An authentication failure SHALL be reported identically whatever its cause. A
request naming a resource in another tenant SHALL be answered as though the
resource did not exist.

#### Scenario: Every credential failure looks the same
- **WHEN** a caller authenticates with an unknown username, and separately
  with a correct username and wrong password, and separately as a disabled
  account
- **THEN** all three receive the same refusal, distinguishable only in the
  server's own logs

#### Scenario: Reading another tenant's resource
- **WHEN** an authenticated caller requests a principal belonging to a
  different tenant
- **THEN** the response is the same not-found outcome returned for an
  identifier that exists nowhere

### Requirement: A raw API key is returned once and never again
When an API key is issued, the system SHALL return its usable value exactly
once, in that response. No later operation SHALL return it.

#### Scenario: Issuing and then listing a key
- **WHEN** a client issues an API key and afterwards reads that key's record
- **THEN** the issue response contains the usable key, and the later read
  contains no value from which the key can be recovered

### Requirement: Responses never carry credential material
No response SHALL contain a password, a password hash, an API key digest, or
signing key material.

#### Scenario: Reading a principal after it is created
- **WHEN** a client provisions a principal and reads it back
- **THEN** the response describes the principal without any representation of
  its password

### Requirement: The contract is published in a machine-readable form
The system SHALL publish a machine-readable description of every reachable
operation, generated from the same definitions the server is built from, so
the description cannot disagree with the implementation.

#### Scenario: A partner discovers the API without reading source
- **WHEN** a partner developer consults the published description
- **THEN** every reachable operation appears in it with its request and
  response shapes, and no operation is missing from it
