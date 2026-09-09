package archtest

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const modulePath = "atsap-api"

// ScanModule walks every .go file (including _test.go) under apiRootDir's
// internal/ and cmd/ trees, parsing only the import block of each
// (parser.ImportsOnly — no type-checking, no build-tag evaluation; a
// literal import path check is all this invariant needs), and returns
// every ACL-boundary violation found.
//
// The one exception is a tagged composition-root test file — see
// composedInTestSuffixes. Those wire concrete adapters on purpose and
// never ship.
func ScanModule(apiRootDir string) ([]Violation, error) {
	var violations []Violation

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
			if isComposedInTest(d.Name()) {
				return nil
			}

			pkgPath, imports, err := parseFileImports(apiRootDir, path)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			for _, imp := range imports {
				if checkImport(pkgPath, imp) || checkIsolation(pkgPath, imp) {
					violations = append(violations, Violation{PackagePath: pkgPath, Import: imp})
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].PackagePath != violations[j].PackagePath {
			return violations[i].PackagePath < violations[j].PackagePath
		}
		return violations[i].Import < violations[j].Import
	})
	return violations, nil
}

// parseFileImports returns the Go import path of the package filePath
// belongs to (derived from its directory relative to apiRootDir, per
// this module's path — go.mod's "module atsap-api") and the list of
// paths it imports.
func parseFileImports(apiRootDir, filePath string) (pkgPath string, imports []string, err error) {
	rel, err := filepath.Rel(apiRootDir, filepath.Dir(filePath))
	if err != nil {
		return "", nil, fmt.Errorf("relative path: %w", err)
	}
	pkgPath = modulePath
	if rel != "." {
		pkgPath = modulePath + "/" + filepath.ToSlash(rel)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, parser.ImportsOnly)
	if err != nil {
		return "", nil, fmt.Errorf("go/parser: %w", err)
	}

	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return "", nil, fmt.Errorf("unquote import %s: %w", imp.Path.Value, err)
		}
		imports = append(imports, path)
	}
	return pkgPath, imports, nil
}
