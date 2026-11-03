//go:build integration

package application_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/application"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	pbxpg "atsap-api/internal/pbx/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

// fakeCarriers answers checks by host, as the carriers behind them would.
// A host with a gate holds its check until the gate is released.
type fakeCarriers struct {
	mu      sync.Mutex
	answers map[string]ports.CarrierCheck
	gates   map[string]chan struct{}
	checked []string
}

func newFakeCarriers(answers map[string]ports.CarrierCheck) *fakeCarriers {
	return &fakeCarriers{answers: answers, gates: map[string]chan struct{}{}}
}

func (f *fakeCarriers) Probe(ctx context.Context, host string, _ int) ports.CarrierCheck {
	f.mu.Lock()
	f.checked = append(f.checked, host)
	gate, answer := f.gates[host], f.answers[host]
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
		}
	}
	return answer
}

// set changes what host's carrier answers from now on.
func (f *fakeCarriers) set(host string, answer ports.CarrierCheck) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers[host] = answer
}

func (f *fakeCarriers) checks(host string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, h := range f.checked {
		if h == host {
			n++
		}
	}
	return n
}

var (
	answering  = ports.CarrierCheck{Outcome: domain.ProbeAnswered, Status: 200, RTT: 40 * time.Millisecond}
	silent     = ports.CarrierCheck{Outcome: domain.ProbeNoResponse}
	notResolve = ports.CarrierCheck{Outcome: domain.ProbeHostNotFound}
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// withProber gives the harness a prober over carriers and a service that
// checks through it.
func withProber(h *harness, carriers *fakeCarriers) (*application.TrunkProber, *application.Service) {
	identify, ok := h.projector.EndpointProjector.(ports.TrunkIdentifyProjector)
	if !ok {
		panic("the harness's engine projector must project identify rows")
	}
	prober := application.NewTrunkProber(h.pool, pbxpg.NewTrunkStore(), identify, carriers, 0, quiet())
	return prober, h.svc.WithTrunkChecker(prober)
}

// trunkHealth reads the trunk's health as the API reports it.
func trunkHealth(ctx context.Context, t *testing.T, svc *application.Service, tenant shareddomain.TenantID, id shareddomain.TrunkID) ports.TrunkView {
	t.Helper()
	all, err := svc.ListTrunks(ctx, tenant, ports.Page{Limit: 50})
	require.NoError(t, err)
	for _, v := range all {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("trunk %s not listed", id)
	return ports.TrunkView{}
}

// A trunk is checked as soon as it is created — the RPC does not wait —
// and the result is recorded with its reason and time.
func TestTrunkProber_ACreatedTrunkIsCheckedAtOnce(t *testing.T) {
	h := newHarness(t)
	carriers := newFakeCarriers(map[string]ports.CarrierCheck{"sip.carrier.example": answering})
	_, svc := withProber(h, carriers)

	created, err := svc.CreateTrunk(h.ctx, h.trunkCmd("Primary"))
	require.NoError(t, err)
	assert.Equal(t, domain.HealthUnknown, created.Health, "the response does not wait for the check")

	require.Eventually(t, func() bool {
		return trunkHealth(h.ctx, t, svc, h.tenant, created.ID).Health == domain.HealthHealthy
	}, 5*time.Second, 20*time.Millisecond)
	v := trunkHealth(h.ctx, t, svc, h.tenant, created.ID)
	assert.Equal(t, domain.ReasonAnswering, v.HealthReason)
	require.NotNil(t, v.HealthObservedAt)
	assert.WithinDuration(t, time.Now(), *v.HealthObservedAt, 10*time.Second)
	assert.Equal(t, 1, carriers.checks("sip.carrier.example"))
}

// Only a new address is checked again; a new name is not. A new address
// returns the trunk to Unknown at once, then to what the new address says.
func TestTrunkProber_ANewAddressIsCheckedAgain(t *testing.T) {
	h := newHarness(t)
	carriers := newFakeCarriers(map[string]ports.CarrierCheck{
		"sip.carrier.example": answering,
		"locslhodt":           notResolve,
	})
	_, svc := withProber(h, carriers)
	created, err := svc.CreateTrunk(h.ctx, h.trunkCmd("Primary"))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return carriers.checks("sip.carrier.example") == 1 }, 5*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		return trunkHealth(h.ctx, t, svc, h.tenant, created.ID).Health == domain.HealthHealthy
	}, 5*time.Second, 20*time.Millisecond)

	update := ports.UpdateTrunkCommand{TenantID: h.tenant, ID: created.ID, Name: "Renamed", Host: "sip.carrier.example", Port: 5060,
		AuthUsername: "acct-1001", ChannelLimit: 30, Priority: 1}
	renamed, err := svc.UpdateTrunk(h.ctx, update)
	require.NoError(t, err)
	assert.Equal(t, domain.HealthHealthy, renamed.Health, "a new name keeps the observation")
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 1, carriers.checks("sip.carrier.example"), "and is not checked again")

	update.Host = "locslhodt"
	moved, err := svc.UpdateTrunk(h.ctx, update)
	require.NoError(t, err)
	assert.Equal(t, domain.HealthUnknown, moved.Health, "the old observation was of another address")
	assert.Nil(t, moved.HealthObservedAt)
	require.Eventually(t, func() bool {
		v := trunkHealth(h.ctx, t, svc, h.tenant, created.ID)
		return v.Health == domain.HealthUnhealthy && v.HealthReason == domain.ReasonHostNotFound
	}, 5*time.Second, 20*time.Millisecond, "the new address is checked at once")
}

// A check of an address since changed is dropped: the newer check stands.
func TestTrunkProber_AStaleCheckIsDropped(t *testing.T) {
	h := newHarness(t)
	carriers := newFakeCarriers(map[string]ports.CarrierCheck{
		"sip.carrier.example":  silent,
		"sip2.carrier.example": answering,
	})
	gate := make(chan struct{})
	carriers.gates["sip.carrier.example"] = gate
	_, svc := withProber(h, carriers)

	created, err := svc.CreateTrunk(h.ctx, h.trunkCmd("Primary"))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return carriers.checks("sip.carrier.example") == 1 }, 5*time.Second, 20*time.Millisecond)

	_, err = svc.UpdateTrunk(h.ctx, ports.UpdateTrunkCommand{TenantID: h.tenant, ID: created.ID, Name: "Primary",
		Host: "sip2.carrier.example", Port: 5060, AuthUsername: "acct-1001", ChannelLimit: 30, Priority: 1})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return trunkHealth(h.ctx, t, svc, h.tenant, created.ID).Health == domain.HealthHealthy
	}, 5*time.Second, 20*time.Millisecond)

	close(gate) // the old address's silence arrives late
	time.Sleep(300 * time.Millisecond)
	v := trunkHealth(h.ctx, t, svc, h.tenant, created.ID)
	assert.Equal(t, domain.HealthHealthy, v.Health, "the late result for the old address changed nothing")
	assert.Equal(t, domain.ReasonAnswering, v.HealthReason)
}

// A scheduled round checks every trunk of every tenant.
func TestTrunkProber_ARoundCoversEveryTenant(t *testing.T) {
	h := newHarness(t)
	carriers := newFakeCarriers(map[string]ports.CarrierCheck{"sip.carrier.example": answering, "dead.carrier.example": silent})
	ours := h.createTrunk(t, "Ours") // created without a checker: never checked
	otherID, otherCtx := h.otherTenant(t)
	cmd := h.trunkCmd("Theirs")
	cmd.TenantID, cmd.Host = otherID, "dead.carrier.example"
	theirs, err := h.svc.CreateTrunk(otherCtx, cmd)
	require.NoError(t, err)
	assert.Equal(t, domain.HealthUnknown, trunkHealth(h.ctx, t, h.svc, h.tenant, ours.ID).Health)

	prober, _ := withProber(h, carriers)
	// T-6: one silent check is not enough; three consecutive failures
	// degrade the trunk.
	for i := 0; i < domain.DegradeAfterFailures; i++ {
		require.NoError(t, prober.Round(context.Background()))
	}

	assert.Equal(t, domain.HealthHealthy, trunkHealth(h.ctx, t, h.svc, h.tenant, ours.ID).Health)
	v := trunkHealth(otherCtx, t, h.svc, otherID, theirs.ID)
	assert.Equal(t, domain.HealthDegraded, v.Health)
	assert.Equal(t, domain.ReasonNoResponse, v.HealthReason)
}

// resolvedTo is an answering carrier whose name resolved to addrs.
func resolvedTo(addrs ...string) ports.CarrierCheck {
	c := answering
	c.Resolved = true
	for _, a := range addrs {
		c.Addresses = append(c.Addresses, netip.MustParseAddr(a))
	}
	return c
}

// identifyRow reads the shared ingress row that recognises carrier calls
// since D-77 — its match and row version (xmin, which moves on every
// write) — after checking the trunk has no identify row of its own.
func (h *harness) identifyRow(t *testing.T, id shareddomain.TrunkID) (match, version string, ok bool) {
	t.Helper()
	assert.Zero(t, h.countRow(t, `SELECT count(*) FROM ps_endpoint_id_ips WHERE id = $1`, domain.TrunkEndpointIdentifier(id)),
		"no identify row of the trunk's own (D-77)")
	err := h.pool.Unwrap().QueryRow(context.Background(),
		`SELECT "match", xmin::text FROM ps_endpoint_id_ips WHERE id = $1`, domain.CarrierIngressEndpoint).Scan(&match, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false
	}
	require.NoError(t, err)
	return match, version, true
}

func (h *harness) storedTrunk(t *testing.T, id shareddomain.TrunkID) domain.Trunk {
	t.Helper()
	var tr domain.Trunk
	require.NoError(t, h.pool.WithTenant(context.Background(), h.tenant, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		tr, err = pbxpg.NewTrunkStore().Get(ctx, tx, id)
		return err
	}))
	return tr
}

// The check is what resolves a trunk's name (D-75): a name is recognised
// by nothing until a check resolves it, then by exactly what it resolved
// to; a later failure keeps those addresses; a new set replaces them; an
// unchanged set writes nothing to the engine.
func TestTrunkProber_RecordsWhatTheNameResolvedTo(t *testing.T) {
	h := newHarness(t)
	carriers := newFakeCarriers(map[string]ports.CarrierCheck{"sip.carrier.example": resolvedTo("2001:db8::10", "203.0.113.10")})
	prober, _ := withProber(h, carriers)
	tr := h.createTrunk(t, "Primary") // created without a checker
	_, _, ok := h.identifyRow(t, tr.ID)
	assert.False(t, ok, "a name not resolved yet recognises nothing")

	require.NoError(t, prober.Round(context.Background()))
	match, version, ok := h.identifyRow(t, tr.ID)
	require.True(t, ok)
	assert.Equal(t, "203.0.113.10,2001:db8::10", match, "recognised by every address the name gave")
	stored := h.storedTrunk(t, tr.ID)
	assert.Len(t, stored.ResolvedAddresses, 2)
	require.NotNil(t, stored.ResolvedAt)
	firstResolved := *stored.ResolvedAt

	time.Sleep(10 * time.Millisecond)
	require.NoError(t, prober.Round(context.Background()))
	_, again, _ := h.identifyRow(t, tr.ID)
	assert.Equal(t, version, again, "an unchanged set writes nothing to the engine")
	assert.True(t, h.storedTrunk(t, tr.ID).ResolvedAt.After(firstResolved), "but records that the name still resolves")

	carriers.set("sip.carrier.example", notResolve)
	require.NoError(t, prober.Round(context.Background()))
	match, _, ok = h.identifyRow(t, tr.ID)
	require.True(t, ok)
	assert.Equal(t, "203.0.113.10,2001:db8::10", match, "a failed lookup keeps the carrier recognised")
	assert.Equal(t, domain.HealthUnhealthy, h.storedTrunk(t, tr.ID).Health, "while its health reports the failure")

	carriers.set("sip.carrier.example", resolvedTo("198.51.100.7"))
	require.NoError(t, prober.Round(context.Background()))
	match, _, _ = h.identifyRow(t, tr.ID)
	assert.Equal(t, "198.51.100.7", match, "a new set replaces the old one")

	differ, ok := h.projector.EndpointProjector.(interface {
		DiffTrunk(context.Context, domain.Trunk) ([]string, error)
	})
	require.True(t, ok)
	drift, err := differ.DiffTrunk(context.Background(), h.storedTrunk(t, tr.ID))
	require.NoError(t, err)
	assert.Empty(t, drift, "reconcile agrees with what the check recorded")
}

// A row written before addresses existed still holds the name; a check
// that cannot resolve it removes the row rather than leave a name for the
// engine to look up, and an IP host is never touched by a check.
func TestTrunkProber_ANameIsNeverLeftForTheEngine(t *testing.T) {
	h := newHarness(t)
	carriers := newFakeCarriers(map[string]ports.CarrierCheck{"locslhodt": notResolve, "203.0.113.10": silent})
	prober, _ := withProber(h, carriers)

	typo := h.trunkCmd("Typo")
	typo.Host = "locslhodt"
	bad, err := h.svc.CreateTrunk(h.ctx, typo)
	require.NoError(t, err)
	_, err = h.pool.Unwrap().Exec(context.Background(),
		`INSERT INTO ps_endpoint_id_ips (id, endpoint, "match") VALUES ($1, $1, 'locslhodt')`, domain.TrunkEndpointIdentifier(bad.ID))
	require.NoError(t, err, "as an older version projected it")

	byIP := h.trunkCmd("By IP")
	byIP.Host = "203.0.113.10"
	ip, err := h.svc.CreateTrunk(h.ctx, byIP)
	require.NoError(t, err)
	before, _, ok := h.identifyRow(t, ip.ID)
	require.True(t, ok, "an IP host is recognised at creation")

	require.NoError(t, prober.Round(context.Background()))
	assert.Zero(t, h.countRow(t, `SELECT count(*) FROM ps_endpoint_id_ips WHERE id = $1`, domain.TrunkEndpointIdentifier(bad.ID)),
		"the old row holding the name is gone from the engine")
	after, _, _ := h.identifyRow(t, ip.ID)
	assert.Equal(t, "203.0.113.10", after)
	assert.Equal(t, before, after, "the IP host stays recognised; the name never appears")
}

// A check whose host changed while it ran records no addresses either: the
// old host's addresses must never be recognised under the new one.
func TestTrunkProber_AStaleCheckRecordsNoAddresses(t *testing.T) {
	h := newHarness(t)
	carriers := newFakeCarriers(map[string]ports.CarrierCheck{"sip.carrier.example": resolvedTo("203.0.113.10")})
	gate := make(chan struct{})
	carriers.gates["sip.carrier.example"] = gate
	_, svc := withProber(h, carriers)

	created, err := svc.CreateTrunk(h.ctx, h.trunkCmd("Primary"))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return carriers.checks("sip.carrier.example") == 1 }, 5*time.Second, 20*time.Millisecond)

	_, err = h.svc.UpdateTrunk(h.ctx, ports.UpdateTrunkCommand{TenantID: h.tenant, ID: created.ID, Name: "Primary",
		Host: "198.51.100.7", Port: 5060, AuthUsername: "acct-1001", ChannelLimit: 30, Priority: 1})
	require.NoError(t, err)
	close(gate)
	time.Sleep(300 * time.Millisecond)

	match, _, _ := h.identifyRow(t, created.ID)
	assert.Equal(t, "198.51.100.7", match, "the late lookup of the old name changed nothing")
	assert.Equal(t, []netip.Addr{netip.MustParseAddr("198.51.100.7")}, h.storedTrunk(t, created.ID).ResolvedAddresses)
}

// D-76: a check that finds only addresses that cannot be a carrier sends
// nothing, reads "Address not allowed", and leaves the carrier recognised
// from its inbound ranges only — never from loopback recorded before the
// rule.
func TestTrunkProber_AddressNotAllowedKeepsOnlyTheRanges(t *testing.T) {
	h := newHarness(t)
	notAllowed := ports.CarrierCheck{Outcome: domain.ProbeAddressNotAllowed}
	carriers := newFakeCarriers(map[string]ports.CarrierCheck{"loop.carrier.example": notAllowed})
	prober, _ := withProber(h, carriers)

	cmd := h.trunkCmd("Loops back")
	cmd.Host, cmd.InboundAddresses = "loop.carrier.example", []string{"203.0.113.0/28"}
	tr, err := h.svc.CreateTrunk(h.ctx, cmd)
	require.NoError(t, err)
	// As D-75 recorded it before the rule existed.
	_, err = h.pool.Unwrap().Exec(context.Background(), `UPDATE ps_endpoint_id_ips SET "match" = '127.0.0.1,::1,203.0.113.0/28' WHERE id = $1`,
		domain.CarrierIngressEndpoint)
	require.NoError(t, err)

	require.NoError(t, prober.Round(context.Background()))
	v := trunkHealth(h.ctx, t, h.svc, h.tenant, tr.ID)
	assert.Equal(t, domain.HealthUnhealthy, v.Health)
	assert.Equal(t, domain.ReasonAddressNotAllowed, v.HealthReason)
	assert.Equal(t, "203.0.113.0/28", h.identifyMatchOf(t, tr.ID), "the ranges stay; loopback is gone")
}

// recordingAlerter records the alerts the application emits.
type recordingAlerter struct {
	mu     sync.Mutex
	alerts []ports.CarrierAlert
}

func (r *recordingAlerter) CarrierDown(_ context.Context, a ports.CarrierAlert) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.alerts = append(r.alerts, a)
	return nil
}

func (r *recordingAlerter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.alerts)
}

// D-80 / HLD T-6: three failed checks degrade; recovery needs five healthy
// minutes; the operator is told once on the failure, never on recovery.
func TestTrunkProber_ThresholdsRecoveryAndAlert(t *testing.T) {
	h := newHarness(t)
	carriers := newFakeCarriers(map[string]ports.CarrierCheck{"dead.carrier.example": silent})
	alerter := &recordingAlerter{}
	base := time.Now().UTC()
	clock := base
	prober, _ := withProber(h, carriers)
	prober = prober.WithCarrierAlerter(alerter).WithClock(func() time.Time { return clock })

	cmd := h.trunkCmd("Dead")
	cmd.Host = "dead.carrier.example"
	tr, err := h.svc.CreateTrunk(h.ctx, cmd)
	require.NoError(t, err)

	round := func() domain.TrunkHealth {
		require.NoError(t, prober.Round(context.Background()))
		return trunkHealth(h.ctx, t, h.svc, h.tenant, tr.ID).Health
	}
	assert.Equal(t, domain.HealthUnknown, round(), "one failed check is not a verdict")
	assert.Equal(t, domain.HealthUnknown, round(), "two are not either")
	assert.Equal(t, domain.HealthDegraded, round(), "the third degrades")
	require.Eventually(t, func() bool { return alerter.count() == 1 }, 2*time.Second, 20*time.Millisecond)
	assert.Equal(t, domain.HealthDegraded, round(), "further failures keep it degraded")
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, alerter.count(), "no repeated alert while failed")

	// Recover: five continuous minutes of healthy checks.
	carriers.set("dead.carrier.example", answering)
	clock = base.Add(time.Minute)
	assert.Equal(t, domain.HealthDegraded, round(), "one healthy check starts the window")
	clock = base.Add(6 * time.Minute)
	assert.Equal(t, domain.HealthHealthy, round(), "five healthy minutes recover")
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, alerter.count(), "recovery is silent")
}

// D-80: five call setups over the rolling window degrade the trunk and
// emit one alert; a further failure does not repeat it.
func TestReportRouteHealth_CallWindowDegradesAndAlertsOnce(t *testing.T) {
	h := newHarness(t)
	alerter := &recordingAlerter{}
	svc := h.svc.WithCarrierAlerter(alerter)
	tr, err := svc.CreateTrunk(h.ctx, h.trunkCmd("Primary"))
	require.NoError(t, err)

	for i := 0; i < domain.MinCallSamples; i++ {
		require.NoError(t, svc.ReportRouteHealth(h.ctx, tr.ID, 503, 0))
	}
	v := trunkHealth(h.ctx, t, svc, h.tenant, tr.ID)
	assert.Equal(t, domain.HealthDegraded, v.Health)
	assert.Equal(t, domain.ReasonCallSetupFailures, v.HealthReason)
	require.Eventually(t, func() bool { return alerter.count() == 1 }, 2*time.Second, 20*time.Millisecond)

	require.NoError(t, svc.ReportRouteHealth(h.ctx, tr.ID, 503, 0))
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, alerter.count(), "still one alert for the same failure")
}
