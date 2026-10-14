package application

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

// WithTrunkChecker returns a copy of s that has c check a trunk's carrier
// as soon as the trunk is created or its address changes. Without one, a
// trunk waits for the next scheduled round.
func (s *Service) WithTrunkChecker(c ports.TrunkChecker) *Service {
	clone := *s
	clone.checker = c
	return &clone
}

// checkTrunk asks the checker, if there is one, to check a trunk now.
func (s *Service) checkTrunk(tenantID shareddomain.TenantID, trunkID shareddomain.TrunkID) {
	if s.checker != nil {
		s.checker.CheckTrunk(tenantID, trunkID)
	}
}

// Check bounds (pbx-trunk-health-probing design D4).
const (
	// probeConcurrency caps the checks in flight in a round.
	probeConcurrency = 8
	// checkBudget bounds one background check, the probe's own budget
	// plus the database write.
	checkBudget = 10 * time.Second
)

// TrunkProber observes every trunk's carrier with the platform's own
// check (D-74): on a schedule, and at once when a trunk is created or its
// address changes. It is the writer of checked health; nothing a caller
// sends reaches it.
type TrunkProber struct {
	pool     *corepostgres.Pool
	trunks   ports.TrunkStore
	probe    ports.CarrierProbe
	interval time.Duration
	now      func() time.Time
	logger   *slog.Logger
}

var _ ports.TrunkChecker = (*TrunkProber)(nil)

// NewTrunkProber wires a prober checking every interval; an interval of
// zero or less disables the schedule, leaving checks on change.
func NewTrunkProber(pool *corepostgres.Pool, trunks ports.TrunkStore, probe ports.CarrierProbe, interval time.Duration, logger *slog.Logger) *TrunkProber {
	return &TrunkProber{pool: pool, trunks: trunks, probe: probe, interval: interval, now: time.Now, logger: logger}
}

// Run checks every trunk of every tenant, then waits the interval and
// starts again, until ctx ends. A round that outlasts the interval is
// followed by the next at once; rounds never overlap.
func (p *TrunkProber) Run(ctx context.Context) {
	if p.interval <= 0 {
		p.logger.Info("pbx: scheduled trunk checks are off (ATSAPBX_TRUNK_PROBE_INTERVAL=0)")
		return
	}
	for {
		if err := p.Round(ctx); err != nil && ctx.Err() == nil {
			p.logger.Error("pbx: trunk check round failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(p.interval):
		}
	}
}

// Round checks every trunk of every tenant once, at most
// probeConcurrency at a time, and returns when all are recorded.
func (p *TrunkProber) Round(ctx context.Context) error {
	tenants, err := listTenants(ctx, p.pool)
	if err != nil {
		return err
	}
	var all []domain.Trunk
	for _, tenantID := range tenants {
		err := p.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			trunks, err := p.trunks.List(ctx, tx, ports.Page{Limit: allRoutes})
			all = append(all, trunks...)
			return err
		})
		if err != nil {
			return err
		}
	}

	slots := make(chan struct{}, probeConcurrency)
	var wg sync.WaitGroup
	for _, t := range all {
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-slots; wg.Done() }()
			p.check(ctx, t)
		}()
	}
	wg.Wait()
	return nil
}

// CheckTrunk checks one trunk now, in the background, so the RPC that
// created or re-addressed it is never delayed by a carrier.
func (p *TrunkProber) CheckTrunk(tenantID shareddomain.TenantID, trunkID shareddomain.TrunkID) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), checkBudget)
		defer cancel()
		var t domain.Trunk
		err := p.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			t, err = p.trunks.Get(ctx, tx, trunkID)
			return err
		})
		if err != nil {
			if !errors.Is(err, ports.ErrNotFound) {
				p.logger.Warn("pbx: could not load a trunk to check it", "trunk_id", trunkID, "error", err)
			}
			return
		}
		p.check(ctx, t)
	}()
}

// check probes one trunk's carrier and records what it found, against the
// address it checked: if the address changed meanwhile, the result is
// dropped and the newer check stands.
func (p *TrunkProber) check(ctx context.Context, t domain.Trunk) {
	result := p.probe.Probe(ctx, t.Host, t.Port)
	if ctx.Err() != nil {
		return // shutting down: a cut-short check is not an observation
	}
	health, reason := domain.ObserveProbe(result.Outcome, result.Status, result.RTT)
	obs := domain.HealthObservation{Health: health, Reason: reason, At: p.now().UTC()}
	err := p.pool.WithTenant(ctx, t.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := p.trunks.SetCheckedHealth(ctx, tx, t.ID, t.Host, t.Port, obs)
		return err
	})
	if err != nil {
		p.logger.Warn("pbx: could not record a trunk check", "trunk_id", t.ID, "error", err)
	}
}
