# Design — extension ownership

## D1. One aggregate, tenant-scoped

`extension_assignments (tenant_id, extension_id, principal_id,
assigned_at)` with `extension_id` unique per tenant. It references the
extension (cascade on delete) but not identity's principals table: the
context boundary stays intact. The application resolves the principal
through a narrow `PrincipalReader` and refuses one that is not an active
member of the tenant.

## D2. Assignment is configuration, never observed state

It is changed only by an authorized administrator, audited with before
and after values, and never touches the engine. An unassigned line is a
lobby phone, fax or conference room: normal (D-65).

## D3. Reads attach the owner

`ListExtensions` reads all assignments for the page in one query and
resolves each distinct principal once; `GetExtension` does the same for
one. The console therefore shows a name/email, never an id, and the
extension list stays a fixed number of queries.

## D4. No call-path change in this slice

Participant attribution (`call_participants.extension_id/principal_id`)
and "my calls" land with LLD-09 reporting (D-87), where the immutable CDR
and usage ledger read them. This change adds no telephony dependency.
