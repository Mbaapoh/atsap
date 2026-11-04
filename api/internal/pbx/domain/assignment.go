package domain

import (
	"errors"
	"time"

	shareddomain "atsap-api/internal/shared/domain"
)

// ExtensionAssignment says that one membership owns one extension (D-65).
// An extension has at most one owner; a person may own several. The
// assignment is configuration, audited like any other extension mutation.
type ExtensionAssignment struct {
	ExtensionID shareddomain.ExtensionID
	PrincipalID shareddomain.PrincipalID
	AssignedAt  time.Time
}

// ErrAssignmentInvalid is an assignment missing either side.
var ErrAssignmentInvalid = errors.New("an assignment needs an extension and a principal")

// Validate checks the assignment is complete.
func (a ExtensionAssignment) Validate() error {
	if a.ExtensionID.IsZero() || a.PrincipalID.IsZero() {
		return ErrAssignmentInvalid
	}
	return nil
}
