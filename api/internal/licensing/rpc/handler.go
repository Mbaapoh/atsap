// Package rpc is licensing's wire surface (LLD-08 §6): LicensingService.
//
// licensing is Tier 0 and imports nothing (HLD 04 §10.1), so this package
// cannot ask identity who the caller is or write identity's audit log.
// It declares what it needs instead — Authority and Auditor — and the
// composition root satisfies both over identity, the pattern the
// provisioning caps established (console-apis-phase-a design D1).
package rpc

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/genproto/atsapbx/v1/atsapbxv1connect"
	"atsap-api/internal/licensing/domain"
	"atsap-api/internal/licensing/ports"
)

// Authority answers "may this request do that" from the authenticated
// context the transport attached. Both return a *connect.Error-free
// sentinel; the handler maps it.
type Authority interface {
	// RequireAuthenticated fails unless the request carries a valid
	// identity.
	RequireAuthenticated(ctx context.Context) error
	// RequireSystemScope fails unless the caller holds installation-wide
	// authority for action.
	RequireSystemScope(ctx context.Context, action string) error
}

// Auditor records that a licence was applied. It is given the outcome
// only — edition and capacity — never the token.
type Auditor interface {
	RecordLicenceApplied(ctx context.Context, edition string, channels int) error
}

// Licence is the slice of the licensing application this surface uses.
type Licence interface {
	Status(ctx context.Context) (ports.LicenseStatus, error)
	ApplyLicenseKey(ctx context.Context, token string) error
}

// Sentinels an Authority returns.
var (
	ErrUnauthenticated  = errors.New("unauthenticated")
	ErrPermissionDenied = errors.New("permission denied")
)

// ActionApplyLicence is the action ApplyLicenseKey authorizes.
const ActionApplyLicence = "licence.apply"

// Handler implements LicensingService.
type Handler struct {
	licence   Licence
	authority Authority
	auditor   Auditor
}

var _ atsapbxv1connect.LicensingServiceHandler = (*Handler)(nil)

// NewHandler wires the handler.
func NewHandler(licence Licence, authority Authority, auditor Auditor) *Handler {
	return &Handler{licence: licence, authority: authority, auditor: auditor}
}

// GetLicenseStatus returns the licence in force to any authenticated
// caller.
func (h *Handler) GetLicenseStatus(ctx context.Context, _ *connect.Request[atsapbxv1.GetLicenseStatusRequest]) (*connect.Response[atsapbxv1.GetLicenseStatusResponse], error) {
	if err := h.authority.RequireAuthenticated(ctx); err != nil {
		return nil, mapAuthErr(err)
	}
	st, err := h.licence.Status(ctx)
	if err != nil {
		return nil, internalErr()
	}
	return connect.NewResponse(&atsapbxv1.GetLicenseStatusResponse{Status: toProto(st)}), nil
}

// ApplyLicenseKey applies a token for a system-scope caller (design D3).
// The token never appears in a response, an error or the audit record:
// every error here is built from a constant.
func (h *Handler) ApplyLicenseKey(ctx context.Context, req *connect.Request[atsapbxv1.ApplyLicenseKeyRequest]) (*connect.Response[atsapbxv1.ApplyLicenseKeyResponse], error) {
	if err := h.authority.RequireSystemScope(ctx, ActionApplyLicence); err != nil {
		return nil, mapAuthErr(err)
	}
	if err := h.licence.ApplyLicenseKey(ctx, req.Msg.GetToken()); err != nil {
		if errors.Is(err, domain.ErrUntrusted) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("licence token does not verify"))
		}
		return nil, internalErr()
	}
	st, err := h.licence.Status(ctx)
	if err != nil {
		return nil, internalErr()
	}
	// The licence is applied; an audit failure is logged by the auditor
	// and must not report the application itself as failed.
	_ = h.auditor.RecordLicenceApplied(ctx, st.Entitlement.Edition, st.Entitlement.Channels)
	return connect.NewResponse(&atsapbxv1.ApplyLicenseKeyResponse{Status: toProto(st)}), nil
}

func mapAuthErr(err error) error {
	if errors.Is(err, ErrPermissionDenied) {
		return connect.NewError(connect.CodePermissionDenied, errors.New("not permitted"))
	}
	return connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
}

func internalErr() error {
	return connect.NewError(connect.CodeInternal, errors.New("licence operation failed"))
}

func toProto(st ports.LicenseStatus) *atsapbxv1.LicenseStatus {
	e := st.Entitlement
	out := &atsapbxv1.LicenseStatus{
		State:         stateToProto(e.State),
		Edition:       e.Edition,
		Channels:      int32(e.Channels),
		ChannelsInUse: int32(st.ChannelsInUse),
		MaxTenants:    int32(e.MaxTenants),
		MaxExtensions: int32(e.MaxExtensions),
		Condition:     conditionToProto(st.Conditions.Condition),
		Tier:          tierToProto(st.Conditions.Tier),
		Capacity:      capacityToProto(st.Conditions.Capacity),
	}
	if e.State != domain.StateValid {
		out.Reason = string(e.Reason)
	}
	if st.ExpiresAt != nil {
		out.ExpiresAt = timestamppb.New(*st.ExpiresAt)
	}
	if st.LastConfirmedAt != nil {
		out.LastConfirmedAt = timestamppb.New(*st.LastConfirmedAt)
	}
	return out
}

func stateToProto(s domain.State) atsapbxv1.LicenseState {
	switch s {
	case domain.StateSetup:
		return atsapbxv1.LicenseState_LICENSE_STATE_NOT_ACTIVATED
	case domain.StateValid:
		return atsapbxv1.LicenseState_LICENSE_STATE_VALID
	case domain.StateUnverified:
		return atsapbxv1.LicenseState_LICENSE_STATE_UNVERIFIED
	case domain.StateDegraded:
		return atsapbxv1.LicenseState_LICENSE_STATE_DEGRADED
	default:
		return atsapbxv1.LicenseState_LICENSE_STATE_UNSPECIFIED
	}
}

func conditionToProto(c domain.Condition) atsapbxv1.LicenseCondition {
	switch c {
	case domain.ConditionSetup:
		return atsapbxv1.LicenseCondition_LICENSE_CONDITION_SETUP
	case domain.ConditionDegraded:
		return atsapbxv1.LicenseCondition_LICENSE_CONDITION_DEGRADED
	case domain.ConditionCapacityReached:
		return atsapbxv1.LicenseCondition_LICENSE_CONDITION_CAPACITY_REACHED
	case domain.ConditionUnverified:
		return atsapbxv1.LicenseCondition_LICENSE_CONDITION_ENTITLEMENT_UNVERIFIED
	case domain.ConditionCapacityWarning:
		return atsapbxv1.LicenseCondition_LICENSE_CONDITION_CAPACITY_WARNING
	case domain.ConditionFreeCommunity:
		return atsapbxv1.LicenseCondition_LICENSE_CONDITION_FREE_COMMUNITY
	case domain.ConditionValid:
		return atsapbxv1.LicenseCondition_LICENSE_CONDITION_VALID
	default:
		return atsapbxv1.LicenseCondition_LICENSE_CONDITION_UNSPECIFIED
	}
}

func tierToProto(t domain.Tier) atsapbxv1.LicenseTier {
	switch t {
	case domain.TierFreeCommunity:
		return atsapbxv1.LicenseTier_LICENSE_TIER_FREE_COMMUNITY
	case domain.TierCommercial:
		return atsapbxv1.LicenseTier_LICENSE_TIER_COMMERCIAL
	default:
		return atsapbxv1.LicenseTier_LICENSE_TIER_UNSPECIFIED
	}
}

func capacityToProto(c domain.CapacityLevel) atsapbxv1.CapacityLevel {
	switch c {
	case domain.CapacityNormal:
		return atsapbxv1.CapacityLevel_CAPACITY_LEVEL_NORMAL
	case domain.CapacityWarning:
		return atsapbxv1.CapacityLevel_CAPACITY_LEVEL_WARNING
	case domain.CapacityReached:
		return atsapbxv1.CapacityLevel_CAPACITY_LEVEL_REACHED
	default:
		return atsapbxv1.CapacityLevel_CAPACITY_LEVEL_UNSPECIFIED
	}
}
