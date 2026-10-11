package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
	telephonydomain "atsap-api/internal/telephony/domain"
	telephonyports "atsap-api/internal/telephony/ports"
)

const (
	actionPlaceCall  = "call.place"
	actionHangupCall = "call.hangup"

	// orphanLegAfter is how old a reserved leg with no call id must be
	// before a count treats it as abandoned (a crash between reserving a
	// channel and originating). Younger ones may be mid-origination.
	orphanLegAfter = time.Minute
)

// Placement is what call placement needs beyond configuration. Supplied
// with WithPlacement; a Service without it refuses to place calls rather
// than guessing.
type Placement struct {
	// Calls is telephony-core's CallService, used directly: pbx-core is
	// Tier 1 and may depend on it, and building InitiateCallCommand here
	// keeps the only BypassScreening assignment in pbx/application
	// (design D3 revision, LLD-03 §10.1).
	Calls     telephonyports.CallService
	Status    ports.CallStatus
	Addresser ports.EndpointAddresser
	Legs      ports.CallLegStore
	// Emergency is the set matched first on every destination (INV-01).
	Emergency domain.EmergencyNumberSet
	// WatchInterval and WatchTimeout bound the health observer (D5).
	WatchInterval time.Duration
	WatchTimeout  time.Duration
}

// DefaultPlacement fills the observer bounds design D5 sets.
func DefaultPlacement(p Placement) Placement {
	if p.Emergency == nil {
		p.Emergency = domain.DefaultEmergencyNumbers
	}
	if p.WatchInterval == 0 {
		p.WatchInterval = 500 * time.Millisecond
	}
	if p.WatchTimeout == 0 {
		p.WatchTimeout = 40 * time.Second
	}
	return p
}

// WithPlacement returns a copy of s able to place calls.
func (s *Service) WithPlacement(p Placement) *Service {
	clone := *s
	p = DefaultPlacement(p)
	clone.placement = &p
	return &clone
}

// PlaceCall places a call from one of the tenant's extensions to a
// destination as dialled (pbx-core/call-placement).
//
// The dial plan decides everything about how the call is reached
// (domain.ResolveDestination): emergency first, then an internal
// extension, then routes. For a trunk-bound call each candidate is tried
// in order for a free channel; the first with room carries it.
func (s *Service) PlaceCall(ctx context.Context, cmd ports.PlaceCallCommand) (ports.PlacedCall, error) {
	if _, err := s.authorize(ctx, actionPlaceCall, cmd.TenantID); err != nil {
		return ports.PlacedCall{}, err
	}
	p := s.placement
	if p == nil {
		return ports.PlacedCall{}, errors.New("call placement is not configured")
	}

	var (
		caller domain.Extension
		res    domain.Resolution
	)
	// The dial plan matches against the tenant's effective emergency set
	// — 112 plus its country's catalogue plus its own additions — so an
	// emergency number is recognized on every origination path (design
	// D6). Without a configured profile the built-in default applies.
	emergency, err := s.effectiveEmergencySet(ctx, cmd.TenantID)
	if err != nil {
		return ports.PlacedCall{}, err
	}
	err = s.pool.WithTenant(ctx, cmd.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		if caller, err = s.store.Get(ctx, tx, cmd.TenantID, cmd.ExtensionID); err != nil {
			return err
		}
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
		res, err = domain.ResolveDestination(cmd.Destination, emergency, lookup, routes, trunks)
		if lookupErr != nil {
			return lookupErr
		}
		return err
	})
	switch {
	case errors.Is(err, domain.ErrDestinationInvalid):
		return ports.PlacedCall{}, fmt.Errorf("%w: %v", ports.ErrInvalidInput, err)
	case err != nil:
		return ports.PlacedCall{}, err
	}

	// Ringing a phone that is not there fails inside the engine with an
	// error that names neither the phone nor the cause — and, worse, would
	// be indistinguishable from a trunk failing. Refuse cleanly instead.
	registered, err := s.projector.RegistrationStatus(ctx, []shareddomain.ExtensionID{caller.ID})
	if err != nil {
		return ports.PlacedCall{}, err
	}
	if statusOr(registered, caller.ID) != domain.StatusRegistered {
		return ports.PlacedCall{}, ports.ErrCallerNotRegistered
	}

	if res.Destination.Kind == domain.DestinationExtension {
		leg := s.newLeg(cmd, caller, nil, nil, res.Emergency)
		if err := s.reserve(ctx, cmd.TenantID, leg); err != nil {
			return ports.PlacedCall{}, err
		}
		return s.originate(ctx, leg, caller, p.Addresser.ExtensionAddress(*res.Destination.ExtensionID),
			telephonydomain.Internal, nil)
	}

	for _, c := range res.Candidates {
		trunkID, routeID := c.Trunk.ID, c.Route.ID
		leg := s.newLeg(cmd, caller, &trunkID, &routeID, res.Emergency)
		ok, err := s.reserveTrunk(ctx, cmd.TenantID, c.Trunk, leg)
		if err != nil {
			return ports.PlacedCall{}, err
		}
		if !ok {
			continue // at its channel limit: next candidate
		}
		return s.originate(ctx, leg, caller, p.Addresser.TrunkAddress(trunkID, cmd.Destination),
			telephonydomain.Outbound, &trunkID)
	}
	return ports.PlacedCall{}, ports.ErrTrunkCapacity
}

func (s *Service) newLeg(cmd ports.PlaceCallCommand, caller domain.Extension,
	trunkID *shareddomain.TrunkID, routeID *shareddomain.RouteID, emergency bool) domain.PlacedCallLeg {
	return domain.PlacedCallLeg{
		ID: uuid.New(), TenantID: cmd.TenantID, ExtensionID: caller.ID,
		TrunkID: trunkID, RouteID: routeID, Destination: cmd.Destination,
		Emergency: emergency, CreatedAt: s.now(),
	}
}

func (s *Service) reserve(ctx context.Context, tenantID shareddomain.TenantID, leg domain.PlacedCallLeg) error {
	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return s.placement.Legs.Insert(ctx, tx, leg)
	})
}

// reserveTrunk takes a channel on trunk for leg, or reports false when
// the trunk is at its channel limit (pbx-core/trunk-routing).
//
// Under the trunk's lock it first releases every leg whose call has
// ended — however it ended, including a hang-up nobody told pbx-core
// about — then counts what is left. The count is therefore exact without
// any event consumer, and survives a restart (design D4). An emergency
// leg is never refused here (INV-01), but is still recorded, so it
// occupies a channel everyone else sees.
func (s *Service) reserveTrunk(ctx context.Context, tenantID shareddomain.TenantID, trunk domain.Trunk, leg domain.PlacedCallLeg) (bool, error) {
	p := s.placement
	reserved := false
	err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if err := p.Legs.LockTrunk(ctx, tx, trunk.ID); err != nil {
			return err
		}
		live, err := p.Legs.LiveOnTrunk(ctx, tx, trunk.ID)
		if err != nil {
			return err
		}
		now := s.now()
		count := 0
		for _, l := range live {
			ended, err := s.legEnded(ctx, tenantID, l, now)
			if err != nil {
				return err
			}
			if ended {
				if err := p.Legs.Release(ctx, tx, l.ID, now); err != nil {
					return err
				}
				continue
			}
			count++
		}
		if !leg.Emergency && count >= trunk.ChannelLimit {
			return nil
		}
		if err := p.Legs.Insert(ctx, tx, leg); err != nil {
			return err
		}
		reserved = true
		return nil
	})
	return reserved, err
}

// legEnded reports whether a live leg's call is over. A leg with no call
// id yet, or whose call telephony-core has no record of, is over only
// once it is older than orphanLegAfter: younger, it may be mid-origination.
func (s *Service) legEnded(ctx context.Context, tenantID shareddomain.TenantID, l domain.PlacedCallLeg, now time.Time) (bool, error) {
	if l.CallID == nil {
		return now.Sub(l.CreatedAt) > orphanLegAfter, nil
	}
	out, err := s.placement.Status.Status(ctx, tenantID, *l.CallID)
	if errors.Is(err, ports.ErrNotFound) {
		return now.Sub(l.CreatedAt) > orphanLegAfter, nil
	}
	if err != nil {
		return false, err
	}
	return out.Ended, nil
}

// originate hands the reserved leg to telephony-core.
//
// BypassScreening is assigned here and only here in shipped code, from
// the dial plan's emergency match alone (LLD-03 §10.1, archtest).
func (s *Service) originate(ctx context.Context, leg domain.PlacedCallLeg, caller domain.Extension,
	destAddress string, direction telephonydomain.Direction, trunkID *shareddomain.TrunkID) (ports.PlacedCall, error) {
	p := s.placement
	callID, err := p.Calls.InitiateCall(ctx, telephonyports.InitiateCallCommand{
		TenantID:          leg.TenantID,
		Direction:         direction,
		SourceNumber:      caller.Number,
		DestNumber:        leg.Destination,
		SourceEndpointURI: p.Addresser.ExtensionAddress(caller.ID),
		DestEndpointURI:   destAddress,
		BypassScreening:   leg.Emergency,
	})
	if err != nil {
		s.releaseLeg(ctx, leg)
		// The caller's device was confirmed registered, so a failure to
		// set the call up at all is laid at the trunk's door: it leaves
		// selection until a good outcome restores it (design D5).
		if trunkID != nil {
			_ = s.ReportRouteHealth(ctx, *trunkID, 0, 0)
		}
		return ports.PlacedCall{}, fmt.Errorf("place call: %w", err)
	}

	if err := s.pool.WithTenant(ctx, leg.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		return p.Legs.AttachCall(ctx, tx, leg.ID, callID)
	}); err != nil {
		return ports.PlacedCall{}, err
	}

	// A call Screening refused comes back as a call id whose call has
	// already ended. That is a refusal, and the caller is told so, with
	// the reason telephony-core recorded.
	out, err := p.Status.Status(ctx, leg.TenantID, callID)
	if err == nil && out.Ended && out.AnsweredAt == nil {
		s.releaseLeg(ctx, leg)
		return ports.PlacedCall{}, fmt.Errorf("%w: %s", ports.ErrCallRefused, out.Reason)
	}

	if trunkID != nil {
		go s.watchLeg(context.WithoutCancel(ctx), leg.TenantID, *trunkID, callID)
	}
	return ports.PlacedCall{CallID: callID}, nil
}

func (s *Service) releaseLeg(ctx context.Context, leg domain.PlacedCallLeg) {
	_ = s.pool.WithTenant(ctx, leg.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		return s.placement.Legs.Release(ctx, tx, leg.ID, s.now())
	})
}

// watchLeg observes a trunk leg until it is answered or ends, and reports
// the one outcome that genuinely implicates the carrier: an answer
// (design D5). Answer time is not reported as latency — it includes how
// long a person took to pick up. A call that ends unanswered reports
// nothing: without a SIP cause, a busy callee and a dead carrier look the
// same. The context carries the caller's tenant for ReportRouteHealth.
func (s *Service) watchLeg(ctx context.Context, tenantID shareddomain.TenantID, trunkID shareddomain.TrunkID, callID shareddomain.CallID) {
	p := s.placement
	deadline := time.Now().Add(p.WatchTimeout)
	for time.Now().Before(deadline) {
		out, err := p.Status.Status(ctx, tenantID, callID)
		if err == nil {
			if out.AnsweredAt != nil {
				_ = s.ReportRouteHealth(ctx, trunkID, 200, 0)
				return
			}
			if out.Ended {
				return
			}
		}
		time.Sleep(p.WatchInterval)
	}
}

// HangupCall ends a call placed through PlaceCall (design D6). Another
// tenant's call, or one never placed here, is ErrNotFound. Ending a call
// that has already ended succeeds with no effect.
func (s *Service) HangupCall(ctx context.Context, tenantID shareddomain.TenantID, callID shareddomain.CallID) error {
	if _, err := s.authorize(ctx, actionHangupCall, tenantID); err != nil {
		return err
	}
	p := s.placement
	if p == nil {
		return errors.New("call placement is not configured")
	}

	var leg domain.PlacedCallLeg
	if err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		leg, err = p.Legs.Get(ctx, tx, callID)
		return err
	}); err != nil {
		return err
	}

	if out, err := p.Status.Status(ctx, tenantID, callID); err == nil && out.Ended {
		s.releaseLeg(ctx, leg)
		return nil
	}
	if err := p.Calls.HangupCall(ctx, callID, "api_hangup"); err != nil {
		// Lost a race with the call ending on its own: that is the
		// outcome the caller asked for, not a failure.
		if out, sErr := p.Status.Status(ctx, tenantID, callID); sErr != nil || !out.Ended {
			return fmt.Errorf("hang up call: %w", err)
		}
	}
	s.releaseLeg(ctx, leg)
	return nil
}
