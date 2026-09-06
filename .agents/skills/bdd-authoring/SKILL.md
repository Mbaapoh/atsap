---
name: bdd-authoring
description: Write behavioral design examples from BRD requirements and PRD stories. Use when a change needs concrete behavior examples, or when asked to specify behavior by example.
---

Specify behavior the way the BRD and PRD do: concrete examples in plain
domain language. Each example shows one behavior through situation, event,
and outcome, and traces to the requirement it demonstrates. No Gherkin, no
Cucumber keywords anywhere — not in the examples, not in the method. These
examples feed OpenSpec delta-spec scenarios and acceptance tests: the same
behavior in three notations, never three different behaviors.

**Lens:** the requirements owner describing behavior; the architect lens
checks every example traces to a requirement — no link, no example.

## Ground truth (read before writing)

- `docs/BRD.md` — functional requirements (`FBR-Rx-xx`): the capabilities
  that must exist.
- `docs/PRD.md` — user stories (`US-xx.x`), acceptance criteria (`AC-xx.x`),
  the Call lifecycle states (§11.1), and the invariants (e.g. INV-01
  emergency bypass, INV-09 enforced compliance). Stories and ACs are the only
  behavior source; never invent an example without one.
- `openspec/changes/<name>/specs/` — where examples land as delta-spec
  scenarios while a change is in progress.
- Ubiquitous language (glossary skill): `Call`, `Participant`, agent,
  supervisor, compliance officer, partner, campaign, tenant.

## Method

1. **One example per behavior**, titled after its requirement
   (`Example: AC-15.3 slow-down approaching the abandonment limit`).
   State shared setup once per group (`Common setup: ...`), never repeated
   per example.
2. **Three plain parts, always in this order, each a short paragraph:**
   - **Situation:** the state of the world before anything happens —
     tenant, campaign or call state, configuration in force. No action here.
   - **Event:** the single meaningful occurrence that triggers the behavior.
   - **Outcome:** what each affected party observes — what the caller hears,
     what the agent sees, what the supervisor sees, what the auditor can
     prove afterward, what the system records. Every claim observable;
     nothing about hidden rows unless the record itself is the contract
     (tamper-evident CDRs, audit log).
3. **Concrete values, short examples.** Named people, real thresholds,
   actual time bounds from the AC (e.g. "within 60 seconds"). An example
   that could describe any system describes none.
4. **Compliance examples show refusal** (PRD INV-09): the unlawful call is
   prevented, the over-limit campaign stops, no role overrides.
5. **Emergency examples show the bypass** (PRD INV-01): Screening states are
   absent from the example entirely, not merely passed through.
6. **Transfer examples show identity continuity** (D-17, PRD §11.1): channels
   change, the Participant and its billable duration continue unbroken.
7. **Variants are enumerated, not duplicated**: calling-hour windows,
   consent jurisdictions, and retention periods go in a compact list inside
   one example, never as copy-pasted near-identical examples.
8. Close each example with its mapping line: `Traces: AC-15.3, EPIC-15`.
9. **Hand off to delta specs in house format.** An example becomes one
   OpenSpec scenario without changing behavior: Situation feeds the
   scenario context, Event becomes the WHEN line, Outcome becomes the
   THEN lines, under `#### Scenario:` beneath a SHALL-voiced
   `### Requirement:` — the exact shape in
   `openspec/changes/telephony-core-originate-bridge-hangup/specs/telephony-core/call-lifecycle/spec.md`,
   which is the template. No Gherkin keywords enter at any stage; the
   WHEN/THEN bullets are this project's spec format, not Gherkin.

## Proper scenarios — worked examples from our PRD

### Example: AC-15.3 slow-down approaching the abandonment limit

- **Situation:** A predictive campaign is running with ten agents on a clean
  list. The abandonment rate over the trailing window stands just below the
  legal limit, and the compliance officer has configured automatic pacing.
- **Event:** Fresh abandonments push the trailing rate to the configured
  approach threshold.
- **Outcome:** Within 60 seconds the dialer reduces its pacing rate; agents
  notice longer waits between connected calls; the supervisor wallboard flags
  the campaign as throttled with the reason shown; if the rate still reaches
  the legal limit, the campaign stops placing new calls entirely while
  in-progress conversations finish normally.
- **Traces:** AC-15.3, EPIC-15 (R2 scope; implemented after the
  power-dialling loop proves itself — D-27).

### Example: INV-01 emergency call bypasses Screening

- **Situation:** A tenant with spend caps exhausted and several suspended
  routes attempts an emergency call from an agent extension.
- **Event:** The agent dials the emergency number.
- **Outcome:** The call proceeds immediately to routing with no capacity,
  spend, suspension, or compliance check applied; no Screening record is
  written; the call detail record marks the emergency bypass explicitly so
  the auditor sees why the gates were skipped.
- **Traces:** PRD §11.1 Screening state, INV-01.

### Example: US-10.1 partner self-service licensing with isolation

- **Situation:** A partner holds a valid tier entitlement and has never
  contacted us about this customer.
- **Event:** The partner issues a license to their own customer through the
  Partner Portal.
- **Outcome:** The license is active immediately with no action from us; the
  partner sees only their own customers' licenses afterward; a second partner
  attempting to register the same customer is refused with both parties told
  what happened.
- **Traces:** US-10.1, AC-10.1, AC-10.4, EPIC-10.

### Example: D-17 transfer keeps one Participant

- **Situation:** An agent is in an Active call with a customer; a supervisor
  decides to take over and initiates an attended transfer.
- **Event:** The supervisor joins, the agent leaves, and the underlying
  Asterisk channels are replaced mid-participation.
- **Outcome:** The customer experiences one continuous call with no
  re-answer; the agent's participation ends and the supervisor's begins while
  the Call itself never leaves Active; per-second usage records show one
  unbroken customer participation with no double-counted seconds and no gap
  across the handover.
- **Traces:** PRD §11.1 (participants join and leave without the call
  changing state), D-17.

## Constraints

- Domain language only; no Asterisk internals (channels, bridges, dialplan)
  except where the handover itself is the behavior, and then only as
  observed continuity.
- An example that contradicts a D-entry stops and is flagged with both
  wordings. If the contradiction is intentional, it routes through the
  architectural-decision-records skill as an explicit revisit — never
  silent drift (per `openspec/config.yaml`).
- Placement of example files is confirmed per change (default: inside the
  change's delta specs); the skill outputs text plus mapping regardless.
