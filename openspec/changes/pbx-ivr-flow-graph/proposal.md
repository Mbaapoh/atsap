# IVR flow graph: typed destinations, multi-level menus and tenant schedules

## Why

The architecture review of 2026-11-07 (D-91) compared the shipped IVR
(D-89 time routing, D-90 single-level menu) with D-22, BRD FBR-R1-06 and
current vendor practice (3CX, VitalPBX, FreePBX, FusionPBX, Amazon
Connect). The versioning, rollback, ARI execution and fallbacks are right
and stay. Four things are below the industry standard and block the next
Phase B work:

1. **Destinations are extensions only.** Every vendor lets an IVR option
   reach a ring group, queue, another menu, voicemail, an announcement, an
   external number or hang-up. `pbx-queues` cannot add a queue as a
   destination without another schema change.
2. **There is one menu level.** D-22 says flows are a node graph that the
   domain interprets; FBR-R1-06 requires multi-level menus.
3. **Business hours are one UTC window applied to every day.** Weekends
   read as open, and a tenant outside UTC must convert by hand.
   VitalPBX/FreePBX time groups and 3CX office hours have several ranges
   per weekday in a time zone, plus a manual override.
4. **Menu behaviour gaps.** There is no re-prompt before the fallback
   (LLD-03 §3.2 already asks for one), no direct dial and no `*`/`#`.
   Deleting an extension does not check the flows that target it.

## What Changes

- A revision becomes a **typed node graph** (schema v2): `schedule`,
  `menu`, `play`, `goto` and `hangup` nodes, with an `entry` node.
  **BREAKING** for the flow API payload only. Existing v1 revisions are
  converted on read and never rewritten; v1 requests are accepted and
  translated for one release.
- A **typed `Destination {kind, ref}`**: `extension`, `flow`, `external`
  and `hangup` now. The `queue`, `ring_group` and `voicemail` kinds are
  reserved and refused at publish until their change ships.
- **Schedules**: tenant-owned, named, an IANA time zone (the tenant's
  default zone, overridable), several ranges per weekday, holidays
  evaluated in that zone, and a manual override (`open`/`closed`, until
  the next transition or until cleared, audited).
- **Menu**: bounded re-prompt (default 2), optional direct dial of an
  extension number, keys `0–9`, `*` and `#`.
- **Publish validation**: refuses dead ends, unreachable nodes, a cycle
  that can be taken without caller input, menu depth above 5, unknown or
  reserved destination kinds, and an `external` destination the flow's
  tenant may not dial (outbound class, fraud guard).
- **Extension deletion** refuses while an active revision targets the
  extension and names the flows.
- **Console**: the Call flows editor becomes a tree editor (menu →
  option → node) with schedule management. A drag-and-drop canvas is
  `pbx-ivr-flows`, later.

## Impact

- **Specs:** `pbx-core/call-flows` (modified and added requirements).
- **Database:** migration 0026: `ivr_flow_versions.schema_version`,
  `graph JSONB`; `ivr_schedules` (RLS); `tenants.time_zone`. v1 columns
  stay readable.
- **Code:** `pbx/domain` (graph, destination, schedule, the pure
  interpreter); `pbx/application` (publish validation, override, delete
  guard); `telephony` IVR session (node stepping, re-prompt, direct dial);
  proto, OpenAPI and TS clients; the console Call flows screen.
- **Decisions:** implements D-91 and honours D-22 and D-47 (no dialplan).
  It supersedes the "UTC window" and "extension destination" parts of
  D-89 and D-90.
- **Out of scope:** prompt upload (the media-storage decision), `TestFlow`
  simulation, a visual canvas, AI flow generation (LLD-10), and queue,
  ring-group and voicemail execution.
