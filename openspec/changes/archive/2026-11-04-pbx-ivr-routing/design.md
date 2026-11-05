# Design — IVR routing

## D1. Flow on the DID

A flow owns one DID (`tenant_id, number` unique) and is mutually exclusive
with a direct inbound number. The inbound router checks flows first; the
application refuses creating one when the other exists.

## D2. Immutable revisions, movable active pointer

A published revision is never edited. `Publish` appends the next version
and moves `ivr_flows.active_version`; `Rollback` only moves the pointer.
Audit records both.

## D3. UTC time, holidays, safe default

Business hours are minutes from midnight UTC; start == end means open all
day, an overnight window is supported. A holiday always takes the closed
branch. If the closed branch has no destination, the call is refused as
not found rather than left silent.

## D4. This slice is routing, not menu

No prompt is played and no DTMF is collected. The `prompt` field is
reserved; the menu needs ARI answer/play/DTMF state and is its own change
(D-89). Ownership attribution for the chosen extension is unchanged.
