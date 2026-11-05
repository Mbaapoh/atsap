# LLD-05 — Webhook Delivery

| | |
|---|---|
| **Bounded context** | `webhook-delivery` (Tier 1, HLD 04 §10.1) |
| **Status** | **Draft — design only.** The transactional outbox and NATS worker already exist; no partner webhook endpoint ships yet |
| **Traces to** | BRD FBR-R1-11; PRD EPIC-05 (US-05.2/05.5, AC-05.2/05.3), INV-06/INV-07; HLD 01 §3.2, HLD 04 §10.1; DECISIONS D-57 |
| **Why this LLD** | The API is the product (INV-07). Partners need reliable, signed events without polling the database |

## 1. Scope and non-goals

**In scope:** tenant-owned webhook subscriptions; event selection; signed
HTTP POST delivery; at-least-once with exponential retry; dead-letter and
manual replay; delivery history/observability; SSRF-safe destinations;
secret rotation.

**Not in scope:** the event schema of each producer (owned by its context),
a live telemetry WebSocket (a separate streaming API), a message broker
for partners, or synchronous callbacks into the call path.

**Hard invariant:** delivery is **asynchronous and never blocks call
processing** (PRD INV-06). A dead partner endpoint must have no effect on
a call, a CDR, or an API response.

## 2. Prior art (2026-11-04)

| Source | What it establishes | What we adopt |
|---|---|---|
| [3CX](https://www.3cx.com/) | A mature PBX exposes call activity and integrations around its call control | Events are a public product surface, not an internal log |
| [VitalPBX](https://wiki.vitalpbx.com/) | REST APIs and integrations are expected in a modern PBX | Versioned public API, per-tenant credentials |
| [Vodia](https://web.vodia.com/features) | Reporting, CDR and external integrations are part of the platform | Event payloads reference platform ids, never Asterisk channels |
| [FusionPBX](https://docs.fusionpbx.com/en/latest/) | CDR/XML event plumbing is common | The platform owns the durable event record; the engine's XML is not contract |

Most PBX webhook systems stop at "POST and retry". AtsaPBX adds **signed
payloads, a dead-letter queue, manual replay and tenant-scoped delivery
history** because AC-05.3 requires them.

## 3. Data model

```sql
webhook_subscriptions (
  id UUID PRIMARY KEY, tenant_id UUID NOT NULL,
  url TEXT NOT NULL, secret_ciphertext BYTEA NOT NULL,
  events TEXT[] NOT NULL, description TEXT,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at, updated_at, disabled_at TIMESTAMPTZ,
  UNIQUE (tenant_id, url)
);
webhook_deliveries (
  id UUID PRIMARY KEY, tenant_id UUID NOT NULL,
  subscription_id UUID NOT NULL, event_id UUID NOT NULL,
  event_type TEXT NOT NULL, payload JSONB NOT NULL,
  status TEXT NOT NULL,           -- pending | delivered | retrying | dead
  attempts INT NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ, last_status INT, last_error TEXT,
  created_at, delivered_at TIMESTAMPTZ,
  UNIQUE (subscription_id, event_id)
);
webhook_delivery_attempts (
  delivery_id UUID NOT NULL, attempt INT NOT NULL,
  at TIMESTAMPTZ NOT NULL, status INT, latency_ms INT, error TEXT,
  PRIMARY KEY (delivery_id, attempt)
);
```

All tables are RLS-scoped. `payload` is the public event body; it never
contains a channel, endpoint, password or token. `secret_ciphertext` uses
the identity keyring (D-66); it is never returned by any read.

## 4. Event contract

The existing outbox worker publishes domain events to NATS. A webhook
fan-out consumer reads the same event, filters by subscriptions and writes
one `webhook_deliveries` row per matching subscription **in the same
transaction as the fan-out decision**.

Envelope:

```json
{
  "id": "uuid", "type": "call.terminated", "version": 1,
  "tenant_id": "uuid", "occurred_at": "RFC3339",
  "data": { ... public fields only ... }
}
```

Rules:
- Event names are stable and documented in API.md; adding a field is
  compatible, removing/changing one needs a version.
- At-least-once; consumers must use `id` for idempotency.
- Ordering is **not** promised across events; a partner may receive
  `call.terminated` after a later event type. Per-aggregate ordering is
  best-effort only.
- `data` starts with `call.*`, `extension.*`, `trunk.*`, `report.export`
  and later `campaign.*`, `recording.*`; no event is emitted for a
  configuration read.

## 5. Delivery algorithm

1. Select due rows (`status IN (pending,retrying) AND next_attempt_at <= now`)
   with `FOR UPDATE SKIP LOCKED`, bounded batch.
2. Sign `timestamp + "." + raw_body` with HMAC-SHA256 using the
   subscription secret; headers `X-Atsa-Event`, `X-Atsa-Delivery`,
   `X-Atsa-Signature: v1=<hex>`, `X-Atsa-Timestamp`.
3. POST with a 10-second timeout and a redirect policy of **no
   redirects**. A non-2xx or timeout is a failed attempt.
4. Retry schedule: 10 s, 30 s, 2 min, 10 min, 30 min, 2 h, 6 h, 24 h;
   maximum 24 h / 12 attempts, then `dead`.
5. On success write the attempt and set `delivered`; on failure keep the
   payload for replay.
6. A manual replay creates a new delivery row (new id) from the same
   payload, audited with the actor.
7. A subscription disabled or deleted stops new fan-out; existing rows
   are retried only if the operator replays them.

**SSRF protection:** only `http`/`https`, no userinfo, no link-local,
loopback, private (RFC1918), CGNAT, multicast or metadata addresses;
resolve and re-check the destination immediately before each attempt.
Redirects are refused. Optional allowlist by partner CIDR.

## 6. API

`WebhookService` (public, tenant-scoped):
- `CreateWebhook`, `ListWebhooks`, `UpdateWebhook`, `DeleteWebhook`
- `RotateWebhookSecret` (returned once, never read back)
- `ListWebhookDeliveries` (paged, status/event/time filters)
- `ReplayWebhookDelivery`
- `TestWebhook` (sends a signed `webhook.test` event)

Every mutation is audited. The secret is only returned by create/rotate,
matching the extension/API-key contract. There is no endpoint that returns
another tenant's delivery.

## 7. Failure, observability and security

- Delivery is best-effort against the partner, **durable** on our side:
  a database row exists before the first POST.
- Metrics: due backlog, attempts, success rate by status class, p95
  latency, dead count, oldest pending age. Alerts when the dead count
  rises or backlog age exceeds 15 min.
- Logs carry delivery/subscription ids, never payload bodies or secrets.
- A partner endpoint outage cannot affect calls (INV-06): the fan-out
  consumer is separate from the call path and has no synchronous caller.
- Outbound requests use a dedicated HTTP client with no cookies, no
  environment proxy unless configured, and a bounded connection pool.

## 8. Tests and Definition of Done

- Unit: signing vector, retry schedule, event filtering, SSRF validator,
  idempotency key.
- Integration: outbox → fan-out → delivery rows → retry → dead → replay;
  RLS cross-tenant; disabled subscription; secret rotation.
- Live: a local receiver verifies HMAC and receives a call event within
  AC-05.2's 2 s (excluding retry); a 500 endpoint retries and dead-letters;
  a replay is audited; a call still completes with the receiver down.
- DoD: AC-05.2/05.3 proven, delivery history visible over the API, no
  synchronous impact on calls.

## 9. Open decisions for the owner

1. Default maximum subscriptions per tenant (proposed 20).
2. Whether partners may subscribe to `report.export` and usage events in
   R1.0 (proposed yes for usage; export only via pull).
3. Payload PII: call numbers are included (partners bill from them);
   confirm the partner agreement covers this.
4. Retention of delivery payloads after success (proposed 30 days; dead
   90 days).

**References:** [PRD EPIC-05](../PRD.md), [HLD 01 §3.2](../hld/01-architecture.md),
[HLD 04 §10.1](../hld/04-bounded-contexts.md), [TRD](../TRD.md),
[3CX](https://www.3cx.com/), [VitalPBX](https://wiki.vitalpbx.com/),
[FusionPBX](https://docs.fusionpbx.com/en/latest/).
