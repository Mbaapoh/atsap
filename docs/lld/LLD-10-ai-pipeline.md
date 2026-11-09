# LLD-10 — AI Pipeline

| | |
|---|---|
| **Bounded context** | `ai-pipeline` (Tier 2, HLD 04 §6) |
| **Status** | **Draft — revision 2 (2026-11-07)** to the D-98 Definition of Ready. Optional capability; no provider ships; **the audio-tap spike (AI-S1) gates the R1.0 build** |
| **Flows** | AI-01 audio tap to the tenant's provider; AI-02 AI-assisted IVR draft generation; AI-06 AI failure mid-call (HLD-20 §7). AI-03 (transcripts/summaries) and AI-04 (assist) are **R2**; AI-05 (voice agent) is an **R2 add-on** (D-96, D-97) |
| **Traces to** | BRD FBR-R1-06, FBR-R1-07, FBR-R1-11, BR-07; PRD EPIC-04 (US-04.1–04.7, AC-04.1–04.8), §12.5, §13.1, INV-03/04/05/06/10/11; HLD 04 §6, §10.1; HLD-09 F-03, §3.2; HLD-21 §6; DECISIONS D-05, D-06, D-23, D-48, D-56, D-91, D-95, D-96, D-97, D-98 |
| **Why this LLD** | "No AI lock-in" is a product promise. AI must be optional, per-tenant, provider-neutral and unable to affect a call |

**Revision 2 resolves these D-98 audit findings:** §9 Q3 of revision 1
proposed "PCMU frames over WebSocket"; the engine-to-platform leg is now
fixed as ARI `externalMedia` with `format=slin16` (D-95, HLD-21 §6), and
the provider wire format is the adapter's concern (§3.3).
`ai_usage_seconds` was cited but undefined — DDL in §3.6. The catalogue
had AI-03 in R1.0 — it is **not** R1.0: R1.0 ships the pipeline (audio to
the partner's provider) and IVR draft generation, nothing that stores or
shows transcripts. HLD 04 §6's `TranscriptSegment` aggregate is R2.

## 1. Scope and non-goals

**R1.0:** per-tenant enablement; provider configuration with write-only
credentials; one stream per speaker through snoop + `externalMedia`;
bounded ring buffer with drop accounting; provider adapters behind a port;
circuit breaker and tenant suspension; streamed-seconds ledger; PCI pause
honoured; IVR draft generation from text.

**Not R1.0:** transcripts, summaries, agent assist, sentiment (R2,
EPIC-18); the AI voice agent on the live leg (R2 add-on, D-96); a
built-in model; any AI on the emergency path (BR-09).

**Invariants:** AC-04.5 no audio leaves a tenant that has not enabled AI;
INV-04 a provider failure or slowdown never affects a call; INV-05 the
platform works fully with AI off; INV-11 credentials never appear in
logs, exports or errors; INV-06 accounting is asynchronous.

## 2. Industry parity

| Product | What it does | Our deliberate difference |
|---|---|---|
| Amazon Connect | Live media streaming to Kinesis Video Streams with separate customer/agent tracks; Contact Lens analytics and PII redaction (post-call) | Same per-speaker separation, but to **any** provider the partner chooses; no bundled analytics in R1.0 |
| 3CX | Built-in AI transcription/summaries in its own stack | We ship the pipe, not the model; the partner holds and pays the provider |
| VitalPBX | AI agents and transcription add-ons | Provider-neutral adapters, no SDK lock-in |
| FusionPBX | Speech modules with several engines (FreeSWITCH) | Pluggable adapters with tail-drop and a breaker, proven by a chaos test |
| Asterisk ARI | `snoop` and `externalMedia` primitives | We use exactly these; no `chan_audiosocket` dialplan |

## 3. AI-01 — Tap call audio to the tenant's provider

### 3.1 Trigger, actors, preconditions

| Item | Value |
|---|---|
| Trigger | `participant.joined` event (consumer `ai-tap`, durable) — telephony-core never calls ai-pipeline (HLD 04 §10.1) |
| Actors | Partner engineer configures providers (`ai.manage`, US-04.1/04.2); tenant admin enables streaming (`ai.manage`, US-04.3); the system taps |
| Preconditions | Tenant `ai_stream_policies.enabled`; a provider configured and enabled; participant role in `roles`; entitlement: AI channel available (LLD-11, OWNER DECISION §7); breaker not open; tenant not AI-suspended; consent per `ai_stream_policies.require_consent`, read from the `consent` field stamped on `participant.joined` (LLD-09 §4.11) — ai-pipeline never reads pbx-core settings; call not `pci_paused` (else the stream is created `paused`, §3.2 step 14) |
| Not met | No tap; when the reason is breaker/suspension/entitlement, an `audio_streams` row with status `unavailable` and reason is written so US-04.5 can show it; when AI is off, **nothing** is written and no ARI call is made (AC-04.5) |

### 3.2 Sequence (exact ARI)

| # | Owner | Step |
|---|---|---|
| 0 | ai-pipeline | **One ordered consumer per call.** Durable consumer `ai-tap` on `participant.joined`, `participant.left`, `call.terminated`, `call.consent_satisfied`, `call.consent_failed`, `call.pci_paused`, `call.pci_resumed` and internal `ai.tap_lost`; messages are dispatched to a per-call actor keyed by `call_id` (hash-partitioned onto `ai.tap.partitions` workers, each processing its calls' messages strictly in stream order); acks after the actor has persisted the resulting state. JetStream order per call holds because all these subjects are in one stream and published by one telephony call actor |
| 1 | ai-pipeline | Receives `participant.joined`; `DecideTap(policy, participant, state)` (pure) |
| 2 | ai-pipeline | `require_consent = false` → proceed. Else by `participant.joined.consent`: `satisfied`/`not_required` → proceed; `pending` → INSERT `audio_streams` with status **`awaiting_consent`** and `consent_deadline = now + ai.tap.consent_wait` (persisted, so a restart resumes the wait) and stop here; the call actor moves it on `call.consent_satisfied` (→ step 3) or `call.consent_failed` (→ `unavailable{consent}`); a sweeper every 5 s moves rows past `consent_deadline` to `unavailable{consent_timeout}`. `not_evaluated`/`failed` → `unavailable{consent}` (no announcement was played for this call) |
| 3 | ai-pipeline | INSERT (or move the `awaiting_consent` row to) `audio_streams` `connecting`; opens a UDP relay socket on `127.0.0.1:<port>` from `ai.tap.port_range` |
| 4 | ai-pipeline → telephony | `MediaTap.Start(ctx, TapRequest{CallID, ParticipantID, StreamID, RelayAddr})` (telephony inbound port) |
| 5 | telephony ACL | `POST /channels/{participantChannel}/snoop?spy=in&whisper=none&app=atsapbx&appArgs=aitap,<streamId>&snoopId=aitap-<streamId>` — `spy=in` is what that speaker says; `whisper=none` means nothing can be injected into the call |
| 6 | telephony ACL | `POST /channels/externalMedia?app=atsapbx&channelId=aixm-<streamId>&external_host=127.0.0.1:<port>&encapsulation=rtp&transport=udp&connection_type=client&format=slin16&direction=both` |
| 7 | telephony ACL | `POST /bridges?type=mixing&bridgeId=aitapbr-<streamId>&name=aitap`, then `POST /bridges/aitapbr-<streamId>/addChannel?channel=aitap-<streamId>,aixm-<streamId>` |
| 8 | telephony ACL | `StasisStart` for `aitap-*`/`aixm-*` channels is recognised by `appArgs`/id prefix and **never** creates a Participant, counts capacity or produces a CDR |
| 9 | ai-pipeline | Adapter `Open` (connect timeout 3 s) → `streaming`; event `ai.stream_started` |
| 10 | ai-pipeline relay | Reads RTP: 12-byte header, payload 640 bytes = 20 ms of 16 kHz 16-bit mono **big-endian** L16 (RTP network order; SPIKE AI-S1) → converts to little-endian PCM → `ring.Push` (drop-oldest when full) |
| 11 | ai-pipeline worker | `ring.Pop` → `stream.Write(frame)` with a 200 ms write deadline; frames counted sent/dropped |
| 12 | ai-pipeline | Each minute: INSERT `ai_usage_seconds` bucket; on stop the final partial bucket |
| 13 | stop | On `participant.left`, `call.terminated`, an unexpected loss of a tap channel reported by telephony as `MediaTap` `TapLost` (a snoop deleted by `Suspend` is **not** a loss and never stops the stream), policy disable, or AI-06 fallback: `MediaTap.Stop` → `DELETE /channels/aitap-<id>`, `DELETE /channels/aixm-<id>`, `DELETE /bridges/aitapbr-<id>`; adapter `Close`; status `stopped`; event `ai.stream_stopped{seconds, frames_sent, frames_dropped}` |
| 14 | PCI pause | Telephony calls `MediaTap.Suspend` synchronously inside `PauseCallRecording` (deletes only `aitap-<id>`; `aixm-`/`aitapbr-` and the provider session stay). ai-pipeline sets the stream `paused` on `call.pci_paused` (no frames, no billing; the stall detector watches provider writes, not input, so silence does not trigger `reconnecting`). On resume telephony calls `MediaTap.Resume` (new snoop added to the same bridge) and ai-pipeline returns the stream to `streaming` on `call.pci_resumed`. A participant who joins while paused (`participant.joined.pci_paused = true`): stream row created `paused`, `MediaTap.Start` deferred to `call.pci_resumed` (telephony refuses it earlier with `ErrPCIPaused`) |

Stream start target: ≤ 1 s after `participant.joined` (p95). The call
never waits on any step: every step is after the media path exists.

### 3.3 Frame format and transport (resolves revision 1 §9 Q3)

| Leg | Format | Why |
|---|---|---|
| Asterisk → platform | `externalMedia`, `encapsulation=rtp`, `transport=udp`, `format=slin16` to a loopback relay | D-95/HLD-21 §6; RTP/UDP carries slin16 without a framing spike; loopback keeps audio inside the deployment until the adapter |
| AudioSocket (`encapsulation=audiosocket`, TCP) | Not default | Historic AudioSocket audio is 8 kHz slin; slin16 support on 22.8 is SPIKE AI-S1 |
| `chan_websocket` (newer Asterisk 22 releases) | Not default | Present only in recent 22.x; evaluated in AI-S1 as a future option |
| Platform → provider | Adapter-specific: e.g. PCM16 16 kHz over WebSocket (OpenAI-compatible realtime, Deepgram `linear16`), or HTTP chunked for self-hosted Whisper-compatible servers | The provider's wire is the adapter's concern; the domain sees `AudioFrame{PCM16LE, 16 kHz, 20 ms, seq, ts}` only |

### 3.4 Ring buffer, drop policy, breaker

| Item | Value |
|---|---|
| Capacity | `ai.tap.buffer_frames` = 12 (240 ms) per stream |
| Drop policy | **Drop-oldest** (keeps latency bounded and the freshest audio); `frames_dropped++`; never blocks the relay reader |
| Degraded signal | > 5 % of frames dropped in a 10 s window → `ai.stream_degraded` (once per minute max) |
| Write deadline | 200 ms per frame; exceeded → counts as a drop, and 3 s of consecutive failed writes → `reconnecting` (AI-06) |
| Breaker | Per (tenant, provider): **open** after 5 stream failures in 60 s; **half-open** after 30 s (one trial stream); closed on trial success |
| Tenant suspension (PRD §13.1 escalation) | 20 stream failures in 15 min → tenant AI `suspended` with a console notice until an admin calls `ResumeAIForTenant` |
| Isolation | Separate goroutine pool (`ai.workers`, default 256 streams), own HTTP/WS client with bounded connections; no shared lock with telephony |

### 3.5 State machine — `AudioStream` (AI-01 + AI-06)

| From | Trigger / guard | To | Effects |
|---|---|---|---|
| — | tap decided | `connecting` | row inserted |
| — | breaker open / suspended / no entitlement | `unavailable` (terminal) | reason recorded; `ai.stream_unavailable` |
| `connecting` | adapter open ok and tap channels up | `streaming` | `ai.stream_started` |
| `connecting` | open fails / 3 s timeout | `reconnecting` | — |
| `streaming` | write errors or no provider ack/keepalive for 3 s | `reconnecting` | gap starts; `ai.stream_reconnecting` |
| `reconnecting` | reopen succeeds within 5 s (attempts at 0, 0.5, 1, 2 s) | `streaming` | gap closed; `ai.stream_resumed{gap_ms}` |
| `reconnecting` | 5 s elapsed | `failed` (terminal for this participant on this call) | tap torn down; breaker counts a failure; `ai.stream_failed`; alert |
| `streaming`/`connecting` | `call.pci_paused` | `paused` | billing stops; tap snoop already deleted by telephony |
| — | joined while `pci_paused` | `paused` | no tap channels yet |
| `paused` | `call.pci_resumed` | `streaming` (or `connecting` if the tap was deferred) | billing resumes |
| `paused`/`streaming` | `ai.tap_lost{reason: resume_failed}` (LLD-01 emits it when `MediaTap.Resume` fails) | `failed{resume_failed}` | `MediaTap.Stop` to remove the remaining `aixm-`/bridge; adapter closed; usage closed; `ai.stream_failed`; breaker **not** counted (engine-side failure, not the provider) |
| any live | `ai.tap_lost{reason: other}` | `failed{tap_lost}` | as above |
| — | `participant.joined` with `consent = pending` and `require_consent` | `awaiting_consent` | `consent_deadline` set |
| `awaiting_consent` | `call.consent_satisfied` | `connecting` (or `paused` if `pci_paused`) | — |
| `awaiting_consent` | `call.consent_failed` / deadline / `participant.left` | `unavailable{consent…}` / `stopped` | — |
| any non-terminal | participant left / call ended / policy disabled | `stopped` | `ai.stream_stopped` |

During `reconnecting` the relay keeps reading and dropping (frames are
counted as dropped) so Asterisk never sees back-pressure.

### 3.6 Data model — migration `0066_ai_pipeline` — number provisional (D-100)

```sql
CREATE TABLE ai_providers (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  kind TEXT NOT NULL CHECK (kind IN ('openai_compatible','deepgram','whisper_http','self_hosted_ws')),
  capabilities TEXT[] NOT NULL,                 -- {'stream_audio','generate_text'}
  endpoint TEXT NOT NULL CHECK (length(endpoint) <= 2048),
  model TEXT,
  credentials_ciphertext BYTEA, key_id TEXT,    -- write-only (INV-11)
  allow_private_endpoint BOOLEAN NOT NULL DEFAULT FALSE,  -- operator-only flag for self-hosted
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_by UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, name)
);
CREATE TABLE ai_stream_policies (                -- one per tenant
  tenant_id UUID PRIMARY KEY REFERENCES tenants(id),
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  provider_id UUID REFERENCES ai_providers(id),
  roles TEXT[] NOT NULL DEFAULT '{CALLER,AGENT}',
  directions TEXT[] NOT NULL DEFAULT '{inbound,outbound}',
  send_caller_number BOOLEAN NOT NULL DEFAULT FALSE,  -- metadata minimisation (§6)
  require_consent BOOLEAN NOT NULL DEFAULT TRUE,      -- OWNER DECISION (LLD-09 §11 item 6)
  suspended_at TIMESTAMPTZ, suspended_reason TEXT,
  updated_by UUID NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (NOT enabled OR provider_id IS NOT NULL)
);
CREATE TABLE ai_text_policies (                 -- AI-02 text generation, separate from audio streaming
  tenant_id UUID PRIMARY KEY REFERENCES tenants(id),
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  provider_id UUID REFERENCES ai_providers(id), -- must have capability 'generate_text'
  redact BOOLEAN NOT NULL DEFAULT TRUE,
  updated_by UUID NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (NOT enabled OR provider_id IS NOT NULL)
);
CREATE TABLE audio_streams (                     -- operational state + final counters
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  call_id UUID NOT NULL, participant_id UUID NOT NULL, provider_id UUID NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('awaiting_consent','connecting','streaming','reconnecting','paused','stopped','failed','unavailable')),
  consent_deadline TIMESTAMPTZ,                 -- set only while awaiting_consent
  status_reason TEXT,
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(), ended_at TIMESTAMPTZ,
  frames_sent BIGINT NOT NULL DEFAULT 0, frames_dropped BIGINT NOT NULL DEFAULT 0,
  gaps JSONB NOT NULL DEFAULT '[]',             -- [{start, end, cause}]
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audio_streams_call ON audio_streams (tenant_id, call_id);
CREATE INDEX audio_streams_started ON audio_streams (tenant_id, started_at DESC);

CREATE TABLE ai_usage_seconds (                  -- evidence: INSERT-only (BR-07, AC-04.8)
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  stream_id UUID NOT NULL REFERENCES audio_streams(id),
  call_id UUID NOT NULL, participant_id UUID NOT NULL, provider_id UUID NOT NULL,
  bucket_start TIMESTAMPTZ NOT NULL,             -- minute boundary, UTC
  seconds SMALLINT NOT NULL CHECK (seconds BETWEEN 0 AND 60),   -- seconds actually streamed (paused/reconnecting excluded)
  PRIMARY KEY (stream_id, bucket_start)
);
CREATE INDEX ai_usage_tenant ON ai_usage_seconds (tenant_id, bucket_start);

CREATE TABLE ai_flow_generations (               -- AI-02 evidence, INSERT-only
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  actor_principal_id UUID NOT NULL, provider_id UUID NOT NULL,
  description_sha256 BYTEA NOT NULL, redactions INT NOT NULL,
  outcome TEXT NOT NULL CHECK (outcome IN ('valid','valid_with_unresolved','invalid','provider_error','refused')),
  issues JSONB NOT NULL DEFAULT '[]',
  tokens_in INT, tokens_out INT, latency_ms INT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- RLS `ENABLE` + `FORCE` with
  `USING (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid)` on all.
- `ai_usage_seconds`, `ai_flow_generations`: `REVOKE UPDATE, DELETE, TRUNCATE … FROM atsapbx_app`
  + `atsapbx_forbid_mutation()` trigger (function created in `0060`, LLD-05 §4.6).
- Retention purge by `atsap_retention`: grants per LLD-09 §3.6 "Retention
  role grants" (`SELECT, DELETE` on `ai_usage_seconds`, `audio_streams`,
  `ai_flow_generations`), deleting in FK order `ai_usage_seconds` →
  `audio_streams`.
- Credentials sealed with the identity keyring (D-66); no read path
  returns them.
- Retention: `audio_streams` and `ai_usage_seconds` follow
  `cdr.retention_months`; no audio is ever stored by this context.

### 3.7 Ports

```go
// domain (pure)
func DecideTap(p StreamPolicy, part ParticipantJoined, s TenantAIState, consent ConsentState) TapDecision
func NextStreamState(s StreamState, ev StreamEvent, now time.Time) (StreamState, []DomainEvent)
func BreakerNext(b Breaker, outcome Outcome, now time.Time) Breaker
func UsageBuckets(start, end time.Time, gaps []Gap) []UsageBucket
func L16BEToPCM16LE(dst, src []byte) // no allocation

// ring buffer (pure data structure)
type Ring struct{ /* fixed slots */ }
func NewRing(frames int) *Ring
func (r *Ring) Push(f AudioFrame) (dropped bool) // never blocks
func (r *Ring) Pop(ctx context.Context) (AudioFrame, error)

// outbound ports
type MediaTap interface { // implemented by telephony-core (inbound port there)
    Start(ctx context.Context, req TapRequest) (TapHandle, error) // ErrPCIPaused while the call is paused
    Stop(ctx context.Context, h TapHandle) error
    Suspend(ctx context.Context, h TapHandle) error // delete the snoop only; called by telephony itself on PCI pause
    Resume(ctx context.Context, h TapHandle) error  // recreate the snoop into the existing tap bridge
}
// Telephony reports unexpected loss of tap channels on an internal event
// `ai.tap_lost{stream_id, reason}`; deletions it made itself are not reported.
type Adapter interface {
    Kind() ProviderKind
    Open(ctx context.Context, cfg ProviderConfig, meta StreamMeta) (Stream, error)
}
type Stream interface {
    Write(ctx context.Context, f AudioFrame) error // deadline-bound; never called from the relay goroutine
    Close(ctx context.Context) error
    Healthy() bool
}
type TextGenerator interface { // AI-02
    Generate(ctx context.Context, req TextRequest) (TextResult, error)
}
type ProviderStore interface { /* CRUD; Credentials(ctx, id) only for adapters */ }
type UsageLedger interface { AppendBucket(ctx context.Context, b UsageBucket) error }
```

Adapters are selected by `kind`; no provider SDK past the adapter; a new
SDK dependency needs the owner (AGENTS.md).

### 3.8 API — `AIService` (tenant-scoped)

| RPC | Fields | Permission | Refusals → console words |
|---|---|---|---|
| `CreateAIProvider` | `name, kind, endpoint, model?, credentials (write-only), capabilities[]` | `ai.manage` | `InvalidArgument` "That endpoint is not allowed: use a public https/wss URL." · `AlreadyExists` "A provider with this name exists." |
| `UpdateAIProvider` | field mask; `credentials` replaces (rotation affects new streams only) | `ai.manage` | `NotFound` |
| `DeleteAIProvider` | `id` | `ai.manage` | `FailedPrecondition` "This provider is in use by your streaming policy." |
| `ListAIProviders` | → providers (no credentials, `has_credentials: bool`) | `ai.read` | — |
| `TestAIProvider` | `id` → `ok, latency_ms, message` (sends 1 s of a fixed tone, no call audio) | `ai.manage` | `Unavailable` "The provider did not respond within 5 seconds." |
| `GetAITextPolicy` / `UpdateAITextPolicy` | `enabled, provider_id, redact` | `ai.read` / `ai.manage` | `FailedPrecondition` "This provider cannot generate text." |
| `GetAIStreamPolicy` / `UpdateAIStreamPolicy` | `enabled, provider_id, roles, directions, send_caller_number` | `ai.read` / `ai.manage` | `FailedPrecondition` "Choose a provider before enabling streaming." · `PermissionDenied` (entitlement) "AI streaming is not included in your plan." |
| `ResumeAIForTenant` | — | `ai.manage` | `FailedPrecondition` "AI is not suspended." |
| `ListAudioStreams` | `call_id? from, to, status?` → streams with counters and gaps | `ai.read` | `InvalidArgument` "Range ≤ 31 days." |
| `GetAIUsage` | `from, to, group_by (tenant\|call\|participant\|provider)` → seconds (AC-04.8, US-04.7) | `ai.read` | — |

**Default role mapping** (`TENANT_ADMIN`/`PLATFORM_ADMIN` keep `*`):
`ai.manage` and `ivr.generate` — TENANT_ADMIN only; `ai.read` —
SUPERVISOR. AGENT has no `ai.*` action.

Provider switching (US-04.2) is `UpdateAIStreamPolicy`: new streams use
the new provider immediately; live streams finish on the old one.

### 3.9 Configuration

| Key | Default | Range | Scope |
|---|---|---|---|
| `ai.tap.buffer_frames` | 12 (240 ms) | 5–100 | installation |
| `ai.tap.port_range` | 40000–40999 | any UDP range | installation |
| `ai.tap.consent_wait` | 30 s | 5–60 s | installation |
| `ai.tap.partitions` | 16 per-call worker partitions | 1–256 | installation |
| `ai.connect_timeout` | 3 s | 1–10 s | installation |
| `ai.stall_timeout` | 3 s | 1–10 s | installation |
| `ai.reconnect_window` | 5 s | 2–30 s | installation |
| `ai.write_deadline` | 200 ms | 50–1000 ms | installation |
| `ai.breaker` | 5 failures / 60 s; half-open 30 s | — | installation |
| `ai.suspend_after` | 20 failures / 15 min | 5–100 | installation |
| `ai.workers` | 256 streams | 16–4096 | installation |
| `ai_stream_policies.*` | AI off | — | tenant |

### 3.10 Failure and recovery

| Failure | Behaviour |
|---|---|
| Provider down / slow | AI-06 states; call untouched (INV-04) |
| `externalMedia` or snoop creation fails | Stream `failed{tap_setup}`; any created tap channel deleted; call untouched |
| Participant hangs up mid-setup | Snoop creation 404 → stream `stopped`; created channels/bridge deleted |
| App restart | Relay sockets gone; Asterisk tap channels remain and send RTP to a closed port (harmless). On start the ACL lists `GET /channels` and `GET /bridges` and deletes every `aitap-*`, `aixm-*`, `aitapbr-*`; open `audio_streams` rows (`connecting`/`streaming`/`reconnecting`/`paused`) are closed `failed{restart}` with `ended_at = now` (seconds counted up to the last bucket written) and **each publishes `ai.stream_failed{reason: restart, seconds}`** in the same transaction, so reporting's open/closed pairing (LLD-09 §3.2 step 12) completes; `awaiting_consent` rows keep waiting until their deadline |
| Asterisk restart | Tap channels vanish with the call (LLD-01) |
| Relay port exhausted | Stream `unavailable{no_port}`; alert |
| Concurrent stop + reconnect | All transitions for a stream go through one goroutine (stream actor) |

### 3.11 Invariants, events, audit

| Rule | How it holds |
|---|---|
| AC-04.5 | `DecideTap` returns `none` before any ARI call or socket when AI is off; test I-01 asserts zero ARI tap calls and zero UDP sockets |
| INV-04 | Tap is after the media path; `whisper=none`; relay never blocks; teardown deletes only tap channels |
| AC-04.2 | One stream per participant (`spy=in` on that participant's channel) |
| INV-11 | Credentials sealed, never returned, redacted in logs and errors (adapter errors are mapped to fixed messages) |
| BR-07 | `ai_usage_seconds` INSERT-only |

Events (`tenant.<id>.event.ai.<action>`): `ai.stream_started`,
`ai.stream_reconnecting`, `ai.stream_resumed`, `ai.stream_degraded`,
`ai.stream_failed`, `ai.stream_stopped`, `ai.stream_unavailable`,
`ai.provider_circuit_opened`, `ai.provider_circuit_closed`,
`ai.tenant_suspended`. Public (LLD-05 §3.2): started, stopped, failed.
Audit: `ai.provider.create/update/delete`, `ai.policy.update`,
`ai.tenant_resume`.

### 3.12 Tests

| # | Tier | Given / When / Then |
|---|---|---|
| T-01 | U | Given a full ring of 12, When a 13th frame is pushed, Then the oldest is dropped and `Push` returns `dropped = true` without blocking |
| T-02 | U | Given `streaming`, When 3 s of write failures occur, Then `reconnecting`; reopen at 1 s → `streaming` with `gap_ms ≈ 1000` |
| T-03 | U | Given 5 failures in 60 s, Then the breaker opens; after 30 s one trial is allowed |
| T-04 | U | Given a 150 s stream with a 20 s pause, When `UsageBuckets` runs, Then total seconds = 130 |
| T-05 | U | Given a big-endian L16 payload, When converted, Then samples match the golden PCM |
| I-01 | I | Given a tenant with AI off, When `participant.joined` arrives, Then no row, no ARI call (fake ACL records none) and no socket (AC-04.5) |
| I-02 | I | Given AI on, When two participants join, Then two streams with distinct participant ids (AC-04.2) |
| I-03 | I | Given tenant B, When it lists tenant A's providers, Then nothing; credentials never appear in any response |
| I-04 | I | Given `ai_usage_seconds`, When the app role updates a row, Then refused |
| E-01 | E | Given a fake local provider, When a rig call runs 60 s, Then the provider receives ~3,000 frames per speaker and `GetAIUsage` reports ~60 s per participant |
| E-02 | E | Given the provider is `SIGKILL`ed mid-call, Then the call audio continues (RTP counters on both phones unchanged), the stream goes `failed` within 5 s and an alert fires (AC-04.3) |
| E-03 | E | Given a provider that reads 1 frame/s, Then frames drop and the call's RTP jitter is unchanged (AC-04.4) |
| E-04 | E | Given PCI pause, Then the fake provider receives no frame in the interval |
| E-05 | E | Given full load (installation capacity) with AI on vs off, Then MOS and jitter distributions are equal within measurement noise (AC-04.1) |
| I-06 | I | Given a call's `participant.joined`, `call.pci_paused` and `participant.left` delivered in order to the per-call actor, Then the stream ends `stopped` and never re-taps; a restart between messages resumes from persisted state |
| I-07 | I | Given a stream in `paused`, When `ai.tap_lost{resume_failed}` arrives, Then it becomes `failed{resume_failed}`, the breaker count is unchanged and `ai.stream_failed` is published |
| I-08 | I | Given an `awaiting_consent` row and an app restart, When `call.consent_satisfied` arrives after restart, Then the tap starts; with no event, Then after the deadline the row is `unavailable{consent_timeout}` |
| I-05 | I | Given `require_consent` and `participant.joined.consent = pending`, When `call.consent_failed` arrives, Then the stream is `unavailable{consent}` and no ARI tap call is made; with `satisfied` the tap starts |
| C-01 | C | Given the AI settings page, When credentials are saved, Then the field shows "Saved" and never the value |
| A-01 | A | Given one self-hosted and one commercial provider, When the admin switches by configuration only, Then the next call streams to the new provider (AC-04.6) |

### 3.13 SPIKE AI-S1 (gates the build; HLD-21 §10 item 2)

On the pinned Asterisk 22.8: (a) `externalMedia` RTP `slin16` byte order,
packetisation (20 ms?) and payload type; (b) the accepted `direction`
values; (c) whether AudioSocket carries slin16 and whether `chan_websocket`
is present; (d) behaviour when the consumer stops reading (UDP: drops,
no stall expected; TCP: does the snoop stall the bridge?); (e) that a
`whisper=none` snoop injects nothing when the externalMedia side sends
audio; (f) added latency at the relay. Exit: written results; T-05 and
E-02/E-03 become the regression tests.

## 4. AI-02 — AI-assisted IVR draft generation (FBR-R1-06)

### 4.1 Trigger, actors, preconditions

| Item | Value |
|---|---|
| Trigger | Console IVR builder "Generate from description", or a partner calling `GenerateIVRDraft` (FBR-R1-06: public API) |
| Actors | Tenant administrator (`ivr.generate`) |
| Preconditions | **"AI enabled for text"** means exactly: `ai_text_policies.enabled = true` **and** its `provider_id` names an enabled `ai_providers` row whose `capabilities` contain `generate_text` **and** `ai_stream_policies.suspended_at IS NULL` (tenant AI suspension applies to both). It is independent of audio streaming: a tenant may generate flows without streaming any call audio, and vice versa. Rate limit not exceeded. Not met → the RPC refuses and the builder hides the button (the builder works fully without AI) |

### 4.2 Sequence

| # | Owner | Step |
|---|---|---|
| 1 | console | Loads known destinations through pbx-core's public list RPCs (extensions, queues, ring groups, voicemail, schedules) |
| 2 | console/partner | `AIService.GenerateIVRDraft{description, language, time_zone, known_destinations[]{id, kind, label}}` |
| 3 | ai-pipeline | `Redact(description)` (pure, §6) → `BuildPrompt` (pure): fixed system prompt + the D-91 graph JSON Schema + rules + the destination list + the redacted description |
| 4 | ai-pipeline | `TextGenerator.Generate` on the adapter for `ai_text_policies.provider_id`, with JSON-schema-constrained output where the adapter supports it; timeout 30 s |
| 5 | ai-pipeline | Parse → `ivrgraph.Validate(graph, known)` (pure, shared with pbx-core publish) |
| 6 | ai-pipeline | Invalid → **one** repair round sending the issues back; still invalid → `outcome = invalid`, return issues, no graph |
| 7 | ai-pipeline | INSERT `ai_flow_generations`; audit `ivr.generate` |
| 8 | console | Opens the graph **unsaved** in the builder; unresolved destinations are highlighted; the admin edits and saves through pbx-core's existing revision RPCs, then publishes as any other revision |

**Draft-only by construction:** `ai-pipeline` may not depend on
`pbx-core` (HLD 04 §10.1), so it has no code path that can write or
publish a flow. Publishing remains a separate, audited human action.

**Prompt rules (system prompt, versioned `ivr-gen-v1`):** node kinds only
`schedule, menu, play, goto, hangup`; ≤ 10 options per menu; depth ≤ 5;
`goto` targets only ids from `known_destinations`, otherwise
`{kind: "unresolved", label}`; never an emergency number or any external
number not in the list; prompts as text for TTS-free review (the admin
records or chooses audio); output JSON only.

### 4.3 State

Generation is stateless request/response; the evidence row records the
outcome (`valid`, `valid_with_unresolved`, `invalid`, `provider_error`,
`refused`).

### 4.4 API

| RPC | Fields | Permission | Refusals → console words |
|---|---|---|---|
| `GenerateIVRDraft` | in: `description (20–2,000 chars), language (BCP 47), time_zone (IANA), known_destinations[] (≤ 500)`; out: `generation_id, graph (D-91 v2 JSON), unresolved[]{node_id, label}, warnings[], redactions` | `ivr.generate` | `FailedPrecondition` "Turn on AI and choose a text provider to generate flows." · `InvalidArgument` "Describe the flow in 20 to 2,000 characters." · `ResourceExhausted` "Generation limit reached; try again in {n} minutes." · `Unavailable` "The AI provider did not answer. You can build the flow by hand." · `FailedPrecondition` "The AI could not produce a valid flow: {first issue}." |

### 4.5 Configuration, failure, invariants

| Key | Default | Range | Scope |
|---|---|---|---|
| `ai.gen.timeout` | 30 s | 5–120 s | installation |
| `ai.gen.rate_limit` | 20 per tenant per hour | 1–500 | tenant |
| `ai.gen.max_tokens` | 4,000 output | 500–16,000 | installation |
| `ai_text_policies.redact` | on | on/off | tenant (OWNER DECISION §6) |

Failure: provider error/timeout → `Unavailable`, row `provider_error`;
malformed JSON → repair round; nothing is ever saved on failure. INV-05:
the builder is fully usable without this RPC. INV-11: credentials never
in the prompt or logs; the description itself is logged only as its hash.

### 4.6 Tests and parity

| # | Tier | Given / When / Then |
|---|---|---|
| G-01 | U | Given a generated graph that targets an id not in `known_destinations`, When validated, Then the issue is reported; `unresolved` placeholders are accepted |
| G-02 | U | Given a description containing "call 0612345678 or a@b.com", When `Redact` runs, Then both are replaced and `redactions = 2` |
| G-03 | I | Given a fake `TextGenerator` returning invalid JSON then valid JSON, Then one repair round and `outcome = valid` |
| G-04 | I | Given AI off, Then `FailedPrecondition` and no provider call |
| G-05 | I | Given the dependency rule, When `internal/ai/**` imports `internal/pbx/**`, Then depguard fails (draft-only guarantee) |
| G-08 | I | Given a fake `TextGenerator` that never answers, When `GenerateIVRDraft` runs, Then after `ai.gen.timeout` (30 s; fake clock) it returns `Unavailable` "The AI provider did not answer…", an `ai_flow_generations` row with `provider_error`, and nothing is saved |
| G-06 | B | Given the builder, When a draft is generated, Then it opens unsaved with unresolved nodes highlighted and Publish is disabled until they are resolved |
| G-07 | A | Given a non-engineer, When they generate and finish a two-level IVR, Then they publish within 30 minutes (AC-03.6 with AI) |

Parity: 3CX and VitalPBX offer no text-to-flow generation in their
documented core; Amazon Connect offers generative assistance in its flow
designer within AWS. Ours is provider-neutral, API-first and can never
publish on its own.

**Cross-LLD dependency:** the D-91 graph type and its structural
validator move to a pure shared package `internal/shared/ivrgraph` so
pbx-core publish and AI-02 apply one rule set (LLD-03 change).

## 5. AI-06 — AI failure mid-call degrades the feature, never the call

| DoR item | Content |
|---|---|
| 1 Trigger / actors | Provider stall, error, disconnect, or slowness on a live stream; the system acts; tenant admin is alerted; agent sees state in R2 assist (R1.0: call detail shows stream state) |
| 2 Preconditions | A stream in `connecting`/`streaming` |
| 3 Sequence | §3.5 transitions: stall 3 s → `reconnecting` → reopen attempts at 0, 0.5, 1, 2 s → resume (gap recorded) or `failed` at 5 s → `MediaTap.Stop` (deletes only tap channels) → breaker update → alert |
| 4 State | `AudioStream` table §3.5; breaker `closed → open → half_open → closed`; tenant `active → suspended → active` (admin) |
| 5 Data | `audio_streams.gaps`, `status_reason`; usage excludes gap seconds |
| 6 Ports | `NextStreamState`, `BreakerNext` (pure) |
| 7 API | `ListAudioStreams`, `ResumeAIForTenant` (§3.8) |
| 8 Config | `ai.stall_timeout` 3 s, `ai.reconnect_window` 5 s, breaker 5/60 s/30 s, suspend 20/15 min (§3.9) |
| 9 Failure | The failure itself is the flow; a failure during teardown leaves tap channels that the restart sweep deletes |
| 10 Invariants / events | INV-03, INV-04, INV-05; `ai.stream_reconnecting`, `ai.stream_resumed`, `ai.stream_failed`, `ai.provider_circuit_opened/closed`, `ai.tenant_suspended` |
| 11 Tests | T-02, T-03, E-02, E-03 (§3.12) plus: Given 20 failures in 15 min (fake clock), Then the tenant is suspended, new joins produce `unavailable{suspended}`, and `ResumeAIForTenant` restores taps for the next participant (I) |
| 12 Parity | Amazon Connect keeps the contact up when analytics fail; 3CX's AI is in-stack. Our difference: a visible state machine, gaps recorded, a per-tenant breaker and suspension per PRD §13.1 |

PRD §12.5 steps 4–9 (agent reconnecting indicator, IVR reverting to
keypad, partial transcript marked incomplete) apply to R2 consumers
(assist, AI menus); in R1.0 no AI feature is in the caller's path, so the
only visible effect is the stream status and the alert.

## 6. PII before the provider

**OWNER DECISION — PII redaction before the provider.** Real-time audio
cannot be redacted without first transcribing it, which would itself send
the audio somewhere. *Recommended R1.0 default:*
1. **Audio:** no redaction; PCI pause (RC-07) stops the tap during card
   capture; the tenant's agreement with its provider covers the rest.
2. **Stream metadata:** minimised — the provider receives `stream_id`,
   `participant role` and language only; the caller's number only when
   `send_caller_number` is on (default off).
3. **AI-02 text:** regex redaction on by default for phone numbers,
   e-mail addresses and card-like digit runs (Luhn-checked) before the
   prompt leaves the deployment.
4. Post-call transcript redaction (Contact Lens-style) is an R2 feature
   with AI-03.

## 7. Owner decisions

| # | Item | Recommended default |
|---|---|---|
| 1 | **OWNER DECISION — PII redaction** (§6) | As in §6 |
| 2 | **OWNER DECISION — AI streaming entitlement** | Configured per tenant without a separate licence in R1.0; each tapped participant counts as one AI channel only for the R2 voice agent (BRD §12.4.2) |
| 3 | **OWNER DECISION — first adapters** | OpenAI-compatible realtime WebSocket + a self-hosted Whisper-compatible HTTP endpoint; no SDK dependency (plain `net/http` + an existing WebSocket library only if already approved) |
| 4 | Stream metadata retention | Follows CDR retention (resolved) |
| 5 | Frame format | Resolved: slin16 externalMedia over RTP to a loopback relay (§3.3) |

**References:** [PRD EPIC-04](../PRD.md), [HLD 04 §6](../hld/04-bounded-contexts.md),
[HLD-09](../hld/09-failure-model.md), [HLD-21 §6](../hld/21-media-and-call-control-flows.md),
[LLD-09](LLD-09-reporting.md),
[Asterisk ARI channels](https://docs.asterisk.org/Latest_API/API_Documentation/Asterisk_REST_Interface/Channels_REST_API/),
[Amazon Connect live media streaming](https://docs.aws.amazon.com/connect/latest/adminguide/customer-voice-streams.html),
[3CX](https://www.3cx.com/), [VitalPBX](https://wiki.vitalpbx.com/),
[FusionPBX speech](https://docs.fusionpbx.com/en/latest/applications/speech.html).
