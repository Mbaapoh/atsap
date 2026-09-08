package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/domain"
)

func TestValidateNumber(t *testing.T) {
	tests := []struct {
		name    string
		number  string
		wantErr error
	}{
		{name: "typical four digit", number: "1000"},
		{name: "minimum length", number: "10"},
		{name: "maximum length", number: strings.Repeat("9", 20)},
		{name: "leading zero is kept", number: "0100"},

		{name: "empty", number: "", wantErr: domain.ErrExtensionNumberEmpty},
		{name: "single digit", number: "1", wantErr: domain.ErrExtensionNumberTooShort},
		{name: "over length", number: strings.Repeat("9", 21), wantErr: domain.ErrExtensionNumberTooLong},

		// Everything below ends up in engine configuration, so the rule is
		// an allowlist: anything not proven dialable is refused.
		{name: "letters", number: "10a0", wantErr: domain.ErrExtensionNumberNotDialable},
		{name: "wildcard", number: "10*", wantErr: domain.ErrExtensionNumberNotDialable},
		{name: "regex metacharacters", number: "1.*0", wantErr: domain.ErrExtensionNumberNotDialable},
		{name: "sql punctuation", number: "10';--", wantErr: domain.ErrExtensionNumberNotDialable},
		{name: "whitespace", number: "10 0", wantErr: domain.ErrExtensionNumberNotDialable},
		{name: "newline", number: "100\n", wantErr: domain.ErrExtensionNumberNotDialable},
		{name: "config separator", number: "100=x", wantErr: domain.ErrExtensionNumberNotDialable},
		{name: "non-ascii digit", number: "１０００", wantErr: domain.ErrExtensionNumberNotDialable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidateNumber(tt.number)
			if tt.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestValidateDisplayName(t *testing.T) {
	assert.NoError(t, domain.ValidateDisplayName("Reception"))
	assert.NoError(t, domain.ValidateDisplayName(strings.Repeat("a", 100)))

	require.ErrorIs(t, domain.ValidateDisplayName(""), domain.ErrDisplayNameEmpty)
	require.ErrorIs(t, domain.ValidateDisplayName(strings.Repeat("a", 101)), domain.ErrDisplayNameTooLong)

	// Counted in runes, not bytes: a 100-character name in a non-Latin
	// script must not be rejected for its UTF-8 size.
	assert.NoError(t, domain.ValidateDisplayName(strings.Repeat("é", 100)))
	require.ErrorIs(t, domain.ValidateDisplayName(strings.Repeat("é", 101)), domain.ErrDisplayNameTooLong)
}

func TestValidateDeviceType(t *testing.T) {
	assert.NoError(t, domain.ValidateDeviceType(domain.DeviceWebRTC))
	assert.NoError(t, domain.ValidateDeviceType(domain.DeviceSIP))

	// An unknown value must fail rather than silently default: turning a
	// typo into WEBRTC hands a desk-phone user a broken extension with no
	// error to explain it.
	require.ErrorIs(t, domain.ValidateDeviceType(""), domain.ErrDeviceTypeUnknown)
	require.ErrorIs(t, domain.ValidateDeviceType("webrtc"), domain.ErrDeviceTypeUnknown)
	require.ErrorIs(t, domain.ValidateDeviceType("PJSIP"), domain.ErrDeviceTypeUnknown)
}

func TestExtension_Validate(t *testing.T) {
	valid := domain.Extension{
		Number:      "1000",
		DisplayName: "Reception",
		DeviceType:  domain.DeviceWebRTC,
	}
	assert.NoError(t, valid.Validate())

	badNumber := valid
	badNumber.Number = "abc"
	require.ErrorIs(t, badNumber.Validate(), domain.ErrExtensionNumberNotDialable)

	badName := valid
	badName.DisplayName = ""
	require.ErrorIs(t, badName.Validate(), domain.ErrDisplayNameEmpty)

	badDevice := valid
	badDevice.DeviceType = "FAX"
	require.ErrorIs(t, badDevice.Validate(), domain.ErrDeviceTypeUnknown)
}
