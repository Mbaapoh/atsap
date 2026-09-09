package domain

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrUntrusted is returned for every payload that fails verification,
// whatever the cause: a bad signature, a signature by a key this build
// does not trust, a malformed signature, or an empty payload.
//
// One error for all of them, deliberately. Distinguishing "wrong
// signature" from "malformed signature" in the returned error tells an
// attacker which half of a forgery attempt was closer, and no legitimate
// caller can act on the difference — in every case the answer is "obtain
// a valid licence from the portal" (LLD-08 §10).
var ErrUntrusted = errors.New("licensing: licence payload is not trusted")

// developmentPublicKeyHex is injected at build time for development and
// CI builds:
//
//	go build -ldflags "-X atsap-api/internal/licensing/domain.developmentPublicKeyHex=<hex>"
//
// It is EMPTY BY DEFAULT, which is the whole point: a build that forgets
// the flag is the strict build, so the failure mode is fail-safe rather
// than fail-open. A released binary trusts one key and rejects a
// development-signed token exactly as it rejects a forgery (D-54).
//
// This is a variable rather than a constant only because -ldflags cannot
// write a constant. Nothing assigns to it at runtime, and nothing may:
// that would be the bypass this design exists to avoid.
var developmentPublicKeyHex string

// productionPublicKeyHex is the vendor key, embedded in the artefact and
// never fetched. A key retrieved at runtime is a key an attacker can
// substitute (LLD-08 §10).
//
// The value below is a placeholder for the real vendor key, which is
// generated once and held offline. It is replaced before the first
// release; until then no production licence exists to verify, and the
// development key is how the dev stack and CI activate.
const productionPublicKeyHex = "0000000000000000000000000000000000000000000000000000000000000000"

// KeySet is the set of public keys a build trusts. Verification always
// runs; only the membership of this set differs between builds, and
// there is no configuration value, environment variable or code path
// that empties it or skips the check (D-54).
type KeySet struct {
	keys []ed25519.PublicKey
}

// NewKeySet builds a key set from the keys given. Empty and malformed
// keys are dropped rather than stored, so a KeySet never contains
// something that cannot verify.
func NewKeySet(keys ...ed25519.PublicKey) KeySet {
	var ks KeySet
	for _, k := range keys {
		if len(k) == ed25519.PublicKeySize {
			ks.keys = append(ks.keys, k)
		}
	}
	return ks
}

// Len reports how many keys this set trusts. It exists so a test can
// assert that a build with no -ldflags trusts exactly one (D-54, R-14).
func (k KeySet) Len() int { return len(k.keys) }

// TrustedKeys returns the keys this build trusts: the production key
// always, plus the development key when one was injected at build time.
func TrustedKeys() KeySet {
	keys := []ed25519.PublicKey{decodeKey(productionPublicKeyHex)}
	if developmentPublicKeyHex != "" {
		keys = append(keys, decodeKey(developmentPublicKeyHex))
	}
	return NewKeySet(keys...)
}

// decodeKey turns a hex-encoded key into a public key, returning nil for
// anything malformed. NewKeySet drops nil, so a corrupt build-time value
// removes a key rather than admitting a broken one.
func decodeKey(h string) ed25519.PublicKey {
	b, err := hex.DecodeString(h)
	if err != nil {
		return nil
	}
	return ed25519.PublicKey(b)
}

// LicenseToken is a verified licence. It can only be produced by
// VerifyToken, which is what makes "verification precedes parsing" a
// property of the type rather than a convention an implementer can
// forget (LLD-08 §3.1, D7 of the change design).
//
// Because of that, an unverified or absent licence is not a zero-valued
// LicenseToken — it is no LicenseToken at all, and the caller uses
// SetupEntitlement instead. See TestZeroTokenIsNotAnEntitlement.
type LicenseToken struct {
	Edition       string    `json:"edition"`
	Capacity      int       `json:"capacity"`       // concurrent channels
	MaxTenants    int       `json:"max_tenants"`    // D-51: 1 single, 0 unlimited
	MaxExtensions int       `json:"max_extensions"` // D-49: 0 unlimited
	ExpiresAt     time.Time `json:"expires_at"`
	InstanceID    string    `json:"instance_id"`
	Fingerprint   [5]string `json:"fingerprint"`
}

// VerifyToken checks the signature over payload against every trusted
// key and only then interprets the bytes.
//
// The ordering is structural, not a comment: json.Unmarshal is not
// reached unless a key verified the payload, and this is the only
// exported way to obtain a LicenseToken. There is deliberately no
// ParseToken, so a caller cannot skip the check by choosing a different
// function — task 2.3 asserts that the exported surface stays that way.
func VerifyToken(payload, signature []byte, keys KeySet) (LicenseToken, error) {
	if len(payload) == 0 || len(signature) != ed25519.SignatureSize {
		return LicenseToken{}, ErrUntrusted
	}

	verified := false
	for _, k := range keys.keys {
		if ed25519.Verify(k, payload, signature) {
			verified = true
			break
		}
	}
	if !verified {
		return LicenseToken{}, ErrUntrusted
	}

	var tok LicenseToken
	if err := json.Unmarshal(payload, &tok); err != nil {
		// A payload that verified but does not parse is our own bug or a
		// version skew, never an attack: only the holder of the private
		// key could have produced it. Distinct from ErrUntrusted so the
		// two are separable in logs.
		return LicenseToken{}, fmt.Errorf("licensing: verified payload did not parse: %w", err)
	}
	return tok, nil
}

// Entitlement turns a verified token into what the installation may do
// at the given time, applying expiry.
//
// An expired licence degrades to the floor rather than disabling
// anything (D-12, BR-LIC-02), and carries ReasonExpired so an
// administrator is told to renew rather than to buy more channels.
func (t LicenseToken) Entitlement(now time.Time) Entitlement {
	e := Entitlement{
		State:         StateValid,
		Edition:       t.Edition,
		Channels:      t.Capacity,
		MaxExtensions: t.MaxExtensions,
		MaxTenants:    t.MaxTenants,
	}
	if !t.ExpiresAt.IsZero() && !now.Before(t.ExpiresAt) {
		return DegradedEntitlement(e, ReasonExpired)
	}
	return e
}
