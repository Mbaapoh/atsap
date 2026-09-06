---
name: architectural-decision-records
description: Record architecture decisions as D-number entries in docs/DECISIONS.md. Use when a decision is taken (stack, pattern, process, policy) or when asked to log an ADR.
---

`docs/DECISIONS.md` is append-only. New entries go at the bottom with a date.
Past entries are never edited — a reversal is a new entry that names the old
one. Read the whole log before proposing anything: most obvious suggestions
(microservices, event sourcing/CQRS, per-entity repositories, sequential
specialist dispatch, 5-nines availability, predictive before power dialling)
were considered and rejected there with reasons not visible elsewhere.

**Lens:** the solo architect deciding — this skill speaks with decision
authority; agents draft, the human approves.

## Method

1. **Read `docs/DECISIONS.md` end to end.** Check for an existing decision
   covering the topic. If one exists and the change genuinely warrants
   revisiting it, say so explicitly (per `openspec/config.yaml`) — never
   silently drift.
2. **Take the next D-number** (max existing + 1). Numbers are never reused,
   never backfilled.
3. **Write the entry** in house format:
   `**D-NN · Title (YYYY-MM-DD).**` followed by some or all of: Decision,
   Context, Alternatives (each with one-line rejection reason), Rationale,
   Consequences, Related Decisions (D-numbers), Traceability (BRD § / PRD
   epic). Match the weight to the blast radius — D-36 (portal stack) is the
   full template; a narrow process tweak can be three sentences.
4. **Place it** under the correct `##` section (Commercial / Requirements that
   were corrected / Architecture / Process). When in doubt, Process.
5. **Cross-link**: related TRD/HLD passages cite the new number; the new
   entry cites them back. Never leave a decision unreferenced by the docs it
   constrains.

## Constraints

- Industry convention followed: ADR form (context, options, consequences) in
  the project's D-number idiom — not generic markdown-ADRs in a side folder.
- No bulk or speculative entries. One decision per entry; decided means
  decided — "options we're still weighing" belong in a change proposal or
  design doc, not the log.
- Third-party skill or dependency approvals cite D-37's per-skill gate.
- Human-in-the-loop: an agent drafts entries; the solo architect decides.
  Never auto-log a decision from an agent proposal — a D-entry without
  explicit human approval is a diary, not a decision.
