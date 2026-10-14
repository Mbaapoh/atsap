//go:build integration

package application_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

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
	prober := application.NewTrunkProber(h.pool, pbxpg.NewTrunkStore(), carriers, 0, quiet())
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
	require.NoError(t, prober.Round(context.Background()))

	assert.Equal(t, domain.HealthHealthy, trunkHealth(h.ctx, t, h.svc, h.tenant, ours.ID).Health)
	v := trunkHealth(otherCtx, t, h.svc, otherID, theirs.ID)
	assert.Equal(t, domain.HealthUnhealthy, v.Health)
	assert.Equal(t, domain.ReasonNoResponse, v.HealthReason)
}
