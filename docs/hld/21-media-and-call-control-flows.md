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

Every flow below keeps these rules (detail, states, DDL, RPCs and tests:
[LLD-01 §11–§24](../lld/LLD-01-telephony-core-walking-skeleton.md)):
- the **Participant** (not the channel) is the domain identity across
  bridges (D-17, D-32). States: `Invited`, `Ringing`, `Connected`,
  `OnHold`, `Parked`, `Transferring`, `Disconnected` (HLD-16 §4);
- the call's main bridge `M` is **never replaced**: parties are added to
  and removed from it, so a bridge recording is continuous;
- usage seconds keep accruing per Participant (D-24); `OnHold`, `Parked`
  and `Transferring` are billable;
- `pbx-core` decides (destinations as ring plans, recalls and fallbacks
  through `InboundRouter.Continue`, lots, groups, rooms); `telephony-core`
  executes (LLD-03 §3.5.1);
- an engine failure mid-operation leaves every party in a known bridge
  or hung up, never orphaned (LLD-01 §22).

### 1.1 Hold and resume

```text
Bridge M [A ⇄ B] ── B holds (API) ──►
  remove A from M; POST /channels/{A}/moh (tenant or object class)   — no extra bridge
Resume: DELETE /channels/{A}/moh; add A back to M
```

- A → `OnHold`; a `hold` operation row makes "already held" impossible.
- **Phone hold** (sendonly re-INVITE, ARI `ChannelHold`): the engine
  plays MOH to the held party too (decided; industry behaviour). The
  bridge is not moved; SPIKE-CC-2 confirms this, else the platform holds
  the peer as for an API hold.
- If the holder hangs up, the held party hears MOH while the platform
  rings the holder back and then follows their no-answer rule; it is
  never left on MOH with nobody behind it. A reminder tone reaches the
  holder every 120 s (default).

### 1.2 Blind (cold) transfer

```text
M [A ⇄ B]; B transfers A to destination D
  1. pbx-core resolves D (D-91) into a ring plan, program or feature
  2. A → Transferring (ringback); B leaves (Disconnected, transferred)
  3. plan legs originated toward C; first answer is added to M → M [A ⇄ C]
  4. no answer: the plan's terminal (target's rule, or recall B after 15 s), then the tenant fallback
```

An external D passes outbound class and fraud screening (D-82, D-85)
exactly like a placed call. `redirect` is not used (§0).

### 1.3 Attended (consultative) transfer

```text
M [A ⇄ B] → A OnHold (channel MOH, out of M)
B consults: B moves to consult bridge K; originate C → K [B ⇄ C]
  swap:     B moves between M and K; whoever B leaves hears MOH
  complete: C moves into M, A resumes, B leaves → M [A ⇄ C]; K destroyed
  cancel / C declines / C hangs up: C gone, B back to M with A
  B hangs up while consulting: completes (drop-completes, FreePBX/3CX parity)
Variant "three-way then drop": add C to M, then B hangs up
```

A keeps one Participant throughout; C is a new Participant on the same
call; "consulting" is an operation state, not a Participant state.

### 1.4 Phone-initiated transfer (SIP REFER from a desk phone or softphone)

Desk phones transfer with their own button and send REFER, so the
platform must handle it, not only API-initiated transfers.
- **Primary:** endpoints projected with `transfer_handling=ari-only`;
  Asterisk raises `ChannelTransfer`; the platform resolves `Refer-To`
  like a dialled number and runs §1.2 or §1.3, reporting
  `transfer_progress` back to the phone. An attended REFER with
  `Replaces` merges the phone's consult call into the original call.
- **Fallback** (SPIKE-CC-1 fails): Asterisk performs the transfer
  natively; the platform reconciles from `BridgeBlindTransfer` /
  `BridgeAttendedTransfer` and the channels' correlation variables.

### 1.5 Park, retrieve, timeout

```text
B parks A: pbx-core claims slot S in tenant lot L (Postgres, unique tenant+lot+slot)
  A → Parked: holding bridge P_S with MOH; slot announced to B (attended) or shown (console, lamp); B leaves
Retrieve: a tenant user dials S (or BLF/console) → A and retriever R in M; slot freed
Timeout (default 60 s per lot): Continue(park_timeout) → ring the parker 20 s → lot fallback
```

Slots never cross tenants (INV-10). Retrieve-versus-timeout races are
decided by one conditional update on the slot row. An engine restart
drops parked channels: the park is recorded as `parked_lost`.

### 1.6 Conference

- **Ad hoc:** the third party is originated into the existing mixing
  bridge M; Asterisk switches the mixing technology itself, so no bridge
  upgrade is needed (default cap: 5 parties).
- **Rooms:** a tenant-owned room (number, PIN, moderator rules) is one
  Call of kind `room` with N Participants, created on first entry and
  ended at the last leave. `pbx-core` checks the PIN; telephony joins,
  mutes, removes and locks. It is a D-91 destination kind `conference`.
- Recording consent (BR-03) is announced to every entrant when the room
  records. Each entrant is a call for licensing.

### 1.7 Pickup

When an extension is ringing (Participant `Ringing`), a user in the same
pickup group dials the pickup code (directed: code + extension).
`pbx-core` picks the oldest ringing call; telephony claims it atomically,
ends the rung leg (`left_reason=picked_up`, "answered elsewhere") and
bridges the caller with the picker, who is recorded as the answering
party (AC-03.13). Group membership is a `pbx-core` concept.

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

**Listening AI** (detail: LLD-10 AI-01):

```text
participant.joined event → ai-pipeline AITap (no-op if AI is disabled for the tenant; telephony never calls ai-pipeline)
  per speaker: snoop spy=in&whisper=none → private mixing bridge
    → externalMedia format=slin16&encapsulation=rtp&transport=udp → the app's loopback relay
  → provider adapter (tenant's choice; each adapter converts to its provider's format)
  R1.0 ends at the provider stream; transcripts and summaries are R2 (AI-03)
```

AudioSocket is used only if spike §10.2 proves it at slin16.

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
2. **Recording (R1.0):** a mixed recording from `bridges/{id}/record` on
   the call's main bridge, with a new segment on each bridge change.
   Separated per-participant snoop recordings are R3.
3. **Storage:** the file is fetched with `GET
   /recordings/stored/{name}/file`, its SHA-256 is checked, it is
   uploaded encrypted to the tenant's object store with retention
   metadata, and then `DELETE /recordings/stored/{name}` removes it from
   the engine (LLD-09 RC-01).
4. **Access:** every access is audited with a reason (BR-04).
5. **Pause for card payments** (RC-07) uses `POST
   /recordings/live/{name}/mute`, not pause: the recording keeps its
   timeline and writes silence. The AI-tap snoop is deleted in the same
   operation, and the interval is stored in the metadata (LLD-09 RC-07;
   spike R-S2).

## 8. Integration and screen-pop (`integration`; LLD-05; EPIC-05/17)

```text
telephony-core / pbx-core / dialer domain events → transactional outbox → NATS (tenant.<id>.event.call.*)
  ├─ webhook-delivery (R1.0): Standard Webhooks signatures; retries at +1 m, 5 m, 15 m, 1 h, 6 h, 24 h;
  │    Unhealthy after 3 failures, Disabled at 24 h; replayable (LLD-05)
  ├─ console/partner event stream: EventService.StreamEvents (ConnectRPC server stream, resume cursor; LLD-05 IN-03)
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
   (§1.4): `transfer_handling=ari-only`, `ChannelTransfer`,
   `transfer_progress` (LLD-01 SPIKE-CC-1).
2. `externalMedia` for the AI tap (§6, AI-01; LLD-10 SPIKE AI-S1):
   latency and behaviour when the consumer stalls; RTP `slin16` **byte
   order**, packetisation and payload type; the accepted **`direction`**
   values; whether AudioSocket carries slin16 and whether
   **`chan_websocket`** is present.
3. Snoop whisper direction semantics verified by recording (AC-16.1;
   LLD-01 SPIKE-CC-3).
4. A talk-detection AMD baseline: accuracy and decision time against the
   2-second connect limit (§4).
5. A phone's own hold in an ARI mixing bridge plays MOH to the held peer
   (two-party and three-party) (§1.1; LLD-01 SPIKE-CC-2).
6. BLF subscriptions to ARI-controlled `Stasis:` device states through a
   static pattern hint, without generated dialplan (LLD-03 SPIKE-P1).
7. **R-S1:** AMI `RTCPReceived`/`RTCPSent` for chan_pjsip (UDP and WebRTC
   rtcp-mux) with report blocks and RTT, and `CHANNEL(rtpqos,…)` at
   hangup (§7; LLD-09 RC-06).
8. **R-S2:** live-recording `mute` writes silence and keeps the timeline,
   and the muted state is observable after an app restart (§7 pause;
   LLD-09 RC-07).

## Traceability

BRD FBR-R1-01, R1-07, R1-09, R2-01..R2-04, BR-03, BR-04, BR-06, BR-09,
BR-10; PRD EPIC-03, 04, 07, 14, 15, 16, 17; HLD-04, HLD-16, HLD-20;
LLD-03, 04, 06, 09, 10; DECISIONS D-17, D-20, D-23, D-24, D-32, D-56,
D-77, D-91, D-95, D-96.
