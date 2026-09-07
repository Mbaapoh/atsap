package domain_test

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

var auditNow = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

// TestNewAuditEntry_RedactsCredentialMaterial is the core guarantee of
// LLD-02 §10.3: a record describing a credential change must show that
// it changed without carrying the credential.
func TestNewAuditEntry_RedactsCredentialMaterial(t *testing.T) {
	before := map[string]any{
		"username":      "admin",
		"password_hash": "$argon2id$v=19$m=19456,t=2,p=1$oldsalt$oldhash",
	}
	after := map[string]any{
		"username":      "admin",
		"password_hash": "$argon2id$v=19$m=19456,t=2,p=1$newsalt$newhash",
		"key_hash":      strings.Repeat("a", 64),
		"api_key":       "raw-key-value",
	}

	entry := domain.NewAuditEntry(
		shareddomain.NewTenantID(), uuid.New(), domain.ActorPrincipal,
		"principal.password.reset", "principal", "p-1",
		before, after, net.ParseIP("198.51.100.7"), auditNow,
	)

	// Scan the serialized record rather than individual fields: this is
	// what actually reaches the database, so a leak anywhere in it —
	// including somewhere the test did not think to look — fails here.
	raw, err := json.Marshal(entry)
	require.NoError(t, err)
	for _, secret := range []string{"oldhash", "newhash", "oldsalt", "newsalt", "raw-key-value", strings.Repeat("a", 64)} {
		assert.NotContains(t, string(raw), secret, "audit record leaked credential material")
	}

	// The fact of the change still has to survive redaction.
	assert.Equal(t, "[REDACTED]", entry.AfterState["password_hash"])
	assert.Equal(t, "admin", entry.AfterState["username"], "non-sensitive fields pass through")
	assert.Contains(t, entry.BeforeState, "password_hash", "the field must remain present, only its value redacted")
}

func TestNewAuditEntry_RedactsNestedAndVariousNames(t *testing.T) {
	after := map[string]any{
		"PasswordHash": "outer-secret",
		// Parent key is NOT sensitive, so it is walked and only the
		// sensitive child is replaced.
		"profile": map[string]any{"secret": "nested-secret", "label": "keep-me"},
		// Parent key IS sensitive, so the whole subtree goes — a map
		// under a key called "credentials" is credential material
		// whatever its shape.
		"credentials": map[string]any{"secret": "buried-secret", "label": "also-gone"},
		"harmless":    "visible",
	}

	entry := domain.NewAuditEntry(
		shareddomain.NewTenantID(), uuid.New(), domain.ActorPrincipal,
		"principal.update", "principal", "p-1",
		nil, after, nil, auditNow,
	)

	raw, err := json.Marshal(entry)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "outer-secret", "case-insensitive name matching")
	assert.NotContains(t, string(raw), "nested-secret", "nested maps are walked")
	assert.NotContains(t, string(raw), "buried-secret", "a sensitive key's whole subtree is dropped")
	assert.NotContains(t, string(raw), "also-gone", "including its non-sensitive siblings")
	assert.Contains(t, string(raw), "visible", "unrelated fields survive")

	walked, ok := entry.AfterState["profile"].(map[string]any)
	require.True(t, ok, "a non-sensitive parent stays a map after redaction")
	assert.Equal(t, "[REDACTED]", walked["secret"])
	assert.Equal(t, "keep-me", walked["label"], "siblings of a redacted field survive")

	assert.Equal(t, "[REDACTED]", entry.AfterState["credentials"],
		"a sensitive parent is replaced wholesale rather than walked")
}

func TestNewAuditEntry_DoesNotMutateCallerMaps(t *testing.T) {
	after := map[string]any{"password": "original"}

	domain.NewAuditEntry(
		shareddomain.NewTenantID(), uuid.New(), domain.ActorPrincipal,
		"principal.update", "principal", "p-1",
		nil, after, nil, auditNow,
	)

	assert.Equal(t, "original", after["password"],
		"redaction must copy, not scribble on the caller's map")
}

func TestNewAuditEntry_NilStatesStayNil(t *testing.T) {
	entry := domain.NewAuditEntry(
		shareddomain.NewTenantID(), uuid.New(), domain.ActorPrincipal,
		"principal.create", "principal", "p-1",
		nil, nil, nil, auditNow,
	)

	assert.Nil(t, entry.BeforeState, "a creation has no before state")
	assert.Nil(t, entry.AfterState)
}

func TestNewSystemAuditEntry(t *testing.T) {
	entry := domain.NewSystemAuditEntry(
		shareddomain.NewTenantID(),
		"tenant.suspend", "tenant", "t-1",
		map[string]any{"status": "ACTIVE"}, map[string]any{"status": "SUSPENDED"},
		auditNow,
	)

	assert.Equal(t, domain.ActorSystem, entry.ActorType)
	assert.Equal(t, domain.SystemActorID, entry.ActorID,
		"a system mutation records the sentinel, never an invented principal")
	assert.Nil(t, entry.IPAddress, "no client address exists for a system mutation")
	assert.Equal(t, "SUSPENDED", entry.AfterState["status"])
}
