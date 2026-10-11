package domain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	shareddomain "atsap-api/internal/shared/domain"
)

// InboundNumber is a tenant's DID: a public number the carrier presents
// that rings one of the tenant's extensions. Numbers are stored digits
// only — a leading "+" is not significant and is stripped on the way in
// (design D7), so "+442071234567" and "442071234567" are the same number.
type InboundNumber struct {
	ID          uuid.UUID
	TenantID    shareddomain.TenantID
	Number      string
	ExtensionID shareddomain.ExtensionID
}

// NormalizeInboundNumber validates number like a destination and
// returns its stored form (digits only).
func NormalizeInboundNumber(number string) (string, error) {
	if err := ValidateDestination(number); err != nil {
		return "", err
	}
	return strings.TrimPrefix(number, "+"), nil
}

// NormalizeDialled reduces a dialled string for DID comparison: a
// leading "+" ignored, matching storage.
func NormalizeDialled(dialled string) string {
	return strings.TrimPrefix(strings.TrimSpace(dialled), "+")
}

// ValidateCountryCode accepts an ISO 3166-1 alpha-2 code (upper case)
// or the empty string (no country set).
func ValidateCountryCode(code string) error {
	if code == "" {
		return nil
	}
	if len(code) != 2 {
		return fmt.Errorf("%w: %q", ErrCountryCodeInvalid, code)
	}
	for i := 0; i < 2; i++ {
		if code[i] < 'A' || code[i] > 'Z' {
			return fmt.Errorf("%w: %q", ErrCountryCodeInvalid, code)
		}
	}
	return nil
}

// ErrCountryCodeInvalid is a country code that is not ISO 3166-1
// alpha-2 upper case.
var ErrCountryCodeInvalid = errors.New("country code must be ISO 3166-1 alpha-2 upper case")
