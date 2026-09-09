package domain_test

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/domain"
	shareddomain "atsap-api/internal/shared/domain"
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

// TestEndpointIdentifier_IsNotDerivedFromTheNumber covers task 2.4. The
// property is negative — the identifier a device presents must not be
// predictable from the extension number — so it is asserted over many
// extensions rather than one.
func TestEndpointIdentifier_IsNotDerivedFromTheNumber(t *testing.T) {
	const number = "1000"

	seen := make(map[string]struct{}, 128)
	for range 128 {
		id := shareddomain.NewExtensionID()
		identifier := domain.EndpointIdentifier(id)

		assert.NotEqual(t, number, identifier, "the identifier must never be the extension number")
		assert.NotContains(t, identifier, number,
			"the identifier must not contain the extension number: knowing the numbering plan must not reveal it")

		_, dup := seen[identifier]
		require.False(t, dup, "identifiers must be unique across extensions — ps_* is one namespace for every tenant")
		seen[identifier] = struct{}{}
	}
}

// The identifier is derived, not stored, so it must be a pure function of
// the extension id — otherwise deprovisioning, which holds only the id,
// could not name the rows it has to remove.
func TestEndpointIdentifier_IsDerivedAndStable(t *testing.T) {
	id := shareddomain.NewExtensionID()

	assert.Equal(t, domain.EndpointIdentifier(id), domain.EndpointIdentifier(id),
		"the same extension id must always yield the same identifier")
	assert.NotEqual(t, domain.EndpointIdentifier(id), domain.EndpointIdentifier(shareddomain.NewExtensionID()),
		"different extensions must yield different identifiers")
	assert.True(t, strings.HasPrefix(domain.EndpointIdentifier(id), "e_"),
		"the prefix distinguishes extension endpoints from the trunk endpoints a later change adds")
}

// TestExtensionIDFromIdentifier covers the inverse of
// EndpointIdentifier, which had no test until 2026-09-09 despite being
// what `atsap-api pbx reconcile` relies on to decide whether a projected
// endpoint belongs to this platform at all.
//
// The consequence of getting it wrong is quiet: an identifier that fails
// to parse is reported as unattributable, so a real orphan would be
// filed as "not ours" and never cleaned up — or, worse, a valid
// extension would be.
func TestExtensionIDFromIdentifier(t *testing.T) {
	t.Run("round-trips every identifier EndpointIdentifier produces", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			id := shareddomain.NewExtensionID()

			got, ok := domain.ExtensionIDFromIdentifier(domain.EndpointIdentifier(id))

			require.True(t, ok)
			assert.Equal(t, id, got)
		}
	})

	t.Run("rejects identifiers that are not ours", func(t *testing.T) {
		valid := domain.EndpointIdentifier(shareddomain.NewExtensionID())

		for name, identifier := range map[string]string{
			"empty":                    "",
			"no prefix":                strings.TrimPrefix(valid, "e_"),
			"wrong prefix":             "x_" + strings.TrimPrefix(valid, "e_"),
			"prefix only":              "e_",
			"not hex":                  "e_zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
			"too short":                "e_00112233445566778899aabbccddee",
			"too long":                 valid + "00",
			"an Asterisk-style name":   "e_1001",
			"a raw extension number":   "1001",
			"upper-case prefix":        "E_" + strings.TrimPrefix(valid, "e_"),
			"embedded null":            "e_00112233445566778899aabbccddee\x00",
			"a plausible foreign name": "e_someothersystemsendpoint00000",
		} {
			t.Run(name, func(t *testing.T) {
				_, ok := domain.ExtensionIDFromIdentifier(identifier)
				assert.False(t, ok, "%q must not parse as one of ours", identifier)
			})
		}
	})

	t.Run("an identifier from another platform is unattributable, not misattributed", func(t *testing.T) {
		// The failure that matters for reconcile: something else's
		// endpoint must never resolve to one of our extension ids.
		_, ok := domain.ExtensionIDFromIdentifier("sip-trunk-carrier-a")
		assert.False(t, ok)
	})
}
