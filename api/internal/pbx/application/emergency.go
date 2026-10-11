package application

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

const (
	actionManageEmergency     = "emergency.manage"
	actionReadEmergency       = "emergency.read"
	actionManageInboundNumber = "inbound_number.manage"
	actionReadInboundNumber   = "inbound_number.read"

	auditResourceEmergency     = "emergency_settings"
	auditResourceEmergencyNum  = "emergency_number"
	auditResourceInboundNumber = "inbound_number"
)

// WithEmergencyStores returns a copy of s able to manage the emergency
// profile and inbound numbers. A Service without them refuses those
// calls rather than guessing.
func (s *Service) WithEmergencyStores(inboundNumbers ports.InboundNumberStore, emergency ports.EmergencyStore) *Service {
	clone := *s
	clone.numbers = inboundNumbers
	clone.emergency = emergency
	return &clone
}

// SetEmergencyProfile sets the tenant's emergency country (design D6).
// The code must be ISO 3166-1 alpha-2 upper case; "" is refused here —
// clearing a country is not an operation, and an empty write would
// silently widen the next call's bypass set to the default.
func (s *Service) SetEmergencyProfile(ctx context.Context, tenantID shareddomain.TenantID, countryCode string) error {
	actor, err := actorFrom(ctx)
	if err != nil {
		return err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionManageEmergency, tenantID.String()); err != nil {
		return err
	}
	if s.emergency == nil {
		return errors.New("emergency profile is not configured")
	}
	if countryCode == "" {
		return fmt.Errorf("%w: country code is required", ports.ErrInvalidInput)
	}
	if err := domain.ValidateCountryCode(countryCode); err != nil {
		return fmt.Errorf("%w: %v", ports.ErrInvalidInput, err)
	}

	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		before, hadBefore, err := s.emergency.GetCountry(ctx, tx)
		if err != nil {
			return err
		}
		if err := s.emergency.SetCountry(ctx, tx, tenantID, countryCode); err != nil {
			return err
		}
		beforeState := map[string]any(nil)
		if hadBefore {
			beforeState = map[string]any{"country_code": before}
		}
		return s.appendAuditEntry(ctx, tx, actor, tenantID, "emergency_profile.set",
			auditResourceEmergency, tenantID.String(),
			beforeState, map[string]any{"country_code": countryCode})
	})
}

// GetEmergencyProfile renders the tenant's effective emergency set:
// 112 plus the catalogue for its country plus its own additions, each
// marked built-in or own (design D6).
func (s *Service) GetEmergencyProfile(ctx context.Context, tenantID shareddomain.TenantID) (ports.EmergencyProfile, error) {
	if _, err := s.authorize(ctx, actionReadEmergency, tenantID); err != nil {
		return ports.EmergencyProfile{}, err
	}
	if s.emergency == nil {
		return ports.EmergencyProfile{}, errors.New("emergency profile is not configured")
	}

	var profile ports.EmergencyProfile
	err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		profile, err = s.readProfile(ctx, tx)
		return err
	})
	return profile, err
}

func (s *Service) readProfile(ctx context.Context, tx pgx.Tx) (ports.EmergencyProfile, error) {
	country, set, err := s.emergency.GetCountry(ctx, tx)
	if err != nil {
		return ports.EmergencyProfile{}, err
	}
	additions, err := s.emergency.ListAdditions(ctx, tx)
	if err != nil {
		return ports.EmergencyProfile{}, err
	}
	effective, inCatalogue := domain.EffectiveEmergencySet(country, additions)

	// Built-in membership is decided per number, not per source: 112 and
	// the country's catalogue numbers are built-in wherever they appear.
	// With no country set, the default 911/999 are floor too, not
	// additions — nothing stored them, so nothing can remove them.
	numbers := make([]ports.EmergencyNumberView, 0, len(effective))
	for n := range effective {
		builtIn := domain.IsCatalogueNumber(country, n)
		if !set && (n == "911" || n == "999") {
			builtIn = true
		}
		numbers = append(numbers, ports.EmergencyNumberView{
			Number:  n,
			BuiltIn: builtIn,
		})
	}
	sort.Slice(numbers, func(i, j int) bool { return numbers[i].Number < numbers[j].Number })

	return ports.EmergencyProfile{
		CountryCode: country,
		CountrySet:  set,
		InCatalogue: set && inCatalogue,
		Numbers:     numbers,
	}, nil
}

// CreateEmergencyNumber adds one of the tenant's own numbers.
func (s *Service) CreateEmergencyNumber(ctx context.Context, tenantID shareddomain.TenantID, number string) error {
	actor, err := actorFrom(ctx)
	if err != nil {
		return err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionManageEmergency, tenantID.String()); err != nil {
		return err
	}
	if s.emergency == nil {
		return errors.New("emergency profile is not configured")
	}
	normalized, err := domain.NormalizeInboundNumber(number)
	if err != nil {
		return fmt.Errorf("%w: %v", ports.ErrInvalidInput, err)
	}

	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		country, _, err := s.emergency.GetCountry(ctx, tx)
		if err != nil {
			return err
		}
		if domain.IsCatalogueNumber(country, normalized) {
			return fmt.Errorf("emergency number %q is already effective: %w", normalized, ports.ErrEmergencyNumberProtected)
		}
		if err := s.emergency.AddAddition(ctx, tx, tenantID, normalized); err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, tenantID, "emergency_number.created",
			auditResourceEmergencyNum, normalized, nil, map[string]any{"number": normalized})
	})
}

// ListEmergencyNumbers returns the tenant's effective set, built-in and
// own alike.
func (s *Service) ListEmergencyNumbers(ctx context.Context, tenantID shareddomain.TenantID) ([]ports.EmergencyNumberView, error) {
	profile, err := s.GetEmergencyProfile(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return profile.Numbers, nil
}

// DeleteEmergencyNumber removes one of the tenant's own numbers.
// Deleting 112 or a catalogue number for the tenant's country is
// refused before storage: the floor no tenant can remove (design D6).
func (s *Service) DeleteEmergencyNumber(ctx context.Context, tenantID shareddomain.TenantID, number string) error {
	actor, err := actorFrom(ctx)
	if err != nil {
		return err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionManageEmergency, tenantID.String()); err != nil {
		return err
	}
	if s.emergency == nil {
		return errors.New("emergency profile is not configured")
	}
	normalized, err := domain.NormalizeInboundNumber(number)
	if err != nil {
		return fmt.Errorf("%w: %v", ports.ErrInvalidInput, err)
	}

	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		country, _, err := s.emergency.GetCountry(ctx, tx)
		if err != nil {
			return err
		}
		if domain.IsCatalogueNumber(country, normalized) {
			return fmt.Errorf("emergency number %q is built-in and cannot be removed: %w", normalized, ports.ErrEmergencyNumberProtected)
		}
		if err := s.emergency.DeleteAddition(ctx, tx, normalized); err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, tenantID, "emergency_number.deleted",
			auditResourceEmergencyNum, normalized, map[string]any{"number": normalized}, nil)
	})
}

// effectiveEmergencySet resolves the tenant's emergency set for the
// dial plan: PlaceCall and the inbound router both read through here,
// so every origination path matches the same numbers (design D6).
func (s *Service) effectiveEmergencySet(ctx context.Context, tenantID shareddomain.TenantID) (domain.EmergencyNumberSet, error) {
	if s.emergency == nil {
		return domain.DefaultEmergencyNumbers, nil
	}
	var (
		country   string
		additions []string
	)
	err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		country, _, err = s.emergency.GetCountry(ctx, tx)
		if err != nil {
			return err
		}
		additions, err = s.emergency.ListAdditions(ctx, tx)
		return err
	})
	if err != nil {
		return nil, err
	}
	set, _ := domain.EffectiveEmergencySet(country, additions)
	return set, nil
}
