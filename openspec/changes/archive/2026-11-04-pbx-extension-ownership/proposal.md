# Extension ownership

## Why

Phase B opens with "whose extension is this" (D-65). The link lives in
pbx-core, keyed by the membership, so voicemail, IVR, queues and
reporting can build on it without identity learning what an extension is.

## What Changes

- An extension may have one owner: an active membership of the same
  tenant. A person may own several extensions.
- `AssignExtension` / `UnassignExtension` are audited; assigning a
  disabled membership or one from another tenant is refused.
- Extension reads and the console show "Assigned to"; the Extensions
  screen assigns, changes and removes an owner.
- Deleting an extension removes its assignment; unassigned lines keep
  working.
- Per-person call attribution and history follow with LLD-09 reporting
  (D-87).

## Impact

- Migration 0022 `extension_assignments`, RLS-scoped.
- Proto/Go/TS/OpenAPI extension fields and two RPCs.
- Console Extensions list, row action and assignment dialog.
- No engine projection change; no telephony change in this slice.
