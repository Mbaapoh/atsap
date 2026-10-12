package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// CreateInboundNumber assigns a DID to one of the tenant's extensions
// (design D7). The extension must exist in the same tenant: another
// tenant's extension reads as not found (INV-10), and the composite
// foreign key enforces the same at the database. A duplicate number in
// the tenant is refused.
func (s *Service) CreateInboundNumber(ctx context.Context, cmd ports.CreateInboundNumberCommand) (ports.InboundNumberView, error) {
	actor, err := actorFrom(ctx)
	if err != nil {
		return ports.InboundNumberView{}, err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionManageInboundNumber, cmd.TenantID.String()); err != nil {
		return ports.InboundNumberView{}, err
	}
	if s.numbers == nil {
		return ports.InboundNumberView{}, errors.New("inbound numbers are not configured")
	}
	normalized, err := domain.NormalizeInboundNumber(cmd.Number)
	if err != nil {
		return ports.InboundNumberView{}, fmt.Errorf("%w: number: %v", ports.ErrInvalidInput, err)
	}

	number := domain.InboundNumber{
		ID:          uuid.New(),
		TenantID:    cmd.TenantID,
		Number:      normalized,
		ExtensionID: cmd.ExtensionID,
	}
	err = s.pool.WithTenant(ctx, cmd.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := s.store.Get(ctx, tx, cmd.TenantID, cmd.ExtensionID); err != nil {
			return err
		}
		if err := s.numbers.Insert(ctx, tx, number); err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, cmd.TenantID, "inbound_number.created",
			auditResourceInboundNumber, number.ID.String(), nil, map[string]any{
				"number":       normalized,
				"extension_id": cmd.ExtensionID.String(),
			})
	})
	if err != nil {
		return ports.InboundNumberView{}, err
	}
	return ports.InboundNumberView{ID: number.ID, Number: normalized, ExtensionID: cmd.ExtensionID}, nil
}

// ListInboundNumbers returns the tenant's DIDs.
func (s *Service) ListInboundNumbers(ctx context.Context, tenantID shareddomain.TenantID) ([]ports.InboundNumberView, error) {
	if _, err := s.authorize(ctx, actionReadInboundNumber, tenantID); err != nil {
		return nil, err
	}
	if s.numbers == nil {
		return nil, errors.New("inbound numbers are not configured")
	}

	var out []ports.InboundNumberView
	err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		numbers, err := s.numbers.List(ctx, tx)
		if err != nil {
			return err
		}
		for _, n := range numbers {
			out = append(out, ports.InboundNumberView{ID: n.ID, Number: n.Number, ExtensionID: n.ExtensionID})
		}
		return nil
	})
	return out, err
}

// DeleteInboundNumber removes one DID. Another tenant's DID reads as
// not found.
func (s *Service) DeleteInboundNumber(ctx context.Context, tenantID shareddomain.TenantID, id uuid.UUID) error {
	actor, err := actorFrom(ctx)
	if err != nil {
		return err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionManageInboundNumber, tenantID.String()); err != nil {
		return err
	}
	if s.numbers == nil {
		return errors.New("inbound numbers are not configured")
	}

	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		// Read before delete so the audit names what left.
		var before map[string]any
		numbers, err := s.numbers.List(ctx, tx)
		if err != nil {
			return err
		}
		for _, n := range numbers {
			if n.ID == id {
				before = map[string]any{"number": n.Number, "extension_id": n.ExtensionID.String()}
			}
		}
		if err := s.numbers.Delete(ctx, tx, id); err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, tenantID, "inbound_number.deleted",
			auditResourceInboundNumber, id.String(), before, nil)
	})
}
