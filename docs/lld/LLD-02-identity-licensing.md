# LLD-02 — Identity & Licensing (split)

**This document no longer exists as written.** It covered two bounded
contexts and was split on 2026-09-08:

- **[LLD-02 — Identity](LLD-02-identity.md)** — everything identity.
  Implemented and archived.
- **[LLD-08 — Licensing](LLD-08-licensing.md)** — everything licensing.
  Draft; nothing built.

## Why

`docs/lld/README.md` states that an LLD covers **"one bounded context at
a time"**, and [HLD 04 §10.1](../hld/04-bounded-contexts.md) lists
`identity` and `licensing` as separate Tier-0 contexts, each depending on
nothing. This document was the only LLD covering two.

Its own §2 justified the merge: the contexts "share one build-order tier
and one cutover (auth + entitlement activate together)". Delivery
disproved that. Identity shipped in three changes — `identity-auth-rbac`,
`identity-api` and `auth-cutover-connectrpc`, all archived — while
licensing shipped nothing at all. They never activated together, and no
point existed at which they could have.

## Why this file remains

It is a tombstone, not content. Archived OpenSpec changes cite this path
as their design source and are immutable:

- `openspec/changes/archive/2026-09-07-identity-auth-rbac/design.md`
- `openspec/changes/archive/2026-09-07-identity-api/design.md`

Deleting the file would leave those citations dangling. Keeping a
redirect costs nothing and preserves the record — the same reasoning that
tombstoned migration `0002` rather than removing it.

Both of those changes are identity work, so their design source is now
[LLD-02 — Identity](LLD-02-identity.md).
