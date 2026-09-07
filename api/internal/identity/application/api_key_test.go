package application_test

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/application"
	shareddomain "atsap-api/internal/shared/domain"
)

var tenantID = shareddomain.NewTenantID()

func TestGenerateAPIKey(t *testing.T) {
	raw, digest, err := application.GenerateAPIKey(tenantID)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(raw, "atsa_"),
		"a leaked key should be recognisable as a credential to revoke")
	assert.Len(t, digest, 64, "SHA-256 as hex is 64 characters")
	_, err = hex.DecodeString(digest)
	assert.NoError(t, err, "digest must be hex")

	assert.True(t, application.VerifyAPIKey(raw, digest))
}

// TestGenerateAPIKey_DigestIsNotReversible: the stored form is what an
// attacker gets from a database disclosure. The raw key must not be
// recoverable or even visible in it.
func TestGenerateAPIKey_DigestIsNotReversible(t *testing.T) {
	raw, digest, err := application.GenerateAPIKey(tenantID)
	require.NoError(t, err)

	assert.NotContains(t, digest, raw)
	assert.NotContains(t, digest, strings.TrimPrefix(raw, "atsa_"))
}

// TestGenerateAPIKey_CarriesItsTenant: the key names the tenant whose
// rows to search, so the digest lookup can run tenant-scoped under RLS
// instead of needing a privileged cross-tenant read.
func TestGenerateAPIKey_CarriesItsTenant(t *testing.T) {
	raw, _, err := application.GenerateAPIKey(tenantID)
	require.NoError(t, err)

	got, err := application.TenantFromAPIKey(raw)
	require.NoError(t, err)
	assert.Equal(t, tenantID, got)
}

func TestTenantFromAPIKey_Malformed(t *testing.T) {
	other := shareddomain.NewTenantID()
	valid, _, err := application.GenerateAPIKey(other)
	require.NoError(t, err)

	tests := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"no prefix", "some-random-string"},
		{"prefix but no tenant segment", "atsa_nounderscorehere"},
		{"tenant segment is not a uuid", "atsa_not-a-uuid_abcdef"},
		{"prefix stripped from a real key", strings.TrimPrefix(valid, "atsa_")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := application.TenantFromAPIKey(tt.raw)
			assert.ErrorIs(t, err, application.ErrMalformedAPIKey)
		})
	}
}

// TestTenantFromAPIKey_NamingATenantIsNotAuthentication: extracting a
// tenant is routing, not proof. A forged key naming a real tenant must
// still fail the digest comparison that actually proves possession.
func TestTenantFromAPIKey_NamingATenantIsNotAuthentication(t *testing.T) {
	real, digest, err := application.GenerateAPIKey(tenantID)
	require.NoError(t, err)

	forged := "atsa_" + tenantID.String() + "_" + strings.Repeat("A", 43)
	extracted, err := application.TenantFromAPIKey(forged)
	require.NoError(t, err, "a forged key still parses — that is the point")
	assert.Equal(t, tenantID, extracted)

	assert.False(t, application.VerifyAPIKey(forged, digest),
		"but it must not verify against a real key's digest")
	assert.True(t, application.VerifyAPIKey(real, digest))
}

// TestGenerateAPIKey_IsUnpredictable guards the entropy claim that makes
// SHA-256 (rather than a slow hash) the right storage choice.
func TestGenerateAPIKey_IsUnpredictable(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for range 100 {
		raw, _, err := application.GenerateAPIKey(tenantID)
		require.NoError(t, err)
		_, dup := seen[raw]
		require.False(t, dup, "generated the same key twice: entropy is broken")
		seen[raw] = struct{}{}
	}
}

func TestVerifyAPIKey(t *testing.T) {
	raw, digest, err := application.GenerateAPIKey(tenantID)
	require.NoError(t, err)

	other, _, err := application.GenerateAPIKey(tenantID)
	require.NoError(t, err)

	tests := []struct {
		name      string
		presented string
		stored    string
		want      bool
	}{
		{"matching key", raw, digest, true},
		{"a different valid key", other, digest, false},
		{"empty presented", "", digest, false},
		{"empty stored digest", raw, "", false},
		{"digest presented instead of the key", digest, digest, false},
		{"key with the prefix stripped", strings.TrimPrefix(raw, "atsa_"), digest, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, application.VerifyAPIKey(tt.presented, tt.stored))
		})
	}
}

// TestHashAPIKey_IsDeterministic: verification works by hashing the
// presented key and comparing, so the same input must always produce the
// same digest — unlike the password hasher, which salts per call.
func TestHashAPIKey_IsDeterministic(t *testing.T) {
	const key = "atsa_some-fixed-value"
	assert.Equal(t, application.HashAPIKey(key), application.HashAPIKey(key))
	assert.NotEqual(t, application.HashAPIKey(key), application.HashAPIKey(key+"x"))
}
