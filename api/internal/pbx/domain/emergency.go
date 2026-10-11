package domain

import "strings"

// EmergencyNumberSet is the set of destinations treated as emergency
// (INV-01). It is owned by the dial plan: the ingress path supplies the
// set, this package only matches against it. Members are stored
// normalized (digits only); empty members are dropped, so a set can
// never contain "" — an empty dialled string matching an empty member
// would bypass Screening for every unparseable destination, and this
// matcher fails closed instead.
type EmergencyNumberSet map[string]struct{}

// NewEmergencyNumberSet builds the set the ingress path matches against,
// normalizing each member the same way dialled input is normalized.
func NewEmergencyNumberSet(numbers ...string) EmergencyNumberSet {
	set := make(EmergencyNumberSet, len(numbers))
	for _, n := range numbers {
		if d := emergencyDigits(n); d != "" {
			set[d] = struct{}{}
		}
	}
	return set
}

// IsEmergency reports whether dialled is an emergency destination
// (INV-01, LLD-03 §3). It is matched BEFORE any other evaluation, on the
// digits as dialled: both sides are reduced to ASCII digits and compared
// for exact equality.
//
// Exact, not prefix: "112" matches {"112"}, but "1120" — a genuine
// subscriber number in several numbering plans — must not. A prefix
// matcher here would exempt ordinary calls from capacity and compliance,
// which is precisely the licence-evasion hole LLD-03 §10.1 exists to
// prevent. Only visual separators and one leading "+" are stripped; any
// other non-digit — notably "*" and "#", which are dialled signalling
// characters, not formatting ("*112" is a feature-code dial) — makes the
// string ordinary, and a dialled string with no digits never matches.
func IsEmergency(dialled string, set EmergencyNumberSet) bool {
	d := emergencyDigits(dialled)
	if d == "" {
		return false
	}
	_, ok := set[d]
	return ok
}

// emergencyDigits reduces its input to the ASCII digits a network
// signals. Formatting callers add ("+112", "112 ", "1-1-2", "(911)") is
// stripped so the match is on what was dialled, not how it was
// punctuated. Anything that is neither a digit nor formatting returns ""
// — fail closed: an exemption from capacity and compliance is granted
// only for a string that unambiguously is the emergency number.
func emergencyDigits(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "+")
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == ' ' || c == '-' || c == '.' || c == '(' || c == ')':
		default:
			return ""
		}
	}
	return b.String()
}
