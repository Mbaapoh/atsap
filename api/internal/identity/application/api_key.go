package application

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	shareddomain "atsap-api/internal/shared/domain"
)

// apiKeyRandomBytes is the entropy behind each generated key. 32 bytes
// (256 bits) from crypto/rand is far past guessable, which is what makes
// the SHA-256 storage choice below sound: a slow hash defends a
// low-entropy human-chosen secret, and this is neither.
const apiKeyRandomBytes = 32

// apiKeyPrefix marks a string as an AtsaPBX API key. It exists so a key
// leaked into a log, a paste, or a repository is recognisable as a
// credential to revoke, rather than an anonymous blob nobody acts on.
const apiKeyPrefix = "atsa_"

// ErrMalformedAPIKey is returned when a presented key is not in the
// expected `atsa_<tenant-id>_<random>` form.
var ErrMalformedAPIKey = errors.New("api key is malformed")

// GenerateAPIKey returns a new API key: the raw value to hand the caller
// exactly once, and the digest to store.
//
// The key carries its own tenant ID — `atsa_<tenant-id>_<random>` —
// because the api_keys table is protected by row-level security, and a
// lookup by digest alone finds nothing without a tenant context to set
// first. Embedding it makes API-key authentication a tenant-scoped read
// like every other one, rather than requiring a second BYPASSRLS role
// with a standing cross-tenant read path over a credentials table.
//
// The tenant ID is not a secret (it is the caller's own, and appears in
// their every request); the unguessable part is the 256 random bits that
// follow it. The raw value is never persisted and cannot be recovered
// from the digest — losing it means issuing a new key, which is the
// intended behaviour, not a limitation to work around.
func GenerateAPIKey(tenantID shareddomain.TenantID) (raw, digest string, err error) {
	buf := make([]byte, apiKeyRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate api key: %w", err)
	}

	raw = apiKeyPrefix + tenantID.String() + "_" + base64.RawURLEncoding.EncodeToString(buf)
	return raw, HashAPIKey(raw), nil
}

// TenantFromAPIKey extracts the tenant a presented key belongs to, so
// the caller can open a tenant-scoped transaction before looking the
// digest up.
//
// This identifies which tenant's rows to search — it authenticates
// nothing on its own. A forged key naming a real tenant still fails at
// the digest comparison, which is the step that actually proves
// possession.
func TenantFromAPIKey(raw string) (shareddomain.TenantID, error) {
	rest, ok := strings.CutPrefix(raw, apiKeyPrefix)
	if !ok {
		return shareddomain.TenantID{}, fmt.Errorf("%w: missing prefix", ErrMalformedAPIKey)
	}
	tenantPart, _, ok := strings.Cut(rest, "_")
	if !ok {
		return shareddomain.TenantID{}, fmt.Errorf("%w: missing tenant segment", ErrMalformedAPIKey)
	}
	tenantID, err := shareddomain.ParseTenantID(tenantPart)
	if err != nil {
		return shareddomain.TenantID{}, fmt.Errorf("%w: %v", ErrMalformedAPIKey, err)
	}
	return tenantID, nil
}

// HashAPIKey returns the stored form of an API key.
//
// SHA-256, deliberately, where passwords get Argon2id: an API key is a
// 256-bit random value, so there is no guessing attack for a slow hash
// to slow down, and verification sits on a hot request path. HLD 04 §7
// specifies SHA-256 for exactly this reason (LLD-02 §5.3).
func HashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// VerifyAPIKey reports whether a presented key matches a stored digest,
// in constant time so the comparison does not leak how much of a guess
// was right.
func VerifyAPIKey(presented, storedDigest string) bool {
	return subtle.ConstantTimeCompare(
		[]byte(HashAPIKey(presented)),
		[]byte(storedDigest),
	) == 1
}
