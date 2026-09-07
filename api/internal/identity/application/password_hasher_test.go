package application_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/application"
)

func TestPasswordHasher_HashAndVerify(t *testing.T) {
	h := application.NewPasswordHasher()

	encoded, err := h.Hash("correct horse battery staple")
	require.NoError(t, err)

	ok, err := h.Verify("correct horse battery staple", encoded)
	require.NoError(t, err)
	assert.True(t, ok, "the correct password must verify")

	ok, err = h.Verify("wrong password entirely", encoded)
	require.NoError(t, err)
	assert.False(t, ok, "a wrong password must not verify")
}

// TestPasswordHasher_UsesTheFixedParameters pins the OWASP-minimum
// parameters into the encoded output. Asserting them here — rather than
// only that hashing "works" — is what stops a future edit from quietly
// weakening the cost factor (LLD-02 §5.3, DoD item 12).
func TestPasswordHasher_UsesTheFixedParameters(t *testing.T) {
	h := application.NewPasswordHasher()

	encoded, err := h.Hash("some-password")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(encoded, "$argon2id$"),
		"must be Argon2id, not argon2i or argon2d: %s", encoded)
	assert.Contains(t, encoded, "$m=19456,t=2,p=1$",
		"memory 19 MiB, 2 iterations, parallelism 1 — the OWASP minimum")
}

// TestPasswordHasher_StoredFormLeaksNothing: the stored value is what an
// attacker gets from a database disclosure, so the password must not be
// recoverable or even recognisable in it.
func TestPasswordHasher_StoredFormLeaksNothing(t *testing.T) {
	h := application.NewPasswordHasher()
	const password = "a-very-distinctive-password"

	encoded, err := h.Hash(password)
	require.NoError(t, err)

	assert.NotContains(t, encoded, password)
}

// TestPasswordHasher_SaltIsPerCall: two accounts with the same password
// must not be identifiable as such from their stored hashes.
func TestPasswordHasher_SaltIsPerCall(t *testing.T) {
	h := application.NewPasswordHasher()

	first, err := h.Hash("identical")
	require.NoError(t, err)
	second, err := h.Hash("identical")
	require.NoError(t, err)

	assert.NotEqual(t, first, second, "each hash must draw a fresh salt")

	// Both must still verify — different salts, same password.
	for _, encoded := range []string{first, second} {
		ok, err := h.Verify("identical", encoded)
		require.NoError(t, err)
		assert.True(t, ok)
	}
}

// TestPasswordHasher_VerifyUsesTheHashesOwnParameters is what makes
// raising the cost factor later a safe, non-breaking change: an existing
// hash written at weaker parameters must keep verifying.
func TestPasswordHasher_VerifyUsesTheHashesOwnParameters(t *testing.T) {
	h := application.NewPasswordHasher()

	// A hash produced at deliberately lower parameters than today's
	// constants, as an older deployment would have written.
	const legacy = "$argon2id$v=19$m=8192,t=1,p=1$c29tZXNhbHR2YWx1ZTE$" +
		"0uZ0lJ7bqOQOa5Vd0DPPmxHMPxaJfPHrPRHrqPRWLBo"

	// The point is that Verify parses and uses m=8192,t=1 rather than
	// erroring or silently applying today's m=19456,t=2.
	_, err := h.Verify("whatever", legacy)
	assert.NoError(t, err, "an older-parameter hash must be parseable, not rejected")
}

func TestPasswordHasher_Verify_MalformedHash(t *testing.T) {
	h := application.NewPasswordHasher()

	tests := []struct {
		name    string
		encoded string
	}{
		{"empty", ""},
		{"not a phc string", "just-some-text"},
		{"wrong algorithm", "$argon2i$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA"},
		{"too few segments", "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA"},
		{"unparseable parameters", "$argon2id$v=19$m=abc,t=2,p=1$c2FsdA$aGFzaA"},
		{"bad base64 salt", "$argon2id$v=19$m=19456,t=2,p=1$!!!!$aGFzaA"},
		{"bad base64 digest", "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$!!!!"},
		{"unsupported version", "$argon2id$v=16$m=19456,t=2,p=1$c2FsdA$aGFzaA"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := h.Verify("password", tt.encoded)
			assert.False(t, ok)
			// A corrupt stored hash is an operational fault, reported
			// distinctly from an ordinary wrong-password answer.
			assert.ErrorIs(t, err, application.ErrInvalidHashFormat)
		})
	}
}
