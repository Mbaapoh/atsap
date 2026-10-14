# identity/sessions Specification

## Purpose
Establishes how a person stays signed in to the console: a session that outlives the short-lived access token, can be withdrawn at any moment, detects a stolen refresh secret, and lets a person who belongs to several tenants move between them without ever holding authority over more than one at a time.

## Requirements

### Requirement: Signing in starts a session

A successful password sign-in SHALL start a session and return:
- an access token naming one membership;
- the memberships the person may choose between;
- a refresh secret, delivered only in a cookie that page scripts cannot read and that the browser sends only to same-site requests.

The refresh secret SHALL be stored only as a one-way digest.

#### Scenario: A person with one tenant is signed straight in

- **WHEN** a person who is a member of exactly one tenant signs in
- **THEN** the access token names that membership and the refresh secret is set in an HttpOnly, SameSite=Strict cookie

#### Scenario: A person with several tenants chooses one

- **WHEN** a person who is a member of several tenants signs in
- **THEN** the response lists those memberships, and choosing one issues an access token for that tenant only

#### Scenario: The refresh secret cannot be read back

- **WHEN** a session's stored record is inspected directly in the database
- **THEN** the refresh secret is not present and cannot be derived from what is stored

### Requirement: Refreshing rotates the secret and detects reuse

Refreshing a session SHALL issue a new access token and a new refresh secret, and the previous secret SHALL stop working, except that it is accepted once more as a retry during a grace window. If the immediately previous secret is presented within 30 seconds of being rotated away — the response that carried its replacement having been lost — the system SHALL treat the request as a retry: it SHALL rotate again, issuing a new access token and refresh secret, and the secret whose response was lost SHALL stop working. The window SHALL be measured from the original rotation, and retries SHALL NOT extend it. Each retry SHALL be recorded as an authentication event. If a rotated secret is presented after the window has closed, the system SHALL revoke the whole session, because only a copy can present it.

#### Scenario: Refresh keeps a person signed in

- **WHEN** a person refreshes their session before it expires
- **THEN** they receive a new access token and a new refresh secret, and the previous secret works only as a retry within the grace window

#### Scenario: A refresh whose response was lost is retried

- **WHEN** a session is refreshed, the response never reaches the browser, and the browser refreshes again with the previous secret within 30 seconds
- **THEN** the refresh succeeds with a new access token and a new refresh secret, the session is not revoked, the secret issued in the lost response no longer works, and the retry is recorded as an authentication event

#### Scenario: Retries do not extend the window

- **WHEN** the previous secret is presented again more than 30 seconds after it was first rotated away, even though it was accepted as a retry within the window
- **THEN** the request is refused and the session is revoked

#### Scenario: A replayed secret revokes the session

- **WHEN** a refresh secret that was rotated away more than 30 seconds ago is presented
- **THEN** the request is refused, the session is revoked, and the current secret and every access token of that session stop working on their next use

#### Scenario: An older or unknown secret is refused

- **WHEN** a secret is presented that is neither the session's current secret nor its immediately previous one
- **THEN** the request is refused as unauthenticated and the session is unchanged

### Requirement: Switching tenant never widens a token

A person SHALL change the tenant they act in by refreshing their session for another of their memberships. Each access token SHALL name exactly one membership, and a refresh SHALL refuse a tenant the person is not an active member of.

#### Scenario: Switching to another tenant

- **WHEN** a person refreshes their session naming another tenant they belong to
- **THEN** the new access token names that tenant's membership, and requests naming the previous tenant are refused with it

#### Scenario: Switching to a tenant the person does not belong to

- **WHEN** a person refreshes naming a tenant where they have no active membership
- **THEN** the refresh is refused, and the response is the same whether or not that tenant exists

### Requirement: Sessions expire when idle and absolutely

A session SHALL expire 30 minutes after its last refresh, and SHALL expire 12 hours after it began whatever its activity. An expired session SHALL NOT be refreshed.

#### Scenario: An idle session expires

- **WHEN** a session is not refreshed for more than 30 minutes
- **THEN** refreshing it is refused, and the person must sign in again

#### Scenario: An active session still ends at 12 hours

- **WHEN** a session has been refreshed continually for 12 hours
- **THEN** the next refresh is refused, and the person must sign in again

### Requirement: Ending a session takes effect on the next request

Signing out, a password change or reset, a replayed refresh secret, and disabling the person's account SHALL each end the affected sessions. Every access token issued from an ended session SHALL be refused on its next use, without waiting for it to expire.

#### Scenario: Signing out ends access at once

- **WHEN** a person signs out and their access token is then presented
- **THEN** it is refused as unauthenticated, although it has not yet expired, and the refresh cookie is cleared

#### Scenario: Disabling an account ends its sessions

- **WHEN** a platform operator disables a person's account while they are signed in
- **THEN** their next request is refused, and their session cannot be refreshed
