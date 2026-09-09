// Package archtest holds repo-wide architectural invariant checks — the
// kind that don't belong to any single bounded context. This complements
// rather than replaces .golangci.yml's depguard acl-adapter-boundary
// rule: depguard covers domain/application/shared for fast CI feedback;
// this package's test additionally scans the whole module (every
// internal/ package, present and future), matching
// docs/hld/01-architecture.md §6.1's Dependency Invariant Test and LLD-01
// §8 DoD item 8.
package archtest

import "strings"

// forbiddenImports may only be imported by acl (and its own
// subpackages) or cmd (the composition root, and its dev tools) — raw
// Asterisk concepts stay inside the ACL (docs/hld/01-architecture.md
// §1.2 rule 3).
//
// pbx/acl/asterisk holds the PJSIP Realtime projection (D-47) and is
// listed for the same reason its telephony siblings are: it is the only
// package permitted to name ps_* tables, and everything else reaches the
// engine through ports.EndpointProjector. It was added on 2026-09-08,
// when the gap became visible — .golangci.yml's pbx-acl-boundary rule
// was pbx's only gate, and the lint task ran without build tags, so no
// gate had ever inspected a tagged pbx file. telephony had two gates
// throughout; pbx had one that could not see half its own tree.
var forbiddenImports = []string{
	"atsap-api/internal/telephony/acl/ari",
	"atsap-api/internal/telephony/acl/ami",
	"atsap-api/internal/pbx/acl/asterisk",
}

// exemptPackagePrefixes are package paths allowed to import
// forbiddenImports: the ACL itself (and its subpackages), cmd/ (the
// composition root that wires concrete adapters together, plus
// cmd/ari-playground, a dev tool that talks to ARI/AMI directly by
// design — the same exemption .golangci.yml's depguard rule documents),
// and internal/telephony/e2e (task 10.2's walking-skeleton test, which
// composes telephony-core the same way cmd/atsap-api's main.go does, to
// drive and verify a real call against live Asterisk).
var exemptPackagePrefixes = []string{
	"atsap-api/internal/telephony/acl",
	"atsap-api/cmd",
	"atsap-api/internal/telephony/e2e",
	"atsap-api/internal/pbx/acl",
	"atsap-api/internal/pbx/e2e",
}

// isolatedContexts inverts the question the list above asks.
//
// forbiddenImports answers "who may import X" — it protects a package
// from its consumers. This answers "what may X import" — it protects a
// context from its own dependencies. A Tier-0 context that depends on
// nothing (HLD 04 §10.1) cannot be expressed by the first shape at all:
// nothing about `telephony` is forbidden in general, only its appearance
// inside `licensing`.
//
// Keyed by the depending package prefix; the values are prefixes it may
// not import. Adding a Tier-0 context here is one line, and doing so is
// how §10.1's "may depend on: nothing" rows stop being prose.
var isolatedContexts = map[string][]string{
	"atsap-api/internal/licensing": {
		"atsap-api/internal/telephony",
		"atsap-api/internal/pbx",
		"atsap-api/internal/identity",
	},
}

// checkIsolation reports whether pkgPath importing importPath breaks an
// isolatedContexts rule. Pure, like checkImport, so it is testable
// without touching a file system.
//
// A context may always import its own subpackages, which the prefix
// match would otherwise forbid the moment a context's name is a prefix
// of another's.
func checkIsolation(pkgPath, importPath string) bool {
	for ctx, forbidden := range isolatedContexts {
		if pkgPath != ctx && !strings.HasPrefix(pkgPath, ctx+"/") {
			continue
		}
		for _, f := range forbidden {
			if importPath == f || strings.HasPrefix(importPath, f+"/") {
				return true
			}
		}
	}
	return false
}

// composedInTestSuffixes are file names exempt from the boundary check
// because they are composition roots, not shipped code: an integration
// or e2e test wires concrete adapters the way cmd/atsap-api's main.go
// does. Go links such a file into a separate test binary and only behind
// its build tag, so it is not part of the dependency graph this
// invariant protects — the same reasoning that exempts
// internal/telephony/e2e by package, and the same carve-out
// .golangci.yml makes for depguard.
//
// Deliberately narrow: a plain _test.go is NOT exempt. An ordinary unit
// test has no business constructing an ACL adapter, and reaching for one
// is the signal that a port is missing.
var composedInTestSuffixes = []string{
	"_integration_test.go",
	"_e2e_test.go",
}

// isComposedInTest reports whether fileName is a tagged composition-root
// test file. Split out so it is directly testable, like checkImport.
func isComposedInTest(fileName string) bool {
	for _, suffix := range composedInTestSuffixes {
		if strings.HasSuffix(fileName, suffix) {
			return true
		}
	}
	return false
}

// Violation is one forbidden import found in one package.
type Violation struct {
	PackagePath string
	Import      string
}

// checkImport reports whether pkgPath importing importPath violates the
// ACL boundary. Pure and side-effect-free so it can be tested directly
// against both a violating and a clean case, independent of scanning any
// real files (BoundaryViolations does that separately).
func checkImport(pkgPath, importPath string) bool {
	for _, forbidden := range forbiddenImports {
		if importPath != forbidden {
			continue
		}
		for _, exempt := range exemptPackagePrefixes {
			if pkgPath == exempt || strings.HasPrefix(pkgPath, exempt+"/") {
				return false
			}
		}
		return true
	}
	return false
}
