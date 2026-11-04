package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	identitydomain "atsap-api/internal/identity/domain"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// AssignExtension makes a membership the owner of an extension (D-65).
// The assignment is tenant-scoped by construction and is audited; it
// never changes the engine projection.
func (s *Service) AssignExtension(ctx context.Context, cmd ports.AssignExtensionCommand) error {
	actor, err := actorFrom(ctx)
	if err != nil {
		return err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionAssignExtension, cmd.TenantID.String()); err != nil {
		return err
	}
	if s.assignments == nil || s.principals == nil {
		return fmt.Errorf("%w: extension assignment is not configured", ports.ErrInvalidInput)
	}
	principal, err := s.principals.GetPrincipal(ctx, cmd.TenantID, cmd.PrincipalID)
	if err != nil {
		return err
	}
	if principal.Status != identitydomain.PrincipalActive {
		return fmt.Errorf("%w: the membership is disabled", ports.ErrInvalidInput)
	}
	assignment := domain.ExtensionAssignment{
		ExtensionID: cmd.ExtensionID, PrincipalID: cmd.PrincipalID, AssignedAt: s.now(),
	}
	if err := assignment.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ports.ErrInvalidInput, err)
	}

	return s.pool.WithTenant(ctx, cmd.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := s.store.Get(ctx, tx, cmd.TenantID, cmd.ExtensionID); err != nil {
			return err
		}
		before, err := s.assignments.Get(ctx, tx, cmd.TenantID, cmd.ExtensionID)
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			return err
		}
		if err := s.assignments.Put(ctx, tx, cmd.TenantID, assignment); err != nil {
			return err
		}
		after := map[string]any{"principal_id": cmd.PrincipalID.String(), "display_name": principal.DisplayName, "email": principal.Email}
		var prior map[string]any
		if err == nil {
			prior = map[string]any{"principal_id": before.PrincipalID.String()}
		}
		return s.appendAuditEntry(ctx, tx, actor, cmd.TenantID, "extension.assigned",
			"extension", cmd.ExtensionID.String(), prior, after)
	})
}

// UnassignExtension removes an owner. Unassigning an extension that has no
// owner succeeds without effect, so a retried request is harmless.
func (s *Service) UnassignExtension(ctx context.Context, tenantID shareddomain.TenantID, extensionID shareddomain.ExtensionID) error {
	actor, err := actorFrom(ctx)
	if err != nil {
		return err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionUnassignExtension, tenantID.String()); err != nil {
		return err
	}
	if s.assignments == nil {
		return nil
	}
	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.assignments.Get(ctx, tx, tenantID, extensionID)
		if errors.Is(err, ports.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := s.assignments.Delete(ctx, tx, tenantID, extensionID); err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, tenantID, "extension.unassigned",
			"extension", extensionID.String(),
			map[string]any{"principal_id": before.PrincipalID.String()}, nil)
	})
}

func (s *Service) attachAssignment(ctx context.Context, tenantID shareddomain.TenantID, view *ports.ExtensionView) error {
	if s.assignments == nil {
		return nil
	}
	var a domain.ExtensionAssignment
	err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		a, err = s.assignments.Get(ctx, tx, tenantID, view.ID)
		return err
	})
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.setAssignment(ctx, tenantID, view, a)
}

func (s *Service) attachAssignments(ctx context.Context, tenantID shareddomain.TenantID, views []ports.ExtensionView) error {
	if s.assignments == nil || len(views) == 0 {
		return nil
	}
	ids := make([]shareddomain.ExtensionID, len(views))
	for i := range views {
		ids[i] = views[i].ID
	}
	var byExtension map[shareddomain.ExtensionID]domain.ExtensionAssignment
	if err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		byExtension, err = s.assignments.List(ctx, tx, tenantID, ids)
		return err
	}); err != nil {
		return err
	}
	principals := map[shareddomain.PrincipalID]identitydomain.Principal{}
	for i := range views {
		a, ok := byExtension[views[i].ID]
		if !ok {
			continue
		}
		p, ok := principals[a.PrincipalID]
		if !ok {
			var err error
			p, err = s.principals.GetPrincipal(ctx, tenantID, a.PrincipalID)
			if err != nil {
				return err
			}
			principals[a.PrincipalID] = p
		}
		views[i].AssignedPrincipalID = a.PrincipalID
		views[i].AssignedPrincipalName = p.DisplayName
		views[i].AssignedPrincipalEmail = p.Email
	}
	return nil
}

func (s *Service) setAssignment(ctx context.Context, tenantID shareddomain.TenantID, view *ports.ExtensionView, a domain.ExtensionAssignment) error {
	p, err := s.principals.GetPrincipal(ctx, tenantID, a.PrincipalID)
	if err != nil {
		return err
	}
	view.AssignedPrincipalID = a.PrincipalID
	view.AssignedPrincipalName = p.DisplayName
	view.AssignedPrincipalEmail = p.Email
	return nil
}
