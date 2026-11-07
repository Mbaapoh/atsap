# LLD-05 — Webhook Delivery and the Public Event Stream

| | |
|---|---|
| **Bounded context** | `webhook-delivery` (Tier 1, HLD 04 §8, §10.1) |
| **Status** | **Draft — design only (revision 2, 2026-11-07).** The transactional outbox and NATS publisher exist (D-57); no consumer, subscription or public stream exists yet |
| **Flows** | IN-02 signed, retried, replayable webhooks; FL-05 webhook endpoint down; IN-03 public event stream (HLD-20 §6, §9) |
| **Traces to** | BRD FBR-R1-11, BR-07; PRD EPIC-05 (US-05.2, 05.3, 05.5, 05.7; AC-05.2, 05.3, 05.6, 05.8), §13.3, INV-06/07/08/10/11; HLD 04 §8, §10.1; HLD-09 F-04, F-09; HLD-21 §8; DECISIONS D-56, D-57, D-58, D-66, D-69, D-98 |
| **Why this LLD** | The API is the product (INV-07). Partners need reliable, signed, ordered events without polling, and a live stream for telemetry |

**Revision 2 resolves the D-98 audit contradictions:** the retry schedule
is now PRD §13.3's (1 m, 5 m, 15 m, 1 h, 6 h, 24 h) and the `Unhealthy` /
`Disabled` states exist (§4.4); HLD-09 F-04 and §3.1 are aligned with
this section, which is the single source. Ordering per destination is
promised, as PRD §13.3 requires (revision 1 said it was not). Delivery
records are append-only (BR-07).

## 1. Scope and non-goals

**In scope (R1.0):** tenant-owned subscriptions; the public event
catalogue with versioned schemas (§3); Standard-Webhooks-compatible
signing; ordered at-least-once delivery with the PRD retry schedule;
`Unhealthy`/`Disabled` states; dead-letter retention and manual replay;
delivery history; SSRF-safe destinations; secret rotation; and a public
server-streaming event API with resumable cursors (§6, IN-03).

**Not in scope:** each producer's internal event (owned by its context);
CRM/ERP connectors (IN-04, R2); synchronous callbacks into the call path;
a partner-facing message broker; exactly-once delivery (D-58).

**Hard invariant (INV-06):** delivery never blocks or delays call
processing. A dead endpoint has no effect on a call, a CDR or an API
response.

## 2. Industry parity (DoR item 12)

| Product | What it does | Our deliberate difference |
|---|---|---|
| 3CX (v20) | Call Control API with a WebSocket event feed; CRM integration templates | We also push signed webhooks with retry and replay; a partner need not hold a socket open |
| VitalPBX | REST API and integrations; events largely via AMI on the box | No engine protocol is exposed; events carry platform ids only |
| FusionPBX | Relies on the FreeSWITCH event socket and custom scripts | Durable, tenant-scoped delivery history and replay are product features |
| Amazon Connect | Contact events through EventBridge; contact trace records through Kinesis Data Streams | Same split (push events + pull/stream records), but self-hosted, with per-destination ordering and a signed HTTP contract |
| Stripe / Standard Webhooks | `id.timestamp.body` HMAC, 5-minute tolerance, secret rotation with dual signatures | Adopted as-is so partners reuse existing verifier libraries |

## 3. Public event catalogue (versioned)

### 3.1 Envelope (schema `atsapbx.event.v1`)

```json
{
  "id": "0193f0f6-…",               // event id = outbox row id; idempotency key
  "type": "call.terminated",
  "version": 1,                      // major version of `data` for this type
  "tenant_id": "uuid",
  "occurred_at": "2026-11-07T10:15:02.123Z",
  "sequence": 4182,                  // per subscription (webhooks) or omitted (stream; cursor instead)
  "data": { }
}
```

Rules:
- `data` carries platform ids, E.164 numbers and RFC 3339 UTC times
  (millisecond precision). It never carries a channel, bridge, endpoint
  URI, password, token, key or audio.
- Adding an optional field is compatible. Removing, renaming or changing
  the meaning of a field publishes the type at `version: N+1`; both
  versions are offered for at least 12 months (AC-05.6). A subscription
  pins a version per type at creation (default: current).
- A pure translator `ToPublic` (§4.7) maps an internal outbox event to a
  public event through an **explicit allowlist**. An internal event not in
  the table below is never delivered.

### 3.2 Catalogue v1

| Type | Producer | Emitted when | `data` fields (v1) | Webhook | Stream |
|---|---|---|---|---|---|
| `call.initiated` | telephony-core | Call created | `call_id, direction, from, to, started_at` | yes | yes |
| `call.active` | telephony-core | ≥2 participants connected (answered) | `call_id, answered_at` | yes | yes |
| `call.terminated` | telephony-core | Call reaches Terminated | `call_id, ended_at, termination_reason, duration_seconds` | yes | yes |
| `participant.joined` | telephony-core | Participant connected | `call_id, participant_id, role, extension_id?, principal_id?` | yes | yes |
| `participant.left` | telephony-core | Participant disconnected | `call_id, participant_id, left_at, billable_seconds` | yes | yes |
| `participant.quality` | reporting | 30 s quality window closed (LLD-09 RC-06) | `call_id, participant_id, window_start, window_end, mos?, jitter_ms?, loss_pct?, rtt_ms?, direction` | opt-in | yes |
| `cdr.recorded` | reporting | Ledger row appended (LLD-09 RC-05) | full CDR incl. participants and billing fields (AC-05.4) | yes | yes |
| `cdr.adjusted` | reporting | Adjustment appended | `call_id, adjustment_id, kind, delta_seconds?, delta_cost_micros?, reason, actor_id` | yes | yes |
| `cdr.anchored` | reporting | Chain head anchored | `records_seq, records_head, adjustments_seq, adjustments_head, anchored_at` | yes | yes |
| `recording.started` | telephony-core | Capture began after consent | `recording_id, call_id, policy_id, consent: announced\|not_required` | yes | yes |
| `recording.paused` / `recording.resumed` | telephony-core | PCI pause/resume (RC-07) | `recording_id, call_id, at, actor_id?, auto: bool` | yes | yes |
| `recording.stored` | telephony-core | Object written and verified | `recording_id, call_id, size_bytes, sha256, duration_seconds, retention_until` | yes | yes |
| `recording.failed` | telephony-core | Start or upload failed | `recording_id?, call_id, stage: start\|consent\|upload, reason` | yes | yes |
| `recording.deleted` | reporting | Retention/erasure deletion | `recording_id, call_id, deleted_at, cause: retention\|erasure` | yes | yes |
| `ai.stream_started` / `ai.stream_stopped` / `ai.stream_failed` | ai-pipeline | LLD-10 AI-01/AI-06 | `stream_id, call_id, participant_id, provider_id, seconds?, frames_dropped?, reason?` | yes | yes |
| `extension.created` / `.updated` / `.deleted` | pbx-core | Admin change | `extension_id, number, owner_principal_id?` | yes | yes |
| `ivr.flow_published` | pbx-core | Publish/rollback | `flow_id, revision, actor_id` | yes | yes |
| `report.export_ready` | reporting | Async export done | `export_id, kind, row_count, expires_at` | yes | yes |
| `webhook.test` | webhook-delivery | `TestWebhook` | `subscription_id, nonce` | target only | no |

The full JSON Schema per type/version lives in `api/schemas/events/<type>.v<N>.json`
and is published with the API docs; CI validates every catalogue row has
a schema and a golden example (test W-05).

**Event naming on the bus:** `tenant.<tenant_id>.event.<type>` (the
existing publisher, D-57), e.g. `tenant.<id>.event.recording.stored`.

## 4. IN-02 — Signed, ordered, retried, replayable webhooks

### 4.1 Trigger, actors, preconditions (DoR 1–2)

| Item | Value |
|---|---|
| Trigger | A published domain event on `tenant.*.event.>` matching an active subscription; or `ReplayWebhookDelivery` / `TestWebhook` |
| Actors | Partner developer or tenant administrator (PRD persona) manages subscriptions with `webhook.manage`; reads history with `webhook.read`; the system delivers |
| Preconditions | Tenant active; subscription `active` or `unhealthy` (a `disabled` one holds, §4.4); destination passes the SSRF check at create **and** before each attempt; entitlement: none in R1.0 (webhooks are core, not a paid module) |
| Not met | Create with an unsafe URL → `InvalidArgument`; tenant suspended → events still fan out and are `held` (no outbound POST) |

### 4.2 Sequence (DoR 3)

| # | Owner | Step |
|---|---|---|
| 1 | producer | Writes domain row + `outbox` row in one transaction (D-57) |
| 2 | outbox worker | Publishes to JetStream `tenant.<id>.event.<type>` with `Nats-Msg-Id = outbox id` |
| 3 | webhook-delivery fan-out | Durable consumer `webhook-fanout` (explicit ack, `MaxAckPending 1000`, `AckWait 30s`) receives the message |
| 4 | fan-out | `ToPublic` (pure) maps it; not in catalogue → ack and stop |
| 5 | fan-out | In **one transaction** under the subscription row lock: for each matching subscription, `seq = next_seq`, `next_seq += 1`, INSERT `webhook_deliveries` (frozen payload) and `webhook_queue` (`pending`, or `held` if subscription `disabled`). Conflict on `(subscription_id, event_id)` → skip (idempotent redelivery, D-58) |
| 6 | fan-out | Commit, then ack the NATS message. Crash before ack → redelivery → step 5 skips |
| 7 | sender | Every 1 s: `ClaimDue` selects due queue rows with `FOR UPDATE SKIP LOCKED`, batch ≤ 100; for an `ordered` subscription only its lowest-`seq` undelivered row is eligible (head of line) |
| 8 | sender | Resolve host → `ValidateDestination` against resolved IPs (pure) → refuse = attempt outcome `ssrf_refused` |
| 9 | sender | Sign (§4.3) and POST with dial/TLS timeout 5 s, total timeout 10 s, no redirects, response body read ≤ 4 KiB |
| 10 | sender | INSERT `webhook_delivery_attempts`; update queue row (`delivered`, or `retrying` with `next_attempt_at = NextAttempt(...)`, or `dead`); update subscription health (§4.4) — one transaction |
| 11 | sender | On state change emit `webhook.endpoint_unhealthy` / `webhook.endpoint_disabled` / `webhook.endpoint_recovered` (internal, alert + console; never delivered to the failing endpoint) |

### 4.3 Signing (Standard Webhooks v1)

| Header | Value |
|---|---|
| `webhook-id` | event `id` (replays keep the same id so partner dedupe still works) |
| `webhook-timestamp` | Unix seconds at signing (each attempt re-signs) |
| `webhook-signature` | `v1,<base64(HMAC-SHA256(secret, id + "." + timestamp + "." + raw_body))>`; during rotation two space-separated signatures (new first) |
| `X-AtsaPBX-Event` | event type (informational; not signed separately) |
| `X-AtsaPBX-Delivery` | delivery id (differs on replay) |
| `User-Agent` | `AtsaPBX-Webhooks/1` |

- Secret: 32 random bytes, presented once as `whsec_<base64>`; stored
  encrypted with the identity keyring (D-66); never returned again.
- **Receiver tolerance (documented):** reject a timestamp more than
  **300 s** from the receiver's clock; compare signatures in constant
  time; dedupe on `webhook-id`.
- Rotation: `RotateWebhookSecret` creates a new secret; the old one keeps
  signing alongside for `webhook.secret_overlap` (default 24 h) then
  expires.

### 4.4 State machines (DoR 4)

**Subscription health** (PRD §13.3, HLD-09 §3.1):

| From | Event / guard | To | Side effects |
|---|---|---|---|
| `active` | attempt fails, `consecutive_failures` reaches 3 | `unhealthy` | `failing_since` = first failure time; alert; console badge |
| `active`/`unhealthy` | attempt succeeds | `active` | `consecutive_failures = 0`, `failing_since = NULL`; `webhook.endpoint_recovered` if was unhealthy |
| `unhealthy` | a delivery reaches `dead` (its +24 h retry failed) **or** `now − failing_since ≥ 24 h` | `disabled` (`disabled_reason = sustained_failure`) | All `pending`/`retrying` rows → `held`; alert at high severity |
| any | HTTP `410 Gone` | `disabled` (`endpoint_gone`) | as above |
| any | `DisableWebhook` | `disabled` (`manual`) | as above; audited |
| `disabled` | `EnableWebhook` | `active` | `held` rows → `pending` (`attempts = 0`, `next_attempt_at = now`), oldest `seq` first; audited |
| any | `DeleteWebhook` | deleted (`deleted_at`) | queue rows → `expired`; history kept for retention |

While `unhealthy`, the schedule is unchanged — it is already slow. In
`ordered` mode the head row's 24 h failure is exactly "24 h of continuous
failure", so the two `disabled` guards coincide.

**Delivery (queue row):**

| From | Event | To |
|---|---|---|
| — | fan-out | `pending` (or `held` when subscription disabled) |
| `pending`/`retrying` | 2xx | `delivered` |
| `pending`/`retrying` | failure, retries left | `retrying` (`next_attempt_at` per §4.5) |
| `retrying` | failure after the +24 h retry | `dead` |
| `held` | `EnableWebhook` | `pending` |
| `dead`/`delivered`/`held` | `ReplayWebhookDelivery` | (original unchanged) a **new** delivery row `pending`, `replay_of` = original |
| any non-terminal | retention elapsed | `expired` |

### 4.5 Retry schedule (single source; HLD-09 F-04 follows this)

Initial attempt at fan-out, then **six retries** at offsets from the first
attempt: **+1 m, +5 m, +15 m, +1 h, +6 h, +24 h**, each with ±10 %
jitter. PRD §13.3's "six attempts across 24 hours" counts these retries.
After the +24 h retry fails the delivery is `dead`. Failure = any non-2xx,
timeout, connect/TLS error or SSRF refusal; 3xx is a failure (no
redirects).

### 4.6 Data model (DoR 5) — migration `0060_webhook_delivery`

```sql
CREATE TABLE webhook_subscriptions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  url TEXT NOT NULL CHECK (length(url) <= 2048),
  description TEXT CHECK (length(description) <= 200),
  event_types TEXT[] NOT NULL CHECK (cardinality(event_types) BETWEEN 1 AND 64),
  event_versions JSONB NOT NULL DEFAULT '{}',          -- {"call.terminated":1}
  ordering TEXT NOT NULL DEFAULT 'ordered' CHECK (ordering IN ('ordered','unordered')),
  state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active','unhealthy','disabled')),
  disabled_reason TEXT CHECK (disabled_reason IN ('sustained_failure','endpoint_gone','manual')),
  consecutive_failures INT NOT NULL DEFAULT 0,
  failing_since TIMESTAMPTZ,
  next_seq BIGINT NOT NULL DEFAULT 1,
  created_by UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX webhook_subscriptions_url ON webhook_subscriptions (tenant_id, url) WHERE deleted_at IS NULL;

CREATE TABLE webhook_secrets (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  subscription_id UUID NOT NULL REFERENCES webhook_subscriptions(id),
  secret_ciphertext BYTEA NOT NULL, key_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ                                 -- set on rotation
);

-- Evidence: INSERT-only (BR-07). Payload frozen at fan-out.
CREATE TABLE webhook_deliveries (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  subscription_id UUID NOT NULL REFERENCES webhook_subscriptions(id),
  event_id UUID NOT NULL, event_type TEXT NOT NULL, event_version SMALLINT NOT NULL,
  seq BIGINT NOT NULL, payload JSONB NOT NULL,
  replay_of UUID REFERENCES webhook_deliveries(id), replayed_by UUID,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (subscription_id, seq)
);
CREATE UNIQUE INDEX webhook_deliveries_once ON webhook_deliveries (subscription_id, event_id) WHERE replay_of IS NULL;

CREATE TABLE webhook_delivery_attempts (               -- INSERT-only
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  delivery_id UUID NOT NULL REFERENCES webhook_deliveries(id),
  attempt SMALLINT NOT NULL CHECK (attempt BETWEEN 1 AND 7),
  started_at TIMESTAMPTZ NOT NULL, duration_ms INT NOT NULL,
  http_status INT,
  outcome TEXT NOT NULL CHECK (outcome IN ('success','http_error','timeout','connect_error','tls_error','ssrf_refused')),
  error TEXT CHECK (length(error) <= 500),
  response_excerpt TEXT CHECK (length(response_excerpt) <= 1024),
  PRIMARY KEY (delivery_id, attempt)
);

-- Mutable scheduling state (operational, not evidence).
CREATE TABLE webhook_queue (
  delivery_id UUID PRIMARY KEY REFERENCES webhook_deliveries(id),
  tenant_id UUID NOT NULL REFERENCES tenants(id),
  subscription_id UUID NOT NULL, seq BIGINT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending','retrying','held','delivered','dead','expired')),
  attempts SMALLINT NOT NULL DEFAULT 0,
  first_attempt_at TIMESTAMPTZ, next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX webhook_queue_due ON webhook_queue (next_attempt_at) WHERE status IN ('pending','retrying');
CREATE INDEX webhook_queue_head ON webhook_queue (subscription_id, seq) WHERE status IN ('pending','retrying','held');
```

- **RLS** on all five tables, `ENABLE` + `FORCE`, policy
  `FOR ALL USING (tenant_id = (NULLIF(current_setting('app.tenant_id', true), ''))::uuid)`.
- **Grants:** `REVOKE UPDATE, DELETE, TRUNCATE ON webhook_deliveries, webhook_delivery_attempts FROM atsapbx_app`
  plus the shared `atsapbx_forbid_mutation()` trigger (defined in `0061`,
  LLD-09 §RC-05) on both, which permits DELETE only for the retention role.
- **Worker role:** `atsap_webhook_worker` (`BYPASSRLS`, created by the
  deploy bootstrap like `atsap_outbox_worker`) with `SELECT, UPDATE` on
  `webhook_queue`, `webhook_subscriptions`; `INSERT, SELECT` on
  `webhook_deliveries`, `webhook_delivery_attempts`; `SELECT` on
  `webhook_secrets`. It needs cross-tenant claim; RPCs use the app role
  under RLS.
- **Retention:** `delivered` and `expired` rows (and their attempts) are
  purged after `webhook.retention_days`; `dead`/`held` likewise
  (PRD: "the tenant's retention window").

### 4.7 Ports (DoR 6) — `internal/webhooks/...`

```go
// domain (pure)
func ToPublic(ev sharedports.OutboxEvent, catalogue Catalogue) (PublicEvent, bool, error)
func Matches(sub Subscription, ev PublicEvent) bool
func Sign(secrets [][]byte, id string, ts time.Time, body []byte) string          // "v1,<b64> v1,<b64>"
func NextAttempt(first time.Time, retriesDone int, jitter float64) (time.Time, bool) // false = dead
func Classify(status int, err error) Outcome
func NextHealth(h Health, o Outcome, now time.Time) (Health, []HealthEvent)
func ValidateDestination(u *url.URL, addrs []netip.Addr, p SSRFPolicy) error

// ports
type SubscriptionStore interface {
    Create(ctx context.Context, s Subscription, secret []byte) error
    Get(ctx context.Context, id SubscriptionID) (Subscription, error)
    List(ctx context.Context, page Page) ([]Subscription, error)
    Update(ctx context.Context, s Subscription) error
    SetHealth(ctx context.Context, tx pgx.Tx, id SubscriptionID, h Health) error
}
type DeliveryStore interface {
    FanOut(ctx context.Context, ev PublicEvent) (int, error)              // step 5, idempotent
    ClaimDue(ctx context.Context, now time.Time, limit int) ([]Claimed, error)
    Complete(ctx context.Context, c Claimed, a Attempt, next QueueUpdate, h Health) error
    Replay(ctx context.Context, id DeliveryID, actor PrincipalID) (DeliveryID, error)
    History(ctx context.Context, f HistoryFilter, page Page) ([]DeliveryView, error)
}
type Sender interface { Send(ctx context.Context, r SignedRequest) (SendResult, error) }
type Resolver interface { LookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) }
type SecretBox interface { Seal(plain []byte) ([]byte, string, error); Open(ct []byte, keyID string) ([]byte, error) } // identity keyring
```

The `Sender` uses a dedicated `http.Client` (no cookies, no environment
proxy unless `webhook.proxy_url` is set, `MaxConnsPerHost 4`, custom
`DialContext` that re-checks the dialled IP so DNS rebinding cannot pass
step 8). The archtest `httpclient` rule already forbids default clients.

### 4.8 API contract (DoR 7) — `WebhookService` (public, tenant-scoped)

| RPC | Request → Response | Permission | Refusals (Connect code → console words) |
|---|---|---|---|
| `CreateWebhook` | `tenant_id, url, event_types[], description?, ordering?` → `webhook, secret (once)` | `webhook.manage` | `InvalidArgument` "That address is not allowed: use a public https URL." · `InvalidArgument` "Unknown event type: {type}." · `AlreadyExists` "A webhook for this URL already exists." · `ResourceExhausted` "You have reached the limit of {n} webhooks." |
| `ListWebhooks` | `tenant_id, page_size ≤ 100, page_token` → `webhooks[], next_page_token` | `webhook.read` | — |
| `GetWebhook` | `id` → `webhook` (state, failing_since, backlog counts) | `webhook.read` | `NotFound` "Webhook not found." (also for another tenant's id — INV-10) |
| `UpdateWebhook` | `id, url?, event_types?, description?, ordering?` (field mask) | `webhook.manage` | as Create; changing `ordering` while backlog > 0 → `FailedPrecondition` "Deliver or clear the backlog before changing ordering." |
| `DeleteWebhook` | `id` | `webhook.manage` | `NotFound` |
| `DisableWebhook` / `EnableWebhook` | `id` → `webhook` | `webhook.manage` | `FailedPrecondition` "Already {state}." |
| `RotateWebhookSecret` | `id` → `secret (once), old_expires_at` | `webhook.manage` | `NotFound` |
| `TestWebhook` | `id` → `attempt` (status, latency) — synchronous, one attempt, not retried, not counted toward health | `webhook.manage` | `DeadlineExceeded` "Your endpoint did not answer within 10 seconds." |
| `ListWebhookDeliveries` | `subscription_id?, status?, event_type?, from?, to?, page` → `deliveries[]` with attempts | `webhook.read` | `InvalidArgument` "Time range is limited to 31 days." |
| `ReplayWebhookDelivery` | `delivery_id` → `new_delivery_id` | `webhook.manage` | `FailedPrecondition` "This delivery is still being retried." · `NotFound` |
| `ReplayWebhookDeliveries` | `subscription_id, status ∈ {dead, held}, from, to` → `queued_count` (≤ 10,000) | `webhook.manage` | `ResourceExhausted` "More than 10,000 deliveries: narrow the range." |

Every mutation writes an identity audit entry (`webhook.create`,
`webhook.update`, `webhook.delete`, `webhook.disable`, `webhook.enable`,
`webhook.rotate_secret`, `webhook.replay`) naming the actor; the secret
never appears in an audit or log.

### 4.9 Configuration and limits (DoR 8)

| Key | Default | Range | Scope |
|---|---|---|---|
| `webhook.max_subscriptions` | 20 | 1–100 | tenant (installation cap) |
| `webhook.timeout_total` | 10 s | 2–30 s | installation |
| `webhook.timeout_connect` | 5 s | 1–10 s | installation |
| `webhook.max_payload_bytes` | 256 KiB | fixed | installation (larger events are refused at catalogue design time) |
| `webhook.retry_offsets` | 1m,5m,15m,1h,6h,24h | fixed in R1.0 | installation |
| `webhook.unhealthy_after` | 3 consecutive failures | fixed | — |
| `webhook.disable_after` | 24 h continuous failure | fixed | — |
| `webhook.secret_overlap` | 24 h | 1–168 h | installation |
| `webhook.retention_days` | 30 | 7–90 | tenant |
| `webhook.held_cap` | 100,000 rows per subscription | 10k–1M | installation; beyond it the oldest held rows become `expired` and an alert fires |
| `webhook.unordered_concurrency` | 4 in flight per subscription | 1–16 | subscription |
| `webhook.workers` | 32 senders | 4–256 | installation; per-tenant fair share = max(2, workers / active tenants) |
| `webhook.claim_interval` | 1 s | 0.2–5 s | installation |
| `webhook.allow_private_cidrs` | empty | CIDR list | installation (operator only, for on-prem partners) |
| `webhook.proxy_url` | unset | URL | installation |

### 4.10 Failure and recovery (DoR 9) — includes FL-05

| Failure | Behaviour |
|---|---|
| **FL-05 endpoint down / 5xx / timeout** | Retries per §4.5; `unhealthy` after 3 (≈5 min); `disabled` at 24 h; later events `held`; alert on each transition and when held backlog > 10,000; calls unaffected (INV-06) |
| Endpoint slow (> 10 s) | Attempt = timeout; ordered queue waits for the head; no thread is held beyond 10 s |
| Endpoint returns 2xx for some events, 4xx for one | Ordered: that event blocks the head → after 24 h the subscription disables; the partner sees the failing delivery and can fix and replay, or switch to `unordered` |
| DNS now resolves to a private IP | `ssrf_refused` attempt, counts as failure; logged with host only |
| Process crash mid-POST | Queue row was not completed → the claim lapses → re-sent (at-least-once; same `webhook-id`) |
| Crash between fan-out commit and NATS ack | Redelivered → unique index skips → acked |
| NATS down | Outbox retains rows (F-09); fan-out resumes on reconnect; ordering preserved because JetStream preserves stream order |
| NATS restarted without persistence | Published-but-unconsumed events are lost from the stream: production **must** run JetStream file storage on a volume (HLD-09 F-09). Recovery tool: `atsapbx outbox republish --since <t>` re-publishes retained outbox rows; fan-out dedupes |
| Postgres down | Sender and fan-out pause; nothing is lost (outbox) |
| Secret decryption fails (key rotated away) | No POST is made; the attempt is recorded as `connect_error` with error `secret_unavailable`; alert at high severity |
| Concurrent `EnableWebhook` + sender | Health update and queue transitions are under the subscription row lock |

**Ordering caveat (documented to partners):** order is JetStream stream
order per tenant, which is outbox insert order; two events from
concurrent transactions may commit in the opposite order to insert. Use
`sequence` to detect gaps and `occurred_at` for business order. A replay
is necessarily out of order.

### 4.11 Invariants, events, audit (DoR 10)

| Rule | How it holds |
|---|---|
| INV-06 | Fan-out and sender are NATS/DB workers with no caller on the call path; no call-path code imports `webhooks` (depguard) |
| INV-07 | Every console webhook screen uses `WebhookService` only |
| INV-08 / BR-07 | `webhook_deliveries` and attempts are INSERT-only (revoke + trigger); replays are new rows |
| INV-10 | RLS on every table; `NotFound` for another tenant's id; a subscription receives only its tenant's subject |
| INV-11 | Secret write-only, encrypted, redacted from logs; payload bodies never logged |

Internal events (outbox, not webhook-deliverable): `webhook.endpoint_unhealthy`,
`webhook.endpoint_disabled`, `webhook.endpoint_recovered`,
`webhook.backlog_truncated`. Metrics: `atsapbx_webhook_backlog`,
`_oldest_pending_age_seconds`, `_attempts_total{outcome}`,
`_latency_seconds`, `_dead_total`, `_subscriptions{state}`.

### 4.12 Test scenarios (DoR 11)

| # | Tier | Given / When / Then |
|---|---|---|
| W-01 | U | Given the Standard Webhooks published test vector, When `Sign` runs, Then the signature matches byte for byte |
| W-02 | U | Given a first attempt at T, When `NextAttempt` is called for retries 0..6, Then offsets are 1m,5m,15m,1h,6h,24h ±10 % and retry 6 returns `false` (dead) |
| W-03 | U | Given 3 consecutive failures, When `NextHealth` runs, Then state is `unhealthy`; Given then one success, Then `active` |
| W-04 | U | Given `http://169.254.169.254`, `http://10.0.0.1`, `http://[::1]`, `https://user@host`, `http://100.64.0.1`, When `ValidateDestination` runs, Then each is refused; a public https URL passes |
| W-05 | U | Given every catalogue row, When the schema test runs, Then each has a JSON Schema and its golden example validates; an internal-only event maps to `false` |
| W-06 | I | Given a subscription to `call.terminated`, When the outbox publishes one, Then exactly one delivery row exists and redelivering the NATS message adds none |
| W-07 | I | Given an ordered subscription whose endpoint fails, When three events fan out, Then only the head is attempted and seqs 2–3 wait |
| W-08 | I | Given a failing endpoint and a fake clock, When the clock passes +24 h, Then the head is `dead`, the subscription `disabled`, later rows `held`; When `EnableWebhook`, Then held rows deliver in seq order |
| W-09 | I | Given tenant A's delivery id, When tenant B calls `ReplayWebhookDelivery`, Then `NotFound` |
| W-10 | I | Given a delivery row, When the app role runs `UPDATE webhook_deliveries`, Then the statement fails (trigger and grant) |
| W-11 | I | Given a rotated secret, When a delivery is signed within the overlap, Then two signatures are present and either verifies |
| W-12 | E | Given a local receiver verifying HMAC, When a real call ends, Then `call.terminated` arrives within 2 s of the state change (AC-05.2) |
| W-13 | E | Given the receiver returns 500, When a call ends, Then the call still completes normally and the delivery shows `retrying` (INV-06) |
| W-14 | C | Given the console Webhooks page, When an admin creates a webhook, Then the secret is shown once with a copy button and never again |
| W-15 | B | Given a disabled webhook, When the admin opens it, Then the console shows "Disabled after 24 hours of failures", the held count and an Enable button; Enable is audited |
| W-16 | A | Given a partner's receiver, When it is stopped for 10 minutes and restarted, Then the partner sees `unhealthy`, then recovery, and every event arrives once in sequence order (UAT, product-owner) |

## 5. Observability and security

- Logs carry subscription id, delivery id, attempt, outcome, host — never
  the body, the secret, or the full URL query string.
- Alerts: subscription enters `unhealthy` (warning) / `disabled`
  (critical); oldest pending > 15 min; held backlog > 10,000.
- The outbound client never follows redirects, never sends cookies, and
  re-validates the dialled IP (rebinding).

## 6. IN-03 — Public event stream

### 6.1 Transport choice

| Option | Verdict |
|---|---|
| **ConnectRPC server-streaming** `EventService.StreamEvents` | **Chosen.** One handler serves gRPC (FBR-R1-11 names "gRPC streams"), gRPC-Web and the Connect protocol; the console uses it through `@connectrpc/connect-web` fetch streaming; same auth interceptor, same proto contract, same origin (D-69) |
| SSE | Rejected: a second contract style (text/event-stream) outside the proto, and browser `EventSource` cannot send an `Authorization` header |
| WebSocket | Rejected: bidirectional is not needed; a second auth and framing path to secure |

### 6.2 Flow (DoR 1–3)

| Item | Value |
|---|---|
| Trigger / actor | Partner developer (API key with `event.stream`) or the console (session JWT) opens `StreamEvents` |
| Preconditions | Authenticated; permission `event.stream`; tenant concurrent-stream cap not reached; cursor (if any) decrypts and belongs to the tenant |

| # | Step |
|---|---|
| 1 | Interceptor authenticates; handler checks `event.stream` |
| 2 | Decode cursor: AES-256-GCM-sealed `{stream_seq, tenant_id, issued_at}` under `events.cursor_key` (opaque; global sequence numbers would leak other tenants' volume — INV-10) |
| 3 | Create an **ordered ephemeral JetStream consumer**, filter `tenant.<id>.event.>`, `DeliverByStartSequence(stream_seq+1)` or `DeliverNew` |
| 4 | For each message: `ToPublic` + type filter (globs like `call.*`), send `StreamEventsResponse{cursor, event}` |
| 5 | Every 15 s with no event: send a heartbeat `{cursor}` |
| 6 | Every 60 s: re-validate the credential and permission (never cached, D-56); revoked → close `Unauthenticated`/`PermissionDenied` |
| 7 | Client disconnects → consumer deleted; the client reconnects with its last cursor |

### 6.3 Contract

`StreamEvents(StreamEventsRequest{tenant_id, types[] (≤ 32 globs), cursor?, include_telemetry (default false)}) returns (stream StreamEventsResponse{cursor, oneof {event, heartbeat}})`

| Refusal | Code | Console words |
|---|---|---|
| Cursor older than stream retention / NATS restarted | `OutOfRange` | "Live updates restarted. Reloading the latest data." (client then re-reads via List RPCs and reopens without cursor) |
| Cursor from another tenant / tampered | `InvalidArgument` | "Invalid resume position." |
| Too many streams | `ResourceExhausted` | "Too many live connections for this organisation." |
| Client too slow | `ResourceExhausted` (stream closed) | — client resumes from last cursor |

### 6.4 Configuration, backpressure, failure

| Key | Default | Range | Scope |
|---|---|---|---|
| `events.stream_max_age` (JetStream `MaxAge`) | 72 h | 24 h–7 d | installation |
| `events.max_streams` | 10 per tenant | 1–100 | tenant |
| `events.send_buffer` | 1,000 events | 100–10,000 | installation |
| `events.max_lifetime` | 24 h (then graceful close with cursor) | 1–72 h | installation |
| `events.heartbeat` | 15 s | 5–60 s | installation |
| `events.reauth_interval` | 60 s | 15–300 s | installation |

- **Backpressure:** the server never blocks the JetStream consumer behind
  a slow client and never silently drops: when the per-stream buffer is
  full the stream closes with `ResourceExhausted`; the client resumes from
  its cursor and loses nothing still within `stream_max_age`.
- **Failure:** NATS down → open streams receive heartbeats only, then
  `Unavailable` after 30 s; app restart → clients reconnect with cursor;
  telemetry (`participant.quality`) is included only when
  `include_telemetry` is set.
- **Invariants and audit:** INV-10 (subject filter is server-built from
  the authenticated tenant, never from the request; cursor sealed);
  INV-06 (the stream is a NATS consumer, never on the call path); D-56
  (no cached authorization). Opening a stream is not an audit event
  (it reads nothing a List RPC could not); a refusal for another
  tenant's cursor is logged as a security event. No aggregate state:
  the cursor is the only state and it lives with the client.

### 6.5 Tests and parity

| # | Tier | Given / When / Then |
|---|---|---|
| S-01 | U | Given a cursor sealed for tenant A, When tenant B presents it, Then `InvalidArgument` |
| S-02 | I | Given 5 events, When a client reads 3, disconnects and resumes with the third cursor, Then it receives exactly events 4 and 5 |
| S-03 | I | Given a client that stops reading, When 1,001 events arrive, Then the stream closes `ResourceExhausted` and other tenants' streams are unaffected |
| S-04 | I | Given an API key revoked mid-stream, When the 60 s re-check runs, Then the stream closes `Unauthenticated` |
| S-05 | E | Given a console session, When a call is answered, Then the call-history page updates within 2 s (AC-05.2) without polling |
| S-06 | B | Given a NATS restart, When the console's cursor is out of range, Then the page reloads data and shows no error dialog |

Parity: 3CX exposes a WebSocket event feed; Amazon Connect streams agent
events and CTRs through Kinesis with consumer-managed checkpoints. Our
cursor is the checkpoint; our transport is gRPC/Connect so partners get a
typed client from the published proto.

## 7. Owner decisions

1. **OWNER DECISION — payload PII.** `from`/`to` numbers are in
   `call.*` and `cdr.recorded` because partners bill from them.
   *Recommended:* include them; the partner agreement names the partner as
   processor; a per-subscription `redact_numbers` flag (last 4 digits
   only) is offered for non-billing integrations.
2. **OWNER DECISION — production NATS persistence.** Durable consumers
   (webhooks, the CDR ledger) make JetStream state load-bearing.
   *Recommended:* file storage on a volume in production, 72 h `MaxAge`,
   plus the outbox `republish` tool; dev compose may stay ephemeral.
3. Max subscriptions per tenant: 20 (resolved as the default above,
   changeable by config).
4. Webhook retention: 30 days for all statuses (resolved above; PRD's
   "tenant's retention window").

**References:** [PRD EPIC-05](../PRD.md), [HLD 04 §8](../hld/04-bounded-contexts.md),
[HLD-09](../hld/09-failure-model.md), [HLD-21 §8](../hld/21-media-and-call-control-flows.md),
[Standard Webhooks](https://www.standardwebhooks.com/), [3CX](https://www.3cx.com/),
[VitalPBX](https://wiki.vitalpbx.com/), [FusionPBX](https://docs.fusionpbx.com/en/latest/),
[Amazon Connect contact events](https://docs.aws.amazon.com/connect/latest/adminguide/contact-events.html).
