# Design — call detail reporting

## D1. Terminated calls only

A record that can change after it is read is not a CDR. `ListCallRecords`
filters `state = 'Terminated'`; in-flight calls stay on the operational
Call-history page.

## D2. Attribution is captured at call creation

`call_participants.extension_id` and `principal_id` are set from the
caller/callee extension and its owner **at that moment**. A carrier caller
and an external destination have no attribution. A desk-phone caller and
an internal destination do. Ownership changes later do not rewrite
history.

## D3. Reporting is read-only and RLS-scoped

The reporting store reads `calls`, `call_participants` and `usage_seconds`
under the tenant's RLS context. It never reads an engine endpoint. It does
not mutate a call or a usage row.

## D4. Agent scope is server-side

An agent sees records where their principal is a participant. An
administrator or supervisor sees the tenant. The filter from the client is
narrowed, never widened, before it reaches the store.

## D5. Export is bounded and audited

CSV uses the same filters and is capped at 10,000 records. Each export
writes one `report.export` audit entry with the range and count; the CSV
itself is never logged. Recording/quality/retention are separate slices.
