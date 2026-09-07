// Package rpc implements the identity context's public ConnectRPC
// surface (openspec change identity-api): it maps each of the nine
// IdentityService RPCs onto the existing application.Service, translating
// port errors to Connect codes and enforcing authorization before any
// mutation runs.
//
// Every handler except AuthenticateUser assumes the AuthInterceptor has
// already run and attached a TenantContext (application.WithTenantContext).
// A handler that finds no context treats it as "no permission", never as
// "skip the check".
package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/genproto/atsapbx/v1/atsapbxv1connect"
	"atsap-api/internal/identity/application"
	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// IdentityHandler implements atsapbxv1connect.IdentityServiceHandler.
type IdentityHandler struct {
	svc *application.Service

	atsapbxv1connect.UnimplementedIdentityServiceHandler
}

var _ atsapbxv1connect.IdentityServiceHandler = (*IdentityHandler)(nil)

// NewIdentityHandler returns a handler backed by svc.
func NewIdentityHandler(svc *application.Service) *IdentityHandler {
	return &IdentityHandler{svc: svc}
}

// defaultAuditLimit bounds a ListAudit with no limit, so one call cannot
// demand the whole table.
const defaultAuditLimit = 100

// --- AuthenticateUser ----------------------------------------------

func (h *IdentityHandler) AuthenticateUser(ctx context.Context, req *connect.Request[atsapbxv1.AuthenticateUserRequest]) (*connect.Response[atsapbxv1.AuthenticateUserResponse], error) {
	tenantID, err := shareddomain.ParseTenantID(req.Msg.GetTenantId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid tenant_id"))
	}
	token, err := h.svc.AuthenticateUser(ctx, tenantID, req.Msg.GetUsername(), req.Msg.GetPassword())
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.AuthenticateUserResponse{
		Token:       token.Token,
		ExpiresAt:   timestamppb.New(token.ExpiresAt),
		TenantId:    token.TenantID.String(),
		PrincipalId: token.PrincipalID.String(),
	}), nil
}

// --- Tenant administration ------------------------------------------

func (h *IdentityHandler) ProvisionTenant(ctx context.Context, req *connect.Request[atsapbxv1.ProvisionTenantRequest]) (*connect.Response[atsapbxv1.ProvisionTenantResponse], error) {
	tc, err := requireContext(ctx)
	if err != nil {
		return nil, err
	}
	// Creating a tenant is installation authority, never authority within
	// a tenant. AuthorizeSystem requires a system-scope binding, so a
	// TENANT_ADMIN wildcard cannot match tenant.create against its own
	// tenant (identity-api task 2a.2).
	if err := h.svc.AuthorizeSystem(ctx, tc.PrincipalID, tc.TenantID, "tenant.create"); err != nil {
		return nil, mapErr(err)
	}
	tenant, err := h.svc.ProvisionTenant(ctx, h.actor(tc, req), req.Msg.GetName(), req.Msg.GetResidencyZone())
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.ProvisionTenantResponse{Tenant: toProtoTenant(tenant)}), nil
}

func (h *IdentityHandler) ProvisionPrincipal(ctx context.Context, req *connect.Request[atsapbxv1.ProvisionPrincipalRequest]) (*connect.Response[atsapbxv1.ProvisionPrincipalResponse], error) {
	actor, err := h.authorizeForTenant(ctx, "principal.create", req, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	tenantID, err := shareddomain.ParseTenantID(req.Msg.GetTenantId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid tenant_id"))
	}
	principal, err := h.svc.ProvisionPrincipal(ctx, actor, tenantID,
		req.Msg.GetUsername(), req.Msg.GetEmail(), req.Msg.GetPassword(), req.Msg.GetRole())
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.ProvisionPrincipalResponse{Principal: toProtoPrincipal(principal)}), nil
}

func (h *IdentityHandler) SetTenantStatus(ctx context.Context, req *connect.Request[atsapbxv1.SetTenantStatusRequest]) (*connect.Response[atsapbxv1.SetTenantStatusResponse], error) {
	actor, err := h.authorizeForTenant(ctx, "tenant.status.set", req, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	tenantID, err := shareddomain.ParseTenantID(req.Msg.GetTenantId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid tenant_id"))
	}
	status, err := tenantStatusFromProto(req.Msg.GetStatus())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := h.svc.SetTenantStatus(ctx, actor, tenantID, status); err != nil {
		return nil, mapErr(err)
	}
	tenant, err := h.svc.LoadTenant(ctx, tenantID)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.SetTenantStatusResponse{Tenant: toProtoTenant(tenant)}), nil
}

func (h *IdentityHandler) SetPrincipalStatus(ctx context.Context, req *connect.Request[atsapbxv1.SetPrincipalStatusRequest]) (*connect.Response[atsapbxv1.SetPrincipalStatusResponse], error) {
	actor, err := h.authorizeForTenant(ctx, "principal.status.set", req, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	tenantID, err := shareddomain.ParseTenantID(req.Msg.GetTenantId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid tenant_id"))
	}
	principalID, err := shareddomain.ParsePrincipalID(req.Msg.GetPrincipalId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid principal_id"))
	}
	status, err := principalStatusFromProto(req.Msg.GetStatus())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := h.svc.SetPrincipalStatus(ctx, actor, tenantID, principalID, status); err != nil {
		return nil, mapErr(err)
	}
	principal, err := h.svc.LoadPrincipal(ctx, tenantID, principalID)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.SetPrincipalStatusResponse{Principal: toProtoPrincipal(principal)}), nil
}

func (h *IdentityHandler) GrantRole(ctx context.Context, req *connect.Request[atsapbxv1.GrantRoleRequest]) (*connect.Response[atsapbxv1.GrantRoleResponse], error) {
	tc, err := requireContext(ctx)
	if err != nil {
		return nil, err
	}
	// A system-scoped grant, or a grant of the platform role, is platform
	// authority and can never be issued by a tenant's own administrators
	// (proto GrantRole scope comment). Everything else authorizes against
	// the target tenant like any other mutation.
	scope := req.Msg.GetScope()
	if scope == "" {
		scope = domain.ScopeTenant
	}
	var actor application.Actor
	if scope == domain.ScopeSystem || req.Msg.GetRole() == "PLATFORM_ADMIN" {
		if err := h.svc.AuthorizeSystem(ctx, tc.PrincipalID, tc.TenantID, "principal.role.grant"); err != nil {
			return nil, mapErr(err)
		}
		actor = h.actor(tc, req)
	} else if actor, err = h.authorizeForTenant(ctx, "principal.role.grant", req, req.Msg.GetTenantId()); err != nil {
		return nil, err
	}
	tenantID, err := shareddomain.ParseTenantID(req.Msg.GetTenantId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid tenant_id"))
	}
	principalID, err := shareddomain.ParsePrincipalID(req.Msg.GetPrincipalId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid principal_id"))
	}
	if err := h.svc.GrantRole(ctx, actor, tenantID, principalID, req.Msg.GetRole(), scope); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.GrantRoleResponse{}), nil
}

// --- API keys --------------------------------------------------------

func (h *IdentityHandler) IssueApiKey(ctx context.Context, req *connect.Request[atsapbxv1.IssueApiKeyRequest]) (*connect.Response[atsapbxv1.IssueApiKeyResponse], error) {
	actor, err := h.authorizeForTenant(ctx, "apikey.issue", req, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	tenantID, err := shareddomain.ParseTenantID(req.Msg.GetTenantId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid tenant_id"))
	}
	principalID, err := shareddomain.ParsePrincipalID(req.Msg.GetPrincipalId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid principal_id"))
	}
	var expiresAt *time.Time
	if ts := req.Msg.GetExpiresAt(); ts != nil && ts.IsValid() {
		t := ts.AsTime()
		expiresAt = &t
	}
	raw, key, err := h.svc.IssueAPIKey(ctx, actor, tenantID, principalID, expiresAt)
	if err != nil {
		return nil, mapErr(err)
	}
	resp := &atsapbxv1.IssueApiKeyResponse{
		ApiKeyId: key.ID.String(),
		RawKey:   raw, // the one and only place the usable key appears
	}
	if key.ExpiresAt != nil {
		resp.ExpiresAt = timestamppb.New(*key.ExpiresAt)
	}
	return connect.NewResponse(resp), nil
}

func (h *IdentityHandler) RevokeApiKey(ctx context.Context, req *connect.Request[atsapbxv1.RevokeApiKeyRequest]) (*connect.Response[atsapbxv1.RevokeApiKeyResponse], error) {
	actor, err := h.authorizeForTenant(ctx, "apikey.revoke", req, req.Msg.GetTenantId())
	if err != nil {
		return nil, err
	}
	tenantID, err := shareddomain.ParseTenantID(req.Msg.GetTenantId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid tenant_id"))
	}
	keyID, err := shareddomain.ParseApiKeyID(req.Msg.GetApiKeyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid api_key_id"))
	}
	if err := h.svc.RevokeAPIKey(ctx, actor, tenantID, keyID); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.RevokeApiKeyResponse{}), nil
}

// --- Audit -----------------------------------------------------------

func (h *IdentityHandler) ListAudit(ctx context.Context, req *connect.Request[atsapbxv1.ListAuditRequest]) (*connect.Response[atsapbxv1.ListAuditResponse], error) {
	if _, err := h.authorizeForTenant(ctx, "audit.read", req, req.Msg.GetTenantId()); err != nil {
		return nil, err
	}
	tenantID, err := shareddomain.ParseTenantID(req.Msg.GetTenantId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid tenant_id"))
	}
	limit := int(req.Msg.GetLimit())
	if limit <= 0 {
		limit = defaultAuditLimit
	}
	entries, err := h.svc.ListAudit(ctx, tenantID, limit)
	if err != nil {
		return nil, mapErr(err)
	}
	records := make([]*atsapbxv1.AuditRecord, 0, len(entries))
	for _, e := range entries {
		records = append(records, toProtoAudit(e))
	}
	return connect.NewResponse(&atsapbxv1.ListAuditResponse{Records: records}), nil
}

// --- authorization helpers ------------------------------------------

// authorizeForTenant runs the actor/authorization preamble for an RPC
// whose target tenant the body names. A tenant-scoped caller is bound to
// its own tenant (and the interceptor enforces the body match); a
// system-scoped caller may act on any tenant, authorized through its
// system binding.
func (h *IdentityHandler) authorizeForTenant(ctx context.Context, action string, req connect.AnyRequest, targetTenant string) (application.Actor, error) {
	tc, err := requireContext(ctx)
	if err != nil {
		return application.Actor{}, err
	}
	if targetTenant == tc.TenantID.String() {
		err = h.svc.AuthorizeAction(ctx, tc.PrincipalID, action, targetTenant)
	} else {
		err = h.svc.AuthorizeSystem(ctx, tc.PrincipalID, tc.TenantID, action)
	}
	if err != nil {
		return application.Actor{}, mapErr(err)
	}
	return h.actor(tc, req), nil
}

func requireContext(ctx context.Context) (*ports.TenantContext, error) {
	tc, ok := application.TenantContextFrom(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	return tc, nil
}

// actor builds the audit actor for an authenticated caller, capturing the
// client address when the transport provides one.
func (h *IdentityHandler) actor(tc *ports.TenantContext, req connect.AnyRequest) application.Actor {
	return application.PrincipalActor(tc.PrincipalID, clientIP(req.Peer()))
}

// clientIP extracts the caller's host from the connect peer address.
func clientIP(peer connect.Peer) net.IP {
	host, _, err := net.SplitHostPort(peer.Addr)
	if err != nil {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip
	}
	return nil
}

// --- error mapping ---------------------------------------------------

// mapErr converts a service error to a Connect error. Failures disclose
// nothing about other accounts or tenants (spec requirement), so the
// mapping is by sentinel and the message is fixed.
func mapErr(err error) error {
	switch {
	case errors.Is(err, ports.ErrInvalidCredentials),
		errors.Is(err, ports.ErrPrincipalNotActive),
		errors.Is(err, ports.ErrTenantNotActive):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
	case errors.Is(err, ports.ErrPermissionDenied):
		return connect.NewError(connect.CodePermissionDenied, errors.New("not permitted"))
	case errors.Is(err, ports.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("not found"))
	case errors.Is(err, ports.ErrUsernameTaken):
		return connect.NewError(connect.CodeAlreadyExists, errors.New("username already exists in this tenant"))
	default:
		return connect.NewError(connect.CodeInternal, fmt.Errorf("identity operation failed: %w", err))
	}
}

// --- proto conversion helpers ---------------------------------------

func toProtoTenant(t domain.Tenant) *atsapbxv1.Tenant {
	return &atsapbxv1.Tenant{
		TenantId:      t.ID.String(),
		Name:          t.Name,
		Status:        tenantStatusToProto(t.Status),
		ResidencyZone: t.ResidencyZone,
		CreatedAt:     timestamppb.New(t.CreatedAt),
	}
}

func toProtoPrincipal(p domain.Principal) *atsapbxv1.Principal {
	return &atsapbxv1.Principal{
		PrincipalId: p.ID.String(),
		TenantId:    p.TenantID.String(),
		Username:    p.Username,
		Email:       p.Email,
		Role:        p.Role,
		Status:      principalStatusToProto(p.Status),
		CreatedAt:   timestamppb.New(p.CreatedAt),
	}
}

func toProtoAudit(e domain.AuditEntry) *atsapbxv1.AuditRecord {
	record := &atsapbxv1.AuditRecord{
		Id:           e.ID,
		TenantId:     e.TenantID.String(),
		ActorId:      e.ActorID.String(),
		ActorType:    string(e.ActorType),
		Action:       e.Action,
		ResourceType: e.ResourceType,
		ResourceId:   e.ResourceID,
		CreatedAt:    timestamppb.New(e.CreatedAt),
	}
	if e.BeforeState != nil {
		record.BeforeStateJson = jsonCompact(e.BeforeState)
	}
	if e.AfterState != nil {
		record.AfterStateJson = jsonCompact(e.AfterState)
	}
	return record
}

func jsonCompact(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func tenantStatusFromProto(s atsapbxv1.TenantStatus) (domain.TenantStatus, error) {
	switch s {
	case atsapbxv1.TenantStatus_TENANT_STATUS_ACTIVE:
		return domain.TenantActive, nil
	case atsapbxv1.TenantStatus_TENANT_STATUS_SUSPENDED:
		return domain.TenantSuspended, nil
	default:
		return "", errors.New("status is required")
	}
}

func tenantStatusToProto(s domain.TenantStatus) atsapbxv1.TenantStatus {
	switch s {
	case domain.TenantSuspended:
		return atsapbxv1.TenantStatus_TENANT_STATUS_SUSPENDED
	default:
		return atsapbxv1.TenantStatus_TENANT_STATUS_ACTIVE
	}
}

func principalStatusFromProto(s atsapbxv1.PrincipalStatus) (domain.PrincipalStatus, error) {
	switch s {
	case atsapbxv1.PrincipalStatus_PRINCIPAL_STATUS_ACTIVE:
		return domain.PrincipalActive, nil
	case atsapbxv1.PrincipalStatus_PRINCIPAL_STATUS_DISABLED:
		return domain.PrincipalDisabled, nil
	default:
		return "", errors.New("status is required")
	}
}

func principalStatusToProto(s domain.PrincipalStatus) atsapbxv1.PrincipalStatus {
	switch s {
	case domain.PrincipalDisabled:
		return atsapbxv1.PrincipalStatus_PRINCIPAL_STATUS_DISABLED
	default:
		return atsapbxv1.PrincipalStatus_PRINCIPAL_STATUS_ACTIVE
	}
}

// TenantID extracts the tenant_id a request body names, if it names one,
// for the AuthInterceptor's body-vs-token tenant rule. It lives here
// (next to the generated types) so the interceptor never has to import
// the proto package.
func TenantID(request any) (string, bool) {
	switch r := request.(type) {
	case *atsapbxv1.AuthenticateUserRequest:
		return r.GetTenantId(), r.GetTenantId() != ""
	case *atsapbxv1.ProvisionPrincipalRequest:
		return r.GetTenantId(), r.GetTenantId() != ""
	case *atsapbxv1.SetTenantStatusRequest:
		return r.GetTenantId(), r.GetTenantId() != ""
	case *atsapbxv1.SetPrincipalStatusRequest:
		return r.GetTenantId(), r.GetTenantId() != ""
	case *atsapbxv1.GrantRoleRequest:
		return r.GetTenantId(), r.GetTenantId() != ""
	case *atsapbxv1.IssueApiKeyRequest:
		return r.GetTenantId(), r.GetTenantId() != ""
	case *atsapbxv1.RevokeApiKeyRequest:
		return r.GetTenantId(), r.GetTenantId() != ""
	case *atsapbxv1.ListAuditRequest:
		return r.GetTenantId(), r.GetTenantId() != ""
	default:
		return "", false
	}
}
