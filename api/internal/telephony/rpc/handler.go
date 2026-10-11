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
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/genproto/atsapbx/v1/atsapbxv1connect"
	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/telephony/domain"
	"atsap-api/internal/telephony/ports"
)

// TelephonyHandler implements atsapbxv1connect.TelephonyServiceHandler.
type TelephonyHandler struct {
	callStore ports.CallStore
	calls     ports.CallLister
}

var _ atsapbxv1connect.TelephonyServiceHandler = (*TelephonyHandler)(nil)

// NewTelephonyHandler returns a handler backed by callStore, listing calls
// through calls.
func NewTelephonyHandler(callStore ports.CallStore, calls ports.CallLister) *TelephonyHandler {
	return &TelephonyHandler{callStore: callStore, calls: calls}
}

// maxCallPage caps a ListCalls page; 50 when the caller names none.
const maxCallPage = 200

// ListCalls returns the tenant's calls, newest first (console-apis-phase-a
// design D5). It is guarded exactly as GetCall is — the interceptor
// authenticates and refuses a body tenant that is not the token's — and
// adds no role check: the same exposure GetCall has, recorded, not
// widened. CallSummary has no participant or endpoint field, so no
// engine identifier can be listed.
func (h *TelephonyHandler) ListCalls(ctx context.Context, req *connect.Request[atsapbxv1.ListCallsRequest]) (*connect.Response[atsapbxv1.ListCallsResponse], error) {
	tenantID, err := shareddomain.ParseTenantID(req.Msg.GetTenantId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid tenant_id"))
	}
	filter := ports.CallFilter{State: domain.CallState(req.Msg.GetState()), Direction: domain.Direction(req.Msg.GetDirection())}
	if filter.State != "" && !validCallState(filter.State) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid state"))
	}
	if filter.Direction != "" && filter.Direction != domain.Inbound && filter.Direction != domain.Outbound && filter.Direction != domain.Internal {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid direction"))
	}
	if ts := req.Msg.GetStartedFrom(); ts != nil {
		filter.From = ts.AsTime()
	}
	if ts := req.Msg.GetStartedTo(); ts != nil {
		filter.To = ts.AsTime()
	}
	limit := int(req.Msg.GetLimit())
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, maxCallPage)

	calls, err := h.calls.ListCalls(ctx, tenantID, filter, limit, max(int(req.Msg.GetOffset()), 0))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("call listing failed"))
	}
	out := make([]*atsapbxv1.CallSummary, 0, len(calls))
	for _, c := range calls {
		out = append(out, toCallSummary(c))
	}
	return connect.NewResponse(&atsapbxv1.ListCallsResponse{Calls: out}), nil
}

func validCallState(s domain.CallState) bool {
	switch s {
	case domain.CallInitiated, domain.CallScreening, domain.CallRouting, domain.CallPresenting,
		domain.CallActive, domain.CallDegraded, domain.CallTerminating, domain.CallTerminated:
		return true
	}
	return false
}

func toCallSummary(c ports.CallSummary) *atsapbxv1.CallSummary {
	out := &atsapbxv1.CallSummary{
		CallId: c.ID.String(), Direction: string(c.Direction), State: string(c.State),
		SourceNumber: c.SourceNumber, DestNumber: c.DestNumber,
		StartedAt: timestamppb.New(c.StartedAt), TerminationReason: c.TerminationReason,
	}
	if c.AnsweredAt != nil {
		out.AnsweredAt = timestamppb.New(*c.AnsweredAt)
	}
	if c.EndedAt != nil {
		out.EndedAt = timestamppb.New(*c.EndedAt)
	}
	if c.AnsweredAt != nil && c.EndedAt != nil && c.EndedAt.After(*c.AnsweredAt) {
		out.DurationSeconds = int32(c.EndedAt.Sub(*c.AnsweredAt) / time.Second)
	}
	return out
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
	case *atsapbxv1.ListCallsRequest:
		return r.GetTenantId(), r.GetTenantId() != ""
	default:
		return "", false
	}
}
