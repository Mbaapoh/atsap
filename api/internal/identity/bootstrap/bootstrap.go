// Package bootstrap creates the first tenant and administrator of an
// empty installation (identity-api task 3.3). It exists because every
// provisioning operation requires a token, and every token requires a
// principal that provisioning would have to create — something outside
// that cycle must mint the first pair.
//
// It is invoked by the operator (CLI subcommand), not over the network,
// and refuses to run once any tenant exists, so it cannot be used a
// second time to grant additional administrators.
package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"atsap-api/internal/identity/application"
	"atsap-api/internal/identity/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

// ErrAlreadyBootstrapped is returned when a tenant already exists: the
// bootstrap path is one-time by design.
var ErrAlreadyBootstrapped = errors.New("installation is already bootstrapped: refusing to create a second tenant")

// Store is the minimal storage surface bootstrap needs beyond the
// application service: the one-time emptiness check.
type Store interface {
	HasTenants(ctx context.Context) (bool, error)
}

// Params carries what the operator supplies to create the first tenant
// and its platform administrator.
type Params struct {
	TenantName    string
	ResidencyZone string // fixed at provisioning; never blank
	Username      string
	Email         string
	Password      string
}

// Run provisions the first tenant and a PLATFORM_ADMIN principal bound
// at system scope, so the operator can immediately provision further
// tenants and their administrators over the API. The mutations are
// audited as system actions.
func Run(ctx context.Context, store Store, svc *application.Service, p Params) (shareddomain.TenantID, error) {
	exists, err := store.HasTenants(ctx)
	if err != nil {
		return shareddomain.TenantID{}, fmt.Errorf("check for existing tenants: %w", err)
	}
	if exists {
		return shareddomain.TenantID{}, ErrAlreadyBootstrapped
	}

	tenant, err := svc.ProvisionTenant(ctx, application.SystemActor(), p.TenantName, p.ResidencyZone)
	if err != nil {
		return shareddomain.TenantID{}, fmt.Errorf("provision first tenant: %w", err)
	}

	principal, err := svc.ProvisionPrincipal(ctx, application.SystemActor(),
		tenant.ID, p.Username, p.Email, p.Password, "PLATFORM_ADMIN")
	if err != nil {
		return shareddomain.TenantID{}, fmt.Errorf("provision platform administrator: %w", err)
	}

	if err := svc.GrantRole(ctx, application.SystemActor(),
		tenant.ID, principal.ID, "PLATFORM_ADMIN", domain.ScopeSystem); err != nil {
		return shareddomain.TenantID{}, fmt.Errorf("grant platform scope: %w", err)
	}

	return tenant.ID, nil
}
