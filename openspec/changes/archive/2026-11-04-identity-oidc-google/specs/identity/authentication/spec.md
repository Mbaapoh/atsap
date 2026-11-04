## ADDED Requirements

### Requirement: Google sign-in is optional and discoverable

The system SHALL report whether an installation-wide Google client is
configured, independently of any email, and SHALL show the option to
everyone when it is. With no client configured the option SHALL be
absent and password sign-in SHALL be unchanged.

#### Scenario: Configured Google is offered before an email is typed
- **WHEN** the installation has a Google client
- **THEN** the public sign-in options report Google enabled, and the console offers "Continue with Google"

#### Scenario: Unconfigured Google changes nothing
- **WHEN** the installation has no Google client
- **THEN** the options report it disabled and password sign-in works exactly as before

### Requirement: Google never creates a person

The system SHALL find a person by the provider's issuer and stable
subject only. It SHALL NOT create a person or a membership from a Google
sign-in, and SHALL refuse an account with no link exactly as a wrong
password is refused.

#### Scenario: A linked person signs in
- **WHEN** a person linked to Google completes the callback with a valid, verified ID token
- **THEN** the system starts that person's session

#### Scenario: An unlinked Google account is refused
- **WHEN** a Google account with no link completes the callback
- **THEN** the system refuses with the same answer as a wrong password and creates nothing

### Requirement: Authorization requests are bound and single-use

The system SHALL bind each Google authorization request with a
server-side one-time state, PKCE and a nonce, and SHALL verify the ID
token's signature, issuer, audience, expiry, nonce and verified email
before using it. A state that is expired, unknown or already used SHALL
be refused.

#### Scenario: A replayed callback is refused
- **WHEN** a callback state has already been consumed
- **THEN** the request is refused and no session or link is created

#### Scenario: A nonce for another request is refused
- **WHEN** the ID token's nonce does not match the request's stored nonce
- **THEN** the request is refused

### Requirement: Linking is explicit

The system SHALL link Google on an invitation only when Google's
verified email equals the invited email, and from the account screen
only after the signed-in person re-enters their current password. A
mismatched Google email SHALL NOT consume the invitation. A second link
for the same person or the same Google account SHALL be refused.

#### Scenario: An invitation is accepted with the matching Google account
- **WHEN** the invitee opens the emailed link, chooses Google, and Google's verified email equals the invited email
- **THEN** the invitation is consumed, Google is linked, and a session starts

#### Scenario: A different Google account does not burn the invitation
- **WHEN** the Google email differs from the invited email
- **THEN** the link is refused and the invitation can still be used

#### Scenario: Linking needs the current password
- **WHEN** a signed-in person starts a link without the correct current password
- **THEN** the request is refused and no state is issued

### Requirement: Public mailbox domains cannot be claimed

The system SHALL refuse a claim on a consumer mailbox domain, so no
tenant can route every Gmail or Outlook user to itself.

#### Scenario: A public domain claim is refused
- **WHEN** a caller tries to claim `gmail.com` or another consumer mailbox domain
- **THEN** the claim is refused
