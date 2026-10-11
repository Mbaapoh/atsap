package main

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	identityapp "atsap-api/internal/identity/application"
	identitydomain "atsap-api/internal/identity/domain"
	identityports "atsap-api/internal/identity/ports"
	licensingrpc "atsap-api/internal/licensing/rpc"
)

// licenceAuthority and licenceAuditor satisfy the ports licensing's wire
// surface declares, over identity (console-apis-phase-a design D1).
// licensing is Tier 0 and may not import identity; the composition root
// is the one place permitted to know both, as it already is for the
// capacity verdict and the provisioning caps.
type licenceAuthority struct {
	identity *identityapp.Service
}

func (a licenceAuthority) RequireAuthenticated(ctx context.Context) error {
	if tc, ok := identityapp.TenantContextFrom(ctx); !ok || tc == nil {
		return licensingrpc.ErrUnauthenticated
	}
	return nil
}

// RequireSystemScope: a licence belongs to the installation, so applying
// one takes installation-wide authority — a tenant administrator's
// wildcard does not reach it, because it is bound at tenant scope.
func (a licenceAuthority) RequireSystemScope(ctx context.Context, action string) error {
	tc, ok := identityapp.TenantContextFrom(ctx)
	if !ok || tc == nil {
		return licensingrpc.ErrUnauthenticated
	}
	if err := a.identity.AuthorizeSystem(ctx, tc.PrincipalID, tc.TenantID, action); err != nil {
		if errors.Is(err, identityports.ErrPermissionDenied) {
			return licensingrpc.ErrPermissionDenied
		}
		return err
	}
	return nil
}

type licenceAuditor struct {
	identity *identityapp.Service
}

// RecordLicenceApplied writes one audit row under the caller's tenant.
// It is given edition and channels only: the token never reaches it.
func (a licenceAuditor) RecordLicenceApplied(ctx context.Context, edition string, channels int) error {
	tc, ok := identityapp.TenantContextFrom(ctx)
	if !ok || tc == nil {
		return licensingrpc.ErrUnauthenticated
	}
	return a.identity.RecordAudit(ctx, identitydomain.NewAuditEntry(
		tc.TenantID, uuid.UUID(tc.PrincipalID), identitydomain.ActorPrincipal,
		licensingrpc.ActionApplyLicence, "licence", "installation",
		nil, map[string]any{"edition": edition, "channels": channels},
		nil, time.Now().UTC(),
	))
}

// noTenantInRequest is LicensingService's tenant matcher: its requests
// name no tenant, because the licence is the installation's.
func noTenantInRequest(any) (string, bool) { return "", false }
