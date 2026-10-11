package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// secretBytes is the entropy of every refresh, reset and invitation
// secret: 256 bits (LLD-02 §5.3).
const secretBytes = 32

// NewSecret returns a fresh random secret, in the URL-safe form it travels
// in (a cookie or a link fragment), and the SHA-256 digest that is the
// only thing ever stored. High-entropy secrets need no slow hash, for the
// same reason API keys do not (LLD-02 §5.3).
func NewSecret() (raw, digest string, err error) {
	b := make([]byte, secretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate secret: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, DigestSecret(raw), nil
}

// DigestSecret returns the stored form of a presented secret.
func DigestSecret(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// throttleKey derives a throttle key. Keys are digests, so the throttle
// table holds neither emails nor addresses (D-64).
func throttleKey(kind, value string) string {
	sum := sha256.Sum256([]byte(kind + ":" + value))
	return hex.EncodeToString(sum[:])
}

type sourceKey struct{}

// WithSource records the caller's network address for throttling and
// auth events. Handlers set it from the connection peer, resolving
// X-Forwarded-For only for trusted proxies (design D7); the application
// never reads a header itself.
func WithSource(ctx context.Context, addr string) context.Context {
	return context.WithValue(ctx, sourceKey{}, addr)
}

func sourceFrom(ctx context.Context) string {
	s, _ := ctx.Value(sourceKey{}).(string)
	return s
}
