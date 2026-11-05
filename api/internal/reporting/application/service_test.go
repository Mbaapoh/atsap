package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identityapp "atsap-api/internal/identity/application"
	identityports "atsap-api/internal/identity/ports"
	"atsap-api/internal/reporting/application"
	"atsap-api/internal/reporting/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

type fakeStore struct {
	lastFilter domain.Filter
	records    []domain.CallRecord
}

func (f *fakeStore) List(_ context.Context, _ shareddomain.TenantID, filter domain.Filter, _, _ int) ([]domain.CallRecord, error) {
	f.lastFilter = filter
	return f.records, nil
}
func (f *fakeStore) Export(_ context.Context, _ shareddomain.TenantID, filter domain.Filter, _ int) ([]domain.CallRecord, error) {
	f.lastFilter = filter
	return f.records, nil
}

func ctxFor(principal shareddomain.PrincipalID, roles ...string) context.Context {
	return identityapp.WithTenantContext(context.Background(), &identityports.TenantContext{
		TenantID: shareddomain.NewTenantID(), PrincipalID: principal, Roles: roles,
	})
}

func TestReporting_AgentSeesOnlyOwnRecords(t *testing.T) {
	store := &fakeStore{}
	svc := application.NewService(nil, store, nil)
	agent := shareddomain.NewPrincipalID()
	_, err := svc.ListCallRecords(ctxFor(agent, "AGENT"), domain.Filter{}, application.Page{Limit: 10})
	require.NoError(t, err)
	require.NotNil(t, store.lastFilter.PrincipalID)
	assert.Equal(t, agent, *store.lastFilter.PrincipalID)

	store.lastFilter = domain.Filter{}
	_, err = svc.ListCallRecords(ctxFor(agent, "SUPERVISOR"), domain.Filter{}, application.Page{Limit: 10})
	require.NoError(t, err)
	assert.Nil(t, store.lastFilter.PrincipalID, "a supervisor sees the tenant")
}

func TestReporting_ExportCSV(t *testing.T) {
	started := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	answered := started.Add(time.Second)
	ended := started.Add(61 * time.Second)
	ext := shareddomain.NewExtensionID()
	principal := shareddomain.NewPrincipalID()
	store := &fakeStore{records: []domain.CallRecord{{
		CallID: shareddomain.NewCallID(), Direction: "OUTBOUND", SourceNumber: "1001", DestNumber: "+4420",
		StartedAt: started, AnsweredAt: &answered, EndedAt: &ended, TerminationReason: "normal_clearing",
		Participants: []domain.Participant{{ID: shareddomain.NewParticipantID(), Role: "CALLER",
			ExtensionID: &ext, PrincipalID: &principal, BillableSeconds: 60, UsageSeconds: 60}},
	}}}
	svc := application.NewService(nil, store, nil)
	csv, err := svc.ExportCallRecords(ctxFor(principal, "TENANT_ADMIN"), domain.Filter{})
	require.NoError(t, err)
	text := string(csv)
	assert.True(t, strings.HasPrefix(text, "call_id,direction,"))
	assert.Contains(t, text, ext.String())
	assert.Contains(t, text, principal.String())
	assert.Contains(t, text, "60")
}

func TestReporting_ExportWithoutParticipantsAndMissingContext(t *testing.T) {
	store := &fakeStore{records: []domain.CallRecord{{CallID: shareddomain.NewCallID(), Direction: "INBOUND", SourceNumber: "+4420", DestNumber: "1001"}}}
	svc := application.NewService(nil, store, nil)
	csv, err := svc.ExportCallRecords(ctxFor(shareddomain.NewPrincipalID(), "TENANT_ADMIN"), domain.Filter{})
	require.NoError(t, err)
	assert.Contains(t, string(csv), "+4420")
	_, err = svc.ListCallRecords(context.Background(), domain.Filter{}, application.Page{})
	assert.Error(t, err)
	_, err = svc.ExportCallRecords(context.Background(), domain.Filter{})
	assert.Error(t, err)
}
