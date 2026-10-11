package application

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	identityapp "atsap-api/internal/identity/application"
	identityports "atsap-api/internal/identity/ports"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// Trunk and route actions, authorized like the extension ones.
const (
	actionCreateTrunk = "trunk.create"
	actionUpdateTrunk = "trunk.update"
	actionDeleteTrunk = "trunk.delete"
	actionReadTrunk   = "trunk.read"
	actionCreateRoute = "route.create"
	actionDeleteRoute = "route.delete"
	actionReadRoute   = "route.read"

	auditTrunkResource = "trunk"
	auditRouteResource = "route"
)

// allRoutes bounds the planner's read of a tenant's routing table. A
// selection over a partial table would be a wrong answer, not a slow one.
const allRoutes = 100000

var _ ports.RoutePlanner = (*Service)(nil)

// CreateTrunk connects a carrier: domain row, engine projection and audit
// record in one transaction (LLD-03 §7.2). Health starts Healthy and is
// not a caller input.
func (s *Service) CreateTrunk(ctx context.Context, cmd ports.CreateTrunkCommand) (ports.TrunkView, error) {
	actor, err := s.authorize(ctx, actionCreateTrunk, cmd.TenantID)
	if err != nil {
		return ports.TrunkView{}, err
	}

	now := s.now()
	t := domain.Trunk{
		ID: shareddomain.NewTrunkID(), TenantID: cmd.TenantID,
		Name: cmd.Name, Host: cmd.Host, Port: cmd.Port, AuthUsername: cmd.AuthUsername,
		ChannelLimit: cmd.ChannelLimit, Priority: cmd.Priority,
		Health: domain.HealthHealthy, CreatedAt: now, UpdatedAt: now,
	}
	if err := validateTrunk(t, cmd.Secret); err != nil {
		return ports.TrunkView{}, err
	}
	if cmd.Secret != "" {
		t.CredentialRef = domain.TrunkCredentialRef(t.ID)
	}

	err = s.pool.WithTenant(ctx, cmd.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.trunks.Insert(ctx, tx, t); err != nil {
			return err
		}
		if err := s.projector.ProjectTrunk(ctx, tx, t, secretBytes(cmd.Secret)); err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, t.TenantID, "trunk.created", auditTrunkResource,
			t.ID.String(), nil, trunkAuditState(&t))
	})
	if err != nil {
		return ports.TrunkView{}, err
	}
	return trunkView(t), nil
}

// UpdateTrunk replaces a trunk's configuration and re-projects it.
//
// An empty secret keeps the stored one; an empty username makes the
// trunk IP-authenticated and drops its secret (UpdateTrunkCommand). The
// observed health is carried over untouched.
func (s *Service) UpdateTrunk(ctx context.Context, cmd ports.UpdateTrunkCommand) (ports.TrunkView, error) {
	actor, err := s.authorize(ctx, actionUpdateTrunk, cmd.TenantID)
	if err != nil {
		return ports.TrunkView{}, err
	}

	var after domain.Trunk
	err = s.pool.WithTenant(ctx, cmd.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.trunks.Get(ctx, tx, cmd.ID)
		if err != nil {
			return err
		}
		after = before
		after.Name, after.Host, after.Port = cmd.Name, cmd.Host, cmd.Port
		after.AuthUsername = cmd.AuthUsername
		after.ChannelLimit, after.Priority = cmd.ChannelLimit, cmd.Priority
		after.UpdatedAt = s.now()
		switch {
		case cmd.AuthUsername == "":
			after.CredentialRef = ""
		case cmd.Secret != "":
			after.CredentialRef = domain.TrunkCredentialRef(after.ID)
		}
		if err := validateTrunk(after, cmd.Secret); err != nil {
			return err
		}

		if err := s.trunks.Update(ctx, tx, after); err != nil {
			return err
		}
		if err := s.projector.ProjectTrunk(ctx, tx, after, secretBytes(cmd.Secret)); err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, after.TenantID, "trunk.updated", auditTrunkResource,
			after.ID.String(), trunkAuditState(&before), trunkAuditState(&after))
	})
	if err != nil {
		return ports.TrunkView{}, err
	}
	return trunkView(after), nil
}

// DeleteTrunk removes a trunk, the routes that send to it, and its engine
// state, in one transaction. The routes go with it rather than blocking
// the delete: a route to a trunk that no longer exists can never be
// selected, so keeping it would only leave the console showing a rule
// that does nothing. Each is audited, so the deletion says what it took.
//
// The domain delete runs before RemoveTrunk and fails closed, which is
// the gate in front of tables with no RLS.
func (s *Service) DeleteTrunk(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.TrunkID) error {
	actor, err := s.authorize(ctx, actionDeleteTrunk, tenantID)
	if err != nil {
		return err
	}

	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.trunks.Get(ctx, tx, id)
		if err != nil {
			return err
		}
		removed, err := s.routes.DeleteByTrunk(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := s.trunks.Delete(ctx, tx, id); err != nil {
			return err
		}
		if err := s.projector.RemoveTrunk(ctx, tx, id); err != nil {
			return err
		}
		for i := range removed {
			if err := s.appendAuditEntry(ctx, tx, actor, tenantID, "route.deleted", auditRouteResource,
				removed[i].ID.String(), routeAuditState(&removed[i]), nil); err != nil {
				return err
			}
		}
		return s.appendAuditEntry(ctx, tx, actor, tenantID, "trunk.deleted", auditTrunkResource,
			id.String(), trunkAuditState(&before), nil)
	})
}

// ListTrunks returns one page of the tenant's trunks with their observed
// health.
func (s *Service) ListTrunks(ctx context.Context, tenantID shareddomain.TenantID, page ports.Page) ([]ports.TrunkView, error) {
	if _, err := s.authorize(ctx, actionReadTrunk, tenantID); err != nil {
		return nil, err
	}
	var trunks []domain.Trunk
	if err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		trunks, err = s.trunks.List(ctx, tx, page)
		return err
	}); err != nil {
		return nil, err
	}
	views := make([]ports.TrunkView, 0, len(trunks))
	for _, t := range trunks {
		views = append(views, trunkView(t))
	}
	return views, nil
}

// CreateRoute adds an outbound route to one of the tenant's own trunks.
// Overlap with existing routes is permitted: it is how least-cost
// preference is expressed.
func (s *Service) CreateRoute(ctx context.Context, cmd ports.CreateRouteCommand) (ports.RouteView, error) {
	actor, err := s.authorize(ctx, actionCreateRoute, cmd.TenantID)
	if err != nil {
		return ports.RouteView{}, err
	}

	r := domain.CarrierRoute{
		ID: shareddomain.NewRouteID(), TenantID: cmd.TenantID, PrefixPattern: cmd.PrefixPattern,
		TrunkID: cmd.TrunkID, CostPerMinute: cmd.CostPerMinute, Priority: cmd.Priority, CreatedAt: s.now(),
	}
	if err := r.Validate(); err != nil {
		return ports.RouteView{}, fmt.Errorf("%w: %v", ports.ErrInvalidInput, err)
	}

	err = s.pool.WithTenant(ctx, cmd.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		// Read first, so another tenant's trunk and a nonexistent one give
		// the same answer from the same place. The composite foreign key
		// would refuse it anyway; this keeps the refusal a domain answer.
		if _, err := s.trunks.Get(ctx, tx, cmd.TrunkID); err != nil {
			return err
		}
		if err := s.routes.Insert(ctx, tx, r); err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, r.TenantID, "route.created", auditRouteResource,
			r.ID.String(), nil, routeAuditState(&r))
	})
	if err != nil {
		return ports.RouteView{}, err
	}
	return routeView(r), nil
}

// DeleteRoute removes a route.
func (s *Service) DeleteRoute(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.RouteID) error {
	actor, err := s.authorize(ctx, actionDeleteRoute, tenantID)
	if err != nil {
		return err
	}
	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.routes.Get(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := s.routes.Delete(ctx, tx, id); err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, tenantID, "route.deleted", auditRouteResource,
			id.String(), routeAuditState(&before), nil)
	})
}

// ListRoutes returns one page of the tenant's routes.
func (s *Service) ListRoutes(ctx context.Context, tenantID shareddomain.TenantID, page ports.Page) ([]ports.RouteView, error) {
	if _, err := s.authorize(ctx, actionReadRoute, tenantID); err != nil {
		return nil, err
	}
	var routes []domain.CarrierRoute
	if err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		routes, err = s.routes.List(ctx, tx, page)
		return err
	}); err != nil {
		return nil, err
	}
	views := make([]ports.RouteView, 0, len(routes))
	for _, r := range routes {
		views = append(views, routeView(r))
	}
	return views, nil
}

// --- RoutePlanner ------------------------------------------------------

// SelectOutboundRoute returns the tenant's usable candidates for
// destination, best first (domain.SelectRoutes). It is mechanism for the
// call-placement path, which authorizes the call; it is never on the
// wire (LLD-03 §6).
func (s *Service) SelectOutboundRoute(ctx context.Context, tenantID shareddomain.TenantID, destination string) ([]domain.TrunkRoute, error) {
	var (
		routes []domain.CarrierRoute
		trunks []domain.Trunk
	)
	if err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		if routes, err = s.routes.List(ctx, tx, ports.Page{Limit: allRoutes}); err != nil {
			return err
		}
		trunks, err = s.trunks.List(ctx, tx, ports.Page{Limit: allRoutes})
		return err
	}); err != nil {
		return nil, err
	}
	return domain.SelectRoutes(destination, routes, trunks), nil
}

// ReportRouteHealth records one observed outcome against a trunk
// (design D8). An outcome that says nothing about the carrier — a busy
// callee, say — changes nothing. The change affects later selections
// only: nothing here touches a call already in progress.
//
// The tenant is the authenticated caller's, from ctx, because HLD 04 §2
// fixes this signature without one.
func (s *Service) ReportRouteHealth(ctx context.Context, trunkID shareddomain.TrunkID, statusCode int, latency time.Duration) error {
	tc, ok := identityapp.TenantContextFrom(ctx)
	if !ok || tc == nil {
		return fmt.Errorf("%w: no tenant in context for a health report", identityports.ErrPermissionDenied)
	}
	health, ok := domain.ObserveHealth(statusCode, latency)
	if !ok {
		return nil
	}
	return s.pool.WithTenant(ctx, tc.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		return s.trunks.SetHealth(ctx, tx, trunkID, health)
	})
}

// --- helpers -------------------------------------------------------------

// authorize runs the actor + permission preamble every use case shares.
func (s *Service) authorize(ctx context.Context, action string, tenantID shareddomain.TenantID) (Actor, error) {
	actor, err := actorFrom(ctx)
	if err != nil {
		return Actor{}, err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, action, tenantID.String()); err != nil {
		return Actor{}, err
	}
	return actor, nil
}

func validateTrunk(t domain.Trunk, secret string) error {
	if err := t.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ports.ErrInvalidInput, err)
	}
	if err := domain.ValidateTrunkSecret(secret, t.AuthUsername); err != nil {
		return fmt.Errorf("%w: %v", ports.ErrInvalidInput, err)
	}
	return nil
}

func secretBytes(secret string) []byte {
	if secret == "" {
		return nil
	}
	return []byte(secret)
}

// trunkAuditState renders the fields of a trunk that may be recorded.
// The secret is not an input, and the credential reference is reduced to
// whether one exists — absent by construction, like auditState's digest
// (LLD-03 §10.4).
func trunkAuditState(t *domain.Trunk) map[string]any {
	if t == nil {
		return nil
	}
	return map[string]any{
		"trunk_id":      t.ID.String(),
		"name":          t.Name,
		"host":          t.Host,
		"port":          t.Port,
		"auth_username": t.AuthUsername,
		"has_secret":    t.CredentialRef != "",
		"channel_limit": t.ChannelLimit,
		"priority":      t.Priority,
	}
}

func routeAuditState(r *domain.CarrierRoute) map[string]any {
	if r == nil {
		return nil
	}
	return map[string]any{
		"route_id":        r.ID.String(),
		"prefix_pattern":  r.PrefixPattern,
		"trunk_id":        r.TrunkID.String(),
		"cost_per_minute": r.CostPerMinute,
		"priority":        r.Priority,
	}
}

func trunkView(t domain.Trunk) ports.TrunkView {
	return ports.TrunkView{
		ID: t.ID, Name: t.Name, Host: t.Host, Port: t.Port, AuthUsername: t.AuthUsername,
		HasSecret: t.CredentialRef != "", ChannelLimit: t.ChannelLimit, Priority: t.Priority, Health: t.Health,
	}
}

func routeView(r domain.CarrierRoute) ports.RouteView {
	return ports.RouteView{
		ID: r.ID, PrefixPattern: r.PrefixPattern, TrunkID: r.TrunkID,
		CostPerMinute: r.CostPerMinute, Priority: r.Priority,
	}
}
