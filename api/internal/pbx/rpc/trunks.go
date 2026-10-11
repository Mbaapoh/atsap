package rpc

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

func (h *PbxHandler) CreateTrunk(ctx context.Context, req *connect.Request[atsapbxv1.CreateTrunkRequest]) (*connect.Response[atsapbxv1.CreateTrunkResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	m := req.Msg
	view, err := h.svc.CreateTrunk(ctx, ports.CreateTrunkCommand{
		TenantID: tenantID, Name: m.GetName(), Host: m.GetHost(), Port: portOrDefault(m.GetPort()),
		AuthUsername: m.GetAuthUsername(), Secret: m.GetSecret(),
		ChannelLimit: int(m.GetChannelLimit()), Priority: int(m.GetPriority()),
	})
	if err != nil {
		return nil, mapConfigErr(err)
	}
	return connect.NewResponse(&atsapbxv1.CreateTrunkResponse{Trunk: toProtoTrunk(view)}), nil
}

func (h *PbxHandler) ListTrunks(ctx context.Context, req *connect.Request[atsapbxv1.ListTrunksRequest]) (*connect.Response[atsapbxv1.ListTrunksResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	views, err := h.svc.ListTrunks(ctx, tenantID, ports.Page{
		Limit: pageLimit(req.Msg.GetLimit()), Offset: max(int(req.Msg.GetOffset()), 0),
	})
	if err != nil {
		return nil, mapConfigErr(err)
	}
	out := make([]*atsapbxv1.Trunk, 0, len(views))
	for _, v := range views {
		out = append(out, toProtoTrunk(v))
	}
	return connect.NewResponse(&atsapbxv1.ListTrunksResponse{Trunks: out}), nil
}

func (h *PbxHandler) UpdateTrunk(ctx context.Context, req *connect.Request[atsapbxv1.UpdateTrunkRequest]) (*connect.Response[atsapbxv1.UpdateTrunkResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	id, err := parseTrunkID(req.Msg.GetTrunkId())
	if err != nil {
		return nil, err
	}
	m := req.Msg
	view, err := h.svc.UpdateTrunk(ctx, ports.UpdateTrunkCommand{
		TenantID: tenantID, ID: id, Name: m.GetName(), Host: m.GetHost(), Port: portOrDefault(m.GetPort()),
		AuthUsername: m.GetAuthUsername(), Secret: m.GetSecret(),
		ChannelLimit: int(m.GetChannelLimit()), Priority: int(m.GetPriority()),
	})
	if err != nil {
		return nil, mapConfigErr(err)
	}
	return connect.NewResponse(&atsapbxv1.UpdateTrunkResponse{Trunk: toProtoTrunk(view)}), nil
}

func (h *PbxHandler) DeleteTrunk(ctx context.Context, req *connect.Request[atsapbxv1.DeleteTrunkRequest]) (*connect.Response[atsapbxv1.DeleteTrunkResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	id, err := parseTrunkID(req.Msg.GetTrunkId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteTrunk(ctx, tenantID, id); err != nil {
		return nil, mapConfigErr(err)
	}
	return connect.NewResponse(&atsapbxv1.DeleteTrunkResponse{}), nil
}

func (h *PbxHandler) CreateRoute(ctx context.Context, req *connect.Request[atsapbxv1.CreateRouteRequest]) (*connect.Response[atsapbxv1.CreateRouteResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	trunkID, err := parseTrunkID(req.Msg.GetTrunkId())
	if err != nil {
		return nil, err
	}
	view, err := h.svc.CreateRoute(ctx, ports.CreateRouteCommand{
		TenantID: tenantID, PrefixPattern: req.Msg.GetPrefixPattern(), TrunkID: trunkID,
		CostPerMinute: req.Msg.GetCostPerMinuteMicros(), Priority: int(req.Msg.GetPriority()),
	})
	if err != nil {
		return nil, mapConfigErr(err)
	}
	return connect.NewResponse(&atsapbxv1.CreateRouteResponse{Route: toProtoRoute(view)}), nil
}

func (h *PbxHandler) ListRoutes(ctx context.Context, req *connect.Request[atsapbxv1.ListRoutesRequest]) (*connect.Response[atsapbxv1.ListRoutesResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	views, err := h.svc.ListRoutes(ctx, tenantID, ports.Page{
		Limit: pageLimit(req.Msg.GetLimit()), Offset: max(int(req.Msg.GetOffset()), 0),
	})
	if err != nil {
		return nil, mapConfigErr(err)
	}
	out := make([]*atsapbxv1.Route, 0, len(views))
	for _, v := range views {
		out = append(out, toProtoRoute(v))
	}
	return connect.NewResponse(&atsapbxv1.ListRoutesResponse{Routes: out}), nil
}

func (h *PbxHandler) DeleteRoute(ctx context.Context, req *connect.Request[atsapbxv1.DeleteRouteRequest]) (*connect.Response[atsapbxv1.DeleteRouteResponse], error) {
	tenantID, err := h.tenantOf(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	id, err := shareddomain.ParseRouteID(req.Msg.GetRouteId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid route_id"))
	}
	if err := h.svc.DeleteRoute(ctx, tenantID, id); err != nil {
		return nil, mapConfigErr(err)
	}
	return connect.NewResponse(&atsapbxv1.DeleteRouteResponse{}), nil
}

// mapConfigErr is mapErr for trunks and routes, with one difference: an
// invalid input names the offending field (spec trunk-routing, "a reason
// identifying the offending field"). The text is the domain validation
// error, which describes only the caller's own input — never a table,
// constraint or engine object — so it is safe to return. Everything
// else maps exactly as for extensions, including the fixed text for
// internal failures.
func mapConfigErr(err error) error {
	if errors.Is(err, ports.ErrInvalidInput) {
		msg := strings.TrimPrefix(err.Error(), ports.ErrInvalidInput.Error()+": ")
		return connect.NewError(connect.CodeInvalidArgument, errors.New(msg))
	}
	return mapErr(err)
}

func parseTrunkID(s string) (shareddomain.TrunkID, error) {
	id, err := shareddomain.ParseTrunkID(s)
	if err != nil {
		return shareddomain.TrunkID{}, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid trunk_id"))
	}
	return id, nil
}

// portOrDefault applies the SIP default for an unset port, as every
// SIP-trunk interface does. An explicit out-of-range value is still
// refused by validation.
func portOrDefault(p int32) int {
	if p == 0 {
		return domain.DefaultSIPPort
	}
	return int(p)
}

func toProtoTrunk(v ports.TrunkView) *atsapbxv1.Trunk {
	return &atsapbxv1.Trunk{
		TrunkId: v.ID.String(), Name: v.Name, Host: v.Host, Port: int32(v.Port),
		AuthUsername: v.AuthUsername, HasSecret: v.HasSecret,
		ChannelLimit: int32(v.ChannelLimit), Priority: int32(v.Priority), Health: healthToProto(v.Health),
	}
}

func toProtoRoute(v ports.RouteView) *atsapbxv1.Route {
	return &atsapbxv1.Route{
		RouteId: v.ID.String(), PrefixPattern: v.PrefixPattern, TrunkId: v.TrunkID.String(),
		CostPerMinuteMicros: v.CostPerMinute, Priority: int32(v.Priority),
	}
}

func healthToProto(h domain.TrunkHealth) atsapbxv1.TrunkHealth {
	switch h {
	case domain.HealthHealthy:
		return atsapbxv1.TrunkHealth_TRUNK_HEALTH_HEALTHY
	case domain.HealthDegraded:
		return atsapbxv1.TrunkHealth_TRUNK_HEALTH_DEGRADED
	case domain.HealthUnhealthy:
		return atsapbxv1.TrunkHealth_TRUNK_HEALTH_UNHEALTHY
	default:
		return atsapbxv1.TrunkHealth_TRUNK_HEALTH_UNSPECIFIED
	}
}
