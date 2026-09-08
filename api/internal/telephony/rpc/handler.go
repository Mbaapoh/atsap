// Package rpc implements telephony-core's public ConnectRPC API
// (docs/hld/01-architecture.md §3.1). GetCall reads from CallStore
// directly, not through CallService — a query naturally reads the
// durable store, not the orchestrator's transient in-flight working set
// (D-31: scaled-down DDD, no full CQRS, but nothing stops a query from
// bypassing the command-oriented service when there's no domain
// invariant to protect on the read path).
package rpc

import (
	"context"
	"fmt"

	"connectrpc.com/connect"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/genproto/atsapbx/v1/atsapbxv1connect"
	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/telephony/ports"
)

// TelephonyHandler implements atsapbxv1connect.TelephonyServiceHandler.
type TelephonyHandler struct {
	callStore ports.CallStore
}

var _ atsapbxv1connect.TelephonyServiceHandler = (*TelephonyHandler)(nil)

// NewTelephonyHandler returns a handler backed by callStore.
func NewTelephonyHandler(callStore ports.CallStore) *TelephonyHandler {
	return &TelephonyHandler{callStore: callStore}
}

// GetCall returns a Call's current state and participants. The
// response never contains an Asterisk channel identifier — proven by
// TestGetCall_NoChannelIDLeakage, an automated scan of every string
// field in the response (docs/hld/01-architecture.md §6.2).
func (h *TelephonyHandler) GetCall(ctx context.Context, req *connect.Request[atsapbxv1.GetCallRequest]) (*connect.Response[atsapbxv1.GetCallResponse], error) {
	tenantID, err := shareddomain.ParseTenantID(req.Msg.GetTenantId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid tenant_id: %w", err))
	}
	callID, err := shareddomain.ParseCallID(req.Msg.GetCallId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid call_id: %w", err))
	}

	call, err := h.callStore.GetCall(ctx, tenantID, callID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("call not found: %w", err))
	}

	resp := &atsapbxv1.GetCallResponse{
		CallId:       call.ID.String(),
		TenantId:     call.TenantID.String(),
		State:        string(call.State),
		Direction:    string(call.Direction),
		SourceNumber: call.SourceNumber,
		DestNumber:   call.DestNumber,
	}
	for _, p := range call.Participants {
		resp.Participants = append(resp.Participants, &atsapbxv1.Participant{
			ParticipantId:   p.ID.String(),
			Role:            string(p.Role),
			EndpointUri:     p.EndpointURI,
			State:           string(p.State),
			BillableSeconds: int32(p.BillableSeconds),
		})
	}
	return connect.NewResponse(resp), nil
}

// TenantID extracts the tenant_id a request body names, for the
// AuthInterceptor's body-vs-token tenant rule. It lives here, next to the
// generated types, so the interceptor never has to import the proto
// package — the same arrangement identity/rpc.TenantID uses.
//
// Before the auth cutover this service was mounted without the
// interceptor and GetCall took whatever tenant the caller supplied, which
// meant anyone reaching the port could read any tenant's call. The
// interceptor now refuses a body tenant that is not the token's, and this
// is what tells it which field to compare.
func TenantID(request any) (string, bool) {
	switch r := request.(type) {
	case *atsapbxv1.GetCallRequest:
		return r.GetTenantId(), r.GetTenantId() != ""
	default:
		return "", false
	}
}
