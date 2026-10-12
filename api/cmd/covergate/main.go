// Command covergate enforces the coverage floors in
// quality/coverage-floors.json against a Go coverage profile
// (quality-and-supply-chain-gates, D-71).
//
//	covergate -profile coverage/go/merged.out -floors ../quality/coverage-floors.json -section all
//	covergate ... -ratchet   # raise floors to what was measured; never lowers
//
// The floors have one section per set of tiers measured: "unit" (the
// hermetic tier CI runs everywhere) and "all" (unit, integration and e2e
// on the rig, where more code is reachable).
//
// Coverage is a tripwire, not a target (docs/TESTING.md §4): the floors
// start at what the suites measure and can only go up, so a change that
// drops coverage fails, and raising a floor is a reviewed commit.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Floors is the go section of quality/coverage-floors.json: a minimum
// total and per-package minimums, in percent of statements.
type Floors struct {
	Total    float64            `json:"total"`
	Packages map[string]float64 `json:"packages"`
}

type floorsFile struct {
	Go      map[string]Floors `json:"go"`
	Console json.RawMessage   `json:"console,omitempty"`
}

// Coverage is statements covered out of statements, per package and in
// total.
type Coverage struct {
	Covered, Total int
}

// Percent is the coverage in percent; an empty package is fully covered.
func (c Coverage) Percent() float64 {
	if c.Total == 0 {
		return 100
	}
	return 100 * float64(c.Covered) / float64(c.Total)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("covergate", flag.ContinueOnError)
	fs.SetOutput(errOut)
	profile := fs.String("profile", "", "merged Go coverage profile (go tool cover format)")
	floorsPath := fs.String("floors", "", "quality/coverage-floors.json")
	section := fs.String("section", "", `floors section: "unit" or "all"`)
	ratchet := fs.Bool("ratchet", false, "raise the floors to the measured values (never lowers)")
	if err := fs.Parse(args); err != nil || *profile == "" || *floorsPath == "" || *section == "" {
		_, _ = fmt.Fprintln(errOut, "usage: covergate -profile <file> -floors <file> -section unit|all [-ratchet]")
		return 2
	}

	f, err := os.Open(*profile)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "covergate:", err)
		return 2
	}
	defer func() { _ = f.Close() }()
	total, perPkg, err := Measure(f)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "covergate:", err)
		return 2
	}

	raw, err := os.ReadFile(*floorsPath)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "covergate:", err)
		return 2
	}
	var file floorsFile
	if err := json.Unmarshal(raw, &file); err != nil {
		_, _ = fmt.Fprintln(errOut, "covergate: floors:", err)
		return 2
	}

	floors, ok := file.Go[*section]
	if !ok {
		_, _ = fmt.Fprintf(errOut, "covergate: no go.%s section in %s\n", *section, *floorsPath)
		return 2
	}

	if *ratchet {
		file.Go[*section] = Ratchet(floors, total, perPkg)
		buf, err := json.MarshalIndent(file, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintln(errOut, "covergate:", err)
			return 2
		}
		if err := os.WriteFile(*floorsPath, append(buf, '\n'), 0o600); err != nil {
			_, _ = fmt.Fprintln(errOut, "covergate:", err)
			return 2
		}
		_, _ = fmt.Fprintf(out, "go.%s floors ratcheted: total %.1f%%\n", *section, file.Go[*section].Total)
		return 0
	}

	failures := Check(floors, total, perPkg)
	_, _ = fmt.Fprintf(out, "Go coverage (%s): %.1f%% of %d statements (floor %.1f%%)\n", *section, total.Percent(), total.Total, floors.Total)
	for _, msg := range failures {
		_, _ = fmt.Fprintln(out, "BELOW FLOOR:", msg)
	}
	if len(failures) > 0 {
		return 1
	}
	return 0
}

// Measure reads a coverage profile and returns total and per-package
// coverage. A block listed more than once (several test binaries) counts
// as covered if any listing covered it.
func Measure(r io.Reader) (Coverage, map[string]Coverage, error) {
	type block struct {
		stmts   int
		covered bool
	}
	blocks := map[string]*block{}
	sc := bufio.NewScanner(r)
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if first {
			first = false
			if strings.HasPrefix(line, "mode:") {
				continue
			}
		}
		// file.go:12.3,14.5 2 1
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return Coverage{}, nil, fmt.Errorf("malformed profile line %q", line)
		}
		stmts, err1 := strconv.Atoi(fields[1])
		count, err2 := strconv.Atoi(fields[2])
		if err := errors.Join(err1, err2); err != nil {
			return Coverage{}, nil, fmt.Errorf("malformed profile line %q: %w", line, err)
		}
		b := blocks[fields[0]]
		if b == nil {
			b = &block{stmts: stmts}
			blocks[fields[0]] = b
		}
		b.covered = b.covered || count > 0
	}
	if err := sc.Err(); err != nil {
		return Coverage{}, nil, err
	}

	var total Coverage
	perPkg := map[string]Coverage{}
	for key, b := range blocks {
		file := key[:strings.LastIndex(key, ":")]
		pkg := path.Dir(file)
		c := perPkg[pkg]
		c.Total += b.stmts
		total.Total += b.stmts
		if b.covered {
			c.Covered += b.stmts
			total.Covered += b.stmts
		}
		perPkg[pkg] = c
	}
	return total, perPkg, nil
}

// Check returns one message per floor the measured coverage is below. A
// package with a floor that no longer appears in the profile is a
// failure too: a floor must not silently stop applying.
func Check(f Floors, total Coverage, perPkg map[string]Coverage) []string {
	var failures []string
	if total.Percent()+1e-9 < f.Total {
		failures = append(failures, fmt.Sprintf("total %.1f%% < %.1f%%", total.Percent(), f.Total))
	}
	names := make([]string, 0, len(f.Packages))
	for name := range f.Packages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c, ok := perPkg[name]
		switch {
		case !ok:
			failures = append(failures, fmt.Sprintf("%s has a floor but no coverage data", name))
		case c.Percent()+1e-9 < f.Packages[name]:
			failures = append(failures, fmt.Sprintf("%s %.1f%% < %.1f%%", name, c.Percent(), f.Packages[name]))
		}
	}
	return failures
}

// Ratchet raises each floor to the measured value, rounded down to one
// decimal, and never lowers one.
func Ratchet(f Floors, total Coverage, perPkg map[string]Coverage) Floors {
	down := func(p float64) float64 { return math.Floor(p*10) / 10 }
	next := Floors{Total: math.Max(f.Total, down(total.Percent())), Packages: map[string]float64{}}
	for name, floor := range f.Packages {
		if c, ok := perPkg[name]; ok {
			floor = math.Max(floor, down(c.Percent()))
		}
		next.Packages[name] = floor
	}
	return next
}
