// Package nats implements postgres.OutboxPublisher against NATS
// JetStream: the outbox worker's only concrete way of actually getting
// an event onto the wire (docs/hld/01-architecture.md §3.2).
package nats

import (
	"context"
	"fmt"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	corepostgres "atsap-api/internal/postgres"
)

// StreamName is the durable JetStream stream this publisher ensures
// exists, covering every tenant's events.
const StreamName = "ATSAPBX_EVENTS"

// subjectFilter is the stream's subject filter and the pattern every
// published subject matches: tenant.<tenant_id>.event.<event_type>
// (docs/hld/01-architecture.md §3.2), e.g.
// "tenant.<uuid>.event.call.initiated".
const subjectFilter = "tenant.*.event.>"

// dedupeWindow is how long JetStream remembers a message id in order to
// discard a republish of it.
//
// Delivery is at-least-once by construction: OutboxWorker publishes and
// only then marks the row published, inside one transaction, so a crash
// between the two republishes the event on the next drain. That ordering
// is correct — reversing it would lose events instead — and the cost is
// duplicates, which this window absorbs.
//
// Five minutes covers a crash-and-restart comfortably while keeping the
// server-side dedupe table small. It does NOT make delivery
// exactly-once: a republish after the window still arrives, so consumers
// remain responsible for idempotency (D-58). This narrows the common
// case; it does not remove the requirement.
const dedupeWindow = 5 * time.Minute

// Publisher implements corepostgres.OutboxPublisher.
type Publisher struct {
	js jetstream.JetStream
}

var _ corepostgres.OutboxPublisher = (*Publisher)(nil)

// NewPublisher connects a JetStream context over nc and ensures
// StreamName exists (creating it if this is a fresh NATS instance).
func NewPublisher(ctx context.Context, nc *natsgo.Conn) (*Publisher, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("nats: jetstream context: %w", err)
	}

	if _, err := js.Stream(ctx, StreamName); err != nil {
		_, err = js.CreateStream(ctx, jetstream.StreamConfig{
			Name:       StreamName,
			Subjects:   []string{subjectFilter},
			Duplicates: dedupeWindow,
		})
		if err != nil {
			return nil, fmt.Errorf("nats: create stream %s: %w", StreamName, err)
		}
	}

	return &Publisher{js: js}, nil
}

// Publish sends ev to tenant.<tenant_id>.event.<event_type>, carrying the
// outbox row id as the JetStream message id so a republish of the same
// row inside dedupeWindow is discarded by the server rather than
// delivered twice.
//
// The outbox row id is the right key precisely because it is stable
// across republishes: the row is rewritten only to set published_at, and
// a redelivery is by definition the same row being drained again.
func (p *Publisher) Publish(ctx context.Context, ev corepostgres.OutboxEvent) error {
	subject := fmt.Sprintf("tenant.%s.event.%s", ev.TenantID, ev.EventType)
	if _, err := p.js.Publish(ctx, subject, ev.Payload, jetstream.WithMsgID(ev.ID)); err != nil {
		return fmt.Errorf("nats: publish to %s: %w", subject, err)
	}
	return nil
}
