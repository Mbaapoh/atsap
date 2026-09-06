// Package domain defines the identifier types shared across every
// AtsaPBX bounded context: TenantID, CallID, and ParticipantID.
package domain

import (
	"fmt"

	"github.com/google/uuid"
)

// TenantID identifies a tenant. The zero value is invalid.
type TenantID uuid.UUID

// NewTenantID generates a new random TenantID.
func NewTenantID() TenantID {
	return TenantID(uuid.New())
}

// ParseTenantID parses s as a TenantID.
func ParseTenantID(s string) (TenantID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return TenantID{}, fmt.Errorf("parse tenant id: %w", err)
	}
	return TenantID(id), nil
}

// String returns the canonical UUID string form.
func (id TenantID) String() string {
	return uuid.UUID(id).String()
}

// IsZero reports whether id is the zero value.
func (id TenantID) IsZero() bool {
	return id == TenantID{}
}

// CallID identifies a Call aggregate. The zero value is invalid.
type CallID uuid.UUID

// NewCallID generates a new random CallID.
func NewCallID() CallID {
	return CallID(uuid.New())
}

// ParseCallID parses s as a CallID.
func ParseCallID(s string) (CallID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return CallID{}, fmt.Errorf("parse call id: %w", err)
	}
	return CallID(id), nil
}

// String returns the canonical UUID string form.
func (id CallID) String() string {
	return uuid.UUID(id).String()
}

// IsZero reports whether id is the zero value.
func (id CallID) IsZero() bool {
	return id == CallID{}
}

// ParticipantID identifies a CallParticipant entity. The zero value is
// invalid.
type ParticipantID uuid.UUID

// NewParticipantID generates a new random ParticipantID.
func NewParticipantID() ParticipantID {
	return ParticipantID(uuid.New())
}

// ParseParticipantID parses s as a ParticipantID.
func ParseParticipantID(s string) (ParticipantID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return ParticipantID{}, fmt.Errorf("parse participant id: %w", err)
	}
	return ParticipantID(id), nil
}

// String returns the canonical UUID string form.
func (id ParticipantID) String() string {
	return uuid.UUID(id).String()
}

// IsZero reports whether id is the zero value.
func (id ParticipantID) IsZero() bool {
	return id == ParticipantID{}
}
