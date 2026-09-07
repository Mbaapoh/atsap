// Package domain implements the identity bounded context's aggregates:
// Tenant, Principal, RoleBinding, ApiKey, and AuditEntry, plus the pure
// policy functions that govern them. Zero I/O, and — being Tier 0 (HLD
// 04-bounded-contexts.md §10.1) — zero dependency on any other bounded
// context.
package domain

import (
	"net"
	"time"

	"github.com/google/uuid"

	shareddomain "atsap-api/internal/shared/domain"
)

// TenantStatus is whether a tenant may transact. Suspension blocks new
// authenticated access without destroying anything and is fully
// reversible (AC-01.7); it never terminates a call already in progress
// (INV-03) — that guarantee lives with the caller, not here.
type TenantStatus string

const (
	TenantActive    TenantStatus = "ACTIVE"
	TenantSuspended TenantStatus = "SUSPENDED"
)

// Valid reports whether s is a recognised status.
func (s TenantStatus) Valid() bool {
	return s == TenantActive || s == TenantSuspended
}

// PrincipalStatus is whether an individual account may authenticate.
type PrincipalStatus string

const (
	PrincipalActive   PrincipalStatus = "ACTIVE"
	PrincipalDisabled PrincipalStatus = "DISABLED"
)

// Valid reports whether s is a recognised status.
func (s PrincipalStatus) Valid() bool {
	return s == PrincipalActive || s == PrincipalDisabled
}

// ActorType distinguishes who performed an audited action. A mutation
// made by a scheduled worker has no principal behind it, so AuditEntry
// records SystemActorID with ActorSystem rather than inventing one.
type ActorType string

const (
	ActorPrincipal ActorType = "principal"
	ActorAPIKey    ActorType = "api_key"
	ActorSystem    ActorType = "system"
)

// Valid reports whether a is a recognised actor type.
func (a ActorType) Valid() bool {
	return a == ActorPrincipal || a == ActorAPIKey || a == ActorSystem
}

// SystemActorID is the well-known sentinel recorded as an audit entry's
// actor when the mutation was system-initiated. audit_logs.actor_id is
// NOT NULL by design (HLD 03-domain-model.md §5) — a nullable actor
// invites "unknown who did this" rows, so system actions are named
// explicitly instead.
var SystemActorID = uuid.UUID{}

// Tenant is one partner organisation: the root of tenancy that every
// other record hangs from.
type Tenant struct {
	ID     shareddomain.TenantID
	Name   string
	Status TenantStatus
	// ResidencyZone is fixed at provisioning and never changes
	// afterwards. Storage routing against it is a later LLD's job; this
	// context guarantees only that the value exists and cannot be
	// blanked (LLD-02 §10.6).
	ResidencyZone string
	CreatedAt     time.Time
}

// Principal is one user account, meaningful only within its tenant.
type Principal struct {
	ID       shareddomain.PrincipalID
	TenantID shareddomain.TenantID
	// Username is unique per tenant, never globally: two tenants may
	// each have an "admin" and they are unrelated accounts (AC-01.2).
	Username string
	Email    string
	// PasswordHash is an encoded Argon2id hash. No plaintext password
	// is held anywhere in this struct's lifetime (INV-11).
	PasswordHash string
	Role         string
	Status       PrincipalStatus
	CreatedAt    time.Time
}

// RoleBinding grants one role to one principal within one scope.
// Authorization is the sum of a principal's bindings and nothing else:
// absence of a binding is denial (LLD-02 §10.1).
type RoleBinding struct {
	PrincipalID shareddomain.PrincipalID
	TenantID    shareddomain.TenantID
	Role        string
	// Scope is "tenant" for tenant-wide authority, or a narrower form
	// such as "department:<id>".
	Scope     string
	CreatedAt time.Time
}

// ApiKey is a machine credential. The raw key exists only in the
// response that creates it; this record holds a digest and nothing from
// which the key can be recovered.
type ApiKey struct {
	ID          shareddomain.ApiKeyID
	TenantID    shareddomain.TenantID
	PrincipalID shareddomain.PrincipalID
	KeyHash     string
	ExpiresAt   *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
}

// Usable reports whether the key may authenticate at time now — neither
// revoked nor past its expiry.
func (k ApiKey) Usable(now time.Time) bool {
	if k.RevokedAt != nil && !now.Before(*k.RevokedAt) {
		return false
	}
	if k.ExpiresAt != nil && !now.Before(*k.ExpiresAt) {
		return false
	}
	return true
}

// AuditEntry is one immutable record of an administrative mutation.
// Construct it with NewAuditEntry, never as a literal: the constructor
// is what strips credential material, and a literal would bypass that
// (LLD-02 §10.3).
type AuditEntry struct {
	ID           int64
	TenantID     shareddomain.TenantID
	ActorID      uuid.UUID
	ActorType    ActorType
	Action       string
	ResourceType string
	ResourceID   string
	BeforeState  map[string]any
	AfterState   map[string]any
	IPAddress    net.IP
	CreatedAt    time.Time
}
