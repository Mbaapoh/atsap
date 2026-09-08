## MODIFIED Requirements

### Requirement: Only obtaining a token may be done unauthenticated
The system SHALL require valid authentication on **every operation of the
public API**, in every service, except the one that exchanges credentials
for a token. That one SHALL be reachable without prior authentication,
because a caller cannot hold a token before obtaining one.

Where an operation names a tenant in its request, that tenant SHALL match
the authenticated tenant, and a mismatch SHALL be refused. A caller
holding platform (system) scope is exempt from the match, because acting
across tenants is what that scope means.

No operation SHALL derive the tenant it acts on from the request alone. A
caller-supplied tenant that is never checked against a token is
indistinguishable from no tenancy at all.

#### Scenario: Mutating operations refuse an unauthenticated caller
- **WHEN** a caller with no credentials attempts to provision a tenant,
  provision a principal, change a status, grant a role, issue a key, or read
  audit history
- **THEN** each attempt is refused as unauthenticated, and no record is
  created, changed, or returned

#### Scenario: Reading operations refuse an unauthenticated caller
- **WHEN** a caller with no credentials attempts to read a call, an
  extension, or any other tenant-owned resource through the public API
- **THEN** the attempt is refused as unauthenticated, and nothing about
  the resource is returned — including whether it exists

#### Scenario: A caller cannot name a tenant that is not their own
- **WHEN** an authenticated caller without platform scope supplies a
  tenant other than the one their token authenticates
- **THEN** the request is refused, and the operation does not run against
  either tenant

#### Scenario: Obtaining a token needs no token
- **WHEN** a caller presents a valid username and password with no other
  credentials
- **THEN** a token is returned
