---
name: dependency-guard
description: Check a feature/change for dependency creep against the bounded-context matrix and package rules. Use when proposing, applying, reviewing, or committing a change, or when asked to verify architectural boundaries.
---

Every feature is checked against the dependency contracts before it lands.
A violation blocks the change; only an explicit human approval downgrades
one — and that approval is recorded, never silent.

**Lens:** the architect guarding the module boundaries — every import
priced, every edge defended, no creep on credit.

## Ground truth (read before checking)

- `docs/hld/04-bounded-contexts.md` §10 — the authoritative build graph,
  the §10.1 allowed-dependency matrix, and the §10.2 LLD build sequence.
  A change needing a higher-tier context is out of order, not absorbable.
- `docs/hld/01-architecture.md` §1.2 (package rules) and §6 (the three
  verification gates the machines enforce).
- `docs/TRD.md` "Dependency graph" — document lineage and the condensed
  tier view; `docs/lld/README.md` — what is built in which order.
- `docs/TOOLSET.md` §6 (dependency addition protocol) + `api/go.mod`
  (pinned, permissive-licence only).
- The change under check: `openspec/changes/<name>/` (proposal, delta
  specs, design, tasks) and its diff (`git status`, `git diff`).
- Ubiquitous language (glossary skill): findings name `Call`,
  `Participant`/`CallParticipant`, never `Leg`.

## Method

1. **Locate the change on the graph.** Name its bounded context (change
   name → owning LLD → context; default assumption is wrong — verify).
   Confirm the context is the one currently being built per
   `docs/lld/README.md`. If the change imports, calls, or reads state
   from a higher-tier context, stop: flag out-of-order scope per the TRD
   lineage rule. Do not redesign around it; report it.
2. **Enumerate the diff surface.** From `git status`/`git diff` (or the
   change's tasks/specs at propose time) list: every new or changed Go
   import by package; every new table, column, and policy; every new NATS
   subject; every new external module.
3. **Check each item against the contracts:**
   - §10.1 matrix: no forbidden context edge (e.g. `telephony-core`
     importing `pbx-core`/`reporting`/`dialer`/`ai-pipeline`/
     `webhook-delivery`); no cross-context table reads; async exits only
     via versioned domain events on tenant-partitioned subjects.
   - §1.2 package rules: `domain` imports nothing; `application` only
     `domain` + `ports`; adapters implement ports; nothing outside
     `internal/telephony/acl` imports ARI/AMI packages or names a
     channel ID. Engine rule (D-41): no engine imports outside its own
     `acl/<engine>` package, and exactly one engine stays wired
     (Asterisk) until a D-logged revisit — a second engine import is a
     BLOCKED finding, not a cleanup note.
   - D-24 seams on every new row, object, and event: `tenant_id`
     present; usage emission where call state changes; new capabilities
     reachable through the same API the portal would use.
   - New external dependency: stdlib first; permissive licence only;
     pinned in `go.mod`; proposed in the change per TOOLSET §6 —
     otherwise blocked.
4. **Run the machines; quote them.** `go build ./...`, `golangci-lint`
   (depguard boundary), and the applicable test tags. A gate failure is a
   finding with the tool's own output as evidence — never paraphrased away.
5. **Verdict table.** One row per check: PASS, or BLOCKED with
   `file:line`, the violated rule, and the exact fix (move the code
   behind a port, invert the dependency, split the change, add the
   missing task). A BLOCKED finding stops the change from proceeding.

## Constraints

- Inside an apply, fix violations you introduced in place and re-run the
  gates. On review (propose/commit), report only — never refactor
  unasked.
- No silent exceptions. A genuine architectural reversal routes through
  the architectural-decision-records skill as an explicit revisit; a
  missing-behavior question routes to bdd-authoring, never into a quiet
  import.
- A check with no diff surface (docs-only change) still verifies §10
  order and the D-number citation check — scope can creep in prose too.
- Findings cite `file:line` and the exact rule (e.g. "HLD 04 §10.1,
  row telephony-core"). "Looks coupled" without a cited rule is not a
  finding.
