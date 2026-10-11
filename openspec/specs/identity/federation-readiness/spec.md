# identity/federation-readiness Specification

## Purpose
Prepares sign-in for single sign-on with a customer's own identity provider: accounts that need no password, sign-in that first asks which method applies to an email, provider domains a tenant can claim, and external identities linked by the provider's own identifier rather than by email. The protocols themselves arrive in a later change.

## Requirements

### Requirement: An account may have no password

The system SHALL allow an account to exist without a password. Password sign-in for such an account SHALL fail exactly as a wrong password does.

#### Scenario: A password-less account cannot sign in with a password

- **WHEN** someone attempts a password sign-in for an account that has no password
- **THEN** it is refused with the same response, and after the same work, as a wrong password

### Requirement: Sign-in begins by discovering the method

The system SHALL answer, for any email and without authentication, which sign-in methods apply to it. While no identity provider requires SSO for the email's domain, the answer SHALL be password sign-in. The answer for an email with no account SHALL be identical to the answer for an email with an account in the same domain.

#### Scenario: Discovery offers a password today

- **WHEN** the sign-in methods are requested for any email on an installation with no enforced identity provider
- **THEN** the answer is password sign-in

#### Scenario: Discovery is not an account oracle

- **WHEN** the sign-in methods are requested for an email with no account and for an email with one, in the same domain
- **THEN** the two answers are identical

### Requirement: A verified domain belongs to one provider, and enforcement disables passwords

An email domain SHALL be claimed by at most one identity provider in the installation, and a claim SHALL take effect only once the domain is verified. When the claiming provider requires SSO, discovery SHALL name that provider for the domain's emails, and password sign-in and password resets SHALL be refused for them.

#### Scenario: A domain cannot be claimed twice

- **WHEN** a second identity provider claims a domain already claimed by another
- **THEN** the claim is refused, and the first provider keeps the domain

#### Scenario: An enforced provider disables password sign-in

- **WHEN** a domain is verified for a provider that requires SSO, and a member of that domain attempts a password sign-in
- **THEN** the attempt is refused, and discovery names the provider instead

#### Scenario: An unverified claim changes nothing

- **WHEN** a provider claims a domain that is not yet verified
- **THEN** discovery and password sign-in for that domain's emails behave as if no provider existed

### Requirement: External identities are linked by provider and subject, never by email

An identity from an external provider SHALL be linked to an account by the provider and the provider's subject identifier. A provider and subject SHALL be linked to at most one account. An email asserted by a provider SHALL NOT, on its own, link to or sign in to an existing account.

#### Scenario: One external identity maps to one account

- **WHEN** a link is created for a provider and subject that are already linked to another account
- **THEN** it is refused, and the existing link is unchanged

#### Scenario: A matching email is not a link

- **WHEN** a provider asserts an email that belongs to an existing account that has no link for that provider and subject
- **THEN** no access to that account is granted on the strength of the email
