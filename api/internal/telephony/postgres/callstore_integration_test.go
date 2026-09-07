//go:build integration

package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/shared/event"
	"atsap-api/internal/telephony/domain"
	"atsap-api/internal/telephony/ports"
	telephonypostgres "atsap-api/internal/telephony/postgres"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://atsapbx_app:devpassword123@localhost:15432/atsapbx?sslmode=disable"
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "migrations")
}

func resetSchema(t *testing.T, databaseURL string) {
	t.Helper()
	_ = corepostgres.MigrateDownAll(databaseURL, migrationsDir(t))
	require.NoError(t, corepostgres.MigrateUp(databaseURL, migrationsDir(t), false))
	t.Cleanup(func() {
		_ = corepostgres.MigrateDownAll(databaseURL, migrationsDir(t))
	})
}

func seedTenant(t *testing.T, pool *corepostgres.Pool, tenantID shareddomain.TenantID) {
	t.Helper()
	_, err := pool.Unwrap().Exec(context.Background(),
		`INSERT INTO tenants (id, name) VALUES ($1, $2)`, tenantID.String(), "test-tenant")
	require.NoError(t, err)
}

func newTestCall(t *testing.T, tenantID shareddomain.TenantID) (*domain.Call, *domain.CallParticipant, *domain.CallParticipant) {
	t.Helper()
	now := time.Now().UTC()
	call := domain.NewCall(shareddomain.NewCallID(), tenantID, domain.Outbound, "1000", "1001", now)
	caller := &domain.CallParticipant{ID: shareddomain.NewParticipantID(), CallID: call.ID, TenantID: tenantID, Role: domain.RoleCaller, EndpointURI: "PJSIP/1000"}
	callee := &domain.CallParticipant{ID: shareddomain.NewParticipantID(), CallID: call.ID, TenantID: tenantID, Role: domain.RoleAgent, EndpointURI: "PJSIP/1001"}
	require.NoError(t, call.AddParticipant(caller))
	require.NoError(t, call.AddParticipant(callee))
	return call, caller, callee
}

// TestCallStore_SaveAndGetCall_RoundTrip proves the adapter persists and
// reconstructs a Call with its participants correctly.
func TestCallStore_SaveAndGetCall_RoundTrip(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)
	ctx := context.Background()

	pool, err := corepostgres.Open(ctx, databaseURL)
	require.NoError(t, err)
	defer pool.Close()

	tenantID := shareddomain.NewTenantID()
	seedTenant(t, pool, tenantID)

	store := telephonypostgres.NewCallStore(pool)
	call, caller, callee := newTestCall(t, tenantID)

	require.NoError(t, call.TransitionToScreening())
	_, err = call.ApplyScreeningVerdict(true, true, time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, call.TransitionToPresenting())
	require.NoError(t, store.SaveCall(ctx, call, nil))

	_, err = call.ConnectParticipant(caller.ID, time.Now().UTC())
	require.NoError(t, err)
	events, err := call.ConnectParticipant(callee.ID, time.Now().UTC())
	require.NoError(t, err)
	require.NotEmpty(t, events)
	require.NoError(t, store.SaveCall(ctx, call, nil))

	got, err := store.GetCall(ctx, tenantID, call.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, domain.CallActive, got.State)
	assert.Equal(t, "1000", got.SourceNumber)
	assert.Equal(t, "1001", got.DestNumber)
	require.Len(t, got.Participants, 2)
	for _, p := range got.Participants {
		assert.Equal(t, domain.ParticipantConnected, p.State)
		assert.NotNil(t, p.AnsweredAt)
	}
}

// TestCallStore_SaveCall_WritesOutboxTransactionally is task 7.1's
// positive case: an event passed to SaveCall produces exactly one outbox
// row, correctly addressed and unpublished, in the same call that
// persisted the domain state.
func TestCallStore_SaveCall_WritesOutboxTransactionally(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)
	ctx := context.Background()

	pool, err := corepostgres.Open(ctx, databaseURL)
	require.NoError(t, err)
	defer pool.Close()

	tenantID := shareddomain.NewTenantID()
	seedTenant(t, pool, tenantID)

	store := telephonypostgres.NewCallStore(pool)
	call, _, _ := newTestCall(t, tenantID)

	require.NoError(t, store.SaveCall(ctx, call, []event.DomainEvent{call.InitiatedEvent()}))

	var eventType, aggregateID string
	var publishedAt *time.Time
	err = pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT event_type, aggregate_id, published_at FROM outbox WHERE tenant_id = $1
		`, tenantID.String()).Scan(&eventType, &aggregateID, &publishedAt)
	})
	require.NoError(t, err)
	assert.Equal(t, "call.initiated", eventType)
	assert.Equal(t, call.ID.String(), aggregateID)
	assert.Nil(t, publishedAt, "unpublished until the outbox worker (task 7.2) picks it up")
}

// unmarshalableEvent has a field encoding/json cannot serialize, used
// only to force SaveCall's outbox-insert step to fail after the domain
// writes have already run in the same transaction.
type unmarshalableEvent struct {
	Ch chan int
}

func (unmarshalableEvent) EventType() string { return "test.unmarshalable" }

// TestCallStore_SaveCall_EventFailureRollsBackDomainWrite is task 7.1's
// core gate: the domain write and the outbox write commit or roll back
// together. Forcing the outbox insert to fail (a JSON-unmarshalable
// event payload) after the calls/call_participants upserts have already
// run in the same transaction proves the whole transaction — including
// the already-executed domain writes — rolls back, not just the failing
// statement.
func TestCallStore_SaveCall_EventFailureRollsBackDomainWrite(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)
	ctx := context.Background()

	pool, err := corepostgres.Open(ctx, databaseURL)
	require.NoError(t, err)
	defer pool.Close()

	tenantID := shareddomain.NewTenantID()
	seedTenant(t, pool, tenantID)

	store := telephonypostgres.NewCallStore(pool)
	call, _, _ := newTestCall(t, tenantID)

	saveErr := store.SaveCall(ctx, call, []event.DomainEvent{unmarshalableEvent{Ch: make(chan int)}})
	require.Error(t, saveErr)

	_, getErr := store.GetCall(ctx, tenantID, call.ID)
	assert.Error(t, getErr, "the call must not exist: a failed outbox write must roll back the domain write in the same transaction")
}

// TestCallStore_AddChannelHistory proves the ACL audit-trail write path.
func TestCallStore_AddChannelHistory(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)
	ctx := context.Background()

	pool, err := corepostgres.Open(ctx, databaseURL)
	require.NoError(t, err)
	defer pool.Close()

	tenantID := shareddomain.NewTenantID()
	seedTenant(t, pool, tenantID)

	store := telephonypostgres.NewCallStore(pool)
	call, caller, _ := newTestCall(t, tenantID)
	require.NoError(t, call.TransitionToScreening())
	_, err = call.ApplyScreeningVerdict(true, true, time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, call.TransitionToPresenting())
	require.NoError(t, store.SaveCall(ctx, call, nil))

	require.NoError(t, store.AddChannelHistory(ctx, tenantID, caller.ID, ports.ChannelRef("atsa-part-1"), "", "originated"))

	var count int
	err = pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM channel_history WHERE participant_id = $1`, caller.ID.String()).Scan(&count)
	})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// TestCallStore_RecordUsageTicks_ContinuousNoDuplication is task 6.3's
// gate: a multi-second simulated connected interval, persisted, produces
// exactly one usage_seconds row per elapsed second with no duplicate and
// no gap — and re-recording the same ticks (as HangupCall's per-
// participant batch could, if retried) stays idempotent (ON CONFLICT DO
// NOTHING), never erroring and never duplicating.
func TestCallStore_RecordUsageTicks_ContinuousNoDuplication(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)
	ctx := context.Background()

	pool, err := corepostgres.Open(ctx, databaseURL)
	require.NoError(t, err)
	defer pool.Close()

	tenantID := shareddomain.NewTenantID()
	seedTenant(t, pool, tenantID)

	store := telephonypostgres.NewCallStore(pool)
	call, caller, callee := newTestCall(t, tenantID)
	require.NoError(t, call.TransitionToScreening())
	_, err = call.ApplyScreeningVerdict(true, true, time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, call.TransitionToPresenting())
	require.NoError(t, store.SaveCall(ctx, call, nil))

	connectedAt := time.Now().UTC()
	disconnectedAt := connectedAt.Add(5 * time.Second)

	var allTicks []domain.UsageTick
	for _, p := range []*domain.CallParticipant{caller, callee} {
		ticks := domain.GenerateUsageTicks(p, call.ID, tenantID, domain.Outbound, connectedAt, disconnectedAt)
		require.Len(t, ticks, 5)
		allTicks = append(allTicks, ticks...)
	}

	require.NoError(t, store.RecordUsageTicks(ctx, allTicks))
	assert.Equal(t, 10, countUsageRows(t, pool, tenantID, call.ID), "5 seconds x 2 participants, no duplicates")

	// Re-recording the same ticks must be a no-op, not an error and not
	// a duplicate — proves the (participant_id, second_ts) primary key
	// backs domain.GenerateUsageTicks' own no-duplicate guarantee.
	require.NoError(t, store.RecordUsageTicks(ctx, allTicks))
	assert.Equal(t, 10, countUsageRows(t, pool, tenantID, call.ID), "re-recording the same ticks must not duplicate rows")

	// Continuity: for one participant, the 5 second_ts values are
	// consecutive whole seconds with no gap.
	var timestamps []time.Time
	err = pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT second_ts FROM usage_seconds WHERE call_id = $1 AND participant_id = $2 ORDER BY second_ts
		`, call.ID.String(), caller.ID.String())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ts time.Time
			if err := rows.Scan(&ts); err != nil {
				return err
			}
			timestamps = append(timestamps, ts)
		}
		return rows.Err()
	})
	require.NoError(t, err)
	require.Len(t, timestamps, 5)
	for i := 1; i < len(timestamps); i++ {
		assert.Equal(t, time.Second, timestamps[i].Sub(timestamps[i-1]), "consecutive seconds, no gap")
	}
}

func countUsageRows(t *testing.T, pool *corepostgres.Pool, tenantID shareddomain.TenantID, callID shareddomain.CallID) int {
	t.Helper()
	var count int
	err := pool.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM usage_seconds WHERE call_id = $1`, callID.String()).Scan(&count)
	})
	require.NoError(t, err)
	return count
}
