// Package domain defines the identifier types shared across every
// AtsaPBX bounded context: TenantID, CallID, ParticipantID, PrincipalID,
// and ApiKeyID.
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

// PrincipalID identifies a Principal (a user account within a tenant).
// The zero value is invalid.
type PrincipalID uuid.UUID

// NewPrincipalID generates a new random PrincipalID.
func NewPrincipalID() PrincipalID {
	return PrincipalID(uuid.New())
}

// ParsePrincipalID parses s as a PrincipalID.
func ParsePrincipalID(s string) (PrincipalID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return PrincipalID{}, fmt.Errorf("parse principal id: %w", err)
	}
	return PrincipalID(id), nil
}

// String returns the canonical UUID string form.
func (id PrincipalID) String() string {
	return uuid.UUID(id).String()
}

// IsZero reports whether id is the zero value.
func (id PrincipalID) IsZero() bool {
	return id == PrincipalID{}
}

// ApiKeyID identifies an API key record. It never identifies the key
// material itself — the raw key is shown once at creation and stored
// only as a digest.
type ApiKeyID uuid.UUID

// NewApiKeyID generates a new random ApiKeyID.
func NewApiKeyID() ApiKeyID {
	return ApiKeyID(uuid.New())
}

// ParseApiKeyID parses s as an ApiKeyID.
func ParseApiKeyID(s string) (ApiKeyID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return ApiKeyID{}, fmt.Errorf("parse api key id: %w", err)
	}
	return ApiKeyID(id), nil
}

// String returns the canonical UUID string form.
func (id ApiKeyID) String() string {
	return uuid.UUID(id).String()
}

// IsZero reports whether id is the zero value.
func (id ApiKeyID) IsZero() bool {
	return id == ApiKeyID{}
}

// ExtensionID identifies an Extension (a dialable endpoint within a
// tenant). The zero value is invalid.
//
// This is also the source of the identifier the Asterisk ACL projects
// into ps_endpoints/ps_auths/ps_aors ("e_" + hex): those tables are one
// flat namespace shared by every tenant, so the projected identifier
// must derive from something globally unique and never from the
// tenant-local extension number (DECISIONS D-47).
type ExtensionID uuid.UUID

// NewExtensionID generates a new random ExtensionID.
func NewExtensionID() ExtensionID {
	return ExtensionID(uuid.New())
}

// ParseExtensionID parses s as an ExtensionID.
func ParseExtensionID(s string) (ExtensionID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return ExtensionID{}, fmt.Errorf("parse extension id: %w", err)
	}
	return ExtensionID(id), nil
}

// String returns the canonical UUID string form.
func (id ExtensionID) String() string {
	return uuid.UUID(id).String()
}

// IsZero reports whether id is the zero value.
func (id ExtensionID) IsZero() bool {
	return id == ExtensionID{}
}
