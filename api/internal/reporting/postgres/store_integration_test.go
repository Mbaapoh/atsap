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
	"atsap-api/internal/reporting/domain"
	reportingpostgres "atsap-api/internal/reporting/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

func testDB(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://atsapbx_app:devpassword123@localhost:15432/atsapbx_test?sslmode=disable"
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "migrations")
}

func TestStore_ListAndExportCallRecords(t *testing.T) {
	url := testDB(t)
	_ = corepostgres.MigrateDownAll(url, migrationsDir(t))
	require.NoError(t, corepostgres.MigrateUp(url, migrationsDir(t)))
	t.Cleanup(func() { _ = corepostgres.MigrateDownAll(url, migrationsDir(t)) })
	pool, err := corepostgres.Open(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	tenant := shareddomain.NewTenantID()
	_, err = pool.Unwrap().Exec(context.Background(), `INSERT INTO tenants (id, name) VALUES ($1, 'Report')`, tenant.String())
	require.NoError(t, err)

	callID, participantID := shareddomain.NewCallID(), shareddomain.NewParticipantID()
	ext, principal := shareddomain.NewExtensionID(), shareddomain.NewPrincipalID()
	started := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	answered, ended := started.Add(time.Second), started.Add(61*time.Second)
	// Insert under the tenant context so RLS accepts the rows.
	require.NoError(t, pool.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO calls (id, tenant_id, direction, state, source_number, dest_number, started_at, answered_at, ended_at, termination_reason)
			VALUES ($1,$2,'OUTBOUND','Terminated','1001','+4420',$3,$4,$5,'normal_clearing')`,
			callID.String(), tenant.String(), started, answered, ended)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO call_participants (id, tenant_id, call_id, role, endpoint_uri, extension_id, principal_id, state, joined_at)
			VALUES ($1,$2,$3,'CALLER','PJSIP/e_x',$4,$5,'Disconnected',$6)`,
			participantID.String(), tenant.String(), callID.String(), ext.String(), principal.String(), started)
		if err != nil {
			return err
		}
		for i := 0; i < 60; i++ {
			_, err = tx.Exec(ctx, `INSERT INTO usage_seconds (tenant_id, participant_id, call_id, second_ts, direction)
				VALUES ($1,$2,$3,$4,'OUTBOUND')`, tenant.String(), participantID.String(), callID.String(), started.Add(time.Duration(i)*time.Second))
			if err != nil {
				return err
			}
		}
		return nil
	}))

	store := reportingpostgres.NewStore(pool)
	records, err := store.List(context.Background(), tenant, domain.Filter{}, 10, 0)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Len(t, records[0].Participants, 1)
	assert.Equal(t, 60, records[0].Participants[0].UsageSeconds)
	require.NotNil(t, records[0].Participants[0].ExtensionID)
	assert.Equal(t, ext, *records[0].Participants[0].ExtensionID)

	byPerson, err := store.List(context.Background(), tenant, domain.Filter{PrincipalID: &principal}, 10, 0)
	require.NoError(t, err)
	assert.Len(t, byPerson, 1)
	byOther, err := store.List(context.Background(), tenant, domain.Filter{PrincipalID: ptr(shareddomain.NewPrincipalID())}, 10, 0)
	require.NoError(t, err)
	assert.Empty(t, byOther)

	csv, err := store.Export(context.Background(), tenant, domain.Filter{}, 100)
	require.NoError(t, err)
	assert.Len(t, csv, 1)
}

func ptr[T any](v T) *T { return &v }
