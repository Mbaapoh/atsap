package domain

import (
	"crypto/md5" //nolint:gosec // Required by SIP digest auth (RFC 8760/3261); see the doc comment on NewExtensionCredential.
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"

	shareddomain "atsap-api/internal/shared/domain"
)

// Credential sizing. 20 random bytes is 160 bits of entropy, encoded as
// 32 base32 characters.
//
// Base32 rather than base64: the secret is typed into desk phones and
// pasted into provisioning files, so it avoids characters that are
// case-sensitive-ambiguous or need escaping. It costs a little length
// and buys far fewer support calls.
const secretEntropyBytes = 20

// ErrRealmRequired is returned when a credential is generated without a
// realm. It is not defaultable: the HA1 is computed over the realm, so a
// wrong or empty one produces a digest that can never authenticate.
var ErrRealmRequired = errors.New("sip realm is required to compute a credential digest")

var credentialEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// EndpointIdentifier is the single identifier an extension has in the
// media engine. It is simultaneously the engine's object id, the SIP
// username the device presents, and the username the digest is computed
// over.
//
// It is ONE value rather than two because Asterisk identifies an inbound
// REGISTER by matching the From user against the endpoint id, not
// against the auth username. Tested on Asterisk 22.8.2: registering with
// From set to the auth username returns 401; From set to the endpoint id
// returns 200 OK. A separately generated username would therefore never
// be what the device presents, and would additionally force a database
// lookup on deprovisioning, where only the ExtensionID is in hand.
//
// Derived, never stored, so it cannot drift from the extension it names.
// Deliberately NOT the extension number: `ps_*` is one flat namespace
// shared by every tenant, so two tenants' extension 1000 would collide
// and one tenant's phone could register against the other's endpoint.
// It also means that learning a tenant runs extensions 1000-1050 — which
// any internal directory reveals — tells an attacker nothing about what
// to present (LLD-03 §10.3).
func EndpointIdentifier(id shareddomain.ExtensionID) string {
	raw := uuid.UUID(id)
	return "e_" + hex.EncodeToString(raw[:])
}

// NewExtensionCredential generates a secret and returns it alongside the
// digest to store. The plaintext is returned to be shown to the operator
// exactly once and is never persisted anywhere.
//
// The digest is the MD5 HA1 — MD5(username:realm:secret) — and NOT
// Argon2id, which is what the rest of this platform uses for passwords.
// That is not a shortcut. SIP digest authentication requires the server
// to hold either the plaintext or the HA1 in order to answer a
// challenge; a one-way slow hash cannot participate. The choice is
// therefore between storing a recoverable plaintext and storing the HA1,
// and the HA1 is strictly better. Verified against Asterisk 22.8.2: a
// ps_auths row with an empty password column and md5_cred set accepts a
// real REGISTER and rejects a wrong secret (DECISIONS D-47, LLD-03 §7.3).
//
// Because the HA1 is password-equivalent and offline-crackable, the
// secret is GENERATED here and never chosen by a user. 160 bits of
// entropy makes cracking pointless, and a secret nobody chose is a
// secret nobody reused somewhere else.
func NewExtensionCredential(username, realm string, rand io.Reader) (plaintext, digest string, err error) {
	if realm == "" {
		return "", "", ErrRealmRequired
	}
	if username == "" {
		return "", "", errors.New("username is required to compute a credential digest")
	}

	b := make([]byte, secretEntropyBytes)
	if _, err := io.ReadFull(rand, b); err != nil {
		return "", "", fmt.Errorf("read random bytes for secret: %w", err)
	}
	plaintext = credentialEncoding.EncodeToString(b)

	return plaintext, HA1(username, realm, plaintext), nil
}

// HA1 computes the SIP digest credential MD5(username:realm:secret) as
// lowercase hex. Exported so the projector and the tests compute it the
// same way, rather than each reimplementing the concatenation order —
// getting that order wrong yields a digest that fails only at
// registration time, far from the code that produced it.
func HA1(username, realm, secret string) string {
	sum := md5.Sum([]byte(username + ":" + realm + ":" + secret)) //nolint:gosec // See above: mandated by the SIP digest scheme, not chosen.
	return hex.EncodeToString(sum[:])
}
