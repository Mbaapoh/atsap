package application

import (
	"strings"
	"unicode"
)

// presentedCallerID is the caller ID a ringing destination is shown, in
// the engine's `"Name" <number>` form, or `<number>` with no name. Both
// parts come from outside: the name is user-entered and a carrier's
// number arrives on the wire. Neither may rewrite what is presented, so
// the number keeps only the characters a SIP user part dials with, and
// the name loses the characters that delimit or escape the form. An
// empty result presents nothing.
func presentedCallerID(number, name string) string {
	number = strings.Map(func(r rune) rune {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)), strings.ContainsRune("+*#._-", r):
			return r
		}
		return -1
	}, number)
	if number == "" {
		return ""
	}
	name = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if strings.ContainsRune(`"<>\`, r) || unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name)), " ")
	if name == "" {
		return "<" + number + ">"
	}
	return `"` + name + `" <` + number + ">"
}
