// Package application orchestrates the identity bounded context: it
// composes the pure domain rules with credential hashing, token issue
// and validation, and the storage ports. It depends on domain and ports
// only — never on another bounded context, never on an adapter
// (docs/hld/01-architecture.md §1.2).
package application

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters, fixed here rather than left to a library default
// that can shift under us between versions. These are the OWASP Password
// Storage Cheat Sheet minimum for Argon2id (LLD-02 §5.3):
//
//	memory      19 MiB
//	iterations  2
//	parallelism 1
//
// Raising them later is safe and does not invalidate existing hashes:
// every hash records the parameters it was produced with, and
// VerifyPassword reads them from the stored value rather than assuming
// today's constants.
const (
	argon2Memory      uint32 = 19 * 1024 // KiB, i.e. 19 MiB
	argon2Iterations  uint32 = 2
	argon2Parallelism uint8  = 1
	argon2SaltLength  int    = 16
	argon2KeyLength   uint32 = 32
)

// ErrInvalidHashFormat is returned when a stored hash cannot be parsed —
// a corrupted row or one written by something that is not this hasher.
// It is deliberately distinct from "the password was wrong": the first
// is an operational fault worth alerting on, the second is routine.
var ErrInvalidHashFormat = errors.New("stored password hash is not in the expected format")

// PasswordHasher hashes and verifies passwords with Argon2id.
type PasswordHasher struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  int
	keyLength   uint32
}

// NewPasswordHasher returns a hasher at the fixed parameters above.
func NewPasswordHasher() *PasswordHasher {
	return &PasswordHasher{
		memory:      argon2Memory,
		iterations:  argon2Iterations,
		parallelism: argon2Parallelism,
		saltLength:  argon2SaltLength,
		keyLength:   argon2KeyLength,
	}
}

// Hash returns an encoded Argon2id hash of password, in the standard
// PHC string format so the parameters travel with the hash:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>
//
// A fresh random salt is drawn per call, so hashing the same password
// twice yields different results — two accounts sharing a password are
// not identifiable from the stored values.
func (h *PasswordHasher) Hash(password string) (string, error) {
	salt := make([]byte, h.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, h.iterations, h.memory, h.parallelism, h.keyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.memory, h.iterations, h.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify reports whether password matches encodedHash.
//
// The comparison is constant-time: a byte-by-byte early exit would leak,
// through timing, how much of a guess was correct.
func (h *PasswordHasher) Verify(password, encodedHash string) (bool, error) {
	memory, iterations, parallelism, salt, want, err := decodeHash(encodedHash)
	if err != nil {
		return false, err
	}

	// Verify with the parameters the hash was created with, not today's
	// constants — otherwise raising the cost parameters would lock out
	// every existing account.
	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(want)))

	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// decodeHash parses a PHC-format Argon2id string back into its
// parameters, salt, and digest.
func decodeHash(encodedHash string) (memory, iterations uint32, parallelism uint8, salt, hash []byte, err error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return 0, 0, 0, nil, nil, ErrInvalidHashFormat
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("%w: version: %v", ErrInvalidHashFormat, err)
	}
	if version != argon2.Version {
		return 0, 0, 0, nil, nil, fmt.Errorf("%w: unsupported argon2 version %d", ErrInvalidHashFormat, version)
	}

	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("%w: parameters: %v", ErrInvalidHashFormat, err)
	}

	salt, err = base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("%w: salt: %v", ErrInvalidHashFormat, err)
	}
	hash, err = base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("%w: digest: %v", ErrInvalidHashFormat, err)
	}

	return memory, iterations, parallelism, salt, hash, nil
}
