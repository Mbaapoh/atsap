package application

import "crypto/ed25519"

// WithVerifyCounter returns an issuer whose signature check increments
// *n, so a test can prove a token was refused before any signature work.
func (i *TokenIssuer) WithVerifyCounter(n *int) *TokenIssuer {
	clone := *i
	clone.verify = func(k ed25519.PublicKey, msg, sig []byte) bool {
		*n++
		return ed25519.Verify(k, msg, sig)
	}
	return &clone
}
