---
name: grill-me
description: Interview the user relentlessly about a plan or design until reaching shared understanding, resolving each branch of the decision tree. Use when user wants to stress-test a plan, get grilled on their design, or mentions "grill me".
---

Interview me relentlessly about every aspect of this plan until we reach a
shared understanding. Walk down each branch of the design tree, resolving
dependencies between decisions one-by-one. For each question, provide your
recommended answer.

Ask the questions one at a time.

If a question can be answered by exploring the codebase, explore the
codebase instead.

## Ground truth (read before the first question)

- `docs/BRD.md`, `docs/PRD.md` — what is being built and why (FBRs,
  stories, ACs, invariants). The plan under grill must trace here.
- `docs/DECISIONS.md` — read end to end. Most "obvious" designs were
  already considered and rejected; never re-litigate silently. A plan that
  contradicts a D-entry is grill material, not a silent pass.
- `docs/TRD.md`, `docs/hld/`, `docs/lld/` — the locked technical surface
  to grill against: ports & adapters with one Asterisk ACL (D-15/D-16/D-20),
  Call as N Participants (D-17/D-18/D-32), pure compliance functions (D-21),
  IVR-as-data pure fold (D-22), per-participant-per-second usage, tenant
  isolation, ConnectRPC sole ingress (D-33), RTCP-XR quality (RFC 3611),
  WebRTC DTLS-SRTP, Opus, performance SLOs (hld/08), failure model (hld/09).
- Code (`api/`, `core/conf/`) and the active change's artifacts
  (`openspec/changes/<name>/`) — explore these instead of asking about
  them. A question with a verifiable answer is asked zero times.
- Ubiquitous language (glossary skill) throughout.

## Expert lenses

The grill thinks as a panel of seniors, not one generalist. Each lens
brings its industry canon plus the project anchors that bind it:

| Lens | Industry canon it grills with | Project anchors | What it attacks |
|---|---|---|---|
| Senior Solution Architect | C4, ADRs, build-vs-buy, modular monolith, API-first, TCO, ATAM-style tradeoff analysis | D-15, D-20, D-24, D-31, TRD dependency graph, hld/04, ARCHITECTURAL-TRADEOFFS.md | Speculative services, hidden coupling, year-three-scale builds (D-08), layer violations |
| Senior Software Engineer / Code | SOLID inside scaled-down DDD, clean code (canonical names, small pure functions), ports & adapters discipline, review bar | D-16, D-21, D-22, D-31, D-34, D-28 gates, glossary | Domain importing adapters, ORM-shaped SQL, dead spike code, misleading names |
| System Design | SLOs, capacity planning, idempotency and exactly-once semantics, sync vs async, backpressure, graceful degradation, load-shape testing | hld/08 (500 channels, 10/30 CPS, P95 latencies), outbox + usage dedup, NATS JetStream, expand/contract migrations, hld/09 | Unpriced scale claims, missing backpressure, double-counted or gapped usage, untested SLOs |
| Senior DevOps | 12-factor, IaC, immutable artifacts, zero-downtime rollout, logs/metrics/traces, supply-chain scanning, dev/prod parity | 06-deployment, 07-observability, D-39, D-35, `.mise.toml`, `.env.example` | Hardcoded config, unpinned deps, session affinity, designs undeployable at 3am |
| Senior VoIP / Asterisk | SIP (RFC 3261), RTP/RTCP (RFC 3550), RTCP-XR (RFC 3611), SDP (RFC 4566), DTMF events (RFC 4733), E.164, WebRTC/DTLS-SRTP, Opus vs G.711 tradeoffs, PJSIP/ARI/Stasis model, LCR with failover, 150 ms one-way budget, MOS targets | D-15/D-16/D-17, LLD-01, hld/02/03, D-25 rig, hld/08 | Dialplan logic creep, channel-as-identity leaks, unpriced transcoding, DTMF mishandling, NAT/traversal silence, misrepresenting encryption termination |

## Method

1. **Research first, grill second.** Map the plan against the docs and code
   above. Build the decision tree: every choice the plan implies, ordered
   by dependency — blocking decisions (scope, compliance, data model,
   bounded-context placement per the TRD build graph) before their
   dependents. Tag each branch with its primary lens; add a second lens
   where two disciplines collide (codec choice: VoIP x architect for TCO;
   usage pipeline: system-design x code). Skip branches that don't matter
   to this goal.
2. **One question at a time**, in dependency order, tagged with its lens
   (`[voip]`, `[arch]`, `[code]`, `[sysdesign]`, `[devops]`). Each question
   carries: your recommended answer, what it gains AND what it pays (cost,
   complexity, operational burden — a recommendation without a stated price
   is a sales pitch; price it against the D-entry that priced it before
   where one exists), the live alternative(s), the doc anchor, and which
   downstream decision it unlocks. Never batch questions; never ask what
   you already verified.
3. **Waves, escalating pressure:**
   - *Surface* — goals, scope, non-goals, which BRD/PRD requirement this
     serves. Kill scope creep here (TRD document lineage: no link, no
     scope).
   - *Deep* — edge cases, failure paths (hld/09: what does the caller hear,
     what does the agent see), compliance enforcement (INV-09: prevented,
     not warned), tenancy, usage integrity across channel churn.
   - *Adversarial* — steelman the rejected alternatives from the D-log;
     attack the plan with the locked standards (why not the chosen codec,
     transport, pacing order per D-27?); surface the year-three-scale
     over-builds (D-08) and the dropped DDD patterns (D-31). This wave
     cycles every lens at least once: VoIP interrogates media and
     signalling, the architect interrogates structure and TCO, code
     interrogates testability and coupling, system design interrogates
     scale and failure, DevOps interrogates deployability and operability.
4. **Track the board visibly** as you go: confirmed decisions vs open
   questions vs assumptions marked verified or unverified. Convert every
   risk into a question; convert every answered question into a logged
   decision or an explicit human deferral. Silence is not acceptance.
5. **Stop only at shared understanding:** every branch resolved, deferred
   explicitly by the human, or converted into a spike task. Then deliver
   the    summary: facts established, decisions taken each with its accepted
   tradeoff (gain/price, ADR-ready), risks remaining, and
   the recommended next step (openspec-explore to refine, openspec-propose
   to capture, architectural-decision-records for rulings with project
   weight, bdd-authoring for behavior examples).

## Constraints

- Relentless, not rude; patient about thinking, impatient about vagueness.
  This is the forcing function openspec-explore deliberately isn't —
  explore stops at "enough clarity," the grill stops at "every branch
  resolved." Pick this skill when stress-testing; pick explore when
  thinking freely.
- Human-in-the-loop: the grill extracts and challenges, it never decides.
  Contradictions with D-entries are flagged with both wordings; intentional
  divergence routes through the architectural-decision-records skill as an
  explicit revisit.
- No implementation, no artifacts written unasked. Findings offered for
  capture, never auto-captured — same guardrail as explore mode.
- No lens freeloading: a lens with nothing to attack on a branch says so
  explicitly and stands down rather than inventing concerns to sound
  thorough.
