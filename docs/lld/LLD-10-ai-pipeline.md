# LLD-10 — AI Pipeline

| | |
|---|---|
| **Bounded context** | `ai-pipeline` (Tier 2, HLD 04 §6) |
| **Status** | **Draft — design only.** Optional capability; no provider ships |
| **Traces to** | BRD FBR-R1-07; PRD EPIC-04 (US-04.1–04.7, AC-04.1–04.8), INV-04/INV-05/INV-06/INV-11; HLD 01 §6, HLD 04 §6; DECISIONS D-05, D-06, D-23, D-48 |
| **Why this LLD** | "No AI lock-in" is a product promise. AI must be optional, per-tenant, provider-agnostic and unable to affect a call |

## 1. Scope and non-goals

**In scope:** per-tenant enable/disable, provider configuration with
encrypted credentials, per-participant audio streaming, provider adapters,
backpressure, streamed-minute accounting, failure isolation, and an API
for the console.

**Not in scope:** AI features themselves (transcription UI, agent assist,
summaries), which are later R2 slices; no AI in the call bridge; no
built-in model.

**Invariants:** no audio leaves a tenant that has not enabled it
(AC-04.5); a provider failure/slowdown never affects the call (AC-04.3/04.4,
INV-04); the platform works fully with AI off (INV-05); credentials never
appear in logs/exports/errors (AC-04.7, INV-11); reporting delivery is
asynchronous (INV-06).

## 2. Prior art (2026-11-04)

| Source | What it establishes | What we adopt |
|---|---|---|
| [Asterisk ARI](https://docs.asterisk.org/Configuration/Interfaces/Asterisk-REST-Interface-ARI/) | ARI exposes media and snoop primitives; Stasis applications own call logic | Snoop/media taps are asynchronous from call control |
| [3CX](https://www.3cx.com/) | AI receptionist/transcription/analytics are optional product surfaces | Optional, per-tenant, never a call dependency |
| [VitalPBX](https://wiki.vitalpbx.com/) | AI agents, transcription and recording analysis | Provider-neutral adapter; no lock-in |
| [FusionPBX](https://docs.fusionpbx.com/en/latest/applications/speech.html) | Speech/transcribe modules with multiple engines | Pluggable provider adapters, bounded queues |
| [Vodia](https://web.vodia.com/features) | Voice agents/analytics alongside core PBX | AI is a layer above a fully working PBX |

The product promise is narrower than these products' bundled AI: **we
ship the pipeline, the partner chooses and pays for the provider**; no
default provider.

## 3. Model

```go
type Provider struct {
    ID, TenantID shared.ID       // tenant-owned
    Kind ProviderKind            // OPENAI_COMPATIBLE | DEEPGRAM | SELF_HOSTED | ...
    Endpoint string              // https/wss
    CredentialsCiphertext []byte // never returned
    Enabled bool
}

type StreamPolicy struct {
    TenantID shared.TenantID
    Enabled bool                 // default false
    ProviderID *shared.ID
    Directions []Direction       // inbound, outbound or both
    IncludeRecording bool
}

type AudioStream struct {
    ID, TenantID, CallID, ParticipantID shared.ID
    ProviderID shared.ID
    StartedAt, EndedAt *time.Time
    FramesSent, FramesDropped int64
    Status                   // connecting | streaming | failed | stopped
}
```

`audio_streams` and `ai_usage_seconds` are RLS-scoped; provider
credentials use the identity keyring and are write-only. The stream's
participant is a platform participant, never a raw channel.

## 4. Audio path and isolation

1. On a call participant connecting, the call service checks an
   `AITap` port **after** the media path is established. The port is a
   no-op when AI is disabled.
2. `TelephonySnoop` starts a snoop/external-media tap per participant
   through ARI; each speaker is a **separate stream** (AC-04.2).
3. A bounded in-memory ring buffer hands frames to a per-stream worker;
   when the provider is slow or the buffer is full, frames are dropped and
   counted. The call media path never waits on the worker (AC-04.4).
4. A provider error closes the stream, marks it failed, emits an alert and
   leaves the call untouched (AC-04.3). Reconnection is a new stream row,
   not a call action.
5. Stopping/pausing recording or AI stops the tap; a call never ends
   because the tap stopped.

The tap adapter is the only code allowed to touch ARI media; it lives in
`ai-pipeline/acl/asterisk` (or the telephony ACL) and never leaks a channel
into domain/API.

### 4.1 Transport and the AI voice agent (2026-11-07)

- **Transport:** the tap is a snoop feeding ARI `externalMedia` at
  slin16 (AudioSocket or RTP encapsulation; HLD-21 §6). The spike
  measures latency and stall behaviour (HLD-21 §10).
- **The AI voice agent** (BRD §12.4.2) is the one AI feature on the live
  leg. Under D-96 it is a call **destination** (`ai_agent`) with a
  latency deadline and an immediate fallback destination. It is never on
  the emergency path, and listening AI stays on copies (D-23).
- **AMD** is not an AI dependency: an AI classifier is an optional
  plugin to the dialler's baseline AMD (HLD-21 §4).

## 5. Provider adapters

```go
type Adapter interface {
    Open(ctx context.Context, cfg ProviderConfig, meta StreamMeta) (Stream, error)
}
type Stream interface {
    Write(ctx context.Context, frame AudioFrame) error // must not block caller
    Close(ctx context.Context) error
    Healthy() bool
}
```

Adapters are selected by `Kind`, not by hard-coded client names. The first
adapters should be an **OpenAI-compatible HTTP/WebSocket** adapter and one
self-hosted adapter; a commercial one is demonstrated per AC-04.6.
Credentials are passed only to the adapter, never logged. Endpoints are
SSRF-validated (https/wss, no private/metadata unless self-hosted is
explicitly allowed by the operator).

## 6. API and reporting

`AIService` (tenant-scoped, admin scope for config):
- `GetAIStreamPolicy` / `UpdateAIStreamPolicy` (enable/disable, provider,
  directions)
- `CreateAIProvider` / `ListAIProviders` / `UpdateAIProvider` /
  `DeleteAIProvider` (secret write-only, rotate)
- `ListAudioStreams` / `GetAudioStream` (status, frames, dropped frames)
- `GetAIUsage` (streamed seconds per tenant/call/participant, AC-04.8)
- `TestAIProvider` (bounded, no call audio; returns a fixed result)

**Out of scope now:** transcripts, summaries, agent assist APIs. They will
consume the same stream and add their own stores.

Reporting joins `ai_usage_seconds` into the call record; streamed duration
is a CDR field (AC-04.8, EPIC-05). No report includes audio or credentials.

## 7. Security, privacy, failure and cost

- Tenant enablement is the gate; a provider cannot be used by another
  tenant (RLS + explicit tenant id).
- Audio is personal data: no logging of frames, no DLQ retaining audio,
  retention of stream metadata per LLD-09.
- Provider credentials encrypted at rest, never returned, redacted in
  audit/logs; rotating a credential affects new streams only.
- Failure isolation: separate worker pool/connection budget; circuit
  breaker per provider; call path has no synchronous dependency.
- Cost/observability: frames sent/dropped, connect latency, provider error
  rate, streamed seconds per tenant. Alert on failures; never on call
  quality unless the tap causes it (it must not).

## 8. Tests and Definition of Done

- Unit: policy gating, ring buffer/drop, adapter selection, SSRF, usage
  arithmetic.
- Integration: enable/disable per tenant; no stream when off; two speakers
  produce two participant streams; provider 500/timeout leaves call
  active; credential never read back; RLS.
- Live: a call with a fake local provider receives audio, killing the
  provider mid-call leaves audio intact, streamed seconds appear in usage;
  a tenant with AI off sends no frame.
- DoD: AC-04.1, 04.3–04.8 proven with at least one self-hosted and one
  commercial adapter; AC-04.2 speaker separation proven by tagged frames.

## 9. Open decisions for the owner

1. Which provider adapters ship first (proposed OpenAI-compatible +
   a local Whisper-compatible endpoint).
2. Whether AI streaming needs its own paid entitlement in R1 or is
   configured per tenant without a separate licence.
3. Audio frame format/transport (PCMU frames over WebSocket vs provider
   SDK); no SDK dependency without a D-28 decision.
4. Retention of stream metadata and dropped-frame counters.

**References:** [PRD EPIC-04](../PRD.md), [HLD 04 §6](../hld/04-bounded-contexts.md),
[Asterisk ARI](https://docs.asterisk.org/Configuration/Interfaces/Asterisk-REST-Interface-ARI/),
[3CX](https://www.3cx.com/), [VitalPBX](https://wiki.vitalpbx.com/),
[FusionPBX speech](https://docs.fusionpbx.com/en/latest/applications/speech.html),
[Vodia](https://web.vodia.com/features), [TRD](../TRD.md).
