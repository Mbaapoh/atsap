package archtest

import (
	"go/ast"
	"go/token"
	"regexp"
	"sort"
	"strings"
)

// engineTables are the media engine's own PJSIP Realtime tables. They are
// Asterisk's vocabulary, and D-41/D-47 confine that vocabulary to the
// ACL: a query naming one of these anywhere else means engine knowledge
// has leaked upward, which is how a "portable" architecture quietly
// becomes an Asterisk-specific one.
var engineTables = []string{
	"ps_endpoints",
	"ps_auths",
	"ps_aors",
	"ps_contacts",
}

// engineTableExemptPrefixes are package paths permitted to name an engine
// table.
//
//   - pbx/acl/asterisk is the ACL itself: naming them is its entire job.
//   - internal/archtest is this checker, which must name what it checks.
//
// The exemption list is deliberately short and deliberately explicit.
// Adding to it is a decision, not a convenience.
var engineTableExemptPrefixes = []string{
	"internal/pbx/acl/asterisk",
	"internal/archtest",
}

// EngineTableUse is one engine-table reference found outside the ACL.
type EngineTableUse struct {
	File  string
	Table string
}

// engineTablePattern matches an engine table name as a whole word, so
// that a column or identifier merely containing one as a substring does
// not trip the check. Built from engineTables so the list above stays the
// single place a table is declared.
var engineTablePattern = regexp.MustCompile(`\b(` + strings.Join(engineTables, "|") + `)\b`)

// ScanEngineTableNames walks every .go file under apiRootDir's internal/
// and cmd/ trees and reports each engine-table name used outside the
// permitted packages.
//
// This exists because depguard cannot do it. depguard matches import
// paths; an engine table appears in a SQL string literal, which no import
// rule can see. Stating that plainly matters more than the check itself:
// a gate believed to cover something it does not is worse than no gate
// (D-28).
func ScanEngineTableNames(apiRootDir string) ([]EngineTableUse, error) {
	return scanTableNames(apiRootDir, engineTablePattern, isEngineTableExempt)
}

// scanTableNames is the shared walker behind every table-confinement
// rule: it reports each match of pattern in a non-test string literal
// outside the packages exempt says may name the tables.
func scanTableNames(apiRootDir string, pattern *regexp.Regexp, exempt func(string) bool) ([]EngineTableUse, error) {
	// Test files are exempt. The invariant this protects is that SHIPPING
	// code outside the ACL never speaks the engine's vocabulary; a test
	// that verifies the ACL actually wrote the rows it claims to, or that
	// the schema has the grants it should, cannot do so without naming
	// them. Tests do not ship, so no engine coupling escapes this way.
	//
	// The alternative — exempting whole packages because they contain such
	// a test — would have punched much larger holes: internal/postgres and
	// internal/pbx/application would both have been waved through
	// entirely, production code included.
	skip := func(rel string) bool { return exempt(rel) || strings.HasSuffix(rel, "_test.go") }

	var uses []EngineTableUse
	err := forEachGoFile(apiRootDir, skip, func(rel string, file *ast.File) error {
		uses = append(uses, tableNamesIn(rel, file, pattern)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(uses, func(i, j int) bool {
		if uses[i].File != uses[j].File {
			return uses[i].File < uses[j].File
		}
		return uses[i].Table < uses[j].Table
	})
	return uses, nil
}

// tableNamesIn reports the engine table names one file's string literals
// mention, each once.
//
// Only STRING LITERALS are inspected, never comments. A doc comment
// explaining why a projection exists, or why a column is deliberately
// empty, is exactly the documentation this codebase wants; flagging it
// would punish good practice and push the explanation out of the file
// that needs it. What must not appear outside the ACL is a *query* naming
// an engine table, and a query is a string.
func tableNamesIn(rel string, file *ast.File, pattern *regexp.Regexp) []EngineTableUse {
	var uses []EngineTableUse
	seen := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		for _, sub := range pattern.FindAllStringSubmatch(lit.Value, -1) {
			m := sub[len(sub)-1]
			if !seen[m] {
				seen[m] = true
				uses = append(uses, EngineTableUse{File: rel, Table: m})
			}
		}
		return true
	})
	return uses
}

// isEngineTableExempt reports whether relPath sits under a package
// permitted to name an engine table.
func isEngineTableExempt(relPath string) bool {
	for _, prefix := range engineTableExemptPrefixes {
		if strings.HasPrefix(relPath, prefix+"/") {
			return true
		}
	}
	return false
}
