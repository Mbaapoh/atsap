package domain

import (
	"net"
	"strings"
	"time"

	"github.com/google/uuid"

	shareddomain "atsap-api/internal/shared/domain"
)

// redactedPlaceholder replaces a sensitive value's content while leaving
// evidence that the field changed. An audit trail that silently dropped
// the field would lose the fact that a credential was rotated at all.
const redactedPlaceholder = "[REDACTED]"

// sensitiveFieldNames are never recorded in an audit entry's before or
// after state, whatever the caller passes. Matching is case-insensitive
// and substring-based: "password_hash", "PasswordHash", and
// "new_password" all match "password".
//
// Redaction happens here, at construction, rather than at read time —
// a read-time filter is bypassed by the next query path someone adds,
// while a value that was never written cannot leak (LLD-02 §10.3,
// INV-11).
var sensitiveFieldNames = []string{
	"password",
	"secret",
	"token",
	"key_hash",
	"keyhash",
	"apikey",
	"api_key",
	"private",
	"credential",
}

// NewAuditEntry builds an audit record with credential material stripped
// from its before/after state. It is the only supported way to make an
// AuditEntry: constructing the struct literally bypasses redaction.
//
// before and after may be nil (a creation has no before; a deletion has
// no after). The maps passed in are never mutated — a redacted copy is
// taken, so the caller's own data is untouched.
func NewAuditEntry(
	tenantID shareddomain.TenantID,
	actorID uuid.UUID,
	actorType ActorType,
	action, resourceType, resourceID string,
	before, after map[string]any,
	ipAddress net.IP,
	now time.Time,
) AuditEntry {
	return AuditEntry{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    actorType,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		BeforeState:  redactMap(before),
		AfterState:   redactMap(after),
		IPAddress:    ipAddress,
		CreatedAt:    now,
	}
}

// NewSystemAuditEntry records a mutation no person performed — a
// scheduled worker, say. It exists so callers never have to invent an
// actor ID for such a mutation, and so those rows are queryable as a
// class rather than being indistinguishable from a real principal's.
func NewSystemAuditEntry(
	tenantID shareddomain.TenantID,
	action, resourceType, resourceID string,
	before, after map[string]any,
	now time.Time,
) AuditEntry {
	return NewAuditEntry(
		tenantID, SystemActorID, ActorSystem,
		action, resourceType, resourceID,
		before, after, nil, now,
	)
}

// redactMap returns a copy of m with sensitive values replaced. Nested
// maps are walked too: a credential one level down is still a
// credential.
func redactMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if isSensitiveField(k) {
			out[k] = redactedPlaceholder
			continue
		}
		if nested, ok := v.(map[string]any); ok {
			out[k] = redactMap(nested)
			continue
		}
		out[k] = v
	}
	return out
}

// isSensitiveField reports whether a field name indicates credential
// material. Substring matching is deliberate: it errs toward redacting
// a field that turns out to be harmless, which is the cheap mistake.
// The expensive mistake is the reverse.
func isSensitiveField(name string) bool {
	lower := strings.ToLower(name)
	for _, needle := range sensitiveFieldNames {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
