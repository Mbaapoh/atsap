## MODIFIED Requirements

### Requirement: A principal belongs to exactly one tenant
Every principal SHALL be one person's membership of exactly one tenant: it
SHALL belong to exactly one tenant and to exactly one user account. The
system SHALL NOT permit a principal to exist without a tenant or without a
user, nor to act for any tenant other than its own. Identifiers of
principals SHALL remain stable, so that every record naming a principal
continues to name the same membership.

#### Scenario: Principal is created under a tenant
- **WHEN** a principal is provisioned for a tenant
- **THEN** the principal is associated with exactly that tenant and one
  user account, and its identity is meaningful only within that tenant

#### Scenario: Acting as a membership never reaches another tenant
- **WHEN** a person who is a member of two tenants acts with an access
  token for one of them and names the other in a request
- **THEN** the request is refused, exactly as it would be for a person
  belonging only to the first tenant

## REMOVED Requirements

### Requirement: Usernames are unique per tenant, not globally
**Reason:** People now sign in with an email that is unique across the
installation, and a person has one account however many tenants they
belong to (identity/user-accounts, D-61). A per-tenant username was the
identifier of a per-tenant account, and that kind of account no longer
exists.
**Migration:** Existing principals become memberships of users keyed by
their email; the username column is dropped after the migration. Clients
that sent `username` to `ProvisionPrincipal` send `email` (and optionally
`display_name`); `AuthenticateUser` reads its `username` field as the
email until it is retired.
