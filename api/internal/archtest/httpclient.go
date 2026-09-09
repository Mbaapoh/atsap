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

// DefaultHTTPClientUse is one use of an unbounded standard-library HTTP
// client.
type DefaultHTTPClientUse struct {
	File     string
	Line     int
	Selector string
}

func (u DefaultHTTPClientUse) String() string {
	return fmt.Sprintf("%s:%d uses %s", u.File, u.Line, u.Selector)
}

// bannedHTTPSelectors are the standard library's zero-timeout HTTP
// entry points.
//
// http.DefaultClient has no Timeout, and the package-level helpers
// (http.Get, http.Post, http.Head, http.PostForm) all use it. A request
// to a service that accepts the connection and never answers blocks
// until the caller's context expires — and in a long-lived process that
// context is often the process itself.
//
// http.DefaultTransport is deliberately NOT listed, and the reason is
// worth stating because the first draft of this rule did list it and
// immediately rejected correct code.
// `http.DefaultTransport.(*http.Transport).Clone()` is the documented
// way to start from the standard library's settings and override only
// what you need — proxy handling, HTTP/2 and TLS defaults come along for
// free — and it is what internal/telephony/acl/ari does. Banning the
// identifier would have punished the good pattern to catch a rarer bad
// one, and a rule that rejects correct code is a rule people learn to
// route around. The client-level Timeout is what this gate protects.
var bannedHTTPSelectors = map[string]string{
	"http.DefaultClient": "has no Timeout: construct &http.Client{Timeout: ...}",
	"http.Get":           "uses http.DefaultClient, which has no Timeout",
	"http.Post":          "uses http.DefaultClient, which has no Timeout",
	"http.PostForm":      "uses http.DefaultClient, which has no Timeout",
	"http.Head":          "uses http.DefaultClient, which has no Timeout",
}

// ScanDefaultHTTPClient reports every use of an unbounded HTTP client
// under apiRootDir's internal/ and cmd/ trees, including test files.
//
// Tests are deliberately in scope. The 2026-09-09 outage was diagnosed
// slowly precisely because a TEST helper used http.DefaultClient: its
// own ten-second deadline could not fire while it was parked inside Do,
// so a stalled engine produced a hang whose symptom named neither
// Asterisk nor HTTP. A test that hangs teaches nothing; a test that
// fails in ten seconds naming what stalled points at the problem.
//
// The one exemption is this rule's own test fixtures, which must be able
// to write the banned form in order to prove the rule rejects it.
func ScanDefaultHTTPClient(apiRootDir string) ([]DefaultHTTPClientUse, error) {
	var uses []DefaultHTTPClientUse

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
			if strings.Contains(filepath.ToSlash(path), "internal/archtest/") {
				return nil
			}

			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return fmt.Errorf("parse %s: %w", path, perr)
			}

			rel, _ := filepath.Rel(apiRootDir, path)
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				name := pkg.Name + "." + sel.Sel.Name
				if _, banned := bannedHTTPSelectors[name]; banned {
					uses = append(uses, DefaultHTTPClientUse{
						File:     filepath.ToSlash(rel),
						Line:     fset.Position(sel.Pos()).Line,
						Selector: name,
					})
				}
				return true
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return uses, nil
}
