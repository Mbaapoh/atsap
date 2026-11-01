package ingress_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ingress"
	shareddomain "atsap-api/internal/shared/domain"
	telephonyports "atsap-api/internal/telephony/ports"
)

type fakeMatcher struct {
	matches     []domain.IngressMatch
	err         error
	gotSource   netip.Addr
	gotNumber   string
	calledTimes int
}

func (f *fakeMatcher) Match(_ context.Context, source netip.Addr, number string) ([]domain.IngressMatch, error) {
	f.calledTimes++
	f.gotSource, f.gotNumber = source, number
	return f.matches, f.err
}

func attribute(t *testing.T, m *fakeMatcher, req telephonyports.AttributionRequest) (telephonyports.Attribution, string, error) {
	t.Helper()
	var log bytes.Buffer
	a := ingress.NewAttributor(m, slog.New(slog.NewTextHandler(&log, nil)))
	got, err := a.Attribute(context.Background(), req)
	return got, log.String(), err
}

// D-77: the attributor answers for the ingress only, routes on exactly
// one match, and refuses none or several — logging several for the
// operator without naming a tenant on the wire.
func TestIngressAttributor(t *testing.T) {
	a := domain.IngressMatch{TenantID: shareddomain.NewTenantID(), TrunkID: shareddomain.NewTrunkID()}
	b := domain.IngressMatch{TenantID: shareddomain.NewTenantID(), TrunkID: shareddomain.NewTrunkID()}
	ingress := telephonyports.AttributionRequest{SourceEndpoint: domain.CarrierIngressEndpoint, SourceAddress: "203.0.113.9:5060", Dialled: "+44 20 7123 0001"}

	t.Run("another endpoint names its own tenant", func(t *testing.T) {
		m := &fakeMatcher{}
		got, _, err := attribute(t, m, telephonyports.AttributionRequest{SourceEndpoint: "t_0123", SourceAddress: "203.0.113.9:5060"})
		require.NoError(t, err)
		assert.False(t, got.Applies)
		assert.Zero(t, m.calledTimes, "no cross-tenant read for a configured endpoint")
	})

	t.Run("exactly one routes as that trunk", func(t *testing.T) {
		m := &fakeMatcher{matches: []domain.IngressMatch{a}}
		got, _, err := attribute(t, m, ingress)
		require.NoError(t, err)
		assert.Equal(t, telephonyports.Attribution{Applies: true, TenantID: a.TenantID, SourceEndpoint: domain.TrunkEndpointIdentifier(a.TrunkID)}, got)
		assert.Equal(t, netip.MustParseAddr("203.0.113.9"), m.gotSource, "the transport address, port dropped")
		assert.Equal(t, domain.NormalizeDialled("+44 20 7123 0001"), m.gotNumber, "the number normalised as DIDs are")
	})

	t.Run("none refuses, quietly", func(t *testing.T) {
		got, log, err := attribute(t, &fakeMatcher{}, ingress)
		require.NoError(t, err)
		assert.Equal(t, telephonyports.Attribution{Applies: true, Refused: true}, got)
		assert.Empty(t, log)
	})

	t.Run("several refuse, and the operator is told", func(t *testing.T) {
		got, log, err := attribute(t, &fakeMatcher{matches: []domain.IngressMatch{a, b}}, ingress)
		require.NoError(t, err)
		assert.Equal(t, telephonyports.Attribution{Applies: true, Refused: true}, got)
		assert.Contains(t, log, "configured in several tenants")
		assert.Contains(t, log, a.TrunkID.String())
		assert.NotContains(t, log, a.TenantID.String(), "trunks, not tenants, in the log")
	})

	t.Run("an unreadable source address refuses", func(t *testing.T) {
		m := &fakeMatcher{matches: []domain.IngressMatch{a}}
		got, _, err := attribute(t, m, telephonyports.AttributionRequest{SourceEndpoint: domain.CarrierIngressEndpoint, SourceAddress: ""})
		require.NoError(t, err)
		assert.True(t, got.Refused)
		assert.Zero(t, m.calledTimes)
	})

	t.Run("a lookup failure is an error, not a guess", func(t *testing.T) {
		_, _, err := attribute(t, &fakeMatcher{err: errors.New("db down")}, ingress)
		assert.Error(t, err)
	})
}
