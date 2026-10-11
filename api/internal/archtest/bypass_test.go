package archtest

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScanBypassProvenance_RealRepoIsClean is a syntactic precursor to
// LLD-03 DoD item 9, not that item itself (DoD 9 decodes a PlaceCall
// request, which does not exist yet; see coverage.md G2): no RPC handler in this module can reach BypassScreening, and
// nothing outside pbx/application assigns it in shipped code. A
// caller-settable bypass would be a licence-evasion hole, so this fails
// loudly rather than living as a comment.
func TestScanBypassProvenance_RealRepoIsClean(t *testing.T) {
	uses, err := ScanBypassScreeningProvenance(apiRootDir(t))
	require.NoError(t, err)
	assert.Empty(t, uses, "unlawful contact with BypassScreening: %+v", uses)
}

// TestScanBypassProvenance_DetectsRpcReference fault-injects the wire
// half: an RPC handler reading the flag. A scanner that has only ever
// returned "clean" is indistinguishable from one that returns "clean"
// unconditionally (D-28).
func TestScanBypassProvenance_DetectsRpcReference(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "telephony", "rpc", "handler.go"), `
package rpc

type Request struct{ BypassScreening bool }
`)
	uses, err := ScanBypassScreeningProvenance(root)
	require.NoError(t, err)
	require.Len(t, uses, 1, "an rpc reference to the flag must be reported")
	assert.Contains(t, uses[0].Reason, "wire cannot reach it")
}

// TestScanBypassProvenance_DetectsRogueAssignment fault-injects the
// origin half: shipped code outside pbx/application constructing the
// flag. Today nothing assigns it in production at all — the ingress
// path does not exist yet — so this test is the proof the gate is live
// before there is anything to catch.
func TestScanBypassProvenance_DetectsRogueAssignment(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "telephony", "application", "svc.go"), `
package application

type Command struct{ BypassScreening bool }

func build() Command { return Command{BypassScreening: true} }
`)
	uses, err := ScanBypassScreeningProvenance(root)
	require.NoError(t, err)
	require.Len(t, uses, 1, "a non-pbx/application assignment must be reported")
	assert.Contains(t, uses[0].Reason, "only internal/pbx/application")
}

// TestScanBypassProvenance_AllowsTestsAndPbxApplication keeps the rule
// narrow: _test.go files must be able to construct the flag (a bypass
// that cannot be built in a test cannot be tested), and pbx/application
// is the single production setter LLD-03 §10.1 names.
func TestScanBypassProvenance_AllowsTestsAndPbxApplication(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, filepath.Join(root, "internal", "telephony", "application", "svc_test.go"), `
package application_test

type Command struct{ BypassScreening bool }

func build() Command { return Command{BypassScreening: true} }
`)
	writeGoFile(t, filepath.Join(root, "internal", "pbx", "application", "ingress.go"), `
package application

type Command struct{ BypassScreening bool }

func build() Command { return Command{BypassScreening: true} }
`)
	uses, err := ScanBypassScreeningProvenance(root)
	require.NoError(t, err)
	assert.Empty(t, uses, "tests and pbx/application are the lawful setters")
}
