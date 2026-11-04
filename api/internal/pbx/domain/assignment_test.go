package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"atsap-api/internal/pbx/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

func TestExtensionAssignmentValidation(t *testing.T) {
	ok := domain.ExtensionAssignment{ExtensionID: shareddomain.NewExtensionID(), PrincipalID: shareddomain.NewPrincipalID()}
	assert.NoError(t, ok.Validate())
	assert.ErrorIs(t, domain.ExtensionAssignment{PrincipalID: ok.PrincipalID}.Validate(), domain.ErrAssignmentInvalid)
	assert.ErrorIs(t, domain.ExtensionAssignment{ExtensionID: ok.ExtensionID}.Validate(), domain.ErrAssignmentInvalid)
}
