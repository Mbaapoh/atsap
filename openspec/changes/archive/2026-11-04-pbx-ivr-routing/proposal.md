# IVR routing: business hours, holidays and versioned flows

## Why

The first IVR slice: a DID can enter a flow whose open/closed
destination follows the tenant's UTC business hours and holiday calendar.
This is the routing half of an auto-attendant; the DTMF menu follows in a
separate slice (D-89).

## What Changes

- `ivr_flows` owns a DID and a name; `ivr_flow_versions` holds immutable
  published revisions; the flow points at its active revision.
- A revision carries an open extension, a closed extension and the UTC
  business window.
- Inbound routing checks flows before direct inbound numbers; a holiday
  or time outside the window uses the closed destination. A DID cannot be
  both a flow and a direct inbound number.
- Publish appends a revision; rollback activates an older one.
- Console **Call flows**: create, publish, versions/rollback, delete, and
  the holiday calendar.
- The prompt field is reserved for the DTMF-menu slice; no audio is
  played yet.

## Impact

- Migration 0024 flows/versions/holidays.
- PbxService flow and holiday RPCs; OpenAPI/TS.
- Inbound router uses the flow store.
- Console navigation and page.
