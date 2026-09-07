package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/domain"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  error
	}{
		{"7 characters is one short", strings.Repeat("a", 7), domain.ErrPasswordTooShort},
		{"8 characters is the minimum", strings.Repeat("a", 8), nil},
		{"64 characters is the maximum", strings.Repeat("a", 64), nil},
		{"65 characters is one over", strings.Repeat("a", 65), domain.ErrPasswordTooLong},
		{"empty", "", domain.ErrPasswordTooShort},
		// The point of dropping composition rules: a long, memorable,
		// all-lowercase passphrase with spaces is exactly what NIST
		// recommends and must not be refused for lacking a symbol.
		{"lowercase passphrase with spaces", "correct horse battery staple", nil},
		{"no symbol, no digit, no uppercase", "abcdefghij", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidatePassword(tt.password)
			if tt.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// TestValidatePassword_CountsRunesNotBytes: a multi-byte passphrase well
// within the character limit must not be rejected because its UTF-8
// encoding exceeds it.
func TestValidatePassword_CountsRunesNotBytes(t *testing.T) {
	// 30 runes, 90 bytes in UTF-8 — comfortably inside the 64-rune
	// ceiling but over it if bytes were counted.
	passphrase := strings.Repeat("字", 30)
	require.Greater(t, len(passphrase), domain.MaxPasswordLength)

	assert.NoError(t, domain.ValidatePassword(passphrase))
}
