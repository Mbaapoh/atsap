package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
//   - internal/postgres holds the schema assertions that prove those
//     tables have no tenant_id, no RLS, and the right grants. A test that
//     asserts a table's properties cannot avoid naming it, and losing that
//     assertion to satisfy this rule would trade a real control for a
//     cosmetic one.
//   - internal/archtest is this checker, which must name what it checks.
//
// The exemption list is deliberately short and deliberately explicit.
// Adding to it is a decision, not a convenience.
var engineTableExemptPrefixes = []string{
	"internal/pbx/acl/asterisk",
	"internal/postgres",
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
	var uses []EngineTableUse

	for _, sub := range []string{"internal", "cmd"} {
		root := filepath.Join(apiRootDir, sub)
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return fmt.Errorf("walk %s: %w", path, err)
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}

			rel, err := filepath.Rel(apiRootDir, path)
			if err != nil {
				return fmt.Errorf("relativize %s: %w", path, err)
			}
			rel = filepath.ToSlash(rel)
			if isEngineTableExempt(rel) {
				return nil
			}

			// Only STRING LITERALS are inspected, never comments.
			//
			// A doc comment explaining why a projection exists, or why a
			// column is deliberately empty, is exactly the documentation
			// this codebase wants; flagging it would punish good practice
			// and push the explanation out of the file that needs it. What
			// must not appear outside the ACL is a *query* naming an engine
			// table, and a query is a string.
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}

			seen := map[string]bool{}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				for _, m := range engineTablePattern.FindAllString(lit.Value, -1) {
					if seen[m] {
						continue
					}
					seen[m] = true
					uses = append(uses, EngineTableUse{File: rel, Table: m})
				}
				return true
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Slice(uses, func(i, j int) bool {
		if uses[i].File != uses[j].File {
			return uses[i].File < uses[j].File
		}
		return uses[i].Table < uses[j].Table
	})
	return uses, nil
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
