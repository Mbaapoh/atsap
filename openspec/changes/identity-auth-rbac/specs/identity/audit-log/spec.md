## Purpose

Establishes the immutable record of administrative action: what was changed, by
whom, and from what to what — written once, alterable by no one, and never
carrying the credentials it describes.

## ADDED Requirements

### Requirement: Every administrative mutation writes exactly one audit record
The system SHALL write exactly one audit record for each administrative
mutation, capturing the acting identity, the action, the resource affected, the
state before, the state after, and the time it occurred.

#### Scenario: One mutation produces one record
- **WHEN** an administrator changes a principal's role
- **THEN** exactly one audit record exists for that change, naming the
  administrator, the principal, and both the previous and the new role

#### Scenario: A rejected mutation writes no record
- **WHEN** an attempted mutation is refused by authorization
- **THEN** no audit record is written for it, because nothing changed

### Requirement: Audit records are immutable once written
The system SHALL provide no interface — for any role, including the highest
administrative role — that edits or deletes an audit record.

#### Scenario: No role can alter an audit record
- **WHEN** any principal, including a tenant administrator, attempts to modify
  or remove an existing audit record
- **THEN** the attempt is refused and the record remains exactly as written

### Requirement: Audit records never contain credential material
An audit record's captured before and after state SHALL NOT contain a password,
a password hash, an API key, an API key digest, or signing key material, even
when the mutation being recorded concerned one of those values.

#### Scenario: Password change is recorded without the password
- **WHEN** an administrator resets a principal's password and the change is
  audited
- **THEN** the audit record shows that the credential changed, and contains
  neither the old nor the new password or hash

### Requirement: Audit records are visible only to their own tenant
An audit record SHALL be readable only within the tenant it belongs to. No
caller SHALL read another tenant's audit records through any interface.

#### Scenario: Audit history is tenant-scoped
- **WHEN** a principal reads audit history while another tenant also has
  records
- **THEN** only the caller's own tenant's records are returned, with no
  indication that others exist
