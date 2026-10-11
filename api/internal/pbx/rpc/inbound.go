package rpc

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/pbx/ports"
)

// Emergency-profile and inbound-number handlers
// (pbx-inbound-and-emergency). Built exactly like the trunk handlers:
// tenant from the interceptor-checked request, ids parsed loudly,
// errors mapped without ever passing engine text on.

func (h *PbxHandler) SetEmergencyProfile(ctx context.Context, req *connect.Request[atsapbxv1.SetEmergencyProfileRequest]) (*connect.Response[atsapbxv1.SetEmergencyProfileResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.SetEmergencyProfile(ctx, tenantID, req.Msg.GetCountryCode()); err != nil {
		return nil, mapConfigErr(err)
	}
	return connect.NewResponse(&atsapbxv1.SetEmergencyProfileResponse{}), nil
}

func (h *PbxHandler) GetEmergencyProfile(ctx context.Context, req *connect.Request[atsapbxv1.GetEmergencyProfileRequest]) (*connect.Response[atsapbxv1.GetEmergencyProfileResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	profile, err := h.svc.GetEmergencyProfile(ctx, tenantID)
	if err != nil {
		return nil, mapConfigErr(err)
	}
	return connect.NewResponse(&atsapbxv1.GetEmergencyProfileResponse{
		CountryCode: profile.CountryCode,
		CountrySet:  profile.CountrySet,
		InCatalogue: profile.InCatalogue,
		Numbers:     toProtoEmergencyNumbers(profile.Numbers),
	}), nil
}

func (h *PbxHandler) CreateEmergencyNumber(ctx context.Context, req *connect.Request[atsapbxv1.CreateEmergencyNumberRequest]) (*connect.Response[atsapbxv1.CreateEmergencyNumberResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.CreateEmergencyNumber(ctx, tenantID, req.Msg.GetNumber()); err != nil {
		return nil, mapInboundErr(err)
	}
	return connect.NewResponse(&atsapbxv1.CreateEmergencyNumberResponse{}), nil
}

func (h *PbxHandler) ListEmergencyNumbers(ctx context.Context, req *connect.Request[atsapbxv1.ListEmergencyNumbersRequest]) (*connect.Response[atsapbxv1.ListEmergencyNumbersResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	numbers, err := h.svc.ListEmergencyNumbers(ctx, tenantID)
	if err != nil {
		return nil, mapConfigErr(err)
	}
	return connect.NewResponse(&atsapbxv1.ListEmergencyNumbersResponse{
		Numbers: toProtoEmergencyNumbers(numbers),
	}), nil
}

func (h *PbxHandler) DeleteEmergencyNumber(ctx context.Context, req *connect.Request[atsapbxv1.DeleteEmergencyNumberRequest]) (*connect.Response[atsapbxv1.DeleteEmergencyNumberResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteEmergencyNumber(ctx, tenantID, req.Msg.GetNumber()); err != nil {
		return nil, mapInboundErr(err)
	}
	return connect.NewResponse(&atsapbxv1.DeleteEmergencyNumberResponse{}), nil
}

func (h *PbxHandler) CreateInboundNumber(ctx context.Context, req *connect.Request[atsapbxv1.CreateInboundNumberRequest]) (*connect.Response[atsapbxv1.CreateInboundNumberResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	extensionID, err := parseExtensionID(req.Msg.GetExtensionId())
	if err != nil {
		return nil, err
	}
	created, err := h.svc.CreateInboundNumber(ctx, ports.CreateInboundNumberCommand{
		TenantID: tenantID, Number: req.Msg.GetNumber(), ExtensionID: extensionID,
	})
	if err != nil {
		return nil, mapInboundErr(err)
	}
	return connect.NewResponse(&atsapbxv1.CreateInboundNumberResponse{
		InboundNumber: toProtoInboundNumber(created),
	}), nil
}

func (h *PbxHandler) ListInboundNumbers(ctx context.Context, req *connect.Request[atsapbxv1.ListInboundNumbersRequest]) (*connect.Response[atsapbxv1.ListInboundNumbersResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	numbers, err := h.svc.ListInboundNumbers(ctx, tenantID)
	if err != nil {
		return nil, mapConfigErr(err)
	}
	out := make([]*atsapbxv1.InboundNumber, 0, len(numbers))
	for _, n := range numbers {
		out = append(out, toProtoInboundNumber(n))
	}
	return connect.NewResponse(&atsapbxv1.ListInboundNumbersResponse{Numbers: out}), nil
}

func (h *PbxHandler) DeleteInboundNumber(ctx context.Context, req *connect.Request[atsapbxv1.DeleteInboundNumberRequest]) (*connect.Response[atsapbxv1.DeleteInboundNumberResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(req.Msg.GetInboundNumberId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid inbound_number_id"))
	}
	if err := h.svc.DeleteInboundNumber(ctx, tenantID, id); err != nil {
		return nil, mapInboundErr(err)
	}
	return connect.NewResponse(&atsapbxv1.DeleteInboundNumberResponse{}), nil
}

func toProtoEmergencyNumbers(numbers []ports.EmergencyNumberView) []*atsapbxv1.EmergencyNumber {
	out := make([]*atsapbxv1.EmergencyNumber, 0, len(numbers))
	for _, n := range numbers {
		out = append(out, &atsapbxv1.EmergencyNumber{Number: n.Number, BuiltIn: n.BuiltIn})
	}
	return out
}

func toProtoInboundNumber(n ports.InboundNumberView) *atsapbxv1.InboundNumber {
	return &atsapbxv1.InboundNumber{
		InboundNumberId: n.ID.String(), Number: n.Number, ExtensionId: n.ExtensionID.String(),
	}
}

// mapInboundErr maps the inbound-and-emergency errors. Deleting a
// built-in emergency number is a failed precondition, not a fault; a
// duplicate DID is already-exists, like a duplicate extension number.
// Anything else falls through to the shared mappers, whose internal
// case never passes underlying text on (DoD 13).
func mapInboundErr(err error) error {
	switch {
	case errors.Is(err, ports.ErrEmergencyNumberProtected):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("emergency number is built-in and cannot be removed"))
	case errors.Is(err, ports.ErrInboundNumberTaken):
		return connect.NewError(connect.CodeAlreadyExists, errors.New("inbound number already assigned in this tenant"))
	}
	return mapConfigErr(err)
}
