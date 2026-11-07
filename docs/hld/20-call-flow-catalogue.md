<!-- OpenSpec: TRD-HLD-20 -->
# Call Flow Catalogue

**Purpose.** This is one inventory of every call and user flow a modern
PBX and contact-centre platform is expected to have. It is checked
against the BRD and PRD and against industry practice (3CX, VitalPBX,
FreePBX, FusionPBX, ViciDial-class diallers, and the FCC abandonment
rules). For each flow it records the audience, the release, where it is
designed, and whether it is built. It answers "which flows do we have,
and are they designed?" in one place. **A flow marked *Missing* has no
design anywhere and must get one (LLD section plus OpenSpec scenarios)
before it is proposed.**

**Reviewed:** 2026-11-07 (architecture review, D-91 to D-94); realtime sequences added in [21-media-and-call-control-flows.md](21-media-and-call-control-flows.md) (D-95, D-96).

**Status key:**
- **Built:** implemented and proven on the tiers in PROGRESS.
- **Partial:** a slice is built; the rest is designed.
- **Designed:** an LLD or HLD section exists; not built.
- **Mentioned:** named in the PRD or BRD only; no flow design.
- **Missing:** an industry-standard flow with no mention at all.

**Release:** R1.0 / R1.1 / R2 / R3 per BRD v1.1 §7 and PRD v1.2. Power
dialling and its abandonment rules are **R1.0** (D-45, D-97); predictive is
**R2** (D-27); live supervision is **R1.0** (D-97).

---

## 1. Administration and onboarding (partner engineer, tenant administrator)

| ID | Flow | Release | Design | Status |
|---|---|---|---|---|
| AD-01 | First run: install, first login, licence | R1.0 | HLD-18 A1 | Built |
| AD-02 | Add a user, give a role, invite | R1.0 | HLD-18 A2, LLD-02 | Built |
| AD-03 | Add an extension a phone registers to | R1.0 | HLD-18 A3 | Built |
| AD-04 | Connect a trunk, routes, DIDs, emergency profile | R1.0 | HLD-18 A4, LLD-03 §7 | Built |
| AD-05 | Assign an extension to a person | R1.0 | LLD-03 §3.1, D-87 | Built |
| AD-06 | Build/publish/rollback an IVR flow | R1.0 | LLD-03 §3.2–3.2.1, D-89–D-91 | Partial (single level; D-91 refactor) |
| AD-07 | Desk-phone auto-provisioning (zero-touch, per-vendor templates) | R1.0 | PRD EPIC-03 | **Mentioned** |
| AD-08 | Bulk import / migration from an incumbent; export | R1.1 | PRD EPIC-13 | Mentioned |
| AD-09 | Upgrade, rollback, diagnostic bundle | R1.0 | PRD EPIC-09, HLD-06 | Designed |
| AD-10 | Enable a module for one tenant (entitlement) | R1.0 | LLD-11 | Designed |
| AD-11 | MFA enrolment; company SSO; SCIM leaver | R1.0 (GA) for MFA/SSO; SCIM later (D-66) | LLD-02 rev. 4 | Designed |

## 2. Business telephony (employee, receptionist)

| ID | Flow | Release | Design | Status |
|---|---|---|---|---|
| PB-01 | Internal call extension → extension | R1.0 | HLD-15, HLD-16 | Built |
| PB-02 | Outbound call over a trunk: class, fraud guard, route by priority/cost, failover on unhealthy carrier | R1.0 | LLD-03 §7, D-80, D-82, D-85 | Built (cost-ordered LCR across carriers to verify against FBR-R1-03) |
| PB-03 | Inbound DID → extension | R1.0 | LLD-03 §7.7 | Built |
| PB-04 | Inbound DID → schedule → IVR menu → destination | R1.0 | LLD-03 §3.2.1 | Partial (D-91) |
| PB-05 | Emergency call bypasses everything (BR-09) | R1.0 (R1.1 dynamic location) | PRD §12.3, EPIC-12 | Built (static); dynamic location Designed |
| PB-06 | Click-to-call (caller rings first) | R1.0 | D-78 | Built |
| PB-07 | Browser softphone: register, call, answer, mute | R1.0 | D-81, D-84; target HLD-21 §9 | Built (no DTMF keypad, no ringtone: PROGRESS R7; per-session credential is the target) |
| PB-08 | Hold / music on hold / resume | R1.0 | HLD-21 §1.1 | Designed; `HoldParticipant` stub |
| PB-09 | Blind transfer | R1.0 | HLD-21 §1.2 | Designed; `TransferParticipant` stub |
| PB-10 | Attended (consultative) transfer | R1.0 | HLD-21 §1.3 | Designed |
| PB-11 | Call park and retrieve (park orbit) | R1.0 | HLD-21 §1.5, LLD-03 §3.3.1 | Designed |
| PB-12 | Call pickup (directed / group) | R1.0 | HLD-21 §1.7, LLD-03 §3.3.1 | Designed |
| PB-13 | Ad-hoc three-way and conference rooms | R1.0 | HLD-21 §1.6 | Designed |
| PB-14 | Ring groups (simultaneous, hunt, memory-hunt) | R1.0 | LLD-03 §3.3.1 (destination kind) | Mentioned (names only in LLD-03 §3.3.1) |
| PB-15 | Forwarding: unconditional / busy / no answer / unavailable | R1.0 | LLD-03 §3.3.1 (per-extension rule) | Mentioned (names only) |
| PB-16 | Follow-me / find-me (desk → browser → mobile) | R1.0 | LLD-03 §3.3.1 | Mentioned (names only) |
| PB-17 | Do not disturb | R1.0 | LLD-03 §3.3.1 | Mentioned (names only) |
| PB-18 | Presence and BLF (busy-lamp field) | R1.0 | PRD EPIC-03 | Mentioned |
| PB-19 | Voicemail: deposit, retrieve, MWI, voicemail-to-email | R1.0 | LLD-03 §3.4 | Designed |
| PB-20 | Hot-desking (log in to any phone) | R1.0 | LLD-03 §3.3 | Designed |
| PB-21 | Paging / intercom | Undecided | — | **Missing**; scope not decided (owner) |
| PB-22 | Mobile app with push (wake on call) | R1.1 | PRD EPIC-11 | Mentioned |
| PB-23 | Fax to PDF/email (T.38) | R1.1 | PRD EPIC-13 | Mentioned |
| PB-24 | Caller ID presentation (owned numbers only, BR-06) | R1.0 | PROGRESS gap | **Missing** (no presentation number yet) |

## 3. Inbound contact centre (agent, supervisor)

| ID | Flow | Release | Design | Status |
|---|---|---|---|---|
| IC-01 | Queue entry from IVR/DID; position/wait announcements; MOH | R1.0 | LLD-03 §3.3, HLD-21 §3 | Designed |
| IC-02 | ACD distribution (ring-all, longest-idle, round-robin, skills/penalty) | R1.0 | LLD-03 §3.3 | Designed (skills-based routing: R2) |
| IC-03 | Agent state: log in/out, ready, not ready (reason codes), wrap-up | R1.0 | LLD-03 §3.1/3.3 | Designed |
| IC-04 | Queue overflow / max wait / no agents → destination | R1.0 | LLD-03 §3.3 | Designed |
| IC-05 | Queue callback (keep place, call back) | R1.0 (AC-03.17) | HLD-21 §3 | Designed |
| IC-06 | Agent screen-pop / CTI with caller context | R2 | HLD-21 §8 | Designed (connectors R2) |
| IC-07 | Disposition and after-call work (manual; AI optional) | R2 (EPIC-18); R1.0 manual disposition has no PRD story | LLD-06 §6, EPIC-18 | **Missing** (R1.0 story needed if wanted) |
| IC-08 | Supervisor listen / whisper / barge | **R1.0** (D-97) | HLD-21 §2, PRD EPIC-16 | Designed |
| IC-09 | Wallboards and real-time queue statistics | R2 | PRD EPIC-16, HLD-12 | Mentioned |
| IC-10 | Blended agent (inbound and outbound on one login) | R2 | HLD-21 §3–4 | Designed |

## 4. Outbound dialling and compliance (campaign manager, agent, compliance officer)

| ID | Flow | Release | Design | Status |
|---|---|---|---|---|
| OB-01 | List import, segmentation, campaign create/start/pause/stop (10 s) | R1.0 | LLD-06 §3–5 | Designed |
| OB-02 | Preview dialling (agent sees the contact, then dials) | R1.0 | LLD-06 §5 | Designed |
| OB-03 | Power/progressive dialling (ratio × available agents) | **R1.0** | LLD-06 §5, D-27, D-45 | Designed |
| OB-04 | Predictive dialling (adaptive pacing on answer, talk and wrap estimates) | R2 | LLD-06 §5, HLD-21 §4 | Designed (pacing algorithm still to specify) |
| OB-05 | Compliance gate before every attempt, including manual dials: DNC, contact-local calling hours, opt-out | R1.0 | LLD-04, D-21 | Designed |
| OB-06 | Abandonment control (definition, measuring window, automatic slow-down, stop) | **R1.0** for power (AC-15.6) | LLD-04 §7, HLD-21 §4 | Designed (profile parameters) |
| OB-07 | Abandoned-call message within 2 s of the greeting; minimum ring time before giving up | **R1.0** (AC-15.6, D-97) | LLD-04 §7, HLD-21 §4 | Designed |
| OB-08 | Answering-machine detection and voicemail drop | R2 (drop R1.0 only after an explicit decision) | LLD-06 §5–6, HLD-21 §4 | Designed (no-AI baseline + optional AI) |
| OB-09 | Dispositions, retry rules, scheduled callbacks in the contact's zone | R1.0 | LLD-06 §6 | Designed |
| OB-10 | Per-attempt evidence, including blocked attempts | R1.0 | LLD-06 §6, AC-14.5 | Designed |
| OB-11 | Carrier failure during a campaign | R1.0 | PRD §12.2 | Designed (trace) |
| OB-12 | Outbound caller ID per campaign/number pool (BR-06) | R1.0 | — | **Missing** |

## 5. Recording, records and compliance (compliance officer, finance, support)

| ID | Flow | Release | Design | Status |
|---|---|---|---|---|
| RC-01 | Recording with a consent announcement before recording (BR-03) | R1.0 | LLD-09, HLD-21 §7 | Designed |
| RC-02 | Recording access with reason, audited (BR-04) | R1.0 | LLD-09 | Designed |
| RC-03 | Retention and permanent deletion (BR-11) | R1.0 | LLD-09 | Designed |
| RC-04 | Call detail record: read, scope by role, audited export | R1.0 | D-88 | Built (read model) |
| RC-05 | Immutable, event-fed CDR ledger with corrections as adjustments (BR-07) | R1.0 | D-92 | Designed (refactor flagged) |
| RC-06 | Per-participant quality score (RTCP-XR) | R1.0 | HLD-07, LLD-09 | Designed |
| RC-07 | Pause/resume recording for card payments (PCI) | R1.0 | HLD-21 §7 | Designed |

## 6. Partner integration (partner developer)

| ID | Flow | Release | Design | Status |
|---|---|---|---|---|
| IN-01 | Every console action through the public API | R1.0 | AC-05.1, HLD-12 | Built for shipped features |
| IN-02 | Signed, retried, replayable webhooks | R1.0 | LLD-05 | Designed |
| IN-03 | Event stream of call states and telemetry | R1.0 | HLD-04, EPIC-05 | Partial (outbox; no public stream) |
| IN-04 | CRM/ERP connector and embeddable call widget | R2 | EPIC-17, HLD-21 §8 | Designed (outline) |
| IN-05 | Omnichannel (chat, messaging, email) in one agent inbox | Beyond R3 | PRD §5 | **Out of scope** (owner decision to change; D-95) |

## 7. AI (optional, per tenant, provider-neutral: VP-5, D-23)

| ID | Flow | Release | Design | Status |
|---|---|---|---|---|
| AI-01 | Tap call audio (copy) to the tenant's provider; switch provider by configuration | R1.0 gate | LLD-10 §4–4.1, HLD-21 §6, D-23 | Designed; **no spike yet** |
| AI-02 | Generate an IVR flow from a description (writes the D-91 graph) | R1.0 | FBR-R1-06, LLD-10 | **Missing** from LLD-10 (FBR-R1-06 R1.0) |
| AI-03 | Transcription and summary after the call | R2 (LLD-10 §6 puts it out of R1.0) | LLD-10, EPIC-18 | Designed only as out of scope; R1.0 claim withdrawn |
| AI-04 | Live agent assist | R2 | EPIC-18 | Mentioned |
| AI-05 | AI voice agent / virtual receptionist (never in the emergency path) | R2 add-on (D-97) | HLD-21 §6, D-96 | Designed (outline) |
| AI-06 | AI failure mid-call degrades the feature, never the call | R1.0 | PRD §12.5, §13.1 | Designed (trace) |

## 8. Specialist dispatch (R3)

| ID | Flow | Release | Design | Status |
|---|---|---|---|---|
| SD-01 | Caller PIN/department identification and budget check | R3 | EPIC-20 | Mentioned |
| SD-02 | Broadcast dispatch: hold the caller, ring qualified specialists together, first to accept is bridged; exactly one acceptance (BR-13) | R3 | EPIC-19 | Mentioned (no LLD yet) |
| SD-03 | Tier escalation and coordinator fallback | R3 | EPIC-19 | Mentioned |
| SD-04 | Per-second margin ledger | R3 | EPIC-21 | Mentioned |

## 9. Failure and capacity flows

| ID | Flow | Design | Status |
|---|---|---|---|
| FL-01 | Capacity exhausted before setup (503, never 486); in-progress calls never dropped | PRD §12.4, HLD-04 | Built (no `Retry-After`: Asterisk limitation) |
| FL-02 | Licence expiry, grace, free-tier floor | LLD-08 | Built |
| FL-03 | Carrier unhealthy → removed from selection, operator alerted | D-80 | Built |
| FL-04 | Media node loss; engine restart; Stasis takeover | HLD-09, D-73 | Partial |
| FL-05 | Webhook endpoint down | PRD §13.3, LLD-05 | Designed |

---

## 10. Design backlog (revised after HLD-21)

**Now designed in HLD-21 (sequence level):** hold, blind and attended
transfer, park, pickup, conference, supervisor monitoring, the ACD with
callback and blended agents, power/predictive with the abandonment
rules, recording with pause, the AI tap and the voice agent, and
screen-pop. Each still needs its LLD refinement into ports and states
before an OpenSpec change.

**Still to design:**
1. **Call-control spikes** (HLD-21 §10), then a `telephony-core` LLD
   revision, then `telephony-call-control`. This is R1.0 and the largest
   gap against 3CX and VitalPBX.
2. **Ring groups, forwarding, follow-me and DND** in detail (LLD-03
   §3.3.1 outline), as D-91 destination kinds and extension rules.
3. **Caller ID presentation** (PB-24, OB-12): a number inventory and a
   presentation rule (BR-06).
4. **Inbound after-call work and disposition** (IC-07).
5. **The predictive pacing algorithm** (OB-04): a pure function and its
   test model.
6. **Desk-phone provisioning** (AD-07) and **paging** (PB-21: decide
   whether it is in scope).
7. **The AI audio-tap spike** (AI-01) before the R1.0 gate.

Each becomes an LLD section first, then OpenSpec scenarios, and the
`product-owner` agent accepts it against the PRD (D-94).

## 11. Coding readiness (D-98 audit, 2026-11-07)

Four read-only reviewers scored every flow against the twelve-item Flow
Definition of Ready ([lld/README.md](../lld/README.md)). **No unbuilt
R1.0 flow is ready for a coding agent.** "Designed" in the tables above
means a sequence exists, not that the LLD is complete.

| Area | Ready | Nearly (≥ 9/12) | Needs detail | No design |
|---|---|---|---|---|
| Built flows (AD-01–05, PB-01–03, PB-05–06, RC-04, FL-01–03) | Built; their LLDs lack DoR-form tests and parity only | — | — | — |
| Call control and PBX features (PB-04, 07–20, 24, AD-06) | 0 | 0 | PB-04, 07–11, 19, 20, AD-06 | PB-12–18, 24 |
| Contact centre and supervision (IC-01–05, 08, 10) | 0 | 0 | IC-01–05, 08, 10 | IC-07 |
| Outbound and compliance (OB-01–03, 05–07, 09–12) | 0 | OB-05 | OB-01–03, 06, 07, 09, 10 | OB-11, OB-12 |
| Admin, recording, records, integration, AI, failure | 0 | AD-10, AD-11 (MFA/SSO), IN-02, FL-05 | AD-09, RC-01–07, IN-03, AI-01, AI-06, FL-04 | AD-07, AI-02 |

**Contradictions found and where they are fixed:**
- **Fixed here:** HLD-16 (barge R1.0); this catalogue (PB-14–17, PB-21,
  IC-07, AI-02, AI-03, AD-11 statuses).
- **Fixed by the LLD rewrites (§12):**
  - abandonment and AMD release labels in LLD-04/06;
  - the agent state set (LLD-03 vs HLD-21: `Reserved`) and the queue
    strategies;
  - Participant state names (HLD-03/16/21);
  - preview sequence vs the nailed-up leg;
  - power pacing vs LLD-06 §10.1;
  - the webhook retry schedule (HLD-09 vs LLD-05);
  - the AI frame format (LLD-10 §9);
  - the CDR source (LLD-09 vs D-92);
  - the `reason` missing from `recording_access`;
  - the engine-restart claim (HLD-09 vs FL-04).

## 12. Plan to reach ready

Each LLD is rewritten to the twelve items, with the reviewers' missing
details and industry gaps as its checklist. One writer owns each file, so
there is no concurrent editing:
- `telephony-core` call control: LLD-01 §11, HLD-03/16/21 states;
- `pbx-core` routing features: LLD-03 (ring groups, forwarding,
  follow-me, DND, pickup, park, conference, caller ID, feature codes,
  presence/BLF, hot-desk, voicemail);
- `pbx-core` contact centre: LLD-03 §3.1/§3.3 and supervision, after the
  routing writer;
- outbound and compliance: LLD-04, LLD-06;
- records, recording, AI and webhooks: LLD-05, LLD-09, LLD-10, HLD-09;
- provisioning and lifecycle: a new LLD-12.

A flow moves to **Ready** in this catalogue only after the
`code-reviewer` (all twelve items) and the `product-owner` (items 1, 11
and 12) have checked its section.

## Traceability

BRD §7, §8 (FBR-R1-01..17, R2-01..07, R3-01..09), BR-03..BR-13; PRD §3,
§12, §13, EPIC-03, 07, 11–20; HLD-15, HLD-16, HLD-18; LLD-03, 04, 06, 09,
10; DECISIONS D-21, D-23, D-27, D-45, D-78–D-96; HLD-21.
