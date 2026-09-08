// Package domain holds pbx-core's pure domain types and rules:
// extensions, their validation, and the credentials they authenticate
// with. It performs no I/O, holds no engine vocabulary, and imports
// nothing outside the standard library and the shared identifier types
// (docs/hld/01-architecture.md §1.2).
package domain

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	shareddomain "atsap-api/internal/shared/domain"
)

// DeviceType is how a device attaches to an extension.
type DeviceType string

const (
	// DeviceWebRTC is a browser softphone.
	DeviceWebRTC DeviceType = "WEBRTC"
	// DeviceSIP is a desk phone or standalone SIP client.
	DeviceSIP DeviceType = "SIP"
)

// RegistrationStatus is what the console shows as a badge. It describes
// the DEVICE, not the configuration: an extension is configured and live
// from the moment it is created, whether or not anything has registered
// to it (D-47 — there is no apply step to be pending on).
type RegistrationStatus string

const (
	// StatusNotRegistered means no device currently holds a registration.
	StatusNotRegistered RegistrationStatus = "NOT_REGISTERED"
	// StatusRegistered means a device holds a current registration.
	StatusRegistered RegistrationStatus = "REGISTERED"
)

// Extension number bounds. The floor rejects a single digit, which would
// collide with feature codes; the ceiling matches the column width.
const (
	MinExtensionNumberLength = 2
	MaxExtensionNumberLength = 20
	MaxDisplayNameLength     = 100
)

// Validation errors, distinct so a caller can tell which rule was missed
// without matching on message text.
var (
	ErrExtensionNumberEmpty       = errors.New("extension number is required")
	ErrExtensionNumberTooShort    = errors.New("extension number is shorter than the minimum length")
	ErrExtensionNumberTooLong     = errors.New("extension number is longer than the maximum length")
	ErrExtensionNumberNotDialable = errors.New("extension number contains a character that cannot be dialled")
	ErrDisplayNameEmpty           = errors.New("display name is required")
	ErrDisplayNameTooLong         = errors.New("display name is longer than the maximum length")
	ErrDeviceTypeUnknown          = errors.New("device type is not one of the supported values")
)

// Extension is a dialable endpoint within one tenant.
//
// Number is tenant-local: two tenants may each have a 1000, and they are
// unrelated extensions with unrelated credentials. AuthUsername is
// generated and deliberately unrelated to Number, so knowing how a
// tenant numbers its extensions tells an attacker nothing about what to
// present. SecretDigest is the MD5 HA1, never the plaintext and never a
// slow hash — see credential.go for why that is forced on us.
type Extension struct {
	ID       shareddomain.ExtensionID
	TenantID shareddomain.TenantID
	Number   string
	// DisplayName is what a user sees; the extensions.department_id
	// column exists in the schema but has no field here, because
	// departments are Phase B and a field nothing can set is a field
	// that lies about what this change supports.
	DisplayName  string
	AuthUsername string
	SecretDigest string
	DeviceType   DeviceType
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ValidateNumber reports whether a caller-supplied extension number may
// be stored.
//
// The permitted set is an explicit allowlist rather than a rejection
// list, because this string ends up in engine configuration: anything
// not proven dialable is refused. Digits only — no wildcards, no
// punctuation, no expression syntax to abuse (LLD-03 §10.5).
func ValidateNumber(number string) error {
	if number == "" {
		return ErrExtensionNumberEmpty
	}
	for _, r := range number {
		if r < '0' || r > '9' {
			return fmt.Errorf("%w: %q", ErrExtensionNumberNotDialable, r)
		}
	}
	// Digits are single-byte, so len is the digit count here; using it
	// rather than RuneCount makes the column-width bound exact.
	switch n := len(number); {
	case n < MinExtensionNumberLength:
		return fmt.Errorf("%w: %d < %d", ErrExtensionNumberTooShort, n, MinExtensionNumberLength)
	case n > MaxExtensionNumberLength:
		return fmt.Errorf("%w: %d > %d", ErrExtensionNumberTooLong, n, MaxExtensionNumberLength)
	}
	return nil
}

// ValidateDisplayName reports whether a display name may be stored.
// Counted in runes: a name in a non-Latin script must not be rejected
// for being "too long" because its UTF-8 encoding is larger.
func ValidateDisplayName(name string) error {
	if name == "" {
		return ErrDisplayNameEmpty
	}
	if n := utf8.RuneCountInString(name); n > MaxDisplayNameLength {
		return fmt.Errorf("%w: %d > %d", ErrDisplayNameTooLong, n, MaxDisplayNameLength)
	}
	return nil
}

// ValidateDeviceType reports whether a device type is one this platform
// supports. Unknown values are refused rather than defaulted: silently
// turning a typo into WEBRTC would hand a desk-phone user a broken
// extension with no error to explain it.
func ValidateDeviceType(d DeviceType) error {
	switch d {
	case DeviceWebRTC, DeviceSIP:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrDeviceTypeUnknown, string(d))
	}
}

// Validate checks every caller-supplied field of an extension. It does
// not check the generated fields (AuthUsername, SecretDigest): those are
// produced by NewExtensionCredential, never supplied.
func (e Extension) Validate() error {
	if err := ValidateNumber(e.Number); err != nil {
		return err
	}
	if err := ValidateDisplayName(e.DisplayName); err != nil {
		return err
	}
	return ValidateDeviceType(e.DeviceType)
}
