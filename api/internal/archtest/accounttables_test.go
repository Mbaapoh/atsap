package archtest

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScanAccountTableNames_RealRepoIsClean: identity's installation-wide
// tables are queried only by identity's store (design D3).
func TestScanAccountTableNames_RealRepoIsClean(t *testing.T) {
	uses, err := ScanAccountTableNames(apiRootDir(t))
	require.NoError(t, err)
	assert.Empty(t, uses, "account tables queried outside identity/postgres: %+v", uses)
}

// The check must be able to fail: a query naming one elsewhere is caught.
func TestScanAccountTableNames_DetectsInjectedViolation(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "pbx", "postgres", "s.go"), `
package postgres

const q = "SELECT email FROM users WHERE id = $1"
const r = "UPDATE sessions SET revoked_at = now()"
`)
	uses, err := ScanAccountTableNames(root)
	require.NoError(t, err)
	require.Len(t, uses, 2)
	assert.Equal(t, "sessions", uses[0].Table)
	assert.Equal(t, "users", uses[1].Table)
}

// The identity store is where these queries belong.
func TestScanAccountTableNames_IdentityStoreIsExempt(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "identity", "postgres", "s.go"), `
package postgres

const q = "SELECT id FROM users JOIN user_credentials c ON c.user_id = users.id"
`)
	uses, err := ScanAccountTableNames(root)
	require.NoError(t, err)
	assert.Empty(t, uses)
}

// "users" in prose is not a query.
func TestScanAccountTableNames_IgnoresProse(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "pbx", "application", "a.go"), `
package application

const msg = "list users and their sessions"
const other = "SELECT * FROM users_archive"
`)
	uses, err := ScanAccountTableNames(root)
	require.NoError(t, err)
	assert.Empty(t, uses)
}
