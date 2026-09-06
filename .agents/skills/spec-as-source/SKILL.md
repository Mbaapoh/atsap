---
name: spec-as-source
description: Extract living specifications from implemented source code. Use when code exists ahead of specs, after a walking-skeleton spike, or when asked what the code actually does.
---

Code is the truth about behaviour; specs are the truth about intent. This
skill reads the first and writes the second — for the D-25 walking skeleton
(whose code is deleted but findings kept), for adapters built ahead of their
change, and whenever implementation has outrun documentation.

**Lens:** the architect reading built truth from code — extract faithfully,
reconcile openly, never invent.

## Ground truth (code layout)

- `api/...` — Go: domain/application layers define ports (interfaces);
  adapters implement them. Domain code never imports adapters (TRD).
- `api/internal/telephony/acl/ari`, `api/internal/telephony/acl/ami` —
  the Asterisk interface surface the anti-corruption layer absorbs.
- `core/conf/*` — dialplan entry points only; all call logic lives in
  application code (D-16).
- Pure units: compliance checks (D-21, inputs in → verdict out), IVR fold
  (D-22, `(node, event) → (next, commands)`), usage accrual
  (per-Participant-per-second, no double-count on channel change).

## Method

1. **Scope to one behaviour surface** (a port, a handler, a pure function).
   Read the code paths end to end, including error and degradation branches
   (hld/09 is the oracle for what "correct failure" looks like).
2. **Write requirements + scenarios** in the house delta-spec shape
   (template: `.../specs/telephony-core/call-lifecycle/spec.md`):
   `### Requirement:` voiced with SHALL/SHALL NOT, each followed by
   `#### Scenario:` bullets (WHEN trigger, THEN observable outcome —
   API response, event published, row written). Draft the behavior first
   with the bdd-authoring skill, then render it into this shape without
   changing meaning. Cite the code path per scenario (`file:line`).
3. **Translate, don't transcribe**: Asterisk channel IDs, bridge juggling,
   and dialplan mechanics stay below the ACL — the spec speaks
   `Call`/`Participant` only.
4. **Reconcile, don't overwrite.** Where extracted behaviour contradicts an
   existing living spec (`openspec/specs/`) or PRD AC, stop and flag both
   wordings with their sources. A human resolves intent-vs-truth conflicts;
   the skill never silently rewrites either side.
5. **Record the seam debts**: every `tenant_id` threading, usage-record
   emission, and outbox publish found in code but missing from specs goes on
   the list (D-24 seams are cheap now, expensive to retrofit).

## Constraints

- D-29 direction holds: BRD/PRD/DECISIONS/TRD/HLD/LLD are read as context,
  never rewritten as specs. Extraction targets `openspec/specs/` (or a
  change's delta specs), never `docs/`.
- No invented behaviour: a scenario without a code path is a proposal, not
  an extraction — route it to openspec-propose instead.
- D-31 boundary: never extract or prescribe event sourcing, CQRS as an
  architecture, per-entity repositories, factories, or value-objects-for-
  everything. Scaled-down DDD keeps aggregates with invariants, domain
  events, ports and adapters, and the one ACL — anything beyond that is
  flagged, not built.
- D-39 applies to examples: no secrets, tokens, audio, or PII in extracted
  fixtures.
