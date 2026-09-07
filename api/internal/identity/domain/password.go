package domain

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// Password length bounds. NIST SP 800-63B §5.1.1.2: length is what
// makes a password hard to guess, while composition rules ("must
// contain a symbol") push users toward predictable patterns like
// "Password1!". So this policy sets a floor and a generous ceiling and
// dictates nothing about content.
//
// The ceiling is not cosmetic: Argon2id hashes whatever it is given, so
// an unbounded password is an unbounded amount of hashing work on an
// unauthenticated code path.
const (
	MinPasswordLength = 8
	MaxPasswordLength = 64
)

// ErrPasswordTooShort and ErrPasswordTooLong are returned by
// ValidatePassword. They are distinct so a caller can tell a user which
// bound they missed without inspecting message text.
var (
	ErrPasswordTooShort = errors.New("password is shorter than the minimum length")
	ErrPasswordTooLong  = errors.New("password is longer than the maximum length")
)

// ValidatePassword reports whether a candidate password is acceptable.
// Pure: no hashing, no I/O, no storage — it answers only "may this be
// used", so the same rule can be applied at provisioning and at reset
// without either path reimplementing it.
//
// Length is counted in runes, not bytes: a 20-character passphrase in a
// non-Latin script must not be rejected for being "too long" because
// its UTF-8 encoding is larger.
func ValidatePassword(password string) error {
	n := utf8.RuneCountInString(password)
	switch {
	case n < MinPasswordLength:
		return fmt.Errorf("%w: %d < %d", ErrPasswordTooShort, n, MinPasswordLength)
	case n > MaxPasswordLength:
		return fmt.Errorf("%w: %d > %d", ErrPasswordTooLong, n, MaxPasswordLength)
	default:
		return nil
	}
}
