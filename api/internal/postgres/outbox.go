package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	sharedports "atsap-api/internal/shared/ports"
)

// OutboxWorker reads unpublished outbox rows across every tenant and
// publishes them. It connects with the narrow BYPASSRLS
// atsap_outbox_worker role (docs/hld/03-domain-model.md §5) — cross-
// tenant reads on the outbox table only are exactly what that role is
// scoped to, nothing else; pool here must be opened with that role's
// credentials, not the app's own atsapbx_app connection.
type OutboxWorker struct {
	pool      *pgxpool.Pool
	publisher sharedports.OutboxPublisher
	logger    *slog.Logger
	batchSize int
}

// NewOutboxWorker returns a worker with a sane default batch size (100,
// matching docs/hld/01-architecture.md §3.2's example). pool must be
// opened with the atsap_outbox_worker role's credentials.
func NewOutboxWorker(pool *pgxpool.Pool, publisher sharedports.OutboxPublisher, logger *slog.Logger) *OutboxWorker {
	return &OutboxWorker{pool: pool, publisher: publisher, logger: logger, batchSize: 100}
}

// PollOnce claims up to one batch of unpublished rows (SELECT ... FOR
// UPDATE SKIP LOCKED, so multiple worker instances never claim the same
// row), publishes each, and marks published ones published_at — all in
// one transaction. A publish failure for one row is logged and that row
// is left unpublished for the next poll (at-least-once delivery); it
// does not abort the whole batch.
//
// Trade-off accepted for this walking skeleton: the NATS publish call
// happens while the claiming transaction is still open, so a slow or
// stuck publisher holds the SKIP LOCKED lock for that long. Acceptable
// here — NATS publish is normally sub-millisecond to a local/nearby
// broker — but a claim-then-publish-then-finalize three-phase design
// would be the fix if publish latency ever becomes a real concern.
func (w *OutboxWorker) PollOnce(ctx context.Context) (published int, err error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin outbox poll: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id, tenant_id, event_type, aggregate_id, payload
		FROM outbox
		WHERE published_at IS NULL
		ORDER BY created_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT $1
	`, w.batchSize)
	if err != nil {
		return 0, fmt.Errorf("query outbox: %w", err)
	}

	var batch []sharedports.OutboxEvent
	for rows.Next() {
		var ev sharedports.OutboxEvent
		if err := rows.Scan(&ev.ID, &ev.TenantID, &ev.EventType, &ev.AggregateID, &ev.Payload); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan outbox row: %w", err)
		}
		batch = append(batch, ev)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate outbox rows: %w", err)
	}

	for _, ev := range batch {
		if err := w.publisher.Publish(ctx, ev); err != nil {
			w.logger.Error("outbox: publish failed, will retry next poll",
				"error", err, "outbox_id", ev.ID, "event_type", ev.EventType)
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE outbox SET published_at = NOW() WHERE id = $1`, ev.ID); err != nil {
			w.logger.Error("outbox: mark published failed", "error", err, "outbox_id", ev.ID)
			continue
		}
		published++
	}

	if err := tx.Commit(ctx); err != nil {
		return published, fmt.Errorf("commit outbox poll: %w", err)
	}
	return published, nil
}

// Run polls every interval until ctx is cancelled. The caller owns the
// goroutine — Run blocks until ctx.Done().
func (w *OutboxWorker) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := w.PollOnce(ctx); err != nil {
				w.logger.Error("outbox: poll failed", "error", err)
			}
		}
	}
}
