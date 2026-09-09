// Package application wires licensing's pure domain to its persistence.
//
// It owns one rule the layers either side cannot enforce alone: the
// entitlement in force is derived by VERIFYING the stored payload, every
// time it is loaded, and never by reading the claim columns (D-53).
package application

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"atsap-api/internal/licensing/domain"
	"atsap-api/internal/licensing/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// Service implements ports.LicenseManager.
type Service struct {
	store  ports.Store
	keys   domain.KeySet
	logger *slog.Logger

	counter *domain.Counter
	now     func() time.Time

	// mu guards cached. The entitlement is verified on load and held in
	// memory because verifying per call setup would put cryptography in
	// the call path, which AC-06.3 forbids. It is a cache of VERIFIED
	// state, which D-56's rule permits — unlike authorization state,
	// which is never cached.
	mu     sync.RWMutex
	cached *domain.Entitlement
}

var _ ports.LicenseManager = (*Service)(nil)

// NewService returns a Service verifying against keys.
func NewService(store ports.Store, keys domain.KeySet, logger *slog.Logger) *Service {
	return &Service{
		store:   store,
		keys:    keys,
		logger:  logger,
		counter: domain.NewCounter(),
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// WithClock injects a clock, for tests that need to drive the grace
// period without waiting seven days.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Entitlement reports what the installation may currently do.
//
// This is the only place an entitlement comes from, and it is always the
// result of verifying the stored payload. Four outcomes, and only one of
// them is an error:
//
//   - no licence      → Setup. No call path, not an error, not the floor.
//   - fails to verify → Degraded at the floor, reported as tampering.
//   - expired         → Degraded at the floor, reported as expired.
//   - grace elapsed   → Degraded at the floor, reported as expired.
//
// A store failure is the error case, and it fails closed: an
// installation that cannot read its licence gets Setup rather than an
// assumption.
func (s *Service) Entitlement(ctx context.Context) (domain.Entitlement, error) {
	s.mu.RLock()
	cached := s.cached
	s.mu.RUnlock()
	if cached != nil {
		return *cached, nil
	}

	e, err := s.load(ctx)
	if err != nil {
		return domain.SetupEntitlement(), err
	}

	s.mu.Lock()
	s.cached = &e
	s.mu.Unlock()
	return e, nil
}

// load reads the stored licence and turns it into an entitlement by
// verifying it. Never reads a claim column: ports.StoredLicense does not
// carry them back from Load, so there is nothing to read.
func (s *Service) load(ctx context.Context) (domain.Entitlement, error) {
	lic, err := s.store.Load(ctx)
	if errors.Is(err, ports.ErrNoLicense) {
		return domain.SetupEntitlement(), nil
	}
	if err != nil {
		return domain.SetupEntitlement(), err
	}

	tok, err := domain.VerifyToken(lic.SignedPayload, lic.Signature, s.keys)
	if err != nil {
		// The row exists but does not verify: someone edited it, the
		// keys rotated, or it was written by a build we do not trust.
		// Degrade and report — never honour what the row claimed, and
		// never disable (BRD §10.2, D-53).
		s.logger.Warn("licensing: stored licence failed verification, degrading",
			"instance_id", lic.InstanceID)
		return domain.DegradedEntitlement(domain.Entitlement{}, domain.ReasonTampered), nil
	}

	now := s.now()
	return domain.ApplyGrace(tok.Entitlement(now), now, lic.LastConfirmedAt), nil
}

// ValidateCapacity reserves a channel for callID.
func (s *Service) ValidateCapacity(ctx context.Context, _ shareddomain.TenantID, callID string, _ int) (domain.CapacityVerdict, error) {
	e, err := s.Entitlement(ctx)
	if err != nil {
		// Fail closed: an installation that cannot establish what it is
		// licensed for does not get to assume.
		return domain.CapacityVerdict{Permitted: false, Reason: domain.ReasonNotActivated}, err
	}
	return s.counter.Reserve(callID, e, s.now()), nil
}

// ReleaseCapacity returns callID's reservation, idempotently (D-58).
func (s *Service) ReleaseCapacity(_ context.Context, callID string) error {
	s.counter.Release(callID)
	return nil
}

// ApplyLicenseKey verifies token and stores it.
//
// Idempotent (D-58): applying the identical token again writes nothing
// and, critically, does not move last_confirmed_at. Startup intake runs
// on every boot, so a re-application that refreshed the confirmation
// time would restart the 7-day grace clock on every restart — an
// installation could then sit disconnected indefinitely and never
// degrade.
func (s *Service) ApplyLicenseKey(ctx context.Context, token string) error {
	payload, signature, err := domain.SplitToken(token)
	if err != nil {
		return err
	}
	tok, err := domain.VerifyToken(payload, signature, s.keys)
	if err != nil {
		return err
	}

	existing, err := s.store.Load(ctx)
	switch {
	case err == nil && bytes.Equal(existing.SignedPayload, payload) && bytes.Equal(existing.Signature, signature):
		// The same licence, already applied. Nothing to write, and
		// nothing to invalidate.
		return nil
	case err != nil && !errors.Is(err, ports.ErrNoLicense):
		return err
	}

	now := s.now()
	if err := s.store.Save(ctx, ports.StoredLicense{
		InstanceID:      tok.InstanceID,
		SignedPayload:   payload,
		Signature:       signature,
		LastConfirmedAt: now,
		Display: ports.DisplayClaims{
			Edition:       tok.Edition,
			Capacity:      tok.Capacity,
			MaxTenants:    tok.MaxTenants,
			MaxExtensions: tok.MaxExtensions,
			Fingerprint:   tok.Fingerprint[:],
			Status:        string(domain.StateValid),
		},
	}); err != nil {
		return err
	}

	s.invalidate()
	return nil
}

// VerifyDailyEntitlement performs the scheduled confirmation.
//
// Never called during call setup (AC-06.3): it is invoked by a
// scheduler, and a call never waits on it. An unreachable or hanging
// entitlement service therefore cannot affect a call — the grace period
// exists precisely so that failure is survivable.
func (s *Service) VerifyDailyEntitlement(ctx context.Context) (domain.State, error) {
	e, err := s.Entitlement(ctx)
	if err != nil {
		return domain.StateSetup, err
	}
	if e.State == domain.StateSetup {
		// Nothing to confirm: there is no licence.
		return domain.StateSetup, nil
	}

	if err := s.store.TouchConfirmed(ctx, s.now()); err != nil {
		// Confirmation failed. That is the case the grace period is for,
		// so it is not an error to the caller — the state simply reflects
		// how long it has been failing.
		s.logger.Warn("licensing: entitlement confirmation failed", "error", err)
		return e.State, nil
	}

	s.invalidate()
	return domain.StateValid, nil
}

// invalidate drops the cached entitlement so the next read re-verifies.
func (s *Service) invalidate() {
	s.mu.Lock()
	s.cached = nil
	s.mu.Unlock()
}
