package rpc

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

func (h *PbxHandler) PlaceCall(ctx context.Context, req *connect.Request[atsapbxv1.PlaceCallRequest]) (*connect.Response[atsapbxv1.PlaceCallResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	extensionID, err := parseExtensionID(req.Msg.GetExtensionId())
	if err != nil {
		return nil, err
	}
	// Built field by field from the request: nothing here can reach a
	// Screening exemption, which only the dial plan decides (DoD 9).
	placed, err := h.svc.PlaceCall(ctx, ports.PlaceCallCommand{
		TenantID: tenantID, ExtensionID: extensionID, Destination: req.Msg.GetDestination(),
	})
	if err != nil {
		return nil, mapCallErr(err)
	}
	return connect.NewResponse(&atsapbxv1.PlaceCallResponse{CallId: placed.CallID.String()}), nil
}

func (h *PbxHandler) HangupCall(ctx context.Context, req *connect.Request[atsapbxv1.HangupCallRequest]) (*connect.Response[atsapbxv1.HangupCallResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	callID, err := shareddomain.ParseCallID(req.Msg.GetCallId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid call_id"))
	}
	if err := h.svc.HangupCall(ctx, tenantID, callID); err != nil {
		return nil, mapCallErr(err)
	}
	return connect.NewResponse(&atsapbxv1.HangupCallResponse{}), nil
}

// mapCallErr maps placement errors. Each precondition gets a fixed message
// naming what to fix; a screening refusal carries the reason telephony
// recorded (a vocabulary like "over_capacity", never engine text).
// Anything else falls through to mapConfigErr, whose internal case never
// passes underlying text on.
func mapCallErr(err error) error {
	switch {
	case errors.Is(err, domain.ErrUnroutable):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("destination is unroutable: no extension or outbound route matches it"))
	case errors.Is(err, ports.ErrTrunkCapacity):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("trunk capacity: every trunk for this destination is at its channel limit"))
	case errors.Is(err, ports.ErrCallerNotRegistered):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("the calling extension has no registered device"))
	case errors.Is(err, ports.ErrCallRefused):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New(err.Error()))
	}
	return mapConfigErr(err)
}
