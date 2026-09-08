// Package rpc implements pbx-core's public ConnectRPC surface: the six
// PbxService methods that let the console — and a partner, through the
// same endpoints — configure extensions.
//
// Two rules govern every handler here.
//
// The tenant in the request body must match the token's. Authorization
// itself happens in the application layer, which is the only place that
// sees a mutation and its permission together; this layer refuses a
// mismatch before that, so a caller cannot even ask about another
// tenant. A resource in another tenant answers as one that does not
// exist (INV-10).
//
// No engine identifier crosses this boundary. Nothing in a response or
// an error names an endpoint, a transport, a dialplan context or a
// projection row. The application layer already returns a view free of
// them; this package must not reintroduce one (PRD principle 4, D-47).
package rpc

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/genproto/atsapbx/v1/atsapbxv1connect"
	identityapp "atsap-api/internal/identity/application"
	identityports "atsap-api/internal/identity/ports"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// maxPageLimit bounds a listing so one call cannot demand every
// extension a tenant has.
const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

// PbxHandler implements atsapbxv1connect.PbxServiceHandler.
type PbxHandler struct {
	svc ports.ConfigService

	atsapbxv1connect.UnimplementedPbxServiceHandler
}

var _ atsapbxv1connect.PbxServiceHandler = (*PbxHandler)(nil)

// NewPbxHandler returns a handler backed by svc.
func NewPbxHandler(svc ports.ConfigService) *PbxHandler {
	return &PbxHandler{svc: svc}
}

func (h *PbxHandler) CreateExtension(ctx context.Context, req *connect.Request[atsapbxv1.CreateExtensionRequest]) (*connect.Response[atsapbxv1.CreateExtensionResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}

	created, err := h.svc.CreateExtension(ctx, ports.CreateExtensionCommand{
		TenantID:    tenantID,
		Number:      req.Msg.GetNumber(),
		DisplayName: req.Msg.GetDisplayName(),
		DeviceType:  deviceTypeFromProto(req.Msg.GetDeviceType()),
	})
	if err != nil {
		return nil, mapErr(err)
	}

	// The secret appears here and in RegenerateSecret's response, nowhere
	// else in the API.
	return connect.NewResponse(&atsapbxv1.CreateExtensionResponse{
		Extension: toProtoExtension(created.Extension),
		Secret:    created.Secret,
	}), nil
}

func (h *PbxHandler) GetExtension(ctx context.Context, req *connect.Request[atsapbxv1.GetExtensionRequest]) (*connect.Response[atsapbxv1.GetExtensionResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	id, err := parseExtensionID(req.Msg.GetExtensionId())
	if err != nil {
		return nil, err
	}

	view, err := h.svc.GetExtension(ctx, tenantID, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.GetExtensionResponse{Extension: toProtoExtension(view)}), nil
}

func (h *PbxHandler) ListExtensions(ctx context.Context, req *connect.Request[atsapbxv1.ListExtensionsRequest]) (*connect.Response[atsapbxv1.ListExtensionsResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}

	views, err := h.svc.ListExtensions(ctx, tenantID, ports.Page{
		Limit:  pageLimit(req.Msg.GetLimit()),
		Offset: max(int(req.Msg.GetOffset()), 0),
	})
	if err != nil {
		return nil, mapErr(err)
	}

	out := make([]*atsapbxv1.Extension, 0, len(views))
	for _, v := range views {
		out = append(out, toProtoExtension(v))
	}
	return connect.NewResponse(&atsapbxv1.ListExtensionsResponse{Extensions: out}), nil
}

func (h *PbxHandler) UpdateExtension(ctx context.Context, req *connect.Request[atsapbxv1.UpdateExtensionRequest]) (*connect.Response[atsapbxv1.UpdateExtensionResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	id, err := parseExtensionID(req.Msg.GetExtensionId())
	if err != nil {
		return nil, err
	}

	if err := h.svc.UpdateExtension(ctx, ports.UpdateExtensionCommand{
		TenantID:    tenantID,
		ID:          id,
		DisplayName: req.Msg.GetDisplayName(),
		DeviceType:  deviceTypeFromProto(req.Msg.GetDeviceType()),
	}); err != nil {
		return nil, mapErr(err)
	}

	view, err := h.svc.GetExtension(ctx, tenantID, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.UpdateExtensionResponse{Extension: toProtoExtension(view)}), nil
}

func (h *PbxHandler) DeleteExtension(ctx context.Context, req *connect.Request[atsapbxv1.DeleteExtensionRequest]) (*connect.Response[atsapbxv1.DeleteExtensionResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	id, err := parseExtensionID(req.Msg.GetExtensionId())
	if err != nil {
		return nil, err
	}

	if err := h.svc.DeleteExtension(ctx, tenantID, id); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.DeleteExtensionResponse{}), nil
}

func (h *PbxHandler) RegenerateSecret(ctx context.Context, req *connect.Request[atsapbxv1.RegenerateSecretRequest]) (*connect.Response[atsapbxv1.RegenerateSecretResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	id, err := parseExtensionID(req.Msg.GetExtensionId())
	if err != nil {
		return nil, err
	}

	rotated, err := h.svc.RegenerateSecret(ctx, tenantID, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.RegenerateSecretResponse{
		Extension: toProtoExtension(rotated.Extension),
		Secret:    rotated.Secret,
	}), nil
}

// --- helpers ---------------------------------------------------------

// tenantOf resolves the tenant a request acts on and refuses one that is
// not the caller's.
//
// pbx-core has no system-scoped path, unlike identity's ProvisionTenant:
// there is no legitimate reason for a platform operator to create an
// extension inside a customer's tenant, and offering the capability
// would mean every such call had to be trusted rather than authorized.
// A mismatch is PermissionDenied rather than NotFound because the caller
// named their own token's mismatch, not another tenant's resource — no
// information about that tenant is disclosed either way.
func (h *PbxHandler) tenantOf(ctx context.Context, requested string) (shareddomain.TenantID, error) {
	tc, ok := identityapp.TenantContextFrom(ctx)
	if !ok || tc == nil {
		return shareddomain.TenantID{}, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	if requested == "" {
		return tc.TenantID, nil
	}
	requestedID, err := shareddomain.ParseTenantID(requested)
	if err != nil {
		return shareddomain.TenantID{}, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid tenant_id"))
	}
	if requestedID != tc.TenantID {
		return shareddomain.TenantID{}, connect.NewError(connect.CodePermissionDenied, errors.New("not permitted"))
	}
	return requestedID, nil
}

func parseExtensionID(s string) (shareddomain.ExtensionID, error) {
	id, err := shareddomain.ParseExtensionID(s)
	if err != nil {
		return shareddomain.ExtensionID{}, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid extension_id"))
	}
	return id, nil
}

func pageLimit(requested int32) int {
	switch {
	case requested <= 0:
		return defaultPageLimit
	case int(requested) > maxPageLimit:
		return maxPageLimit
	default:
		return int(requested)
	}
}

// mapErr translates port errors to Connect codes.
//
// ErrNotFound covers both a nonexistent extension and one in another
// tenant, deliberately: the store cannot distinguish them under RLS and
// this layer must not either (INV-10). The message is fixed text, never
// the wrapped error, so nothing about another tenant's data can leak
// through a formatted string.
func mapErr(err error) error {
	switch {
	case errors.Is(err, identityports.ErrPermissionDenied):
		return connect.NewError(connect.CodePermissionDenied, errors.New("not permitted"))
	case errors.Is(err, ports.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("not found"))
	case errors.Is(err, ports.ErrNumberTaken):
		return connect.NewError(connect.CodeAlreadyExists, errors.New("extension number already exists in this tenant"))
	case errors.Is(err, ports.ErrInvalidInput):
		return connect.NewError(connect.CodeInvalidArgument, errors.New("invalid extension"))
	default:
		// Deliberately does not wrap err: an internal failure's text can
		// name a table, a constraint, or an engine object, and none of
		// those belong in a caller's error (PRD principle 4).
		return connect.NewError(connect.CodeInternal, errors.New("extension operation failed"))
	}
}

func toProtoExtension(v ports.ExtensionView) *atsapbxv1.Extension {
	return &atsapbxv1.Extension{
		ExtensionId:  v.ID.String(),
		Number:       v.Number,
		DisplayName:  v.DisplayName,
		AuthUsername: v.AuthUsername,
		DeviceType:   deviceTypeToProto(v.DeviceType),
		Registration: registrationToProto(v.Registration),
	}
}

// deviceTypeFromProto maps the wire enum onto the domain type. An
// unspecified value is passed through as an empty device type so the
// domain refuses it with a validation error, rather than this layer
// silently choosing one for the caller.
func deviceTypeFromProto(d atsapbxv1.DeviceType) domain.DeviceType {
	switch d {
	case atsapbxv1.DeviceType_DEVICE_TYPE_WEBRTC:
		return domain.DeviceWebRTC
	case atsapbxv1.DeviceType_DEVICE_TYPE_SIP:
		return domain.DeviceSIP
	default:
		return ""
	}
}

func deviceTypeToProto(d domain.DeviceType) atsapbxv1.DeviceType {
	switch d {
	case domain.DeviceWebRTC:
		return atsapbxv1.DeviceType_DEVICE_TYPE_WEBRTC
	case domain.DeviceSIP:
		return atsapbxv1.DeviceType_DEVICE_TYPE_SIP
	default:
		return atsapbxv1.DeviceType_DEVICE_TYPE_UNSPECIFIED
	}
}

func registrationToProto(s domain.RegistrationStatus) atsapbxv1.RegistrationStatus {
	switch s {
	case domain.StatusRegistered:
		return atsapbxv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED
	case domain.StatusNotRegistered:
		return atsapbxv1.RegistrationStatus_REGISTRATION_STATUS_NOT_REGISTERED
	default:
		return atsapbxv1.RegistrationStatus_REGISTRATION_STATUS_UNSPECIFIED
	}
}
