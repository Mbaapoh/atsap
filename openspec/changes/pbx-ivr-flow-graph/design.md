# Design — IVR flow graph

Context: D-91, LLD-03 §3.2.1, PRIOR-ART (IVR rows). Read those first.

## D1. A revision is a graph of five node kinds

```text
RevisionV2 { schema_version: 2, entry: NodeID, nodes: map[NodeID]Node }
schedule { schedule_id, open: NodeID, closed: NodeID, holiday?: NodeID }
menu     { prompt, timeout_s (1–60, default 5), retries (0–5, default 2),
           options: map[key]NodeID  key ∈ 0–9 * #,
           direct_dial: bool, no_input?: NodeID, invalid?: NodeID }
play     { prompt, next: NodeID }
goto     { destination: Destination }
hangup   { reason }
```

NodeIDs are short strings unique within a revision. The graph is stored
whole as `graph JSONB` on the immutable revision row, and nodes are never
shared between revisions. Five kinds cover FBR-R1-06; a new behaviour is a
new kind, never a generic scripting node.

**Rejected:** one table per module, linked by goto strings (the FreePBX
style), because the builder and the API would have to reassemble a graph.
**Rejected:** a general workflow engine, which is more than R1.0 needs.

## D2. Destination is a value type shared with future contexts

`Destination {kind, ref}` with kinds `extension`, `flow`, `external`,
`hangup`, plus reserved kinds that later changes register (LLD-03
§3.2.1): `queue`, `ring_group`, `voicemail`, `voicemail_main`,
`conference` and `park_lot` (and `ai_agent`, R2, D-96). A kind is
enabled by a registry the application layer fills at start-up: `pbx-queues`
registers `queue` and its resolver, with no migration. A `flow` destination
enters another flow's **active** revision at that flow's entry node; it
counts toward the depth limit and must not form a cycle.

## D3. The interpreter is pure

`Step(rev, node, event, now, calendar) -> (next NodeID, []Command)`. The
events are Entered, Digit(k), Timeout and PlaybackFailed. The commands are
Answer, Play(prompt), StartTimer(s), StopPlayback, Route(Destination) and
Hangup(reason). The interpreter does no I/O. Telephony's IVR session holds
`(revision, node, attempts)` per channel and executes the commands over ARI,
as D-90 does today. Unit tests cover the interpreter with no Asterisk.

## D4. Schedules in a time zone

`ivr_schedules {id, tenant_id, name, time_zone, ranges JSONB
[{weekday 0–6, start_minute, end_minute}], override, override_until}`.
`tenants.time_zone` defaults to `UTC` and the console asks for it.
Evaluation converts `now` into the schedule's zone with Go's `time`
package; DST is handled by the zone database. A range with end < start
crosses midnight into the next weekday. A holiday matches the tenant's
holiday calendar on the **local** date. The override is one of `none`,
`open`, `closed`, `open_until_next` or `closed_until_next`; the "until
next" forms clear themselves at the next scheduled transition. Every
override change is audited.

## D5. Migration: convert on read, never rewrite

Migration 0026 adds `schema_version INT NOT NULL DEFAULT 1` and
`graph JSONB` to `ivr_flow_versions`. A v1 row is converted on load:
- a `schedule` node over a synthetic schedule built from the row's UTC
  window, applied to **all seven days**, which preserves today's behaviour
  exactly;
- then a `menu` node (if the row has a menu) or a `goto` to the open
  extension;
- then `goto` nodes for every extension the row names.

A published revision is immutable, so rollback to a v1 revision behaves
exactly as before. New publishes write v2 only. The v1 request fields on
`PublishFlow` are accepted for one release and translated the same way;
OpenAPI marks them deprecated.

## D6. Validation at publish

The publish is refused when:
- the entry is missing;
- a node is unreachable from the entry;
- an option points to a missing node;
- a cycle can be taken without caller input (a menu with a digit option
  back to itself or an ancestor is allowed, since it needs input);
- menu nesting is deeper than 5, counting `flow` hops;
- a destination kind is unknown or reserved;
- an extension is missing;
- a `flow` reference is missing or has no active revision;
- an `external` number the tenant's outbound policy refuses;
- a schedule is missing.

Each refusal names the node.

## D7. Extension deletion guard

`DeleteExtension` queries the **active** revisions of the tenant's flows
for a `goto extension` (or a menu default) naming the id. If any match, it
refuses with `FailedPrecondition` and lists the flow names. Inactive
revisions do not block; rolling back to one later is refused at
activation, because activation re-runs validation.

## D8. Direct dial and re-prompt

With `direct_dial`, the session collects up to the tenant's extension
length (inter-digit timeout 2 s). A full extension number routes there; a
single key that matches an option routes immediately when no extension
starts with it. Otherwise it waits for the inter-digit timeout. Invalid
input or a timeout replays the prompt while `attempts < retries`, then
takes the fallback; a missing fallback hangs up with `ivr_no_input` or
`ivr_invalid_input`.

## Live proof

- **Interpreter and validator:** table-driven unit tests, including DST
  transitions, cross-midnight ranges and conversion of every v1 row shape.
- **Integration:** publish, rollback, the delete guard, schedule RLS and
  v1 rows read as v2.
- **E2e:** an inbound carrier call through a two-level flow on the rig.
- **UAT:** a real handset walks the two levels, direct-dials an extension
  and hits the re-prompt (`docs/uat/ivr-menu.md`, extended).
