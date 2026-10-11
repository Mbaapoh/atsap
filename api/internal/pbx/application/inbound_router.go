package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	identityapp "atsap-api/internal/identity/application"
	identityports "atsap-api/internal/identity/ports"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
	telephonydomain "atsap-api/internal/telephony/domain"
	telephonyports "atsap-api/internal/telephony/ports"
)

// Inbound is what arriving-call routing needs beyond configuration.
// Supplied with WithInbound; a Service without it refuses every
// arriving call as not found rather than guessing.
type Inbound struct {
	// Addresser translates the source endpoint string to a domain
	// identity (design D4). The only engine vocabulary on this path.
	Addresser ports.EndpointAddresser
	// Numbers resolves DIDs to extensions.
	Numbers ports.InboundNumberStore
	// Emergency resolves the tenant's effective emergency set.
	Emergency ports.EmergencyStore
	// Legs reserves trunk channels for trunk-bound destinations.
	Legs ports.CallLegStore
	// Status reads a call's outcome for the health watcher.
	Status ports.CallStatus
	// ReportHealth records observed trunk health. Wired to
	// Service.ReportRouteHealth by the composition root.
	ReportHealth func(ctx context.Context, trunkID shareddomain.TrunkID, statusCode int, latency time.Duration) error
	// WatchInterval and WatchTimeout bound the health observer.
	WatchInterval time.Duration
	WatchTimeout  time.Duration
}

// WithInbound returns a copy of s able to route arriving calls.
func (s *Service) WithInbound(in Inbound) *Service {
	clone := *s
	if in.WatchInterval == 0 {
		in.WatchInterval = 500 * time.Millisecond
	}
	if in.WatchTimeout == 0 {
		in.WatchTimeout = 40 * time.Second
	}
	clone.inbound = &in
	return &clone
}

var _ telephonyports.InboundRouter = (*Service)(nil)

// ResolveInbound implements telephony-core's InboundRouter port
// (design D4): a trunk source resolves the DID through the tenant's
// inbound numbers; an extension source runs the PlaceCall dial plan
// with the tenant's effective emergency set. Unknown sources and
// unassigned DIDs are refused NotFound; a full trunk plant is refused
// Unavailable; BypassScreening is set only on an emergency match.
func (s *Service) ResolveInbound(ctx context.Context, req telephonyports.InboundRequest) (telephonyports.InboundDecision, error) {
	notFound := telephonyports.InboundDecision{Refuse: telephonyports.RefusalNotFound}
	in := s.inbound
	if in == nil {
		return notFound, nil
	}

	ident, ok := in.Addresser.Identify(req.SourceEndpoint)
	if !ok {
		return notFound, nil
	}

	if ident.IsTrunk {
		return s.resolveTrunkCall(ctx, req, ident.TrunkID)
	}
	return s.resolveDeskPhoneCall(ctx, req, ident.ExtensionID)
}
// resolveTrunkCall is a carrier call to a DID: it rings the assigned
// extension, internally, or the carrier gets a 404.
func (s *Service) resolveTrunkCall(ctx context.Context, req telephonyports.InboundRequest, _ shareddomain.TrunkID) (telephonyports.InboundDecision, error) {
	notFound := telephonyports.InboundDecision{Refuse: telephonyports.RefusalNotFound}
	in := s.inbound

	var extID shareddomain.ExtensionID
	err := s.pool.WithTenant(ctx, req.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		number, err := in.Numbers.FindByNumber(ctx, tx, domain.NormalizeDialled(req.Dialled))
		if err != nil {
			return err
		}
		ext, err := s.store.Get(ctx, tx, req.TenantID, number.ExtensionID)
		if err != nil {
			return err
		}
		extID = ext.ID
		return nil
	})
	if errors.Is(err, ports.ErrNotFound) {
		return notFound, nil
	}
	if err != nil {
		return telephonyports.InboundDecision{}, err
	}

	return telephonyports.InboundDecision{
		DestinationURI: in.Addresser.ExtensionAddress(extID),
		Direction:      telephonydomain.Inbound,
	}, nil
}

// resolveDeskPhoneCall runs a registered phone's dialling through the
// same dial plan PlaceCall uses: emergency first, then internal, then
// routes — with the trunk's channel limit reserved and the health
// watcher attached.
func (s *Service) resolveDeskPhoneCall(ctx context.Context, req telephonyports.InboundRequest, dialling shareddomain.ExtensionID) (telephonyports.InboundDecision, error) {
	notFound := telephonyports.InboundDecision{Refuse: telephonyports.RefusalNotFound}
	in := s.inbound

	var (
		res       domain.Resolution
		emergency domain.EmergencyNumberSet
	)
	err := s.pool.WithTenant(ctx, req.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		country, _, err := in.Emergency.GetCountry(ctx, tx)
		if err != nil {
			return err
		}
		additions, err := in.Emergency.ListAdditions(ctx, tx)
		if err != nil {
			return err
		}
		emergency, _ = domain.EffectiveEmergencySet(country, additions)

		routes, err := s.routes.List(ctx, tx, ports.Page{Limit: allRoutes})
		if err != nil {
			return err
		}
		trunks, err := s.trunks.List(ctx, tx, ports.Page{Limit: allRoutes})
		if err != nil {
			return err
		}
		var lookupErr error
		lookup := func(number string) (shareddomain.ExtensionID, bool) {
			e, err := s.store.FindByNumber(ctx, tx, number)
			if err != nil {
				if !errors.Is(err, ports.ErrNotFound) {
					lookupErr = err
				}
				return shareddomain.ExtensionID{}, false
			}
			return e.ID, true
		}
		res, err = domain.ResolveDestination(req.Dialled, emergency, lookup, routes, trunks)
		if lookupErr != nil {
			return lookupErr
		}
		return err
	})
	switch {
	case errors.Is(err, domain.ErrDestinationInvalid):
		return notFound, nil
	case errors.Is(err, domain.ErrUnroutable):
		return notFound, nil
	case err != nil:
		return telephonyports.InboundDecision{}, err
	}

	addresser := in.Addresser
	if res.Destination.Kind == domain.DestinationExtension {
		return telephonyports.InboundDecision{
			DestinationURI:  addresser.ExtensionAddress(*res.Destination.ExtensionID),
			Direction:       telephonydomain.Internal,
			BypassScreening: res.Emergency,
		}, nil
	}

	for _, c := range res.Candidates {
		leg := domain.PlacedCallLeg{
			ID: uuid.New(), TenantID: req.TenantID,
			CallID:      &req.CallID,
			ExtensionID: dialling,
			Destination: req.Dialled,
			TrunkID:     &c.Trunk.ID, RouteID: &c.Route.ID,
			Emergency: res.Emergency, CreatedAt: s.now(),
		}
		// The call id is already known (telephony assigned it before
		// routing), so the reservation lands attached — no later
		// AttachCall, and no orphan window.
		reserved, err := s.reserveTrunk(ctx, req.TenantID, c.Trunk, leg)
		if err != nil {
			return telephonyports.InboundDecision{}, err
		}
		if !reserved {
			continue // at its channel limit: next candidate
		}
		if in.ReportHealth != nil {
			go s.watchInboundLeg(context.WithoutCancel(ctx), req.TenantID, c.Trunk.ID, req.CallID)
		}
		return telephonyports.InboundDecision{
			DestinationURI:  addresser.TrunkAddress(c.Trunk.ID, req.Dialled),
			Direction:       telephonydomain.Outbound,
			BypassScreening: res.Emergency,
		}, nil
	}
	return telephonyports.InboundDecision{Refuse: telephonyports.RefusalUnavailable}, nil
}

// watchInboundLeg reports a trunk-bound inbound leg's health the way
// watchLeg does for placed calls: an answer implicates the carrier
// well, nothing else reports anything. Health reporting reads its
// tenant from the context, and the router's caller (telephony's event
// loop) carries none — so the tenant travels explicitly here.
func (s *Service) watchInboundLeg(ctx context.Context, tenantID shareddomain.TenantID, trunkID shareddomain.TrunkID, callID shareddomain.CallID) {
	in := s.inbound
	ctx = identityapp.WithTenantContext(ctx, &identityports.TenantContext{TenantID: tenantID})
	deadline := time.Now().Add(in.WatchTimeout)
	for time.Now().Before(deadline) {
		out, err := in.Status.Status(ctx, tenantID, callID)
		if err == nil {
			if out.AnsweredAt != nil {
				_ = in.ReportHealth(ctx, trunkID, 200, 0)
				return
			}
			if out.Ended {
				return
			}
		}
		time.Sleep(in.WatchInterval)
	}
}
