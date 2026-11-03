package asterisk

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"atsap-api/internal/pbx/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

// Unit coverage for the pure parts of the projection: the engine values
// derived from a device type and the tenant variable. The database
// projection is covered by the integration tier.

func TestMediaFor(t *testing.T) {
	browser := mediaFor(domain.DeviceWebRTC)
	assert.Equal(t, "ulaw,alaw", browser.allow)
	assert.Equal(t, "yes", browser.webrtc)
	assert.Equal(t, "dtls", browser.mediaEncryption)
	assert.Equal(t, "yes", browser.dtlsAutoGenerate)
	assert.Equal(t, "yes", browser.ice)
	assert.Equal(t, "yes", browser.avpf)
	assert.Equal(t, "yes", browser.rtcpMux)

	phone := mediaFor(domain.DeviceSIP)
	assert.Equal(t, "ulaw,alaw", phone.allow)
	assert.Nil(t, phone.webrtc)
	assert.Nil(t, phone.mediaEncryption)
}

func TestTransportFor(t *testing.T) {
	p := &Projector{cfg: Config{SIPTransport: "transport-udp", WebRTCTransport: "transport-ws"}}
	assert.Equal(t, "transport-ws", p.transportFor(domain.DeviceWebRTC))
	assert.Equal(t, "transport-udp", p.transportFor(domain.DeviceSIP))
}

func TestSetVarFor(t *testing.T) {
	id := shareddomain.NewTenantID()
	assert.Equal(t, "ATSA_TENANT_ID="+id.String(), setVarFor(id))
}

func TestContactFor(t *testing.T) {
	assert.Equal(t, "sip:sip.example.test:5060", contactFor(domain.Trunk{Host: "sip.example.test", Port: 5060}))
	assert.Equal(t, "sip:[2001:db8::1]:5061", contactFor(domain.Trunk{Host: "2001:db8::1", Port: 5061}))
	assert.True(t, isIPv6("2001:db8::1"))
	assert.False(t, isIPv6("sip.example.test"))
}
