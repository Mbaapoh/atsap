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
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"atsap-api/internal/shared/wire"

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
		Outbound:    outboundFromProto(req.Msg.GetOutboundClass()),
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
		Outbound:    outboundFromProto(req.Msg.GetOutboundClass()),
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

func (h *PbxHandler) CreateFlow(ctx context.Context, req *connect.Request[atsapbxv1.CreateFlowRequest]) (*connect.Response[atsapbxv1.FlowResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	cfg, err := flowConfigFromProto(req.Msg.GetConfig())
	if err != nil {
		return nil, err
	}
	view, err := h.svc.CreateFlow(ctx, ports.CreateFlowCommand{TenantID: tenantID, Number: req.Msg.GetNumber(), Name: req.Msg.GetName(), Config: cfg})
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.FlowResponse{Flow: toProtoFlow(view)}), nil
}

func (h *PbxHandler) GetFlow(ctx context.Context, req *connect.Request[atsapbxv1.GetFlowRequest]) (*connect.Response[atsapbxv1.FlowResponse], error) {
	tenantID, id, err := h.flowOf(ctx, req.Msg.GetTenantId(), req.Msg.GetFlowId())
	if err != nil {
		return nil, err
	}
	view, err := h.svc.GetFlow(ctx, tenantID, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.FlowResponse{Flow: toProtoFlow(view)}), nil
}

func (h *PbxHandler) ListFlows(ctx context.Context, req *connect.Request[atsapbxv1.ListFlowsRequest]) (*connect.Response[atsapbxv1.ListFlowsResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	views, err := h.svc.ListFlows(ctx, tenantID)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]*atsapbxv1.Flow, 0, len(views))
	for _, v := range views {
		out = append(out, toProtoFlow(v))
	}
	return connect.NewResponse(&atsapbxv1.ListFlowsResponse{Flows: out}), nil
}

func (h *PbxHandler) PublishFlow(ctx context.Context, req *connect.Request[atsapbxv1.PublishFlowRequest]) (*connect.Response[atsapbxv1.FlowResponse], error) {
	tenantID, id, err := h.flowOf(ctx, req.Msg.GetTenantId(), req.Msg.GetFlowId())
	if err != nil {
		return nil, err
	}
	cfg, err := flowConfigFromProto(req.Msg.GetConfig())
	if err != nil {
		return nil, err
	}
	view, err := h.svc.PublishFlow(ctx, tenantID, id, cfg)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.FlowResponse{Flow: toProtoFlow(view)}), nil
}

func (h *PbxHandler) RollbackFlow(ctx context.Context, req *connect.Request[atsapbxv1.RollbackFlowRequest]) (*connect.Response[atsapbxv1.FlowResponse], error) {
	tenantID, id, err := h.flowOf(ctx, req.Msg.GetTenantId(), req.Msg.GetFlowId())
	if err != nil {
		return nil, err
	}
	view, err := h.svc.RollbackFlow(ctx, tenantID, id, int(req.Msg.GetVersion()))
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.FlowResponse{Flow: toProtoFlow(view)}), nil
}

func (h *PbxHandler) DeleteFlow(ctx context.Context, req *connect.Request[atsapbxv1.DeleteFlowRequest]) (*connect.Response[atsapbxv1.DeleteFlowResponse], error) {
	tenantID, id, err := h.flowOf(ctx, req.Msg.GetTenantId(), req.Msg.GetFlowId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteFlow(ctx, tenantID, id); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.DeleteFlowResponse{}), nil
}

func (h *PbxHandler) ListFlowVersions(ctx context.Context, req *connect.Request[atsapbxv1.ListFlowVersionsRequest]) (*connect.Response[atsapbxv1.ListFlowVersionsResponse], error) {
	tenantID, id, err := h.flowOf(ctx, req.Msg.GetTenantId(), req.Msg.GetFlowId())
	if err != nil {
		return nil, err
	}
	versions, err := h.svc.ListFlowVersions(ctx, tenantID, id)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]*atsapbxv1.FlowVersion, 0, len(versions))
	for _, v := range versions {
		out = append(out, &atsapbxv1.FlowVersion{Version: wire.Int32(int64(v.Version)), Config: flowConfigToProto(v.Config), PublishedAt: timestamppb.New(v.PublishedAt), PublishedBy: v.PublishedBy.String()})
	}
	return connect.NewResponse(&atsapbxv1.ListFlowVersionsResponse{Versions: out}), nil
}

func (h *PbxHandler) ListHolidays(ctx context.Context, req *connect.Request[atsapbxv1.ListHolidaysRequest]) (*connect.Response[atsapbxv1.HolidaysResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	holidays, err := h.svc.ListHolidays(ctx, tenantID)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]*atsapbxv1.Holiday, 0, len(holidays))
	for day, name := range holidays {
		out = append(out, &atsapbxv1.Holiday{Day: day, Name: name})
	}
	return connect.NewResponse(&atsapbxv1.HolidaysResponse{Holidays: out}), nil
}

func (h *PbxHandler) AddHoliday(ctx context.Context, req *connect.Request[atsapbxv1.AddHolidayRequest]) (*connect.Response[atsapbxv1.HolidaysResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.AddHoliday(ctx, tenantID, req.Msg.GetDay(), req.Msg.GetName()); err != nil {
		return nil, mapErr(err)
	}
	return h.ListHolidays(ctx, connect.NewRequest(&atsapbxv1.ListHolidaysRequest{TenantId: tenantID.String()}))
}

func (h *PbxHandler) DeleteHoliday(ctx context.Context, req *connect.Request[atsapbxv1.DeleteHolidayRequest]) (*connect.Response[atsapbxv1.HolidaysResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteHoliday(ctx, tenantID, req.Msg.GetDay()); err != nil {
		return nil, mapErr(err)
	}
	return h.ListHolidays(ctx, connect.NewRequest(&atsapbxv1.ListHolidaysRequest{TenantId: tenantID.String()}))
}

func (h *PbxHandler) flowOf(ctx context.Context, requested, rawID string) (shareddomain.TenantID, shareddomain.FlowID, error) {
	tenantID, err := h.tenantOf(ctx, requested)
	if err != nil {
		return shareddomain.TenantID{}, shareddomain.FlowID{}, err
	}
	id, err := shareddomain.ParseFlowID(rawID)
	if err != nil {
		return shareddomain.TenantID{}, shareddomain.FlowID{}, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid flow_id"))
	}
	return tenantID, id, nil
}

func flowConfigFromProto(c *atsapbxv1.FlowConfig) (domain.FlowConfig, error) {
	if c == nil {
		return domain.FlowConfig{}, connect.NewError(connect.CodeInvalidArgument, errors.New("config is required"))
	}
	out := domain.FlowConfig{BusinessStartMinute: int(c.GetBusinessStartMinute()), BusinessEndMinute: int(c.GetBusinessEndMinute()),
		Prompt: c.GetPrompt(), MenuTimeoutSeconds: int(c.GetMenuTimeoutSeconds()), Menu: map[string]shareddomain.ExtensionID{}}
	for digit, raw := range c.GetMenu() {
		id, err := shareddomain.ParseExtensionID(raw)
		if err != nil {
			return out, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid menu extension_id"))
		}
		out.Menu[digit] = id
	}
	if c.GetNoInputExtensionId() != "" {
		id, err := shareddomain.ParseExtensionID(c.GetNoInputExtensionId())
		if err != nil {
			return out, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid no_input_extension_id"))
		}
		out.NoInputExtensionID = &id
	}
	if c.GetInvalidExtensionId() != "" {
		id, err := shareddomain.ParseExtensionID(c.GetInvalidExtensionId())
		if err != nil {
			return out, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid invalid_extension_id"))
		}
		out.InvalidExtensionID = &id
	}
	if c.GetOpenExtensionId() != "" {
		id, err := shareddomain.ParseExtensionID(c.GetOpenExtensionId())
		if err != nil {
			return out, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid open_extension_id"))
		}
		out.OpenExtensionID = &id
	}
	if c.GetClosedExtensionId() != "" {
		id, err := shareddomain.ParseExtensionID(c.GetClosedExtensionId())
		if err != nil {
			return out, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid closed_extension_id"))
		}
		out.ClosedExtensionID = &id
	}
	return out, nil
}

func flowConfigToProto(c domain.FlowConfig) *atsapbxv1.FlowConfig {
	out := &atsapbxv1.FlowConfig{BusinessStartMinute: wire.Int32(int64(c.BusinessStartMinute)), BusinessEndMinute: wire.Int32(int64(c.BusinessEndMinute)),
		Prompt: c.Prompt, Menu: map[string]string{}, MenuTimeoutSeconds: wire.Int32(int64(c.MenuTimeoutSeconds))}
	for digit, ext := range c.Menu {
		out.Menu[digit] = ext.String()
	}
	if c.OpenExtensionID != nil {
		out.OpenExtensionId = c.OpenExtensionID.String()
	}
	if c.ClosedExtensionID != nil {
		out.ClosedExtensionId = c.ClosedExtensionID.String()
	}
	if c.NoInputExtensionID != nil {
		out.NoInputExtensionId = c.NoInputExtensionID.String()
	}
	if c.InvalidExtensionID != nil {
		out.InvalidExtensionId = c.InvalidExtensionID.String()
	}
	return out
}

func toProtoFlow(v ports.FlowView) *atsapbxv1.Flow {
	return &atsapbxv1.Flow{FlowId: v.ID.String(), Number: v.Number, Name: v.Name, ActiveVersion: wire.Int32(int64(v.ActiveVersion)), Config: flowConfigToProto(v.Config)}
}

func (h *PbxHandler) AssignExtension(ctx context.Context, req *connect.Request[atsapbxv1.AssignExtensionRequest]) (*connect.Response[atsapbxv1.AssignExtensionResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	extensionID, err := parseExtensionID(req.Msg.GetExtensionId())
	if err != nil {
		return nil, err
	}
	principalID, err := shareddomain.ParsePrincipalID(req.Msg.GetPrincipalId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid principal_id"))
	}
	if err := h.svc.AssignExtension(ctx, ports.AssignExtensionCommand{
		TenantID: tenantID, ExtensionID: extensionID, PrincipalID: principalID,
	}); err != nil {
		return nil, mapErr(err)
	}
	view, err := h.svc.GetExtension(ctx, tenantID, extensionID)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.AssignExtensionResponse{Extension: toProtoExtension(view)}), nil
}

func (h *PbxHandler) UnassignExtension(ctx context.Context, req *connect.Request[atsapbxv1.UnassignExtensionRequest]) (*connect.Response[atsapbxv1.UnassignExtensionResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	extensionID, err := parseExtensionID(req.Msg.GetExtensionId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.UnassignExtension(ctx, tenantID, extensionID); err != nil {
		return nil, mapErr(err)
	}
	view, err := h.svc.GetExtension(ctx, tenantID, extensionID)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.UnassignExtensionResponse{Extension: toProtoExtension(view)}), nil
}

func (h *PbxHandler) GetOutboundPolicy(ctx context.Context, req *connect.Request[atsapbxv1.GetOutboundPolicyRequest]) (*connect.Response[atsapbxv1.GetOutboundPolicyResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	p, err := h.svc.GetOutboundPolicy(ctx, tenantID)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.GetOutboundPolicyResponse{Policy: outboundPolicyToProto(p)}), nil
}

func (h *PbxHandler) UpdateOutboundPolicy(ctx context.Context, req *connect.Request[atsapbxv1.UpdateOutboundPolicyRequest]) (*connect.Response[atsapbxv1.UpdateOutboundPolicyResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	policy := domain.OutboundPolicy{
		DailySpendCapMicros: req.Msg.GetDailySpendCapMicros(), MonthlySpendCapMicros: req.Msg.GetMonthlySpendCapMicros(),
		BlockedPrefixes: req.Msg.GetBlockedPrefixes(), AnomalyCallThreshold: int(req.Msg.GetAnomalyCallThreshold()),
		AnomalyWindow:       time.Duration(req.Msg.GetAnomalyWindowSeconds()) * time.Second,
		BusinessStartMinute: int(req.Msg.GetBusinessStartMinute()), BusinessEndMinute: int(req.Msg.GetBusinessEndMinute()),
	}
	if err := h.svc.SetOutboundPolicy(ctx, ports.SetOutboundPolicyCommand{TenantID: tenantID, Policy: policy}); err != nil {
		return nil, mapErr(err)
	}
	view, err := h.svc.GetOutboundPolicy(ctx, tenantID)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.UpdateOutboundPolicyResponse{Policy: outboundPolicyToProto(view)}), nil
}

func (h *PbxHandler) ResumeOutbound(ctx context.Context, req *connect.Request[atsapbxv1.ResumeOutboundRequest]) (*connect.Response[atsapbxv1.ResumeOutboundResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.ResumeOutbound(ctx, tenantID); err != nil {
		return nil, mapErr(err)
	}
	view, err := h.svc.GetOutboundPolicy(ctx, tenantID)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.ResumeOutboundResponse{Policy: outboundPolicyToProto(view)}), nil
}

func outboundPolicyToProto(v ports.OutboundPolicyView) *atsapbxv1.OutboundPolicy {
	out := &atsapbxv1.OutboundPolicy{
		DailySpendCapMicros: v.DailySpendCapMicros, MonthlySpendCapMicros: v.MonthlySpendCapMicros,
		BlockedPrefixes: v.BlockedPrefixes, AnomalyCallThreshold: wire.Int32(int64(v.AnomalyCallThreshold)),
		AnomalyWindowSeconds: wire.Int32(int64(v.AnomalyWindow.Seconds())),
		BusinessStartMinute:  wire.Int32(int64(v.BusinessStartMinute)), BusinessEndMinute: wire.Int32(int64(v.BusinessEndMinute)),
		Suspended: v.Suspended, SuspendedReason: v.SuspendedReason,
	}
	if v.SuspendedAt != nil {
		out.SuspendedAt = timestamppb.New(*v.SuspendedAt)
	}
	return out
}

func (h *PbxHandler) GetSipRegistrar(ctx context.Context, req *connect.Request[atsapbxv1.GetSipRegistrarRequest]) (*connect.Response[atsapbxv1.GetSipRegistrarResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	r, err := h.svc.SipRegistrar(ctx, tenantID)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.GetSipRegistrarResponse{
		Configured: r.Configured,
		Host:       r.Host,
		Port:       wire.Int32(r.Port),
		Transport:  r.Transport,
		WebrtcUrl:  r.WebRTCURL,
		IceServers: iceServersToProto(r.ICEServers),
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
	case errors.Is(err, ports.ErrEntitlementExceeded):
		// FAILED_PRECONDITION, never PERMISSION_DENIED: the caller holds
		// the permission, the installation lacks the licence (AC-06.14).
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("licence entitlement exceeded: extension limit reached"))
	case errors.Is(err, ports.ErrNotLicensed):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("no licence is applied: apply one before adding extensions"))
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
		ExtensionId:            v.ID.String(),
		Number:                 v.Number,
		DisplayName:            v.DisplayName,
		AuthUsername:           v.AuthUsername,
		DeviceType:             deviceTypeToProto(v.DeviceType),
		OutboundClass:          outboundToProto(v.Outbound),
		AssignedPrincipalId:    v.AssignedPrincipalID.String(),
		AssignedPrincipalName:  v.AssignedPrincipalName,
		AssignedPrincipalEmail: v.AssignedPrincipalEmail,
		Registration:           registrationToProto(v.Registration),
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

// outboundFromProto maps the wire enum onto the domain type. An
// unspecified value becomes empty, and the application supplies the safe
// NATIONAL default; an unknown non-zero value is refused by the domain.
func outboundFromProto(c atsapbxv1.OutboundClass) domain.OutboundClass {
	switch c {
	case atsapbxv1.OutboundClass_OUTBOUND_CLASS_UNSPECIFIED:
		return ""
	case atsapbxv1.OutboundClass_OUTBOUND_CLASS_INTERNAL:
		return domain.OutboundInternal
	case atsapbxv1.OutboundClass_OUTBOUND_CLASS_NATIONAL:
		return domain.OutboundNational
	case atsapbxv1.OutboundClass_OUTBOUND_CLASS_INTERNATIONAL:
		return domain.OutboundInternational
	default:
		return domain.OutboundClass(c.String())
	}
}

func outboundToProto(c domain.OutboundClass) atsapbxv1.OutboundClass {
	switch c {
	case domain.OutboundInternal:
		return atsapbxv1.OutboundClass_OUTBOUND_CLASS_INTERNAL
	case domain.OutboundNational:
		return atsapbxv1.OutboundClass_OUTBOUND_CLASS_NATIONAL
	case domain.OutboundInternational:
		return atsapbxv1.OutboundClass_OUTBOUND_CLASS_INTERNATIONAL
	default:
		return atsapbxv1.OutboundClass_OUTBOUND_CLASS_UNSPECIFIED
	}
}

func iceServersToProto(in []ports.ICEServer) []*atsapbxv1.IceServer {
	out := make([]*atsapbxv1.IceServer, 0, len(in))
	for _, s := range in {
		out = append(out, &atsapbxv1.IceServer{Urls: s.URLs, Username: s.Username, Credential: s.Credential})
	}
	return out
}
