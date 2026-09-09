package archtest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckImport covers the pure detection logic directly: a violation
// and a clean case, independent of any real file on disk.
func TestCheckImport(t *testing.T) {
	tests := []struct {
		name    string
		pkg     string
		imp     string
		wantHit bool
	}{
		{"domain importing ari is a violation", "atsap-api/internal/telephony/domain", "atsap-api/internal/telephony/acl/ari", true},
		{"application importing ami is a violation", "atsap-api/internal/telephony/application", "atsap-api/internal/telephony/acl/ami", true},
		{"shared importing ari is a violation", "atsap-api/internal/shared/event", "atsap-api/internal/telephony/acl/ari", true},
		{"acl itself importing ari is exempt", "atsap-api/internal/telephony/acl", "atsap-api/internal/telephony/acl/ari", false},
		{"acl/ari importing ami is exempt (both under acl)", "atsap-api/internal/telephony/acl/ari", "atsap-api/internal/telephony/acl/ami", false},
		{"cmd importing ari is exempt (composition root)", "atsap-api/cmd/atsap-api", "atsap-api/internal/telephony/acl/ari", false},
		{"cmd/ari-playground importing ami is exempt (dev tool)", "atsap-api/cmd/ari-playground", "atsap-api/internal/telephony/acl/ami", false},
		{"e2e importing ari is exempt (walking-skeleton test composition)", "atsap-api/internal/telephony/e2e", "atsap-api/internal/telephony/acl/ari", false},
		{"unrelated import is never a violation", "atsap-api/internal/telephony/domain", "atsap-api/internal/shared/event", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantHit, checkImport(tt.pkg, tt.imp))
		})
	}
}

// TestScanModule_DetectsInjectedViolation proves the scanner itself
// fails on a deliberately introduced violation: a synthetic module tree
// (not the real repo) with a domain-package file importing the ari
// adapter directly.
func TestScanModule_DetectsInjectedViolation(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "telephony", "domain", "violation.go"), `
package domain

import "atsap-api/internal/telephony/acl/ari"

var _ = ari.Client{}
`)

	violations, err := ScanModule(root)
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.Equal(t, "atsap-api/internal/telephony/domain", violations[0].PackagePath)
	assert.Equal(t, "atsap-api/internal/telephony/acl/ari", violations[0].Import)
}

// TestScanModule_ExemptPackagesClean proves a synthetic tree where only
// exempt packages (acl, cmd) import the adapters produces no violations.
func TestScanModule_ExemptPackagesClean(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "telephony", "acl", "wiring.go"), `
package acl

import "atsap-api/internal/telephony/acl/ari"

var _ = ari.Client{}
`)
	writeGoFile(t, filepath.Join(root, "cmd", "atsap-api", "main.go"), `
package main

import "atsap-api/internal/telephony/acl/ami"

func main() { _ = ami.Client{} }
`)

	violations, err := ScanModule(root)
	require.NoError(t, err)
	assert.Empty(t, violations)
}

// TestScanModule_DetectsInjectedPbxViolation is the same fault injection
// for pbx-core's projection boundary (D-47). It is a separate test rather
// than a table case because the pbx entry was added later, on 2026-09-08,
// and a rule that has never rejected anything is a rule nobody has tested
// (D-28) — this is the proof that it does.
func TestScanModule_DetectsInjectedPbxViolation(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "pbx", "application", "violation.go"), `
package application

import "atsap-api/internal/pbx/acl/asterisk"

var _ = asterisk.Projector{}
`)

	violations, err := ScanModule(root)
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.Equal(t, "atsap-api/internal/pbx/application", violations[0].PackagePath)
	assert.Equal(t, "atsap-api/internal/pbx/acl/asterisk", violations[0].Import)
}

// TestCheckIsolation covers the inverse rule: not "who may import this"
// but "what may this import". A Tier-0 context depends on nothing
// (HLD 04 §10.1), which the forbiddenImports shape cannot express —
// there is nothing wrong with importing telephony in general, only with
// importing it from inside licensing.
func TestCheckIsolation(t *testing.T) {
	for name, tc := range map[string]struct {
		pkgPath, importPath string
		want                bool
	}{
		"licensing importing telephony is denied": {
			"atsap-api/internal/licensing/application", "atsap-api/internal/telephony/ports", true},
		"licensing importing identity is denied": {
			"atsap-api/internal/licensing/domain", "atsap-api/internal/identity/domain", true},
		"licensing importing pbx is denied": {
			"atsap-api/internal/licensing/postgres", "atsap-api/internal/pbx/ports", true},
		"licensing importing its own subpackage is fine": {
			"atsap-api/internal/licensing/application", "atsap-api/internal/licensing/domain", false},
		"licensing importing shared is fine": {
			"atsap-api/internal/licensing/domain", "atsap-api/internal/shared/domain", false},
		"telephony importing identity is not this rule's business": {
			"atsap-api/internal/telephony/application", "atsap-api/internal/identity/domain", false},
		"a package merely prefixed like licensing is unaffected": {
			"atsap-api/internal/licensingx/domain", "atsap-api/internal/telephony/ports", false},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, checkIsolation(tc.pkgPath, tc.importPath))
		})
	}
}

// TestScanModule_DetectsInjectedLicensingViolation is the fault
// injection for that rule through the real scanner. It exists because a
// rule that has never rejected anything is a rule nobody has tested
// (D-28), and this one guards a boundary whose first violation will look
// entirely reasonable: licensing publishes a tenant cap that identity
// enforces, so "just import identity to count tenants" is the shortcut
// waiting to be taken.
func TestScanModule_DetectsInjectedLicensingViolation(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "licensing", "application", "violation.go"), `
package application

import "atsap-api/internal/identity/domain"

var _ = domain.Tenant{}
`)

	violations, err := ScanModule(root)
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.Equal(t, "atsap-api/internal/licensing/application", violations[0].PackagePath)
	assert.Equal(t, "atsap-api/internal/identity/domain", violations[0].Import)
}

// TestIsComposedInTest fixes the narrowness of the test-file carve-out:
// a tagged composition-root test is exempt, a plain unit test is not.
// Written as a table so the boundary between the two cannot drift
// unnoticed — widening it to all _test.go files would silently un-gate
// every unit test in the module.
func TestIsComposedInTest(t *testing.T) {
	for name, tc := range map[string]struct {
		fileName string
		want     bool
	}{
		"integration test composes adapters": {"service_integration_test.go", true},
		"e2e test composes adapters":         {"projection_e2e_test.go", true},
		"plain unit test is still gated":     {"service_test.go", false},
		"production file is still gated":     {"service.go", false},
		"substring alone does not exempt":    {"integration_helpers.go", false},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, isComposedInTest(tc.fileName))
		})
	}
}

// TestScanModule_ComposedInTestIsExempt proves the carve-out works
// through the real scanner, not just the predicate: the identical
// violating import is reported in a production file and ignored in a
// tagged integration test beside it.
func TestScanModule_ComposedInTestIsExempt(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "pbx", "application", "harness_integration_test.go"), `
package application_test

import "atsap-api/internal/pbx/acl/asterisk"

var _ = asterisk.Projector{}
`)

	violations, err := ScanModule(root)
	require.NoError(t, err)
	assert.Empty(t, violations, "a tagged composition-root test must not trip the boundary gate")
}

// TestScanModule_RealRepoIsClean is the actual LLD-01 §8 DoD gate: the
// real api/ module, right now, has zero ACL-boundary violations. This
// is what "passes once the violation is removed" means in practice —
// there is currently nothing to remove.
func TestScanModule_RealRepoIsClean(t *testing.T) {
	violations, err := ScanModule(apiRootDir(t))
	require.NoError(t, err)
	assert.Empty(t, violations, "ACL-boundary violations found: %+v", violations)
}

// TestScanModule_SyntaxErrorPropagates covers a malformed .go file
// (invalid syntax) surfacing as an error, not a silent skip or panic.
func TestScanModule_SyntaxErrorPropagates(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "broken", "broken.go"), `this is not valid go`)

	_, err := ScanModule(root)
	assert.Error(t, err)
}

func apiRootDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	// this file: api/internal/archtest/boundary_test.go -> api/
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

func writeGoFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// --- engine table names (D-41, D-47) --------------------------------

// TestScanEngineTableNames_RealRepoIsClean is the check depguard cannot
// perform: an engine table appears in a SQL string literal, and no import
// rule can see a string. Naming ps_endpoints outside the ACL means engine
// knowledge has leaked upward.
func TestScanEngineTableNames_RealRepoIsClean(t *testing.T) {
	uses, err := ScanEngineTableNames(apiRootDir(t))
	require.NoError(t, err)
	assert.Empty(t, uses, "engine table names used outside the ACL: %+v", uses)
}

// TestScanEngineTableNames_DetectsInjectedViolation proves the check can
// actually fail. A scanner that has only ever returned "clean" is
// indistinguishable from one that returns "clean" unconditionally.
func TestScanEngineTableNames_DetectsInjectedViolation(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "pbx", "application", "svc.go"), `
package application

const q = "SELECT id FROM ps_endpoints WHERE id = $1"
`)

	uses, err := ScanEngineTableNames(root)
	require.NoError(t, err)
	require.Len(t, uses, 1, "a table named outside the ACL must be reported")
	assert.Equal(t, "ps_endpoints", uses[0].Table)
	assert.Contains(t, uses[0].File, "application")
}

// The ACL itself must not be flagged — naming those tables is its job.
func TestScanEngineTableNames_AclIsExempt(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "pbx", "acl", "asterisk", "p.go"), `
package asterisk

const q = "DELETE FROM ps_aors WHERE id = $1"
`)

	uses, err := ScanEngineTableNames(root)
	require.NoError(t, err)
	assert.Empty(t, uses, "the ACL is where these names belong")
}

// A word merely containing a table name is not a use of it.
func TestScanEngineTableNames_IgnoresSubstringMatches(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "pbx", "domain", "d.go"), `
package domain

const notATable = "ps_endpoints_archive_summary"
`)

	uses, err := ScanEngineTableNames(root)
	require.NoError(t, err)
	assert.Empty(t, uses, "word-boundary matching must not flag a longer identifier")
}

// TestScanDefaultHTTPClient_RealRepoIsClean is the gate: no package in
// this module reaches for an unbounded HTTP client.
//
// It exists because of a specific incident. On 2026-09-09 Asterisk began
// accepting connections without answering them. Every caller hung rather
// than failing, including an e2e helper using http.DefaultClient whose
// own ten-second deadline could not fire while it was blocked inside Do.
// The visible symptom — "timed out waiting for Stasis app" — named
// neither Asterisk nor HTTP, and sent the reader to debug the change
// under test instead of the rig.
//
// A timeout would have turned two minutes of hanging into ten seconds of
// a named error.
func TestScanDefaultHTTPClient_RealRepoIsClean(t *testing.T) {
	uses, err := ScanDefaultHTTPClient(apiRootDir(t))
	require.NoError(t, err)

	for _, u := range uses {
		t.Errorf("%s — %s", u, bannedHTTPSelectors[u.Selector])
	}
	assert.Empty(t, uses,
		"construct an HTTP client with an explicit Timeout; see internal/telephony/acl/ari's newHTTPClient")
}

// TestScanDefaultHTTPClient_DetectsInjectedUse fault-injects the rule, so
// it is known to reject rather than merely known to pass.
func TestScanDefaultHTTPClient_DetectsInjectedUse(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "svc", "violation.go"), `
package svc

import "net/http"

func fetch(url string) (*http.Response, error) {
	return http.DefaultClient.Get(url)
}
`)

	uses, err := ScanDefaultHTTPClient(root)

	require.NoError(t, err)
	require.Len(t, uses, 1)
	assert.Equal(t, "http.DefaultClient", uses[0].Selector)
	assert.Equal(t, "internal/svc/violation.go", uses[0].File)
}

// TestScanDefaultHTTPClient_AllowsABoundedClient keeps the rule narrow:
// it must object to the unbounded forms, not to using net/http.
func TestScanDefaultHTTPClient_AllowsABoundedClient(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "svc", "ok.go"), `
package svc

import (
	"net/http"
	"time"
)

var client = &http.Client{Timeout: 10 * time.Second}

func fetch(url string) (*http.Response, error) { return client.Get(url) }
`)

	uses, err := ScanDefaultHTTPClient(root)

	require.NoError(t, err)
	assert.Empty(t, uses, "a client with an explicit Timeout is exactly what this rule asks for")
}
