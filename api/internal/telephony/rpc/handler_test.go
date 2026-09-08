package rpc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/shared/event"
	"atsap-api/internal/telephony/domain"
	"atsap-api/internal/telephony/ports"
	"atsap-api/internal/telephony/rpc"
)

type fakeCallStore struct {
	calls map[string]*domain.Call
}

var _ ports.CallStore = (*fakeCallStore)(nil)

func newFakeCallStore() *fakeCallStore {
	return &fakeCallStore{calls: make(map[string]*domain.Call)}
}

func (f *fakeCallStore) SaveCall(_ context.Context, call *domain.Call, _ []event.DomainEvent) error {
	f.calls[call.ID.String()] = call
	return nil
}

func (f *fakeCallStore) GetCall(_ context.Context, _ shareddomain.TenantID, id shareddomain.CallID) (*domain.Call, error) {
	c, ok := f.calls[id.String()]
	if !ok {
		return nil, fmt.Errorf("call %s not found", id)
	}
	return c, nil
}

func (f *fakeCallStore) AddChannelHistory(context.Context, shareddomain.TenantID, shareddomain.ParticipantID, ports.ChannelRef, ports.BridgeID, string) error {
	return nil
}

func (f *fakeCallStore) RecordUsageTicks(context.Context, []domain.UsageTick) error { return nil }

func testCall(t *testing.T) (*domain.Call, shareddomain.TenantID) {
	t.Helper()
	tenantID := shareddomain.NewTenantID()
	call := domain.NewCall(shareddomain.NewCallID(), tenantID, domain.Outbound, "1000", "1001", time.Now().UTC())
	caller := &domain.CallParticipant{
		ID: shareddomain.NewParticipantID(), CallID: call.ID, TenantID: tenantID,
		Role: domain.RoleCaller, EndpointURI: "PJSIP/1000", State: domain.ParticipantConnected, BillableSeconds: 42,
	}
	require.NoError(t, call.AddParticipant(caller))
	call.State = domain.CallActive
	return call, tenantID
}

func TestGetCall_Success(t *testing.T) {
	store := newFakeCallStore()
	call, tenantID := testCall(t)
	store.calls[call.ID.String()] = call

	handler := rpc.NewTelephonyHandler(store)
	resp, err := handler.GetCall(context.Background(), connect.NewRequest(&atsapbxv1.GetCallRequest{
		CallId: call.ID.String(), TenantId: tenantID.String(),
	}))
	require.NoError(t, err)

	assert.Equal(t, call.ID.String(), resp.Msg.GetCallId())
	assert.Equal(t, tenantID.String(), resp.Msg.GetTenantId())
	assert.Equal(t, "Active", resp.Msg.GetState())
	assert.Equal(t, "1000", resp.Msg.GetSourceNumber())
	require.Len(t, resp.Msg.GetParticipants(), 1)
	assert.Equal(t, "PJSIP/1000", resp.Msg.GetParticipants()[0].GetEndpointUri())
	assert.Equal(t, int32(42), resp.Msg.GetParticipants()[0].GetBillableSeconds())
}

func TestGetCall_NotFound(t *testing.T) {
	handler := rpc.NewTelephonyHandler(newFakeCallStore())
	_, err := handler.GetCall(context.Background(), connect.NewRequest(&atsapbxv1.GetCallRequest{
		CallId: shareddomain.NewCallID().String(), TenantId: shareddomain.NewTenantID().String(),
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

func TestGetCall_InvalidTenantID(t *testing.T) {
	handler := rpc.NewTelephonyHandler(newFakeCallStore())
	_, err := handler.GetCall(context.Background(), connect.NewRequest(&atsapbxv1.GetCallRequest{
		CallId: shareddomain.NewCallID().String(), TenantId: "not-a-uuid",
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestGetCall_InvalidCallID(t *testing.T) {
	handler := rpc.NewTelephonyHandler(newFakeCallStore())
	_, err := handler.GetCall(context.Background(), connect.NewRequest(&atsapbxv1.GetCallRequest{
		CallId: "not-a-uuid", TenantId: shareddomain.NewTenantID().String(),
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// TestGetCallResponse_NoChannelFieldsInSchema is the schema-level half
// of the "no infrastructure channel identifier is ever exposed" gate
// (docs/hld/01-architecture.md §6.2): walks GetCallResponse's proto
// field descriptors, recursively into nested messages, and fails if any
// field name references a channel. This catches a future regression
// (someone adding a channel_id field) even without new test content —
// a content-based test alone could not.
func TestGetCallResponse_NoChannelFieldsInSchema(t *testing.T) {
	msg := &atsapbxv1.GetCallResponse{}
	assertNoChannelFields(t, msg.ProtoReflect().Descriptor(), nil)
}

func assertNoChannelFields(t *testing.T, md protoreflect.MessageDescriptor, seen map[protoreflect.FullName]bool) {
	t.Helper()
	if seen == nil {
		seen = make(map[protoreflect.FullName]bool)
	}
	if seen[md.FullName()] {
		return // guard against infinite recursion on self-referential messages
	}
	seen[md.FullName()] = true

	fields := md.Fields()
	for i := range fields.Len() {
		f := fields.Get(i)
		name := strings.ToLower(string(f.Name()))
		assert.NotContains(t, name, "channel", "field %s.%s must not reference a channel", md.FullName(), f.Name())
		if f.Kind() == protoreflect.MessageKind && !f.IsMap() {
			assertNoChannelFields(t, f.Message(), seen)
		}
	}
}

// TestGetCall_NoChannelIDInResponseContent is the content-level half:
// marshals an actual response (with a real participant) to JSON and
// asserts no "channel"-shaped substring appears anywhere in it.
func TestGetCall_NoChannelIDInResponseContent(t *testing.T) {
	store := newFakeCallStore()
	call, tenantID := testCall(t)
	store.calls[call.ID.String()] = call

	handler := rpc.NewTelephonyHandler(store)
	resp, err := handler.GetCall(context.Background(), connect.NewRequest(&atsapbxv1.GetCallRequest{
		CallId: call.ID.String(), TenantId: tenantID.String(),
	}))
	require.NoError(t, err)

	raw, err := protojson.Marshal(resp.Msg)
	require.NoError(t, err)

	var asMap map[string]any
	require.NoError(t, json.Unmarshal(raw, &asMap))
	assert.NotContains(t, strings.ToLower(string(raw)), "channel")
}

// TestTenantID covers the matcher the auth interceptor uses to enforce
// the body-vs-token tenant rule. If it fails to report a tenant the
// request does carry, the interceptor has nothing to compare and a
// mismatched tenant passes silently — which is the hole the cutover
// closes, reopened by omission.
func TestTenantID(t *testing.T) {
	want := shareddomain.NewTenantID().String()

	got, ok := rpc.TenantID(&atsapbxv1.GetCallRequest{TenantId: want, CallId: "x"})
	assert.True(t, ok, "a GetCall request naming a tenant must report it")
	assert.Equal(t, want, got)

	_, ok = rpc.TenantID(&atsapbxv1.GetCallRequest{})
	assert.False(t, ok, "a request naming no tenant reports absent, so the interceptor skips the comparison rather than comparing against empty")

	_, ok = rpc.TenantID(&atsapbxv1.AuthenticateUserRequest{TenantId: want})
	assert.False(t, ok, "this matcher answers only for telephony requests; identity has its own")
}
