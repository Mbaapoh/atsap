package asterisk_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"atsap-api/internal/pbx/acl/asterisk"
	"atsap-api/internal/pbx/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

func TestAddresser(t *testing.T) {
	ext := shareddomain.NewExtensionID()
	trunk := shareddomain.NewTrunkID()
	a := asterisk.Addresser{}

	assert.Equal(t, "PJSIP/"+domain.EndpointIdentifier(ext), a.ExtensionAddress(ext))
	assert.Equal(t, "PJSIP/442071234567@"+domain.TrunkEndpointIdentifier(trunk), a.TrunkAddress(trunk, "+442071234567"),
		"the + is dropped and the call goes via the trunk's endpoint")
	assert.Equal(t, "PJSIP/112@"+domain.TrunkEndpointIdentifier(trunk), a.TrunkAddress(trunk, "112"))
}

func TestAddresser_Identify(t *testing.T) {
	ext := shareddomain.NewExtensionID()
	trunk := shareddomain.NewTrunkID()
	a := asterisk.Addresser{}

	got, ok := a.Identify(domain.EndpointIdentifier(ext))
	assert.True(t, ok, "a projected extension round-trips")
	assert.False(t, got.IsTrunk)
	assert.Equal(t, ext, got.ExtensionID)

	got, ok = a.Identify(domain.TrunkEndpointIdentifier(trunk))
	assert.True(t, ok, "a projected trunk round-trips")
	assert.True(t, got.IsTrunk)
	assert.Equal(t, trunk, got.TrunkID)

	for _, unknown := range []string{"", "1000", "1001", "e_nothex", "t_", "sip-provider", "PJSIP/e_abc"} {
		_, ok := a.Identify(unknown)
		assert.False(t, ok, "anything not projected is refused: %q", unknown)
	}
}
