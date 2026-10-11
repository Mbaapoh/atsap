## ADDED Requirements

### Requirement: A tenant's users and their roles are listable through the API

The system SHALL let an authorized caller list the users of a tenant they administer, each with status and role bindings, page by page. A user of another tenant SHALL never appear, and no credential material — password hash, API key or digest — SHALL appear in any field.

#### Scenario: An administrator lists their tenant's users

- **WHEN** a tenant administrator lists users
- **THEN** every user of that tenant is returned with its status and roles, and no user of any other tenant

#### Scenario: A caller without permission cannot list users

- **WHEN** a caller lacking permission to read users lists them
- **THEN** the request is refused as permission denied

#### Scenario: No credential material is listed

- **WHEN** users are listed
- **THEN** no field contains a password hash or any other credential

### Requirement: A granted role can be revoked through the API

The system SHALL let a caller revoke a role binding under the same authority rules that govern granting it, and SHALL audit the revocation. The system SHALL refuse to revoke the last tenant-administrator binding of a tenant, so no tenant is left without anyone able to administer it. A revoked role SHALL stop authorizing actions from the next request.

#### Scenario: A revoked role stops authorizing

- **WHEN** a role is revoked from a user
- **THEN** the user's next request needing that role is refused, and the revocation is audited

#### Scenario: The last tenant administrator cannot be removed

- **WHEN** a caller revokes the only tenant-administrator binding in a tenant
- **THEN** the request is refused as a failed precondition and the binding remains

#### Scenario: A caller cannot revoke beyond their authority

- **WHEN** a tenant administrator revokes a platform-level role or a binding in another tenant
- **THEN** the request is refused exactly as granting it would be
