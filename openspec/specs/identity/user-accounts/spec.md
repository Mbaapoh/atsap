# identity/user-accounts Specification

## Purpose
Establishes the person behind every membership: one user account per person across the whole installation, joined to tenants through memberships, so that one person needs one login and one future SSO link however many tenants they work in, while no tenant learns about another.

## Requirements

### Requirement: A person has one account across the installation

The system SHALL represent each person as one user account identified by an email address that is unique across the installation, compared without regard to letter case or surrounding whitespace. A user SHALL act in a tenant only through a membership of that tenant, and SHALL hold at most one membership per tenant.

#### Scenario: One person in two tenants signs in once

- **WHEN** a user who is a member of two tenants signs in with their email and password
- **THEN** one sign-in gives them access to both tenants, choosing one at a time, without a second account or password

#### Scenario: Email uniqueness ignores case

- **WHEN** an account exists for `Ana@Example.com` and a new person is added as ` ana@example.COM `
- **THEN** no second account is created; the existing person is the one added

#### Scenario: A second membership of the same tenant is refused

- **WHEN** an administrator adds a person who is already a member of their tenant
- **THEN** the request is refused as already existing, and no second membership is created

### Requirement: Adding a member never reveals the person's other tenants

When a tenant administrator adds a person by email, the system SHALL respond identically whether or not an account already exists for that email. It SHALL NOT reveal, to anyone below platform scope, which other tenants the person belongs to. An initial password supplied for a person who already has an account SHALL NOT replace that person's password.

#### Scenario: Adding an existing person looks like creating one

- **WHEN** an administrator adds an email that already has an account in another tenant, and separately adds an email with no account
- **THEN** both responses have the same shape and content apart from the identifiers, and neither mentions any other tenant

#### Scenario: An existing person's password is not replaced

- **WHEN** an administrator adds an existing person and supplies an initial password
- **THEN** the person still signs in with their own existing password, and the supplied one does not work

### Requirement: A tenant administrator governs the membership, not the person

A tenant administrator SHALL be able to add a person to their tenant, disable or restore that membership, and grant or revoke its roles. Changing a person's email, display name or account status, or anything else that would take effect in other tenants, SHALL require the person themselves or platform scope.

#### Scenario: Disabling a membership leaves other tenants untouched

- **WHEN** a tenant administrator disables a person's membership of their tenant
- **THEN** the person can no longer act in that tenant from their next request, and can still act in every other tenant they belong to

#### Scenario: A tenant administrator cannot disable the person

- **WHEN** a tenant administrator attempts to disable a person's account, or change their email
- **THEN** the request is refused as permission denied, and the account is unchanged

#### Scenario: A disabled account loses every tenant

- **WHEN** a platform operator disables a person's account
- **THEN** the person can act in none of their tenants from their next request

### Requirement: A new person without a password is invited by email

When an administrator adds a person who has no account and supplies no initial password, the system SHALL create the account without a password and SHALL email that person a single-use link to set one. The link SHALL expire after 72 hours. Until a password is set, the account SHALL NOT be usable with a password.

#### Scenario: An invited person sets a password and signs in

- **WHEN** a new person is added without a password, and follows the emailed link to set one
- **THEN** they can sign in with that password, and the link no longer works

#### Scenario: An invitation link expires

- **WHEN** an invitation link is used more than 72 hours after it was sent
- **THEN** it is refused, and the administrator can add the person again to send a new one

### Requirement: Existing accounts survive the move to installation-wide users

Existing principals SHALL become memberships of users without losing an identifier, role binding, API key or audit record. Principals that share an email SHALL become one user only when their stored passwords are the same. Otherwise the upgrade SHALL stop, naming the conflicting accounts, rather than choose one password over the other.

#### Scenario: An existing administrator signs in after the upgrade

- **WHEN** an installation with existing principals is upgraded
- **THEN** each person signs in with their email and their existing password, and keeps every role they had

#### Scenario: A conflicting duplicate stops the upgrade

- **WHEN** two principals in different tenants share an email but have different passwords
- **THEN** the upgrade stops before changing anything, and reports both accounts
