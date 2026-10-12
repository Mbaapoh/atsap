package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// forEachGoFile parses every .go file under apiRootDir's internal/ and
// cmd/ and hands it to visit with its slash-separated path relative to
// apiRootDir. Files for which skip reports true are not parsed. A missing
// internal/ or cmd/ is not an error.
func forEachGoFile(apiRootDir string, skip func(rel string) bool, visit func(rel string, file *ast.File) error) error {
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
			if skip != nil && skip(rel) {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			return visit(rel, file)
		})
		if err != nil {
			return err
		}
	}
	return nil
}
