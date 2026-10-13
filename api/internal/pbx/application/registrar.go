package application

import (
	"context"

	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// WithRegistrar returns a copy of s that reports r as the address devices
// register to. An empty host means the installer has not configured one,
// and the address is reported as not configured.
func (s *Service) WithRegistrar(r ports.SipRegistrar) *Service {
	clone := *s
	if r.Host == "" {
		r = ports.SipRegistrar{}
	} else {
		r.Configured = true
	}
	clone.registrar = r
	return &clone
}

// SipRegistrar returns where the tenant's devices register. Whoever may
// read the tenant's extensions may see it: it is the address every phone
// of the installation already uses, and nothing tenant-specific.
func (s *Service) SipRegistrar(ctx context.Context, tenantID shareddomain.TenantID) (ports.SipRegistrar, error) {
	actor, err := actorFrom(ctx)
	if err != nil {
		return ports.SipRegistrar{}, err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionReadExtension, tenantID.String()); err != nil {
		return ports.SipRegistrar{}, err
	}
	return s.registrar, nil
}
