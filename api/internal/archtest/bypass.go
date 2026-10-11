package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BypassUse is one unlawful contact with the BypassScreening exemption
// point (LLD-03 §4.3, §10.1): the only field in the system whose value
// may disable a commercial control.
type BypassUse struct {
	// File is slash-separated relative to the api/ root, e.g.
	// internal/pbx/rpc/handler.go — stable across machines.
	File string
	// Reason names the half of the invariant the file breaks.
	Reason string
}

// ScanBypassScreeningProvenance enforces the provenance half of LLD-03
// §10.1 as a machine gate (D-28): a caller-settable bypass would be a
// licence-evasion hole, so the field's origin is a security invariant
// with its own test, not a comment. Two rules, both fail-closed:
//
//  1. No file under an rpc/ tree may reference BypassScreening at all —
//     the wire cannot reach it, so any RPC handler that sets, reads, or
//     returns it is a defect, not a feature.
//  2. Assignments to the field in shipped code may appear only in
//     internal/pbx/application — the single place LLD-03 §10.1 allows to
//     set it, from domain.IsEmergency. _test.go files may assign it (a
//     bypass that cannot be constructed in a test cannot be tested);
//     telephony's Screening reads it, which is the enforcement point,
//     not an assignment, and is always allowed.
//
// Like ScanEngineTableNames, this parses syntax only — no type-checking,
// no build-tag evaluation.
func ScanBypassScreeningProvenance(apiRootDir string) ([]BypassUse, error) {
	var uses []BypassUse

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
				return fmt.Errorf("relative path: %w", err)
			}
			rel = filepath.ToSlash(rel)

			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return fmt.Errorf("go/parser: %w", err)
			}

			isTest := strings.HasSuffix(path, "_test.go")
			inRPC := isRPCPath(rel)
			canAssign := isTest || isPbxApplicationPath(rel)

			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.Ident:
					if n.Name == "BypassScreening" && inRPC {
						uses = append(uses, BypassUse{File: rel,
							Reason: "rpc must never reference BypassScreening (LLD-03 §10.1: the wire cannot reach it)"})
						return false
					}
				case *ast.KeyValueExpr:
					if key, ok := n.Key.(*ast.Ident); ok && key.Name == "BypassScreening" && !canAssign {
						uses = append(uses, BypassUse{File: rel,
							Reason: "only internal/pbx/application (and _test.go) may assign BypassScreening (LLD-03 §10.1)"})
						return false
					}
				case *ast.AssignStmt:
					if !canAssign {
						for _, lhs := range n.Lhs {
							if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "BypassScreening" {
								uses = append(uses, BypassUse{File: rel,
									Reason: "only internal/pbx/application (and _test.go) may assign BypassScreening (LLD-03 §10.1)"})
								return false
							}
						}
					}
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
		return uses[i].Reason < uses[j].Reason
	})
	return uses, nil
}

// isRPCPath reports whether rel lives under an rpc/ tree (any context's
// ConnectRPC handlers) — the wire boundary the provenance rule guards.
func isRPCPath(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if part == "rpc" {
			return true
		}
	}
	return false
}

// isPbxApplicationPath reports whether rel is the single production
// location allowed to set the flag (LLD-03 §10.1).
func isPbxApplicationPath(rel string) bool {
	return strings.HasPrefix(rel, "internal/pbx/application/")
}
