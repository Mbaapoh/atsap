package rpc

import (
	"testing"

	"github.com/stretchr/testify/assert"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/pbx/domain"
)

func TestOutboundClassRoundTrip(t *testing.T) {
	cases := []struct {
		wire   atsapbxv1.OutboundClass
		domain domain.OutboundClass
	}{
		{atsapbxv1.OutboundClass_OUTBOUND_CLASS_UNSPECIFIED, ""},
		{atsapbxv1.OutboundClass_OUTBOUND_CLASS_INTERNAL, domain.OutboundInternal},
		{atsapbxv1.OutboundClass_OUTBOUND_CLASS_NATIONAL, domain.OutboundNational},
		{atsapbxv1.OutboundClass_OUTBOUND_CLASS_INTERNATIONAL, domain.OutboundInternational},
	}
	for _, c := range cases {
		assert.Equal(t, c.domain, outboundFromProto(c.wire))
		if c.domain == "" {
			assert.Equal(t, atsapbxv1.OutboundClass_OUTBOUND_CLASS_UNSPECIFIED, outboundToProto(c.domain))
			continue
		}
		assert.Equal(t, c.wire, outboundToProto(c.domain))
	}
}
