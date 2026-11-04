package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"atsap-api/internal/pbx/domain"
)

func TestOutboundClassRanksAndAllows(t *testing.T) {
	assert.True(t, domain.OutboundInternal.Allows(domain.OutboundInternal))
	assert.False(t, domain.OutboundInternal.Allows(domain.OutboundNational))

	assert.True(t, domain.OutboundNational.Allows(domain.OutboundInternal))
	assert.True(t, domain.OutboundNational.Allows(domain.OutboundNational))
	assert.False(t, domain.OutboundNational.Allows(domain.OutboundInternational))

	assert.True(t, domain.OutboundInternational.Allows(domain.OutboundInternal))
	assert.True(t, domain.OutboundInternational.Allows(domain.OutboundNational))
	assert.True(t, domain.OutboundInternational.Allows(domain.OutboundInternational))

	assert.True(t, domain.OutboundClass("").Allows(domain.OutboundNational), "an unset class is the safe NATIONAL default")
	assert.False(t, domain.OutboundClass("GLOBAL").Allows(domain.OutboundInternal), "an unknown class allows nothing")
}

func TestClassifyDialledDestination(t *testing.T) {
	assert.Equal(t, domain.OutboundInternational, domain.ClassifyDialledDestination("+442071234567"))
	assert.Equal(t, domain.OutboundNational, domain.ClassifyDialledDestination("442071234567"))
	assert.Equal(t, domain.OutboundNational, domain.ClassifyDialledDestination("1005"))
}

func TestValidateOutboundClass(t *testing.T) {
	for _, c := range []domain.OutboundClass{domain.OutboundInternal, domain.OutboundNational, domain.OutboundInternational} {
		assert.NoError(t, domain.ValidateOutboundClass(c), c)
	}
	assert.ErrorIs(t, domain.ValidateOutboundClass(""), domain.ErrOutboundClassUnknown)
	assert.ErrorIs(t, domain.ValidateOutboundClass("GLOBAL"), domain.ErrOutboundClassUnknown)
}
