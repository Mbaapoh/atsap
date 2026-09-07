package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"atsap-api/internal/identity/domain"
)

func TestTenantStatus_Valid(t *testing.T) {
	tests := []struct {
		status domain.TenantStatus
		want   bool
	}{
		{domain.TenantActive, true},
		{domain.TenantSuspended, true},
		{domain.TenantStatus("ACTIVE "), false},
		{domain.TenantStatus("active"), false},
		{domain.TenantStatus("DELETED"), false},
		{domain.TenantStatus(""), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.Valid())
		})
	}
}

func TestPrincipalStatus_Valid(t *testing.T) {
	tests := []struct {
		status domain.PrincipalStatus
		want   bool
	}{
		{domain.PrincipalActive, true},
		{domain.PrincipalDisabled, true},
		{domain.PrincipalStatus("LOCKED"), false},
		{domain.PrincipalStatus(""), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.Valid())
		})
	}
}

func TestActorType_Valid(t *testing.T) {
	tests := []struct {
		actor domain.ActorType
		want  bool
	}{
		{domain.ActorPrincipal, true},
		{domain.ActorAPIKey, true},
		{domain.ActorSystem, true},
		{domain.ActorType("robot"), false},
		{domain.ActorType(""), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.actor), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.actor.Valid())
		})
	}
}

func TestApiKey_Usable(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	tests := []struct {
		name      string
		key       domain.ApiKey
		wantUsabe bool
	}{
		{"no expiry, not revoked", domain.ApiKey{}, true},
		{"expires in the future", domain.ApiKey{ExpiresAt: &future}, true},
		{"already expired", domain.ApiKey{ExpiresAt: &past}, false},
		{"expires exactly now is not usable", domain.ApiKey{ExpiresAt: &now}, false},
		{"revoked in the past", domain.ApiKey{RevokedAt: &past}, false},
		{"revoked exactly now is not usable", domain.ApiKey{RevokedAt: &now}, false},
		{"revocation scheduled in the future is still usable", domain.ApiKey{RevokedAt: &future}, true},
		{"revoked wins over a valid expiry", domain.ApiKey{RevokedAt: &past, ExpiresAt: &future}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantUsabe, tt.key.Usable(now))
		})
	}
}

// TestSystemActorID_IsStable guards the sentinel: audit_logs.actor_id is
// NOT NULL, so a system-initiated mutation needs a fixed, recognisable
// actor rather than a random or zero-ish value that varies per process.
func TestSystemActorID_IsStable(t *testing.T) {
	assert.Equal(t, "00000000-0000-0000-0000-000000000000", domain.SystemActorID.String())
}
