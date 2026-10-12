package main

import (
	"strings"
	"testing"
)

const profile = `mode: atomic
atsap-api/internal/a/x.go:1.1,2.2 3 1
atsap-api/internal/a/x.go:3.1,4.2 1 0
atsap-api/internal/b/y.go:1.1,2.2 2 0
atsap-api/internal/b/y.go:1.1,2.2 2 4
atsap-api/internal/b/y.go:5.1,6.2 2 0
`

func TestMeasureMergesRepeatedBlocks(t *testing.T) {
	total, per, err := Measure(strings.NewReader(profile))
	if err != nil {
		t.Fatal(err)
	}
	if total.Covered != 5 || total.Total != 8 {
		t.Fatalf("total = %+v, want 5/8", total)
	}
	if got := per["atsap-api/internal/a"]; got.Covered != 3 || got.Total != 4 {
		t.Fatalf("a = %+v", got)
	}
	if got := per["atsap-api/internal/b"]; got.Covered != 2 || got.Total != 4 {
		t.Fatalf("b = %+v (a block covered by any listing counts)", got)
	}
}

func TestCheckAndRatchet(t *testing.T) {
	total, per, _ := Measure(strings.NewReader(profile))
	f := Floors{Total: 62.5, Packages: map[string]float64{"atsap-api/internal/a": 75, "atsap-api/internal/gone": 10}}
	failures := Check(f, total, per)
	if len(failures) != 1 || !strings.Contains(failures[0], "gone has a floor but no coverage data") {
		t.Fatalf("failures = %v", failures)
	}
	if got := Check(Floors{Total: 62.6}, total, per); len(got) != 1 {
		t.Fatalf("a floor one tenth above the measured total must fail: %v", got)
	}
	r := Ratchet(Floors{Total: 50, Packages: map[string]float64{"atsap-api/internal/a": 90}}, total, per)
	if r.Total != 62.5 {
		t.Fatalf("ratcheted total = %v, want 62.5", r.Total)
	}
	if r.Packages["atsap-api/internal/a"] != 90 {
		t.Fatalf("ratchet must never lower a floor: %v", r.Packages)
	}
}

func TestMalformedProfile(t *testing.T) {
	if _, _, err := Measure(strings.NewReader("mode: set\nnot a line\n")); err == nil {
		t.Fatal("want an error for a malformed line")
	}
}
