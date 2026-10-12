## MODIFIED Requirements

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
