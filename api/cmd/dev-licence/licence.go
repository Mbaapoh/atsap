package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	licensingdomain "atsap-api/internal/licensing/domain"
)

// devEntitlement is generous on purpose: the dev rig exists to exercise
// call paths, and caps of zero leave tenant and extension provisioning
// unlimited so no existing test meets a limit it was not written for.
var devEntitlement = licensingdomain.LicenseToken{
	Edition:    "development",
	Capacity:   16,
	InstanceID: "00000000-0000-0000-0000-0000000000de", // must be a UUID: licensing_state.instance_id is one
}

// DevLicence is what the dev image and compose stack need.
type DevLicence struct {
	PublicKeyHex string
	Token        string
}

const seedFile = "ed25519.seed"

// Generate returns the development public key and a token it signed,
// creating the keypair under dir on first use and reusing it afterwards,
// so the image does not need rebuilding every time a token is re-issued.
func Generate(dir string) (DevLicence, error) {
	priv, err := loadOrCreateKey(dir)
	if err != nil {
		return DevLicence{}, err
	}
	payload, err := json.Marshal(devEntitlement)
	if err != nil {
		return DevLicence{}, fmt.Errorf("encode entitlement: %w", err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	return DevLicence{
		PublicKeyHex: hex.EncodeToString(pub),
		Token:        licensingdomain.EncodeToken(payload, ed25519.Sign(priv, payload)),
	}, nil
}

func loadOrCreateKey(dir string) (ed25519.PrivateKey, error) {
	path := filepath.Join(dir, seedFile)
	raw, err := os.ReadFile(path) // #nosec G304 -- operator-chosen local dev path
	switch {
	case err == nil:
		seed, err := hex.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("%s is not a hex Ed25519 seed; delete it to regenerate", path)
		}
		return ed25519.NewKeyFromSeed(seed), nil
	case errors.Is(err, os.ErrNotExist):
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			return nil, fmt.Errorf("generate key: %w", err)
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create %s: %w", dir, err)
		}
		if err := os.WriteFile(path, []byte(hex.EncodeToString(priv.Seed())+"\n"), 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
		return priv, nil
	default:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
}
