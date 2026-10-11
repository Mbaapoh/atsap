package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	licensingdomain "atsap-api/internal/licensing/domain"
)

// A generated token verifies against the generated key through the same
// code ApplyLicenseKey uses, and carries the dev entitlement.
func TestGenerate_TokenVerifiesWithItsKey(t *testing.T) {
	lic, err := Generate(t.TempDir())
	require.NoError(t, err)

	pub, err := hex.DecodeString(lic.PublicKeyHex)
	require.NoError(t, err)
	payload, sig, err := licensingdomain.SplitToken(lic.Token)
	require.NoError(t, err)

	tok, err := licensingdomain.VerifyToken(payload, sig, licensingdomain.NewKeySet(ed25519.PublicKey(pub)))
	require.NoError(t, err)
	assert.Equal(t, 16, tok.Capacity)
	assert.Zero(t, tok.MaxTenants, "zero caps: provisioning stays unlimited on the dev rig")
	assert.Zero(t, tok.MaxExtensions)
	_, err = uuid.Parse(tok.InstanceID)
	assert.NoError(t, err, "licensing_state.instance_id is a UUID column; a non-UUID fails at apply, not here")
}

// The key is reused, so re-issuing a token never invalidates the image.
func TestGenerate_ReusesTheKeypair(t *testing.T) {
	dir := t.TempDir()
	first, err := Generate(dir)
	require.NoError(t, err)
	second, err := Generate(dir)
	require.NoError(t, err)
	assert.Equal(t, first.PublicKeyHex, second.PublicKeyHex)
}

// The dev token is rejected by a default build, which trusts only the
// vendor key — the property that makes shipping this tool safe.
func TestGenerate_DefaultBuildRejectsTheDevToken(t *testing.T) {
	lic, err := Generate(t.TempDir())
	require.NoError(t, err)
	payload, sig, err := licensingdomain.SplitToken(lic.Token)
	require.NoError(t, err)
	_, err = licensingdomain.VerifyToken(payload, sig, licensingdomain.TrustedKeys())
	assert.ErrorIs(t, err, licensingdomain.ErrUntrusted)
}
