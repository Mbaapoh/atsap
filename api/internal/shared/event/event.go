// Package event defines the DomainEvent contract every bounded context
// publishes through the transactional outbox (see internal/postgres and
// internal/nats).
package event

import "time"

// DomainEvent is implemented by every fact a bounded context publishes
// after a state change (e.g. CallActive, ParticipantJoined).
type DomainEvent interface {
	// EventType returns the dotted event name used as the outbox row's
	// event_type and, combined with the tenant, the NATS subject
	// (e.g. "call.active", "participant.joined").
	EventType() string
}

// Envelope wraps a DomainEvent with the metadata every outbox row and
// NATS message carries, regardless of the event's own payload shape.
type Envelope struct {
	Type        string
	TenantID    string
	AggregateID string
	OccurredAt  time.Time
	Payload     DomainEvent
}

// NewEnvelope builds an Envelope from a DomainEvent, deriving Type from
// the event itself so callers cannot let the two drift apart.
func NewEnvelope(tenantID, aggregateID string, occurredAt time.Time, payload DomainEvent) Envelope {
	return Envelope{
		Type:        payload.EventType(),
		TenantID:    tenantID,
		AggregateID: aggregateID,
		OccurredAt:  occurredAt,
		Payload:     payload,
	}
}
