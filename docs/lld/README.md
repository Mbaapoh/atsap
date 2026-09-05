# Low-Level Design (LLD) Index

LLDs are the implementation-facing layer below the HLD (`docs/hld/`): exact
package paths, function signatures, SQL, and test plans for one bounded
context at a time. Where the HLD says *what shape* a context has (ports,
aggregates, events), the LLD says *exactly how to build it*.

Per [[../DECISIONS.md]] D-29, this stays in `docs/` as a cross-cutting
document, not an OpenSpec spec. **Relationship to OpenSpec:** each LLD below
is written to become the `design.md` (and a source for the delta `specs/`)
of one OpenSpec change under `openspec/changes/<name>/` when it's time to
implement it. `openspec/config.yaml`'s `context:` field already points
Claude Code and OpenCode at `docs/hld/`, `docs/TRD.md` and `docs/DECISIONS.md`,
so `/opsx:propose` for an LLD's change picks this up automatically — an LLD
doc is written, then proposed, not invented fresh by the agent at propose
time.

Build order is fixed by the dependency graph in
[`../hld/04-bounded-contexts.md` §10](../hld/04-bounded-contexts.md#10-bounded-context-build--dependency-graph).
LLDs are written and implemented **one at a time, in that order** — writing
LLD-03 before LLD-01 is built is exactly the scope creep the graph exists
to prevent.

## Index

| # | LLD | Bounded context | Status | OpenSpec change (when proposed) |
|---|---|---|---|---|
| 01 | [Telephony Core Walking Skeleton](LLD-01-telephony-core-walking-skeleton.md) | `telephony-core` | Draft | *(not yet proposed)* |
| 02 | Identity & Licensing | `identity`, `licensing` | Not started | — |
| 03 | PBX Core (extensions, trunks, LCR, IVR) | `pbx-core` | Not started | — |
| 04 | Compliance & Reporting | `compliance`, `reporting` | Not started | — |
| 05 | Webhook Delivery & AI Pipeline | `webhook-delivery`, `ai-pipeline` | Not started | — |
| 06 | Dialer (R2 — power dial, then predictive) | `dialer` | Not started (R2-gated) | — |

Do not start row *N+1* until row *N* is implemented and its walking-skeleton
or integration test passes — per D-26 (dependency-first within a release,
risk-first across it).
