package rpc_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	identityapp "atsap-api/internal/identity/application"
	identityports "atsap-api/internal/identity/ports"
	"atsap-api/internal/reporting/application"
	"atsap-api/internal/reporting/domain"
	"atsap-api/internal/reporting/rpc"
	shareddomain "atsap-api/internal/shared/domain"
)

type fakeStore struct{ records []domain.CallRecord }

func (f fakeStore) List(context.Context, shareddomain.TenantID, domain.Filter, int, int) ([]domain.CallRecord, error) {
	return f.records, nil
}
func (f fakeStore) Export(context.Context, shareddomain.TenantID, domain.Filter, int) ([]domain.CallRecord, error) {
	return f.records, nil
}

func TestHandler_ListAndExport(t *testing.T) {
	tenant, principal := shareddomain.NewTenantID(), shareddomain.NewPrincipalID()
	store := fakeStore{records: []domain.CallRecord{{CallID: shareddomain.NewCallID(), Direction: "INBOUND", SourceNumber: "+4420", DestNumber: "1001"}}}
	h := rpc.NewHandler(application.NewService(nil, store, nil))
	ctx := identityapp.WithTenantContext(context.Background(), &identityports.TenantContext{TenantID: tenant, PrincipalID: principal, Roles: []string{"AGENT"}})

	got, err := h.ListCallRecords(ctx, connect.NewRequest(&atsapbxv1.ListCallRecordsRequest{TenantId: tenant.String()}))
	require.NoError(t, err)
	require.Len(t, got.Msg.GetRecords(), 1)
	assert.Equal(t, "1001", got.Msg.GetRecords()[0].GetDestNumber())

	exported, err := h.ExportCallRecords(ctx, connect.NewRequest(&atsapbxv1.ExportCallRecordsRequest{TenantId: tenant.String()}))
	require.NoError(t, err)
	assert.Contains(t, string(exported.Msg.GetCsv()), "call_id,direction")
}

func TestHandler_FiltersAndErrors(t *testing.T) {
	tenant, principal, ext := shareddomain.NewTenantID(), shareddomain.NewPrincipalID(), shareddomain.NewExtensionID()
	answered := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	ended := answered.Add(2 * time.Minute)
	store := fakeStore{records: []domain.CallRecord{{
		CallID: shareddomain.NewCallID(), Direction: "INTERNAL", SourceNumber: "1001", DestNumber: "1002",
		AnsweredAt: &answered, EndedAt: &ended,
		Participants: []domain.Participant{{ID: shareddomain.NewParticipantID(), Role: "CALLER", ExtensionID: &ext, PrincipalID: &principal, BillableSeconds: 120, UsageSeconds: 120}},
	}}}
	h := rpc.NewHandler(application.NewService(nil, store, nil))
	ctx := identityapp.WithTenantContext(context.Background(), &identityports.TenantContext{TenantID: tenant, PrincipalID: principal, Roles: []string{"TENANT_ADMIN"}})

	got, err := h.ListCallRecords(ctx, connect.NewRequest(&atsapbxv1.ListCallRecordsRequest{
		TenantId: tenant.String(), Limit: 500, Offset: -1,
		Filter: &atsapbxv1.CallRecordFilter{
			From: timestamppb.New(answered), To: timestamppb.New(ended),
			Direction: "INTERNAL", ExtensionId: ext.String(), PrincipalId: principal.String(), Number: "100",
		},
	}))
	require.NoError(t, err)
	require.Len(t, got.Msg.GetRecords(), 1)
	rec := got.Msg.GetRecords()[0]
	assert.Equal(t, int32(120), rec.GetDurationSeconds())
	require.Len(t, rec.GetParticipants(), 1)
	assert.Equal(t, ext.String(), rec.GetParticipants()[0].GetExtensionId())
	assert.Equal(t, principal.String(), rec.GetParticipants()[0].GetPrincipalId())

	for _, req := range []*atsapbxv1.ListCallRecordsRequest{
		{TenantId: "not-a-uuid"},
		{TenantId: tenant.String(), Filter: &atsapbxv1.CallRecordFilter{ExtensionId: "bad"}},
		{TenantId: tenant.String(), Filter: &atsapbxv1.CallRecordFilter{PrincipalId: "bad"}},
	} {
		_, err := h.ListCallRecords(ctx, connect.NewRequest(req))
		require.Error(t, err)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	}
	_, err = h.ExportCallRecords(ctx, connect.NewRequest(&atsapbxv1.ExportCallRecordsRequest{TenantId: "bad"}))
	require.Error(t, err)
}
