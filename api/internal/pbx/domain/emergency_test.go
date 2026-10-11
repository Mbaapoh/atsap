package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsEmergency is the identification half of INV-01 (LLD-03 §3): the
// dialled digits decide emergency, before any other evaluation. Each row
// exists because the adjacent wrong implementation would pass without
// it — stated inline, so a future simplification cannot silently drop
// the case that forbids it.
func TestIsEmergency(t *testing.T) {
	set := NewEmergencyNumberSet("112", "911", "999")

	tests := []struct {
		name    string
		dialled string
		set     EmergencyNumberSet
		want    bool
		why     string
	}{
		{"itu-t single emergency number", "112", set, true,
			"the core case: a configured short code matches"},
		{"nanp emergency number", "911", set, true,
			"the set carries the numbering plan, not the matcher"},
		{"ordinary subscriber number", "1001", set, false,
			"anything unlisted is an ordinary call"},
		{"emergency number with extra digit is ordinary", "1120", set, false,
			"exact match, never prefix: 1120 is a real subscriber range, and prefix matching would exempt ordinary calls from capacity and compliance"},
		{"emergency number as prefix of longer dial", "9111234567", set, false,
			"same trap from the other side: dialling on past 911 does not keep the exemption"},
		{"e.164 plus is tolerated", "+112", set, true,
			"match is on digits as dialled; punctuation is not dialling"},
		{"spaces and dashes are tolerated", " 9-1-1 ", set, true,
			"formatting added by handsets and directories must not defeat the match"},
		{"empty dialled never matches", "", set, false,
			"fail closed: unparseable input is ordinary, never exempt"},
		{"no digits never matches", "abc", set, false,
			"same fail-closed rule for non-numeric input"},
		{"nil set never matches", "112", nil, false,
			"no dial plan configured means no exemptions, not all-exempt"},
		{"empty set never matches", "112", NewEmergencyNumberSet(), false,
			"same rule for an explicitly empty plan"},
		{"empty member cannot poison the set", "112", NewEmergencyNumberSet("", "112"), true,
			"constructor drops empty members so a misconfigured plan cannot match everything"},
		{"star prefix is a feature code, not emergency", "*112", set, false,
			"* is dialled signalling, not punctuation: stripping it would exempt feature-code dials from capacity and compliance"},
		{"hash prefix is a feature code, not emergency", "#999", set, false,
			"same rule for #"},
		{"hash suffix is not formatting", "112#", set, false,
			"a trailing # changes what the network receives; only exact digits are exempt"},
		{"embedded plus is not formatting", "1+12", set, false,
			"only one leading + is E.164 formatting; anywhere else it is not"},
		{"parentheses are formatting", "(911)", set, true,
			"directory formatting is tolerated like spaces and dashes"},
		{"unlisted short code", "000", set, false,
			"short is not emergency by itself: only the plan decides"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsEmergency(tt.dialled, tt.set), tt.why)
		})
	}
}

// TestCountryCatalogue pins every catalogue entry and its source
// (task 3.3): entries are code, not configuration, so no deployment can
// empty them — and a wrong entry is a licence bypass, so each one names
// what verified it.
func TestCountryCatalogue(t *testing.T) {
	for code, entries := range CountryEmergencyCatalogue {
		require.Len(t, code, 2, "codes are ISO 3166-1 alpha-2")
		assert.Equal(t, strings.ToUpper(code), code)
		require.NotEmpty(t, entries, "a catalogue entry with no numbers helps nobody: %s", code)
		for _, e := range entries {
			assert.NotEmpty(t, e.Number, "catalogue number: %s", code)
			assert.NotEqual(t, "112", e.Number, "112 is the separate floor, never a catalogue entry: %s", code)
			assert.NotEmpty(t, e.Source, "every entry names what verified it: %s %s", code, e.Number)
			assert.Equal(t, e.Number, emergencyDigits(e.Number), "catalogue numbers are stored digits-only: %s %s", code, e.Number)
		}
	}
}

// TestEffectiveEmergencySet covers the country profile arithmetic.
func TestEffectiveEmergencySet(t *testing.T) {
	cm, inCat := EffectiveEmergencySet("CM", nil)
	assert.True(t, inCat)
	for _, n := range []string{"112", "117", "118", "119", "113"} {
		assert.True(t, IsEmergency(n, cm), "CM tenant treats %s as emergency", n)
	}
	assert.False(t, IsEmergency("911", cm), "a foreign emergency number is ordinary here")

	def, inCat := EffectiveEmergencySet("", nil)
	assert.False(t, inCat, "no country set is flagged, not catalogued")
	for _, n := range []string{"112", "911", "999"} {
		assert.True(t, IsEmergency(n, def))
	}

	xx, inCat := EffectiveEmergencySet("XX", []string{"15"})
	assert.False(t, inCat, "an uncatalogued country is flagged so the console prompts for numbers")
	assert.True(t, IsEmergency("112", xx))
	assert.True(t, IsEmergency("15", xx), "tenant additions always apply")

	withAdd, _ := EffectiveEmergencySet("cm", []string{"1500"})
	assert.True(t, IsEmergency("1500", withAdd), "country codes match case-insensitively")
}
