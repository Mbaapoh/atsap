<!-- OpenSpec: TRD-HLD-21 -->
# Media and Call-Control Flows (ARI-level design)

**Purpose.** This is how AtsaPBX executes the industry-standard
real-time flows over Asterisk ARI inside the modular monolith:
- hold and music on hold;
- blind, attended and phone-initiated transfer;
- park and pickup;
- conference;
- supervisor monitoring;
- the ACD queue;
- power and predictive outbound with AMD and abandonment control;
- the AI audio tap and the AI voice agent;
- recording;
- integration and screen-pop;
- WebRTC registration.

Each flow names the context that owns it, where state lives, its
failure rules and the invariants it must keep. The flow catalogue
([20-call-flow-catalogue.md](20-call-flow-catalogue.md)) lists *which*
flows exist; this document says *how* the realtime ones run. LLD sections
refine them before an OpenSpec change is proposed.

**Source:** an owner-supplied research note (2026-11-07) describing ten
flows in the style of VICIdial, 3CX, VitalPBX, Amazon Connect and Cisco,
assessed against BRD, PRD and DECISIONS (D-95).

**Status:** design. Nothing in this document is built unless the
catalogue says so.

---

## 0. Assessment of the research note (D-95)

| Research proposal | Verdict | Why / what we do instead |
|---|---|---|
| Telephony context holds no business logic, emits domain events; a separate ACD context; integration behind an ACL with plugin ports | **Adopt** | Already D-20, D-24 and D-31. Events go out through the transactional outbox to NATS JetStream; Asterisk itself never publishes to NATS. |
| Control plane as Go **microservices** | **Reject** | D-20: a modular monolith with ports. Contexts can be extracted later if justified. |
| **Redis** for IVR state, parking slots and ACD sorted sets | **Reject for R1.0** | D-56: PostgreSQL and NATS are the only stateful dependencies. Durable state goes in Postgres; hot state is in process, with Postgres advisory locks for single ownership (§3). |
| **OpenSIPS/edge proxy** in front of the engine pool | **Defer** | D-77 rejected an SBC "now". Asterisk with realtime PJSIP serves R1.0, and an SBC is the scale-out path, kept behind the same attribution rule. |
| ARI `externalMedia` / AudioSocket at 16 kHz to AI | **Adapt** | Use `externalMedia` (AudioSocket or RTP encapsulation, slin16) fed from a **snoop copy** for listening AI (D-23). Only the **AI voice agent** takes the live leg, as an explicit call destination with a fallback (D-96). |
| AI-based AMD in the outbound path | **Adapt** | AMD must work **without AI** (BRD: AI optional; FBR-R2-02 is not an AI feature). The baseline is the engine's talk detection plus heuristics. An AI classifier is an optional plugin that has a decision deadline and **defaults to "human"** on timeout or error. |
| Blind transfer by ARI `channels/{id}/redirect` | **Adapt** | `redirect` sends a channel *out* of Asterisk (SIP 302/REFER). For a transfer inside the platform, originate the target and swap bridge membership (§1.2). |
| Attended transfer: holding bridge, consult bridge, merge | **Adopt** | This is §1.3, with Participant continuity (D-17/D-32): the caller keeps one Participant across bridges. |
| Snoop for silent monitor, whisper and barge | **Adopt** | §2, plus PRD AC-16.1 and AC-16.2: an audit entry and a per-tenant legal switch. |
| Parking in a shared holding bridge with slots in Redis | **Adapt** | A holding bridge per parked call. Slots are per **tenant** and lot in Postgres, unique `(tenant, lot, slot)`. The research omits tenancy and the parked-call timeout; we add both (§1.5). |
| Skill-based ACD, wrap-up, position/EWT announcements | **Adopt** | §3, with state in Postgres and one dispatcher per queue. |
| Predictive "hopper" (VICIdial) | **Adopt, with a rule** | Contacts may be pre-selected and pre-checked, but compliance is **re-evaluated at the moment of dial** (BR-10; time windows move). |
| Omnichannel (chat, WhatsApp, email) in one agent inbox | **Out of scope** | PRD §5: messaging channels are deferred beyond R3. The session model stays channel-neutral so this is not foreclosed. Needs an owner decision to change. |
| RTPEngine/"Wazuh vault" for compliance recording | **Adapt** | Record with ARI bridge/snoop recordings and store them encrypted in the tenant's object store (MinIO/S3, HLD-01). Wazuh is a SIEM, not storage. |
| Plugin CRM/AI integration and screen-pop | **Adopt (R2 connectors)** | §8: outbox → NATS → `integration` context. Webhooks (LLD-05) are R1.0; packaged CRM connectors are R2 (EPIC-17). |

---

## 1. Call control (`telephony-core`; PRD US-03.2, AC-03.2)

Every flow below keeps these rules:
- the **Participant** (not the channel) is the domain identity across
  bridges (D-17, D-32);
- usage seconds keep accruing per Participant (D-24);
- an engine failure mid-operation leaves every party in a known bridge
  or hung up, never orphaned (HLD-16 §5).

### 1.1 Hold and resume

```text
Bridge M [A ⇄ B] ── B presses Hold (API or phone re-INVITE) ──►
  remove A from M; add A to holding bridge H_A (type=holding); start MOH (tenant class)
Resume: remove A from H_A; add A back to M
```

- `HoldParticipant` sets the Participant sub-state to `OnHold` (HLD-16
  §4).
- A phone's own hold (sendonly re-INVITE) is reflected from engine
  events and does not move bridges.
- If the holder hangs up, the held party is not left in silence: the
  call's hold-recall rule rings the holder back or follows a fallback
  destination.

### 1.2 Blind (cold) transfer

```text
M [A ⇄ B]; B transfers A to destination D
  1. resolve D through the D-91 destination registry (extension, queue, flow, external…)
  2. originate C toward D; A hears ringback (MOH optional)
  3. remove B from M (B's Participant → Left, reason transferred)
  4. on C answer: add C to M → M [A ⇄ C]
  5. on C failure/timeout: follow D's no-answer rule (voicemail, recall B, fallback)
```

An external D passes outbound class and fraud screening (D-82, D-85)
exactly like a placed call.

### 1.3 Attended (consultative) transfer

```text
M [A ⇄ B] → hold A in H_A (§1.1)
B consults: originate C → bridge K [B ⇄ C]
  complete: move A from H_A into K, remove B → K [A ⇄ C]; destroy M
  cancel:   hang up C, move A back into M
  C declines/no answer: B returns to A (resume)
Variant "three-way then drop": add A to K before B leaves (§1.6)
```

### 1.4 Phone-initiated transfer (SIP REFER from a desk phone or softphone)

Desk phones transfer with their own button and send REFER, so the
platform must handle it, not only API-initiated transfers.
- **Target:** the engine hands the REFER to the Stasis application, and
  the platform runs §1.2 or §1.3, so the Participants stay consistent.
- **Spike first:** confirm that the pinned Asterisk 22 version exposes
  ARI-side transfer handling (an endpoint transfer-handling option and
  `ChannelTransfer`-style events).
- **Fallback:** let Asterisk perform the transfer natively and reconcile
  from `BridgeBlindTransfer` / `BridgeAttendedTransfer` events.

### 1.5 Park, retrieve, timeout

```text
B parks A: allocate slot S in tenant lot L (Postgres row, unique tenant+lot+slot)
  move A to holding bridge P_S with MOH; announce S to B (prompt or UI); B is free
Retrieve: any tenant user dials the lot's retrieve code + S (or BLF/console click)
  → answer retriever R; move A into new bridge [A ⇄ R]; free S
Timeout (default 60 s per lot): ring B ("comeback"); if no answer → lot fallback destination
```

Slots never cross tenants (INV-10). An engine restart drops parked
channels: the slot rows are swept by reconcile and the call is recorded
as `parked_lost`.

### 1.6 Conference

- **Ad hoc:** the third party is originated into the existing mixing
  bridge M.
- **Rooms:** a tenant-owned room (number, PIN, moderator rules) is a
  persistent bridge created on first entry and destroyed at the last
  leave. It is a D-91 destination kind `conference`.
- Recording consent (BR-03) is announced to every entrant when the room
  records.

### 1.7 Pickup

When an extension is ringing (Participant `Alerting`), a user in the same
pickup group dials the pickup code (directed: code + extension). The
platform answers the picker's channel, cancels the ringing leg (`reason
picked_up`) and bridges the caller with the picker. Group membership is a
`pbx-core` concept.

## 2. Supervisor monitoring (`telephony-core` with `pbx-core` agents; PRD EPIC-16)

```text
Agent call bridge M [Customer ⇄ Agent]
Silent listen: snoop on Agent channel spy=both whisper=none → bridge S [snoop ⇄ Supervisor]
Whisper:       snoop spy=both whisper=out (to the agent only)  → bridge S [snoop ⇄ Supervisor]
Barge:         add Supervisor channel to M (3-way)
```

- Every start and stop writes an audit entry (AC-16.2).
- A tenant or jurisdiction switch can disable monitoring.
- The recording must show that a silent listen was inaudible (AC-16.1).
- The supervisor's endpoint may be a browser phone.

## 3. ACD queue (`pbx-core`; LLD-03 §3.3; PRD US-03.9)

```text
Caller → (DID/IVR destination kind queue) → answer, holding bridge with MOH
  persist queue_entry(tenant, queue, call, priority, entered_at, skills)
Dispatcher (one active per queue: Postgres advisory lock; any instance may hold it)
  pick agent by strategy: ring-all | longest-idle | round-robin | skills+penalty (R2)
  reserve agent (state Ready → Reserved), originate agent leg (ring timeout)
  answer → bridge [Caller ⇄ Agent]; agent → Busy; on hang-up → WrapUp(timer) → Ready
  no answer → agent → NotReady(reason missed) per policy; try next
Periodic: position/EWT prompt on the caller's channel; max-wait/no-agent → overflow destination
Callback (IC-05): caller keeps position, hangs up; at head of queue with an agent reserved → originate caller
```

- **Agent states** are pure transitions (LLD-03 §3.1): `LoggedOut`,
  `Ready`, `Reserved`, `Busy`, `WrapUp`, `NotReady(reason)`.
- **Durable state** is in Postgres. A dispatcher restart rebuilds from
  the rows and from the engine's live channels (reconcile).
- **Blended agents (IC-10):** the outbound pacer (§4) reads the same
  agent states, so an agent is reserved by at most one dispatcher. The
  reservation is a single row update with a state precondition.

## 4. Power and predictive outbound (`dialer` with `compliance`; LLD-06, LLD-04; PRD EPIC-14/15)

```text
Agent logs in to a campaign → agent leg answered once and held in the agent's own bridge ("nailed-up")
Hopper: next N contacts pre-selected per campaign (Postgres), compliance pre-checked
Pacer loop (power: ratio × ready agents; predictive R2: adaptive estimate), per slot:
  1. compliance verdict NOW (DNC, contact-local window, opt-out, abandonment budget) — BR-10
  2. reserve licence capacity; choose presentation number (BR-06)
  3. originate customer via route; ring at least the profile's minimum (FCC: 15 s / 4 rings)
  4. answer → optional AMD on a copy (talk detection; AI plugin with deadline, default human)
       human + agent ready → add customer to the agent's bridge (sub-second)
       human + no agent within the profile's limit (FCC: 2 s after greeting)
             → play identification/opt-out message; record ABANDONED
       machine → voicemail drop (if campaign allows) or hang up; record MACHINE
  5. outcome → disposition rules → retry/callback scheduling (contact's zone)
Abandonment: rolling ratio per campaign per profile window (FCC: 30 days, 3 %);
  approaching → pacer slows within 60 s; breach → campaign stops (AC-15.3)
```

- The **nailed-up agent leg** is the industry technique that makes the
  2-second connect achievable. Without it, ringing the agent after the
  customer answers abandons the call.
- **Jurisdiction profile** (LLD-04): the minimum ring time, the connect
  limit, the abandonment threshold and window, the message requirement,
  and any no-redial window after an abandoned call are parameters, not
  constants.
- AMD time counts against the connect limit. The profile states whether
  AMD may run at all under it.
- Power dialling (R1.0) applies the same abandonment rules. Preview
  dialling cannot abandon.
- Pause/stop halts new attempts within 10 s; calls in progress continue
  (INV-03).

## 5. IVR execution

This is the D-91 graph interpreter (LLD-03 §3.2.1). It runs on ARI
playback and `ChannelDtmfReceived`, with retry counters in the IVR
session. The revision is captured at entry. No Redis is needed:
per-call state lives in the telephony session and survives nothing
beyond the call, which is correct for an IVR.

## 6. AI audio (`ai-pipeline`; LLD-10; D-23, D-96)

**Listening AI** (transcription, assist, quality):

```text
Participant connected → AITap (no-op if AI disabled for the tenant)
  snoop (spy=in|out per speaker) → externalMedia (slin16; AudioSocket or RTP encapsulation)
  → provider adapter (tenant's choice) → transcript segments → events
```

- The call never waits on the tap.
- A failure stops the tap, never the call.
- Each speaker is a separate stream (AC-04.2).

**AI voice agent** (BRD §12.4.2 add-on; D-96):

```text
destination kind ai_agent (D-91 registry) → answer → externalMedia bidirectional on the live leg
  provider loop (STT → LLM → TTS); tools via the public API only
  escalate → route to the configured destination with a context hand-off (transcript, intent)
  provider error / latency deadline / caller presses 0 → fallback destination at once
```

- It is never on the emergency path (BR-09).
- It counts as one AI channel for licensing (BRD §12.4.2).

## 7. Recording (LLD-09; BR-03, BR-04, BR-11; FBR-R1-09)

1. **Consent first:** the announcement plays and finishes *before*
   recording starts (BR-03).
2. **Recording:** `bridges/{id}/record` gives a mixed recording, or
   per-participant snoop recordings give separated channels (R3).
3. **Storage:** the file is moved from the engine to the tenant's object
   store, encrypted, with retention metadata.
4. **Access:** every access is audited with a reason (BR-04).
5. **Pause/resume** for card payments (RC-07) uses the live-recording
   pause, and the gap is recorded in the metadata.

## 8. Integration and screen-pop (`integration`; LLD-05; EPIC-05/17)

```text
telephony-core / pbx-core / dialer domain events → transactional outbox → NATS (tenant.<id>.event.call.*)
  ├─ webhook-delivery (R1.0): signed, retried, replayable (LLD-05)
  ├─ console event stream: agent UI receives "call offered/answered" + caller context
  └─ integration connectors (R2): per-tenant CRMProvider adapter (FetchContactByNumber,
     WriteActivity); result → screen-pop payload on the agent's stream; failures never touch the call
```

- `telephony-core` never imports a connector.
- A connector is per tenant, enabled by entitlement (LLD-11).
- Partners who build their own integration use the same events through
  webhooks or the API (FBR-R1-11).

## 9. WebRTC registration (`pbx-core`; D-81, D-84)

- **Today:** the administrator enters the extension's SIP password each
  session over WS/WSS, and STUN/TURN comes from the installation.
- **Target:** after console sign-in, `pbx-core` checks that the person
  owns the WEBRTC extension (D-87) and issues a **short-lived, per-session
  SIP credential**, projected for that endpoint with an expiry and revoked
  on sign-out or tenant switch. This removes password re-entry (PROGRESS
  R7) without an edge proxy.
- An SBC/edge proxy validating tokens is the scale-out path (D-77, D-95).

## 10. Open spikes (before the owning LLD is finalised)

1. Phone-initiated transfer handling in Stasis on the pinned Asterisk 22
   (§1.4).
2. `externalMedia` with AudioSocket at slin16: latency, and its
   behaviour when the consumer stalls (§6, AI-01).
3. Snoop whisper direction semantics verified by recording (AC-16.1).
4. A talk-detection AMD baseline: accuracy and decision time against the
   2-second connect limit (§4).

## Traceability

BRD FBR-R1-01, R1-07, R1-09, R2-01..R2-04, BR-03, BR-04, BR-06, BR-09,
BR-10; PRD EPIC-03, 04, 07, 14, 15, 16, 17; HLD-04, HLD-16, HLD-20;
LLD-03, 04, 06, 09, 10; DECISIONS D-17, D-20, D-23, D-24, D-32, D-56,
D-77, D-91, D-95, D-96.
