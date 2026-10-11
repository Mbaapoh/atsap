package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

func validTrunk() domain.Trunk {
	return domain.Trunk{
		ID: shareddomain.NewTrunkID(), TenantID: shareddomain.NewTenantID(),
		Name: "Primary carrier", Host: "sip.carrier.example", Port: 5060,
		AuthUsername: "acct-1001", ChannelLimit: 30, Priority: 1,
		Health: domain.HealthHealthy,
	}
}

func TestTrunkValidate(t *testing.T) {
	require.NoError(t, validTrunk().Validate())

	for _, tc := range []struct {
		name   string
		mutate func(*domain.Trunk)
		want   error
	}{
		{"empty name", func(tr *domain.Trunk) { tr.Name = "" }, domain.ErrTrunkNameEmpty},
		{"long name", func(tr *domain.Trunk) { tr.Name = strings.Repeat("n", 101) }, domain.ErrTrunkNameTooLong},
		{"empty host", func(tr *domain.Trunk) { tr.Host = "" }, domain.ErrTrunkHostInvalid},
		{"host with scheme", func(tr *domain.Trunk) { tr.Host = "sip:carrier.example" }, domain.ErrTrunkHostInvalid},
		{"host with port", func(tr *domain.Trunk) { tr.Host = "carrier.example:5060" }, domain.ErrTrunkHostInvalid},
		{"host with user", func(tr *domain.Trunk) { tr.Host = "user@carrier.example" }, domain.ErrTrunkHostInvalid},
		{"host with space", func(tr *domain.Trunk) { tr.Host = "carrier .example" }, domain.ErrTrunkHostInvalid},
		{"host label leading hyphen", func(tr *domain.Trunk) { tr.Host = "-bad.example" }, domain.ErrTrunkHostInvalid},
		{"host empty label", func(tr *domain.Trunk) { tr.Host = "a..example" }, domain.ErrTrunkHostInvalid},
		{"host injection", func(tr *domain.Trunk) { tr.Host = "x.example;transport=tcp" }, domain.ErrTrunkHostInvalid},
		{"port zero", func(tr *domain.Trunk) { tr.Port = 0 }, domain.ErrTrunkPortInvalid},
		{"port too high", func(tr *domain.Trunk) { tr.Port = 65536 }, domain.ErrTrunkPortInvalid},
		{"zero channel limit", func(tr *domain.Trunk) { tr.ChannelLimit = 0 }, domain.ErrChannelLimitInvalid},
		{"negative channel limit", func(tr *domain.Trunk) { tr.ChannelLimit = -1 }, domain.ErrChannelLimitInvalid},
		{"negative priority", func(tr *domain.Trunk) { tr.Priority = -1 }, domain.ErrPriorityInvalid},
		{"username with @", func(tr *domain.Trunk) { tr.AuthUsername = "a@b" }, domain.ErrAuthUsernameInvalid},
		{"username with ;", func(tr *domain.Trunk) { tr.AuthUsername = "a;b" }, domain.ErrAuthUsernameInvalid},
		{"long username", func(tr *domain.Trunk) { tr.AuthUsername = strings.Repeat("u", 101) }, domain.ErrAuthUsernameTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := validTrunk()
			tc.mutate(&tr)
			assert.ErrorIs(t, tr.Validate(), tc.want)
		})
	}

	for _, host := range []string{"203.0.113.10", "2001:db8::1", "sip-1.carrier.example", "localhost"} {
		tr := validTrunk()
		tr.Host = host
		assert.NoError(t, tr.Validate(), host)
	}

	ipAuth := validTrunk()
	ipAuth.AuthUsername = ""
	assert.NoError(t, ipAuth.Validate(), "an IP-authenticated trunk carries no username")
}

func TestValidateTrunkSecret(t *testing.T) {
	assert.NoError(t, domain.ValidateTrunkSecret("", ""), "no secret, IP auth")
	assert.NoError(t, domain.ValidateTrunkSecret("", "acct"), "username alone is allowed")
	assert.NoError(t, domain.ValidateTrunkSecret("s3cret!#", "acct"))
	assert.ErrorIs(t, domain.ValidateTrunkSecret("s3cret", ""), domain.ErrSecretWithoutUsername)
	assert.ErrorIs(t, domain.ValidateTrunkSecret("bad\nsecret", "acct"), domain.ErrTrunkSecretInvalid)
	assert.ErrorIs(t, domain.ValidateTrunkSecret(strings.Repeat("s", 256), "acct"), domain.ErrTrunkSecretInvalid)
}

func TestObserveHealth(t *testing.T) {
	for _, tc := range []struct {
		code    int
		latency time.Duration
		want    domain.TrunkHealth
		ok      bool
	}{
		{200, 300 * time.Millisecond, domain.HealthHealthy, true},
		{200, domain.SlowAnswerThreshold, domain.HealthHealthy, true},
		{200, domain.SlowAnswerThreshold + time.Millisecond, domain.HealthDegraded, true},
		{0, 0, domain.HealthUnhealthy, true},
		{408, 0, domain.HealthUnhealthy, true},
		{503, 0, domain.HealthUnhealthy, true},
		{500, 0, domain.HealthUnhealthy, true},
		{486, 0, "", false}, // busy callee: not the carrier's fault
		{404, 0, "", false},
		{480, 0, "", false},
	} {
		got, ok := domain.ObserveHealth(tc.code, tc.latency)
		assert.Equal(t, tc.ok, ok, "code %d", tc.code)
		assert.Equal(t, tc.want, got, "code %d", tc.code)
	}
}

func TestTrunkEndpointIdentifier(t *testing.T) {
	id := shareddomain.NewTrunkID()
	identifier := domain.TrunkEndpointIdentifier(id)
	assert.True(t, strings.HasPrefix(identifier, "t_"))

	back, ok := domain.TrunkIDFromIdentifier(identifier)
	require.True(t, ok)
	assert.Equal(t, id, back)

	// The two kinds never cross: an extension identifier is not a trunk's.
	_, ok = domain.TrunkIDFromIdentifier(domain.EndpointIdentifier(shareddomain.NewExtensionID()))
	assert.False(t, ok)
	_, ok = domain.ExtensionIDFromIdentifier(identifier)
	assert.False(t, ok)
	_, ok = domain.TrunkIDFromIdentifier("t_nothex")
	assert.False(t, ok)
}
