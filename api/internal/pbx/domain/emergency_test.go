package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
