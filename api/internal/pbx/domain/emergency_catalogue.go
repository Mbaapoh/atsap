package domain

import (
	"sort"
	"strings"
)

// CountryEmergencyCatalogue maps an ISO 3166-1 alpha-2 country code to
// the main national emergency numbers a tenant operating there must
// always be able to dial (pbx-inbound-and-emergency design D6).
//
// It is deliberately NOT exhaustive: it seeds the countries the platform
// operates in first, and every other country is tenant-configured. A new
// country is a data-only addition — one map entry, each number verified
// against the national regulator or the ITU-T emergency-number list,
// with its Source naming what verified it. Anything unverifiable stays
// out (a wrong "emergency" number is a licence bypass; a missing one is
// an INV-01 failure), and the tenant enters its own numbers instead.
//
// 112 is in NO entry: it is the separate international floor, always in
// the effective set regardless of country.
type CatalogueEntry struct {
	// Number is digits only, as dialled (a leading "+" is stripped by
	// the matcher on both sides).
	Number string
	// Source names what verified this number. Never empty.
	Source string
}

// CountryEmergencyCatalogue is the built-in catalogue. Codes are upper
// case ISO 3166-1 alpha-2.
var CountryEmergencyCatalogue = map[string][]CatalogueEntry{
	// Cameroon (home market): police 117, fire 118, ambulance (SAMU)
	// 119, national gendarmerie 113. Verified against the carrier
	// emergency listing citing the DGSN (MTN Cameroon, 2024-06-14),
	// the US State Department travel advisory, and cross-checks.
	"CM": {
		{Number: "117", Source: "MTN Cameroon emergency contacts (citing DGSN) + US State Dept advisory"},
		{Number: "118", Source: "MTN Cameroon emergency contacts (citing DGSN) + US State Dept advisory"},
		{Number: "119", Source: "MTN Cameroon emergency contacts (SAMU) + US State Dept advisory"},
		{Number: "113", Source: "MTN Cameroon emergency contacts (National Gendarmerie) + US State Dept advisory"},
	},
	// United States: 911, the NANP universal emergency number,
	// designated by Congress in 1999 (FCC).
	"US": {
		{Number: "911", Source: "FCC 9-1-1 and E911 Services (universal emergency number, US + Canada)"},
	},
}

// DefaultEmergencySet is the effective set for a tenant with no
// emergency country configured: the international floor plus the two
// numbers a caller is most likely to try unprompted. It is a safe
// default, not an empty one, and the profile reports "no country set"
// so the console can prompt for it.
var DefaultEmergencySet = NewEmergencyNumberSet("112", "911", "999")

// EffectiveEmergencySet resolves the numbers treated as emergency for a
// tenant: {112} plus the catalogue for countryCode plus additions. An
// empty countryCode yields the default set plus additions; a country
// outside the catalogue yields {112} plus additions, and inCatalogue
// reports false so the caller can flag "enter your numbers".
func EffectiveEmergencySet(countryCode string, additions []string) (set EmergencyNumberSet, inCatalogue bool) {
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))
	if countryCode == "" {
		set = NewEmergencyNumberSet("112", "911", "999")
		for _, n := range additions {
			if d := emergencyDigits(n); d != "" {
				set[d] = struct{}{}
			}
		}
		return set, false
	}
	entries, ok := CountryEmergencyCatalogue[countryCode]
	if !ok {
		set = NewEmergencyNumberSet("112")
		for _, n := range additions {
			if d := emergencyDigits(n); d != "" {
				set[d] = struct{}{}
			}
		}
		return set, false
	}
	members := []string{"112"}
	for _, e := range entries {
		members = append(members, e.Number)
	}
	members = append(members, additions...)
	return NewEmergencyNumberSet(members...), true
}

// CatalogueNumbers returns the sorted catalogue numbers for countryCode,
// or nil when the country is not catalogued.
func CatalogueNumbers(countryCode string) []string {
	entries, ok := CountryEmergencyCatalogue[strings.ToUpper(strings.TrimSpace(countryCode))]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Number)
	}
	sort.Strings(out)
	return out
}

// IsCatalogueNumber reports whether number is 112 or a catalogue number
// for the tenant's country — the numbers a tenant must never be able to
// delete (design D6).
func IsCatalogueNumber(countryCode, number string) bool {
	if emergencyDigits(number) == "112" {
		return true
	}
	for _, n := range CatalogueNumbers(countryCode) {
		if emergencyDigits(number) == n {
			return true
		}
	}
	return false
}
