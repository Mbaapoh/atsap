## MODIFIED Requirements

### Requirement: Only obtaining a token may be done unauthenticated
The system SHALL require valid authentication on **every operation of the
public API**, in every service, except those a caller needs before it can
hold a token. These are:
- discovering how an email signs in;
- signing in;
- refreshing a session, which is authenticated by its refresh secret rather
  than a token;
- requesting and completing a password reset.

Those SHALL be reachable without a bearer token, because a caller cannot
hold one before obtaining it, or after losing it.

Where an operation names a tenant in its request, that tenant SHALL match
the authenticated tenant, and a mismatch SHALL be refused. A caller
holding platform (system) scope is exempt from the match, because acting
across tenants is what that scope means.

No operation SHALL derive the tenant it acts on from the request alone. A
caller-supplied tenant that is never checked against a token is
indistinguishable from no tenancy at all.

#### Scenario: Mutating operations refuse an unauthenticated caller
- **WHEN** a caller with no credentials attempts to provision a tenant,
  provision a principal, change a status, grant a role, issue a key, reset
  another person's password, change a password, or read audit history
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
- **WHEN** a caller presents a valid email and password with no other
  credentials
- **THEN** a token is returned

#### Scenario: Recovering access needs no token
- **WHEN** a caller with no token asks how an email signs in, refreshes a
  session with its refresh secret, or requests or completes a password
  reset
- **THEN** each is answered without a bearer token, and nothing else is
  reachable that way

### Requirement: Failures disclose nothing about other tenants or accounts
An authentication failure SHALL be reported identically whatever its cause,
and SHALL take the same work whether or not the account exists. Sign-in
method discovery, password-reset requests and throttling SHALL answer
identically for emails with and without an account. A request naming a
resource in another tenant SHALL be answered as though the resource did not
exist.

#### Scenario: Every credential failure looks the same
- **WHEN** a caller authenticates with an unknown email, and separately
  with a correct email and wrong password, and separately as a disabled
  account
- **THEN** all three receive the same refusal, distinguishable only in the
  server's own logs

#### Scenario: An unknown email costs the same as a wrong password
- **WHEN** sign-in is attempted with an email that has no account
- **THEN** the same password verification work is performed as for an
  existing account with a wrong password, so response time does not reveal
  whether the account exists

#### Scenario: Reading another tenant's resource
- **WHEN** an authenticated caller requests a principal belonging to a
  different tenant
- **THEN** the response is the same not-found outcome returned for an
  identifier that exists nowhere
