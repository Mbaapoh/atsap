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
var forbiddenImports = []string{
	"atsap-api/internal/telephony/acl/ari",
	"atsap-api/internal/telephony/acl/ami",
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
