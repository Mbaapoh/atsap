package application

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"atsap-api/internal/identity/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

// tokenAlgorithm is the one signing algorithm this system issues and the
// only one it accepts. It is a constant, not a parameter read from a
// token: see TokenIssuer.Validate.
const tokenAlgorithm = "EdDSA"

const tokenType = "JWT"

// DefaultTokenLifetime is how long an issued token remains valid.
// Fifteen minutes with no refresh-token flow: machine clients
// re-authenticate cheaply, and a refresh/rotation scheme with its own
// revocation surface is deferred until a human-facing session actually
// needs one (LLD-02 §5.3).
const DefaultTokenLifetime = 15 * time.Minute

// Token validation failures. They are distinct so callers can log
// precisely, but note that all of them are reported to the *client* as
// the same unauthenticated outcome — telling a caller which part of
// their token was wrong is free reconnaissance.
var (
	ErrTokenMalformed           = errors.New("token is malformed")
	ErrTokenAlgorithmNotAllowed = errors.New("token declares an algorithm this system does not accept")
	ErrTokenSignatureInvalid    = errors.New("token signature is not valid")
	ErrTokenExpired             = errors.New("token has expired")
	ErrTokenNotYetValid         = errors.New("token is not valid yet")
	// ErrTokenUnknownKey is a token naming a signing key this system does
	// not hold. It is refused before any signature work, like a wrong alg
	// (D-66).
	ErrTokenUnknownKey = errors.New("token names an unknown signing key")
)

// tokenHeader is the JWT header. Alg is written on issue and *checked*
// on validation, never trusted to select an algorithm.
type tokenHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	// Kid names the signing key (D-66): hex of the first 8 bytes of
	// SHA-256(public key). Absent only on tokens issued before revision 2.
	Kid string `json:"kid,omitempty"`
}

// Claims is the token payload: who the caller is, which tenant they act
// for, and what they may do.
type Claims struct {
	TenantID    string   `json:"tid"`
	PrincipalID string   `json:"sub"`
	Roles       []string `json:"roles,omitempty"`
	// UserID and SessionID tie a console token to its person and session
	// (D-62). Both are absent on API-key and deprecated
	// AuthenticateUser tokens.
	UserID    string `json:"uid,omitempty"`
	SessionID string `json:"sid,omitempty"`
	IssuedAt  int64  `json:"iat"`
	NotBefore int64  `json:"nbf"`
	ExpiresAt int64  `json:"exp"`
}

// TokenIssuer issues and validates Ed25519-signed tokens.
//
// Built on crypto/ed25519 and encoding/json rather than a JWT library:
// TOOLSET.md vets no JWT dependency, and the surface needed here is two
// JSON objects and a signature. A library would bring algorithm
// negotiation — the very thing this type refuses to do (LLD-02 §5.3).
type TokenIssuer struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	kid        string
	// verifiers holds every key a token may be validated against, by kid:
	// the current key and any previous ones (D-66). Only the current key
	// ever signs.
	verifiers map[string]ed25519.PublicKey
	lifetime  time.Duration
	now       func() time.Time
	// verify is ed25519.Verify; a test replaces it to count signature work.
	verify func(ed25519.PublicKey, []byte, []byte) bool
}

// KeyID returns the key id of a public key: hex of the first 8 bytes of
// its SHA-256 (D-66).
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

// PreviousKeysFromHex parses ATSAPBX_JWT_PREVIOUS_KEYS: comma-separated
// hex Ed25519 seeds. Blank entries are ignored; a malformed one fails.
func PreviousKeysFromHex(s string) ([]ed25519.PrivateKey, error) {
	var out []ed25519.PrivateKey
	for _, part := range strings.Split(s, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		k, err := TokenKeyFromHex(part)
		if err != nil {
			return nil, fmt.Errorf("previous token key: %w", err)
		}
		out = append(out, k)
	}
	return out, nil
}

// TokenKeyFromHex decodes a hex-encoded 32-byte Ed25519 seed into the
// private key the issuer signs with. The seed travels as configuration
// (ATSAPBX_JWT_KEY), never in code; an invalid or wrong-length value
// fails here rather than at first use.
func TokenKeyFromHex(s string) (ed25519.PrivateKey, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("decode token key hex: %w", err)
	}
	if len(raw) != ed25519.SeedSize {
		return nil, fmt.Errorf("token key seed must be %d bytes (hex), got %d", ed25519.SeedSize, len(raw))
	}
	return ed25519.NewKeyFromSeed(raw), nil
}

// NewTokenIssuer returns an issuer signing with privateKey.
func NewTokenIssuer(privateKey ed25519.PrivateKey, lifetime time.Duration) (*TokenIssuer, error) {
	if l := len(privateKey); l != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("ed25519 private key must be %d bytes, got %d", ed25519.PrivateKeySize, l)
	}
	if lifetime <= 0 {
		lifetime = DefaultTokenLifetime
	}
	pub, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("private key does not yield an ed25519 public key")
	}
	kid := KeyID(pub)
	return &TokenIssuer{
		privateKey: privateKey,
		publicKey:  pub,
		kid:        kid,
		verifiers:  map[string]ed25519.PublicKey{kid: pub},
		lifetime:   lifetime,
		now:        time.Now,
		verify:     ed25519.Verify,
	}, nil
}

// WithPreviousKeys returns an issuer that also validates tokens signed by
// previous keys, so rotating the signing key signs nobody out (D-66). A
// previous key equal to the current one is a configuration mistake and
// fails, rather than silently doing nothing.
func (i *TokenIssuer) WithPreviousKeys(previous ...ed25519.PrivateKey) (*TokenIssuer, error) {
	clone := *i
	clone.verifiers = make(map[string]ed25519.PublicKey, len(i.verifiers)+len(previous))
	for k, v := range i.verifiers {
		clone.verifiers[k] = v
	}
	for _, p := range previous {
		if len(p) != ed25519.PrivateKeySize {
			return nil, errors.New("previous token key has the wrong size")
		}
		pub, ok := p.Public().(ed25519.PublicKey)
		if !ok {
			return nil, errors.New("previous token key does not yield an ed25519 public key")
		}
		kid := KeyID(pub)
		if kid == i.kid {
			return nil, errors.New("a previous token key equals the current key")
		}
		clone.verifiers[kid] = pub
	}
	return &clone, nil
}

// WithClock replaces the issuer's time source. Tests use it to drive
// expiry deterministically rather than by sleeping.
func (i *TokenIssuer) WithClock(now func() time.Time) *TokenIssuer {
	clone := *i
	clone.now = now
	return &clone
}

// Issue returns a signed token for a principal, tied to no session.
func (i *TokenIssuer) Issue(tenantID shareddomain.TenantID, principalID shareddomain.PrincipalID, roles []string) (string, error) {
	return i.IssueFor(tenantID, principalID, roles, domain.UserID{}, domain.SessionID{})
}

// IssueFor returns a signed token for a membership, naming its person and
// console session when they are set.
func (i *TokenIssuer) IssueFor(tenantID shareddomain.TenantID, principalID shareddomain.PrincipalID, roles []string,
	user domain.UserID, session domain.SessionID) (string, error) {
	issued := i.now().UTC()
	claims := Claims{
		TenantID:    tenantID.String(),
		PrincipalID: principalID.String(),
		Roles:       roles,
		IssuedAt:    issued.Unix(),
		NotBefore:   issued.Unix(),
		ExpiresAt:   issued.Add(i.lifetime).Unix(),
	}
	if !user.IsZero() {
		claims.UserID = user.String()
	}
	if !session.IsZero() {
		claims.SessionID = session.String()
	}

	headerJSON, err := json.Marshal(tokenHeader{Alg: tokenAlgorithm, Typ: tokenType, Kid: i.kid})
	if err != nil {
		return "", fmt.Errorf("encode token header: %w", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode token claims: %w", err)
	}

	signingInput := encodeSegment(headerJSON) + "." + encodeSegment(claimsJSON)
	signature := ed25519.Sign(i.privateKey, []byte(signingInput))

	return signingInput + "." + encodeSegment(signature), nil
}

// Validate checks a token and returns its claims.
//
// The accepted algorithm is this system's own constant. The token's
// `alg` header is compared against it and the token is rejected on any
// mismatch — including `none` — *before* any signature work happens.
// Reading `alg` to decide how to verify is the "algorithm confusion"
// vulnerability; there is no such decision here to get wrong
// (LLD-02 §10.7, OWASP A02/A07).
func (i *TokenIssuer) Validate(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("%w: expected 3 segments, got %d", ErrTokenMalformed, len(parts))
	}

	headerJSON, err := decodeSegment(parts[0])
	if err != nil {
		return Claims{}, fmt.Errorf("%w: header: %v", ErrTokenMalformed, err)
	}
	var header tokenHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return Claims{}, fmt.Errorf("%w: header: %v", ErrTokenMalformed, err)
	}

	// Algorithm check first: a token that is not ours never reaches
	// signature verification at all.
	if subtle.ConstantTimeCompare([]byte(header.Alg), []byte(tokenAlgorithm)) != 1 {
		return Claims{}, fmt.Errorf("%w: %q", ErrTokenAlgorithmNotAllowed, header.Alg)
	}

	// Key selection second, still before any signature work: a token
	// naming a key we do not hold is refused here. A kid-less token
	// (issued before revision 2) is checked against the current key only.
	key := i.publicKey
	if header.Kid != "" {
		k, ok := i.verifiers[header.Kid]
		if !ok {
			return Claims{}, ErrTokenUnknownKey
		}
		key = k
	}

	signature, err := decodeSegment(parts[2])
	if err != nil {
		return Claims{}, fmt.Errorf("%w: signature: %v", ErrTokenMalformed, err)
	}
	signingInput := parts[0] + "." + parts[1]
	if !i.verify(key, []byte(signingInput), signature) {
		return Claims{}, ErrTokenSignatureInvalid
	}

	claimsJSON, err := decodeSegment(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("%w: claims: %v", ErrTokenMalformed, err)
	}
	var claims Claims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return Claims{}, fmt.Errorf("%w: claims: %v", ErrTokenMalformed, err)
	}

	now := i.now().UTC()
	if claims.ExpiresAt != 0 && now.Unix() >= claims.ExpiresAt {
		return Claims{}, ErrTokenExpired
	}
	if claims.NotBefore != 0 && now.Unix() < claims.NotBefore {
		return Claims{}, ErrTokenNotYetValid
	}

	return claims, nil
}

func encodeSegment(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeSegment(s string) ([]byte, error) {
	return base64.RawURLEncoding.Strict().DecodeString(s)
}
