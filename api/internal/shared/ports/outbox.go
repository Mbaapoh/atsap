// Package ports holds cross-cutting outbound contracts that belong to no
// single bounded context — the interfaces infrastructure adapters
// implement and that application code depends on instead of a concrete
// broker or driver.
//
// A bounded context's own ports stay with that context
// (internal/telephony/ports, internal/pbx/ports, and so on). Only
// contracts serving every context alike live here.
package ports

import "context"

// OutboxEvent is one row read from the outbox table, ready to publish.
// Already serialized: the worker reads raw rows from Postgres, never a
// live domain event Go value — only what was persisted as JSON
// (CallStore.SaveCall, or any future bounded context's equivalent).
type OutboxEvent struct {
	ID          string
	TenantID    string
	EventType   string
	AggregateID string
	Payload     []byte
}

// OutboxPublisher is what the outbox worker needs to push one event to
// the message bus, and the seam that keeps the broker replaceable
// (D-57).
//
// It lives here rather than in internal/postgres, where it was first
// written, because it is a messaging contract rather than a persistence
// one: the worker happens to read from Postgres and publish to NATS, but
// neither side belongs to the other. Substituting Kafka, RabbitMQ or
// Redis Streams is then a bounded change — one adapter package plus one
// line in the composition root — because the durability decision was
// made by the outbox pattern, not by the broker.
//
// Delivery is at-least-once by construction (D-58): OutboxWorker
// publishes and only then marks the row published, so a crash between
// the two republishes the event. Implementations should carry
// OutboxEvent.ID as a broker-level message id where the broker can
// deduplicate on it, and consumers must be idempotent regardless.
type OutboxPublisher interface {
	Publish(ctx context.Context, ev OutboxEvent) error
}
