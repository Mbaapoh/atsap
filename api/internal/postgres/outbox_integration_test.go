//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// workerRoleURL connects as the atsap_outbox_worker BYPASSRLS role
// (deploy/postgres/init/02-atsapbx.sql), matching how the worker
// actually runs in production — never the app's own atsapbx_app
// connection.
func workerRoleURL() string {
	return "postgres://atsap_outbox_worker:devpassword123@localhost:15432/atsapbx?sslmode=disable"
}

func openWorkerPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), workerRoleURL())
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, pool.Ping(context.Background()))
	return pool
}

func seedTenantDirect(t *testing.T, pool *postgres.Pool, tenantID shareddomain.TenantID) {
	t.Helper()
	_, err := pool.Unwrap().Exec(context.Background(),
		`INSERT INTO tenants (id, name) VALUES ($1, $2)`, tenantID.String(), "test-tenant")
	require.NoError(t, err)
}

func insertOutboxRowDirect(t *testing.T, pool *postgres.Pool, tenantID shareddomain.TenantID, eventType, aggregateID string) {
	t.Helper()
	err := pool.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO outbox (id, tenant_id, event_type, aggregate_id, payload)
			VALUES (gen_random_uuid(), $1, $2, $3, '{}'::jsonb)
		`, tenantID.String(), eventType, aggregateID)
		return err
	})
	require.NoError(t, err)
}

type fakePublisher struct {
	mu        sync.Mutex
	published []postgres.OutboxEvent
	failAll   error
}

func (f *fakePublisher) Publish(_ context.Context, ev postgres.OutboxEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll != nil {
		return f.failAll
	}
	f.published = append(f.published, ev)
	return nil
}

func (f *fakePublisher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.published)
}

func TestOutboxWorker_PollOnce_PublishesAndMarks(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL, false)
	ctx := context.Background()

	appPool, err := postgres.Open(ctx, databaseURL)
	require.NoError(t, err)
	defer appPool.Close()

	tenantID := shareddomain.NewTenantID()
	seedTenantDirect(t, appPool, tenantID)

	aggregateID := shareddomain.NewCallID().String()
	insertOutboxRowDirect(t, appPool, tenantID, "call.initiated", aggregateID)

	workerPool := openWorkerPool(t)
	publisher := &fakePublisher{}
	worker := postgres.NewOutboxWorker(workerPool, publisher, discardLogger())

	published, err := worker.PollOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, published)
	require.Len(t, publisher.published, 1)
	assert.Equal(t, "call.initiated", publisher.published[0].EventType)
	assert.Equal(t, aggregateID, publisher.published[0].AggregateID)
	assert.Equal(t, tenantID.String(), publisher.published[0].TenantID)

	// A second poll finds nothing left — the row is marked published.
	published, err = worker.PollOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, published)
	assert.Equal(t, 1, publisher.count(), "no re-publish of an already-published row")
}

func TestOutboxWorker_PollOnce_PublishFailure_LeftUnpublished(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL, false)
	ctx := context.Background()

	appPool, err := postgres.Open(ctx, databaseURL)
	require.NoError(t, err)
	defer appPool.Close()

	tenantID := shareddomain.NewTenantID()
	seedTenantDirect(t, appPool, tenantID)
	insertOutboxRowDirect(t, appPool, tenantID, "call.initiated", shareddomain.NewCallID().String())

	workerPool := openWorkerPool(t)
	publisher := &fakePublisher{failAll: errors.New("nats unreachable")}
	worker := postgres.NewOutboxWorker(workerPool, publisher, discardLogger())

	published, err := worker.PollOnce(ctx)
	require.NoError(t, err, "a per-row publish failure must not fail the whole poll")
	assert.Equal(t, 0, published)

	// Retrying with a working publisher succeeds — the row was left
	// unpublished, not lost or skipped forever.
	publisher.failAll = nil
	published, err = worker.PollOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, published)
}

// TestOutboxWorker_ConcurrentPolls_NoDoubleProcessing proves FOR UPDATE
// SKIP LOCKED: two worker instances polling concurrently against the
// same rows never both claim the same row.
func TestOutboxWorker_ConcurrentPolls_NoDoubleProcessing(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL, false)
	ctx := context.Background()

	appPool, err := postgres.Open(ctx, databaseURL)
	require.NoError(t, err)
	defer appPool.Close()

	tenantID := shareddomain.NewTenantID()
	seedTenantDirect(t, appPool, tenantID)

	const n = 20
	for range n {
		insertOutboxRowDirect(t, appPool, tenantID, "call.initiated", shareddomain.NewCallID().String())
	}

	pool1 := openWorkerPool(t)
	pool2 := openWorkerPool(t)
	pub1 := &fakePublisher{}
	pub2 := &fakePublisher{}
	worker1 := postgres.NewOutboxWorker(pool1, pub1, discardLogger())
	worker2 := postgres.NewOutboxWorker(pool2, pub2, discardLogger())

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = worker1.PollOnce(ctx) }()
	go func() { defer wg.Done(); _, _ = worker2.PollOnce(ctx) }()
	wg.Wait()

	total := pub1.count() + pub2.count()
	assert.Equal(t, n, total, "every row published exactly once across both concurrent workers")

	seen := make(map[string]bool)
	for _, ev := range append(append([]postgres.OutboxEvent{}, pub1.published...), pub2.published...) {
		assert.False(t, seen[ev.ID], "row %s claimed by both workers", ev.ID)
		seen[ev.ID] = true
	}
}
