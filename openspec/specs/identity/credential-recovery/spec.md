# identity/credential-recovery Specification

## Purpose
Establishes how a password is changed, recovered and protected from guessing: self-service change, recovery by an emailed link, an administrator reset that cannot be used to take over a person's access to other tenants, and throttling that slows guessing without letting anyone lock a person out.

## Requirements

### Requirement: A person can change their own password

A signed-in person SHALL be able to change their password by supplying their current one. The new password SHALL meet the password length rules. A change SHALL end every other session of that person, and SHALL keep the session that made it.

#### Scenario: Changing the password ends other sessions

- **WHEN** a person signed in on two browsers changes their password on one
- **THEN** the other browser's session is refused on its next request, and the one that made the change continues

#### Scenario: A wrong current password changes nothing

- **WHEN** a person supplies an incorrect current password
- **THEN** the change is refused, and the existing password still works

### Requirement: A forgotten password is recovered by an emailed link

The system SHALL accept a reset request for any email address and SHALL answer every request identically. When the email belongs to an account that may sign in with a password, it SHALL send a single-use link valid for 30 minutes. Completing a reset SHALL set the new password and end every session of that person.

#### Scenario: A reset by email

- **WHEN** a person requests a reset and follows the emailed link to set a new password
- **THEN** they can sign in with the new password, the old one no longer works, every earlier session has ended, and the link no longer works

#### Scenario: An unknown email gets the same answer

- **WHEN** a reset is requested for an email that has no account
- **THEN** the response is identical to the response for a known email, and no email is sent

#### Scenario: An expired or used link is refused

- **WHEN** a reset link is used after 30 minutes, or a second time
- **THEN** it is refused, and the password is unchanged

#### Scenario: A domain that requires SSO gets no reset link

- **WHEN** a reset is requested for an email whose domain is claimed by an identity provider that requires SSO
- **THEN** no link is sent, and the response is identical to every other reset response

### Requirement: An administrator can reset a password, within their authority

An administrator SHALL be able to set a temporary password for a member of their tenant. The person SHALL then be required to change it at their next sign-in, and until they do, that sign-in SHALL permit nothing but the change. A tenant administrator SHALL be refused when the person belongs to any tenant the administrator does not administer; such a reset SHALL require platform scope. Every administrator reset SHALL be audited without the password.

#### Scenario: A temporary password must be changed

- **WHEN** an administrator resets a member's password and the member signs in with the temporary one
- **THEN** the member can change their password, any other request is refused as permission denied until they do, and their earlier sessions have ended

#### Scenario: A shared person cannot be reset from one tenant

- **WHEN** a tenant administrator resets the password of a person who is also a member of a tenant they do not administer
- **THEN** the request is refused as permission denied, and the person's password is unchanged

#### Scenario: The reset is audited without the password

- **WHEN** an administrator resets a password
- **THEN** one audit record names the administrator and the member, and contains no password material

### Requirement: An operator can recover an account off the network

The system SHALL provide a means, available only to an operator on the installation's own server and not over the network, to set a temporary password for an account by email. The person SHALL be required to change it at next sign-in, every session of theirs SHALL end, and the recovery SHALL be recorded in their authentication history and in the audit log of each of their tenants. The temporary password SHALL be shown once and never logged.

#### Scenario: Recovering a locked-out administrator

- **WHEN** an operator runs the recovery on the server for an administrator's email
- **THEN** a temporary password is shown once, the administrator can sign in with it only to change it, and their earlier sessions have ended

#### Scenario: Recovery is not reachable over the network

- **WHEN** the public API is examined for an operation equivalent to the off-network recovery
- **THEN** none exists: over the network, only the authority-checked administrator reset and the emailed link can reset a password

### Requirement: Password guessing is slowed without locking anyone out

The system SHALL slow repeated failed password attempts, counted separately for each account and for each source address. After five consecutive failures, further attempts SHALL be refused for a delay that doubles with each further failure, up to 15 minutes, with a refusal that states the delay. The system SHALL NOT permanently lock an account. Throttling SHALL behave identically for emails that have no account, and a success SHALL clear the account's count.

#### Scenario: Repeated failures are delayed

- **WHEN** six consecutive wrong passwords are submitted for one account
- **THEN** the sixth is refused as exhausted with a retry delay, even if it is correct, and no password is checked until the delay passes

#### Scenario: Unknown and known emails throttle alike

- **WHEN** repeated failures are submitted for an email with no account and for one with an account
- **THEN** both are throttled after the same number of attempts, with the same responses

#### Scenario: The owner signs in after the delay

- **WHEN** the delay has passed and the correct password is submitted
- **THEN** sign-in succeeds, and the account's failure count is cleared

### Requirement: Authentication history is recorded per person

The system SHALL record, in an append-only history keyed by person:
- sign-in successes and failures;
- throttling;
- password changes, reset requests and completed resets;
- session revocations.

No entry SHALL contain a password, a secret or a token. An attempt for an email with no account SHALL be recorded without a person.

#### Scenario: A failed sign-in is recorded without the password

- **WHEN** a sign-in fails because of a wrong password
- **THEN** one history entry records the failure against that person, with the source address and time, and the attempted password appears nowhere
