# Prior art used for the Phase B/C LLDs

**Reviewed 2026-11-04; IVR sources re-checked 2026-11-07 (D-91).** The purpose is to check that the missing LLDs
describe table-stakes behaviour and to identify where AtsaPBX deliberately
differs. Vendor documentation changes; these are links, not copied text.

## Sources

| Product | Documentation used | Relevant capabilities observed |
|---|---|---|
| **3CX** | [IVR / Digital Receptionist](https://www.3cx.com/docs/manual/ivr/), [product site](https://www.3cx.com/) | IVR menus up to 10 options; DID assignment; prompt upload/record (PCM 8 kHz, 16-bit mono WAV); office hours inherited from a department; holiday prompt; no-input and invalid-input destinations; direct-dial toggle; call queues/ring groups; call reporting |
| **VitalPBX** | [Wiki](https://wiki.vitalpbx.com/), [product site](https://vitalpbx.com/) | Queues, real-time reporting, recording, Sonata Stats/Recordings/Dialer/Billing, WebRTC softphone, AI/transcription add-ons |
| **Vodia** | [Feature list](https://web.vodia.com/features), [documentation root](https://doc.vodia.com/) | Auto attendant with custom greetings/language/dial-by-name; ring groups and queues with priority, queue limit, overflow and real-time ACD reporting; outbound call lists; CDR/trunk reporting; Google login; voicemail-to-email |
| **FusionPBX** | [Documentation](https://docs.fusionpbx.com/en/latest/) | IVR menus, call-centre queues (strategies, tiers, max wait/no-agent, MOH, timeout actions, record), time conditions, hot desking, voicemail-to-email, call recordings, CDR |
| **VitalPBX time routing** | [Time Groups and Time Conditions](https://wiki.vitalpbx.com/wiki/vitalpbx/time-groups-and-time-conditions/) | A time group holds several ranges (e.g. Mon–Fri 09:00–17:00 plus Sat 09:00–14:00); a time condition routes matched/unmatched; temporary and permanent manual overrides and a toggle code; time zone per condition |
| **3CX office hours** | [Configure Your Office Hours](https://www.3cx.com/docs/manual/office-hours/) | Office hours per department with its own time zone; IVR destinations for office hours, out of office, break and holiday; manual open/close override that expires at the next scheduled change |
| **FreePBX (Sangoma)** | [Time Conditions Module](https://sangomakb.atlassian.net/wiki/spaces/PG/pages/24248517) | Time conditions route to any module destination (ring group, queue, IVR, inbound route); override states |
| **Amazon Connect** | [CreateContactFlowVersion](https://docs.aws.amazon.com/connect/latest/APIReference/API_CreateContactFlowVersion.html) | A published flow version is immutable and version numbers increase monotonically; flows are block graphs |
| **Asterisk** | [ARI](https://docs.asterisk.org/Configuration/Interfaces/Asterisk-REST-Interface-ARI/), [Queues](https://docs.asterisk.org/Configuration/Applications/Asterisk-Queues/), [Voicemail](https://docs.asterisk.org/Configuration/Applications/Voicemail/), [Reporting](https://docs.asterisk.org/Configuration/Reporting/) | ARI exposes channels/bridges/media and async events under Stasis; `Queue`, `VoiceMail`/`VoiceMailMain`, `Read`/`Playback`; CDR/CEL |

## What is table stakes

These are **not obsolete** in a modern PBX: multi-level IVR/auto-attendant
with prompts, business hours and holidays; ACD queues with agent state,
distribution strategies, queue limits and overflow; voicemail-to-email;
CDR/usage reporting and recording management; a documented API; optional
AI/transcription. The reviewed vendors all present them as current
capabilities, not legacy features.

## Where AtsaPBX deliberately differs

- **Enforcement, not advice.** DNC, calling hours and opt-out are hard
  gates on every outbound attempt (PRD INV-09), including manual agent
  dials. The vendor UIs reviewed configure time conditions and lists; they
  do not establish this enforcement contract.
- **No dialplan generation.** IVR/queues/voicemail run under our ARI/Stasis
  control (D-47), not FreePBX-style generated dialplan plus reload.
- **API-only console.** Every capability in the console must be available
  through the public API (AC-05.1); a UI-only queue or recording control is
  a defect.
- **Immutable, attributed records.** Reporting attributes participants to a
  person/owner (D-88). The append-only, event-fed CDR is the target
  (INV-08); today's read model over telephony's tables is a recorded,
  temporary deviation (D-92).
- **Optional, provider-neutral AI.** We ship the pipeline, not a bundled
  model; no tenant is streamed without enabling it (D-05/D-06, INV-04/05).
- **Multi-tenant isolation is absolute** (INV-10); vendor docs rarely make
  the tenant boundary a contract.

## Resulting design choices

| Area | Choice reflected in |
|---|---|
| IVR prompt formats, 10-option ceiling, no/invalid fallback, office hours/holidays | LLD-03 §3.2; `pbx-ivr-routing` and `pbx-ivr-menu` delivered a single-level menu |
| Typed IVR destinations, multi-level menus as a node graph, weekly schedules in a time zone, manual override | D-91; LLD-03 §3.2.1; `pbx-ivr-flow-graph` (proposed) |
| Queue strategies/penalty/queue limit/MOH/overflow, agent state/hot-desk | LLD-03 §3.3 |
| Voicemail-to-email, greetings, MWI, retention | LLD-03 §3.4, LLD-09 |
| CDR/usage/quality/recordings/retention/export | LLD-09 |
| DNC, local-time windows, opt-out, abandonment | LLD-04, then LLD-06 |
| Signed/retried/replayable events | LLD-05 |
| Optional per-tenant AI streaming, speaker separation, failure isolation | LLD-10 |
| MFA TOTP-first, company OIDC, passkeys behind a dependency decision | LLD-02 §12 |

These LLDs are **designs**, not claims that vendors' behaviour is copied.
Where a vendor documents a limit (3CX's 10 IVR options, for example) it is
cited as prior art, not adopted as a requirement unless the PRD/BRD asks
for it.
