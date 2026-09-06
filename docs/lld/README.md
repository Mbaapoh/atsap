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
| 01 | [Telephony Core Walking Skeleton](LLD-01-telephony-core-walking-skeleton.md) | `telephony-core` | Proposed | [`telephony-core-originate-bridge-hangup`](../../openspec/changes/telephony-core-originate-bridge-hangup/) (in progress) |
| 02 | Identity & Licensing | `identity`, `licensing` | Not started | — |
| 03 | PBX Core (extensions, trunks, LCR, IVR) | `pbx-core` | Not started | — |
| 04 | Compliance & Reporting | `compliance`, `reporting` | Not started | — |
| 05 | Webhook Delivery & AI Pipeline | `webhook-delivery`, `ai-pipeline` | Not started | — |
| 06 | Dialer (R2 — power dial, then predictive) | `dialer` | Not started (R2-gated) | — |

Do not start row *N+1* until row *N* is implemented and its walking-skeleton
or integration test passes — per D-26 (dependency-first within a release,
risk-first across it).

## Granularity: an LLD is neither a feature nor a task

Three distinct grains, not two:

| Level | Grain | Lives in |
|---|---|---|
| **LLD** | One bounded context, built once, evolves over many changes | `docs/lld/LLD-NN-*.md` |
| **OpenSpec change** | One feature-sized slice of that context | `openspec/changes/<name>/` |
| **Task** (`tasks.md` item) | One sitting's worth of work | inside a change folder |

One LLD decomposes into several OpenSpec changes over its lifetime (e.g.
`LLD-01` into "originate/bridge/hangup happy path", then "hold/resume",
then "attended transfer continuity", then "wire Screening to the stub
licensing/compliance ports") — not one change per LLD. Each change is
independently proposed, applied, and archived; the LLD is the stable
design reference they're all cut from, not itself a unit of delivery.

## The propose → apply → archive loop, bit by bit

1. `/opsx:propose "<context>: <one feature slice>"`. The agent reads
   `openspec/config.yaml`'s `context:` — BRD, PRD, DECISIONS, TRD, HLD, and
   the relevant LLD — and drafts `proposal.md` (cites the BRD/PRD
   requirement realised), delta `specs/` (Given/When/Then scenarios),
   `design.md` (from the LLD), and `tasks.md`.
2. **Review before apply.** This is where BRD/PRD stay authoritative: if
   the draft invents behaviour they don't support, or skips an invariant,
   reject it or `/opsx:update` it — don't let it proceed as drafted.
3. `/opsx:apply` — tasks are worked through and checked off, gated by CI
   (D-28: machine gates over human review for anything checkable).
4. Verify against the owning LLD's Definition of Done section — that is
   the acceptance bar, not "tests pass."
5. `/opsx:archive` — delta specs merge into `openspec/specs/<context>/spec.md`,
   which **becomes the new source of truth for what that context currently
   does**. The next change proposed against this context diffs against
   that living spec, not against the LLD doc again — the LLD stays the
   design *rationale*, the archived spec is the current-*behaviour* record.
6. Repeat for the next feature slice in the same context until the owning
   LLD's Definition of Done is fully met. Only then does the next LLD (per
   the dependency-graph build order above) get written and start its own
   cycle.

**Where feedback flows back up, not just down:** if `apply` surfaces a
requirement that's unbuildable as BRD/PRD/TRD describe it, that is not
silently patched. A local mismatch is fixed with `/opsx:update` on the
in-flight change. A real architectural reversal is escalated as a new
`docs/DECISIONS.md` entry (the way D-32 recorded the Leg→Participant
rename) — engineering iteration never silently rewrites BRD/PRD; it
escalates the conflict to whoever owns that document.
