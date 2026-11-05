# Call detail reporting

## Why

LLD-09's first slice: compliance and support need final call records with
duration, outcome and **who** was on the call. The operational call
history is mutable in flight; reporting reads terminated calls only.

## What Changes

- `ReportingService.ListCallRecords` returns terminated calls with
  participants, their extension/owner attribution and usage seconds.
- `ExportCallRecords` returns a bounded CSV of the same filters and writes
  an audit entry naming the person and range (AC-07.5).
- Agents see only records where they are a participant; administrators
  and supervisors see the tenant. A filter cannot widen an agent's scope.
- `call_participants` gains `extension_id`/`principal_id`, filled at call
  creation from the extension owner at that moment (D-65/D-87).
- Console **Reports** page: filters (dates, direction, extension, person,
  number), records table and CSV export.

## Impact

- Migration 0023 participant attribution columns/indexes.
- New `reporting` context and `ReportingService` proto/RPC/OpenAPI.
- Telephony participant creation and PBX placement/inbound attribution.
- Console navigation and Reports page.
- Quality scores, recordings and retention remain later LLD-09 slices.
