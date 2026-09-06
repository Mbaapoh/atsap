---
name: glossary
description: Define and defend project terminology (ubiquitous language). Use when naming a concept, writing specs or docs, or when asked what something is called.
---

One language across BRD, PRD, TRD, HLD, specs, code, and tests (D-31,
ubiquitous language). A term is canonical only if it is used the same way in
all of them. This skill resolves naming disputes and proposes new terms —
it does not create new glossary files.

**Lens:** the language-governing architect — one meaning per term across
docs, code, and tests.

## Source-of-truth order (highest wins)

1. `docs/DECISIONS.md` — renames and term rulings (D-17/D-18, D-32).
2. `docs/TRD.md` terminology note + Domain model section.
3. `docs/hld/03-domain-model.md` — entity names and lifecycle terms.
4. Code identifiers (`api/...`) — must match 1–3; code that disagrees is the
   defect, not the docs.

## Canonical terms (non-exhaustive — extend by the same process)

| Term | Meaning | Never |
|---|---|---|
| Call | Aggregate of N Participants with its own lifecycle (PRD §11.1 states) | "call" as a phone connection / channel |
| Participant (`CallParticipant` where precision matters) | One party's participation in a Call; stable identity across channel churn (D-17, D-32) | `Leg` (banned, D-32); "caller"/"agent" as fixed parties (D-18) |
| Channel | Asterisk telephony resource backing a Participant; transient, replaceable | Treated as domain identity; leaked past the ACL |
| Bridge | Asterisk media mixer joining channels | Confused with a Call |
| Tenant | Isolation boundary: RLS, NATS subjects, JWT scope (D-24) | Optional qualifier; every row/object carries it |
| Campaign | Dialer work unit with list, pacing, compliance rules (R2) | Used for broadcast/PBX concepts |
| Screening | Pre-contact clearance gate (capacity, compliance, permissions) | Skipped except by emergency calls (INV-01) |

## Method for a new or disputed term

1. Quote the conflicting usages (file + line each).
2. Resolve by the source order above; prefer the more precise existing term
   over minting a new one.
3. A genuinely new term ships with: one-sentence definition, what it is not,
   and the first doc/code site that will use it. Term rulings with project
   weight go through the architectural-decision-records skill (new D-entry);
   small clarifications go to the owning doc section directly.
4. After ruling, grep the repo for the losing variant and convert every
   occurrence — a glossary ruling without a sweep is a wish.
5. Clean-code naming: identifiers use canonical terms verbatim
   (`CallParticipant`, never `Leg`, never `CallerAgent`). A name that
   misleads about the concept (a Channel-flavoured name on a Participant)
   is a defect even if it compiles.

## Constraints

- No `GLOSSARY.md` or sidecar term files: the terminology homes are TRD,
  `03-domain-model.md`, and the D-log. New files fragment the language.
- Industry terms (SIP, RTP, RTCP-XR, E.164, MOS) keep their RFC/ITU meanings;
  the glossary only records project-specific usage on top of them.
