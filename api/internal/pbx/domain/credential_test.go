package domain_test

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/domain"
)

// TestHA1_MatchesTheSchemeAsterisTests holds the concatenation order and
// encoding to the one Asterisk actually accepted during the D-47 spike.
// The expected value is not copied from the implementation: it is the
// digest that was inserted into ps_auths.md5_cred and produced a 200 OK
// for a real REGISTER, so if this test ever fails the credential no
// longer authenticates.
func TestHA1_MatchesTheSchemeAsteriskAccepted(t *testing.T) {
	// Exactly the inputs used in the verified spike.
	got := domain.HA1("9002", "asterisk", "s3cret-generated")
	assert.Equal(t, "836b6b01ed9b31bf4bf76615914eb239", got,
		"HA1 must be MD5(username:realm:secret) in lowercase hex — this exact digest was accepted by Asterisk 22.8.2")
}

func TestHA1_IsSensitiveToEveryComponent(t *testing.T) {
	base := domain.HA1("user", "realm", "secret")
	assert.NotEqual(t, base, domain.HA1("other", "realm", "secret"), "username must affect the digest")
	assert.NotEqual(t, base, domain.HA1("user", "other", "secret"), "realm must affect the digest")
	assert.NotEqual(t, base, domain.HA1("user", "realm", "other"), "secret must affect the digest")
}

func TestNewExtensionCredential_IsDeterministicForAGivenReader(t *testing.T) {
	fixed := bytes.Repeat([]byte{0xAB}, 64)

	plaintext, digest, err := domain.NewExtensionCredential("u1", "asterisk", bytes.NewReader(fixed))
	require.NoError(t, err)

	assert.NotEmpty(t, plaintext)
	assert.Equal(t, domain.HA1("u1", "asterisk", plaintext), digest,
		"the returned digest must be the HA1 of the returned plaintext, or the credential cannot authenticate")

	again, digestAgain, err := domain.NewExtensionCredential("u1", "asterisk", bytes.NewReader(fixed))
	require.NoError(t, err)
	assert.Equal(t, plaintext, again, "same reader bytes must yield the same secret")
	assert.Equal(t, digest, digestAgain)
}

func TestNewExtensionCredential_NeverRepeatsWithCryptoRand(t *testing.T) {
	seen := make(map[string]struct{}, 256)
	for range 256 {
		plaintext, _, err := domain.NewExtensionCredential("u1", "asterisk", rand.Reader)
		require.NoError(t, err)
		_, dup := seen[plaintext]
		require.False(t, dup, "generated secrets must not repeat")
		seen[plaintext] = struct{}{}
	}
}

// The secret is offline-crackable once the HA1 leaks, so its entropy is
// the control. 20 random bytes encoded as base32 is 32 characters.
func TestNewExtensionCredential_HasFullEntropyAndSafeAlphabet(t *testing.T) {
	plaintext, _, err := domain.NewExtensionCredential("u1", "asterisk", rand.Reader)
	require.NoError(t, err)

	assert.Len(t, plaintext, 32, "20 random bytes in unpadded base32 is 32 characters")
	for _, r := range plaintext {
		assert.True(t, strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567", r),
			"secret must stay in the base32 alphabet so it survives being typed into a desk phone: %q", r)
	}
}

func TestNewExtensionCredential_RequiresARealm(t *testing.T) {
	_, _, err := domain.NewExtensionCredential("u1", "", rand.Reader)
	require.ErrorIs(t, err, domain.ErrRealmRequired,
		"an empty realm produces a digest that can never authenticate, so it must fail loudly at generation")
}

// TestNewAuthUsername_IsNotDerivedFromTheNumber covers task 2.4. The
// property is negative — the username must not be predictable from the
// extension number — so it is asserted over many generations rather than
// one.
func TestNewAuthUsername_IsNotDerivedFromTheNumber(t *testing.T) {
	const number = "1000"

	seen := make(map[string]struct{}, 128)
	for range 128 {
		username, err := domain.NewAuthUsername(rand.Reader)
		require.NoError(t, err)

		assert.NotEqual(t, number, username, "the username must never be the extension number")
		assert.NotContains(t, username, number,
			"the username must not contain the extension number: knowing the numbering plan must not reveal it")

		_, dup := seen[username]
		require.False(t, dup, "generated usernames must not repeat")
		seen[username] = struct{}{}
	}
}
