package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// CreateFlow publishes the first revision of an IVR flow.
func (s *Service) CreateFlow(ctx context.Context, cmd ports.CreateFlowCommand) (ports.FlowView, error) {
	actor, err := actorFrom(ctx)
	if err != nil {
		return ports.FlowView{}, err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionManageFlows, cmd.TenantID.String()); err != nil {
		return ports.FlowView{}, err
	}
	if s.flows == nil {
		return ports.FlowView{}, fmt.Errorf("%w: flows are not configured", ports.ErrInvalidInput)
	}
	number := domain.NormalizeFlowNumber(cmd.Number)
	flow := domain.Flow{ID: shareddomain.NewFlowID(), TenantID: cmd.TenantID, Number: number, Name: cmd.Name, ActiveVersion: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
	if err := flow.Validate(); err != nil {
		return ports.FlowView{}, fmt.Errorf("%w: %v", ports.ErrInvalidInput, err)
	}
	if err := cmd.Config.Validate(); err != nil {
		return ports.FlowView{}, fmt.Errorf("%w: %v", ports.ErrInvalidInput, err)
	}
	version := domain.FlowVersion{FlowID: flow.ID, Version: 1, Config: cmd.Config, PublishedAt: s.now(), PublishedBy: actor.PrincipalID}
	err = s.pool.WithTenant(ctx, cmd.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		if s.inbound.Numbers != nil {
			if _, err := s.inbound.Numbers.FindByNumber(ctx, tx, number); err == nil {
				return ports.ErrInboundNumberTaken
			} else if !errors.Is(err, ports.ErrNotFound) {
				return err
			}
		}
		if err := s.flows.Create(ctx, tx, cmd.TenantID, flow, version); err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, cmd.TenantID, "flow.created", "flow", flow.ID.String(), nil,
			map[string]any{"number": flow.Number, "name": flow.Name, "version": 1})
	})
	if err != nil {
		return ports.FlowView{}, err
	}
	return flowView(flow, version), nil
}

// GetFlow reads the active revision.
func (s *Service) GetFlow(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.FlowID) (ports.FlowView, error) {
	actor, err := actorFrom(ctx)
	if err != nil {
		return ports.FlowView{}, err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionManageFlows, tenantID.String()); err != nil {
		return ports.FlowView{}, err
	}
	if s.flows == nil {
		return ports.FlowView{}, ports.ErrNotFound
	}
	var f domain.Flow
	var v domain.FlowVersion
	err = s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		f, v, err = s.flows.Get(ctx, tx, tenantID, id)
		return err
	})
	if err != nil {
		return ports.FlowView{}, err
	}
	return flowView(f, v), nil
}

// ListFlows reads every flow with its active revision.
func (s *Service) ListFlows(ctx context.Context, tenantID shareddomain.TenantID) ([]ports.FlowView, error) {
	actor, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionManageFlows, tenantID.String()); err != nil {
		return nil, err
	}
	if s.flows == nil {
		return nil, nil
	}
	var out []ports.FlowView
	err = s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.flows.List(ctx, tx, tenantID)
		return err
	})
	return out, err
}

// PublishFlow appends a revision and makes it active.
func (s *Service) PublishFlow(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.FlowID, cfg domain.FlowConfig) (ports.FlowView, error) {
	actor, err := actorFrom(ctx)
	if err != nil {
		return ports.FlowView{}, err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionManageFlows, tenantID.String()); err != nil {
		return ports.FlowView{}, err
	}
	if err := cfg.Validate(); err != nil {
		return ports.FlowView{}, fmt.Errorf("%w: %v", ports.ErrInvalidInput, err)
	}
	if s.flows == nil {
		return ports.FlowView{}, ports.ErrNotFound
	}
	var f domain.Flow
	var v domain.FlowVersion
	err = s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		f, _, err = s.flows.Get(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		version, err := s.flows.Publish(ctx, tx, tenantID, id, cfg, actor.PrincipalID, s.now())
		if err != nil {
			return err
		}
		f.ActiveVersion = version
		v = domain.FlowVersion{FlowID: id, Version: version, Config: cfg, PublishedAt: s.now(), PublishedBy: actor.PrincipalID}
		return s.appendAuditEntry(ctx, tx, actor, tenantID, "flow.published", "flow", id.String(), nil, map[string]any{"version": version})
	})
	if err != nil {
		return ports.FlowView{}, err
	}
	return flowView(f, v), nil
}

// RollbackFlow makes an earlier revision active.
func (s *Service) RollbackFlow(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.FlowID, version int) (ports.FlowView, error) {
	actor, err := actorFrom(ctx)
	if err != nil {
		return ports.FlowView{}, err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionManageFlows, tenantID.String()); err != nil {
		return ports.FlowView{}, err
	}
	var f domain.Flow
	var v domain.FlowVersion
	err = s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.flows.Rollback(ctx, tx, tenantID, id, version); err != nil {
			return err
		}
		var err error
		f, v, err = s.flows.Get(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, tenantID, "flow.rolled_back", "flow", id.String(), nil, map[string]any{"version": version})
	})
	if err != nil {
		return ports.FlowView{}, err
	}
	return flowView(f, v), nil
}

// DeleteFlow removes a flow and its revisions.
func (s *Service) DeleteFlow(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.FlowID) error {
	actor, err := actorFrom(ctx)
	if err != nil {
		return err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionManageFlows, tenantID.String()); err != nil {
		return err
	}
	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.flows.Delete(ctx, tx, tenantID, id); err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, tenantID, "flow.deleted", "flow", id.String(), nil, nil)
	})
}

// ListFlowVersions reads published revisions, newest first.
func (s *Service) ListFlowVersions(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.FlowID) ([]ports.FlowVersionView, error) {
	actor, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionManageFlows, tenantID.String()); err != nil {
		return nil, err
	}
	var out []ports.FlowVersionView
	err = s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.flows.Versions(ctx, tx, tenantID, id)
		return err
	})
	return out, err
}

// ListHolidays returns the tenant's holiday calendar.
func (s *Service) ListHolidays(ctx context.Context, tenantID shareddomain.TenantID) (map[string]string, error) {
	actor, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionManageFlows, tenantID.String()); err != nil {
		return nil, err
	}
	var out map[string]string
	err = s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.flows.ListHolidays(ctx, tx, tenantID)
		return err
	})
	return out, err
}

// AddHoliday adds or renames a closed day.
func (s *Service) AddHoliday(ctx context.Context, tenantID shareddomain.TenantID, day, name string) error {
	actor, err := actorFrom(ctx)
	if err != nil {
		return err
	}
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return fmt.Errorf("%w: day must be YYYY-MM-DD", ports.ErrInvalidInput)
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionManageFlows, tenantID.String()); err != nil {
		return err
	}
	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.flows.AddHoliday(ctx, tx, tenantID, day, name); err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, tenantID, "holiday.added", "holiday", day, nil, map[string]any{"name": name})
	})
}

// DeleteHoliday removes a closed day.
func (s *Service) DeleteHoliday(ctx context.Context, tenantID shareddomain.TenantID, day string) error {
	actor, err := actorFrom(ctx)
	if err != nil {
		return err
	}
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return fmt.Errorf("%w: day must be YYYY-MM-DD", ports.ErrInvalidInput)
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionManageFlows, tenantID.String()); err != nil {
		return err
	}
	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.flows.DeleteHoliday(ctx, tx, tenantID, day); err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, tenantID, "holiday.removed", "holiday", day, nil, nil)
	})
}

func flowView(f domain.Flow, v domain.FlowVersion) ports.FlowView {
	return ports.FlowView{ID: f.ID, Number: f.Number, Name: f.Name, ActiveVersion: f.ActiveVersion, Config: v.Config}
}
