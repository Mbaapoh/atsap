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
