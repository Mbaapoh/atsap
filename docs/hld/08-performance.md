<!-- OpenSpec: TRD-HLD-09 -->
# Performance & Scalability

## 1. Concurrency Targets & Latency SLOs (BRD §7.1, DECISIONS D-08)

AtsaPBX is architected to operate efficiently on a single modest server while supporting horizontal clustering across regional media nodes:

| Dimension / Metric | Single Node Target (Launch) | Multi-Node Regional Target (Year 3, D-08) | Verification Method |
|---|---|---|---|
| **Concurrent Streams** | 500 active WebRTC/SIP channels | 50,000 channels (150–250 media nodes) | Distributed SIPp load generator |
| **Call Setup Rate** | 10 CPS sustained, 30 CPS burst | 1,000+ CPS aggregate | SIPp dial storm testing |
| **Internal Setup Latency** | P95 < 1.0s | P95 < 1.0s | End-to-end WebRTC call harness |
| **External Setup Latency** | P95 < 1.5s (excluding carrier) | P95 < 1.5s | Carrier mock harness |
| **One-Way Audio Latency** | P95 < 150 ms (intra-region) | P95 < 200 ms (three-way specialist) | Injected acoustic loopback |
| **API Latency (ConnectRPC)** | P95 < 500 ms, P99 < 1000 ms | P95 < 500 ms | k6 / ghz gRPC load tests |
| **Live Telemetry Latency** | UI updates within 2 seconds | UI updates within 2 seconds | WebSocket event latency probe |

---

## 2. High-Throughput Transactional Outbox Worker

To decouple high-frequency telephony events from persistence and network I/O, the Transactional Outbox worker is designed for concurrent, lock-free draining:

```text
[telephony-core / pbx-core Transactions]
           │
           │ INSERT INTO outbox (...)  (Within application DB transaction)
           ▼
┌────────────────────────────────────────────────────────────────────────┐
│                          outbox Table                                  │
│  (Indexed on: published_at WHERE published_at IS NULL)                 │
└────────────────────────────────────────────────────────────────────────┘
       ▲                     ▲                     ▲
       │                     │                     │
  Worker Thread 1       Worker Thread 2       Worker Thread N
       │                     │                     │
       └─────────────────────┼─────────────────────┘
                             │
                             │ SELECT id FROM outbox
                             │ WHERE published_at IS NULL
                             │ ORDER BY id
                             │ FOR UPDATE SKIP LOCKED
                             │ LIMIT 100;
                             ▼
               [Publish to NATS JetStream]
                             │
                             │ (On NATS Ack)
                             ▼
               UPDATE outbox SET published_at = NOW() WHERE id IN (...)
```

### 2.1 Outbox Optimization Invariants

1. **`FOR UPDATE SKIP LOCKED`:** Multiple worker goroutines drain the queue simultaneously without lock contention or duplicate publishing.
2. **Batching:** Events are read and committed in batches of 100 items, reducing database round-trips by 90%.
3. **Table Pruning:** A daily cron job deletes or truncates published rows older than 7 days (`DELETE FROM outbox WHERE published_at < NOW() - INTERVAL '7 days'`).

---

## 3. Database Partitioning & Query Tuning

High call volumes generate tens of millions of records per month. AtsaPBX implements PostgreSQL 16 declarative **declarative range partitioning** by month for append-only tables:

```sql
-- Partitioned by Month on second_ts
CREATE TABLE usage_seconds_partitioned (
    tenant_id UUID NOT NULL,
    participant_id UUID NOT NULL,
    call_id UUID NOT NULL,
    second_ts TIMESTAMPTZ NOT NULL,
    direction VARCHAR(20) NOT NULL,
    carrier_id UUID,
    cost_micros BIGINT NOT NULL DEFAULT 0,
    revenue_micros BIGINT NOT NULL DEFAULT 0,
    quality_mos NUMERIC(3, 2) NOT NULL DEFAULT 4.50,
    quality_jitter NUMERIC(6, 2) NOT NULL DEFAULT 0.0,
    quality_loss NUMERIC(5, 2) NOT NULL DEFAULT 0.0,
    ai_streamed BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (participant_id, second_ts)
) PARTITION BY RANGE (second_ts);

-- Partition creation for 2026-09
CREATE TABLE usage_seconds_2026_09 PARTITION OF usage_seconds_partitioned
    FOR VALUES FROM ('2026-09-01 00:00:00+00') TO ('2026-10-01 00:00:00+00');
```

Similar partitioning applies to `channel_history` and `audit_logs`.

### 3.1 Connection Pooling (`pgxpool`)

The Go application maintains a bounded pool:
- `MaxConns: 50` per node.
- `MinConns: 10`.
- `MaxConnIdleTime: 5m`.
- Automatic statement preparation caching (`pgx.QueryExecModeCacheStatement`).

---

## 4. In-Memory Caching & Lock-Free Read State

1. **PBX Routing & Flow Cache:**
   - Active extension directories, carrier LCR tables, and published IVR flow graphs are cached in memory using lock-free read structures (`sync.Map` or atomic pointers).
   - Invalidation is event-driven: when an admin edits an IVR flow, `pbx-core` publishes `event.pbx.flow.published` over NATS, which purges local cache copies across all core instances within 100 ms.
2. **Atomic Capacity Counting:**
   - Active channel counts per tenant are tracked in atomic memory counters (`atomic.Int64`) synchronized with Redis or periodic PostgreSQL reconciliation, eliminating DB locking overhead on call setup.

---

## 5. Verification & Performance Acceptance Framework

1. **CPS Storm Benchmark:** Uses SIPp to blast 30 CPS for 10 minutes against carrier ingress; validates that P95 setup latency stays below 1.5s, no memory leaks occur, and zero SIP errors are generated.
2. **500 Concurrent WebRTC Soak Test:** Sustains 500 active audio sessions for 4 hours with background IVR and DTMF interaction; validates that CPU remains under 75% on a 4-vCPU node, one-way delay stays under 150 ms, and outbox lag stays under 5 seconds.
3. **Outbox Drain Test:** Generates 50,000 outbox records while simulating temporary NATS disconnections; asserts that all 50,000 records are drained in order with exactly zero lost events upon reconnection.


