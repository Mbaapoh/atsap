//go:build integration

package nats_test

import (
	"context"
	"os"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atsapnats "atsap-api/internal/nats"
	corepostgres "atsap-api/internal/postgres"
)

func natsURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("NATS_URL"); v != "" {
		return v
	}
	return "nats://localhost:4222"
}

// TestPublisher_PublishesToTenantSubject proves task 7.2's NATS side:
// an OutboxEvent is published to tenant.<tenant_id>.event.<event_type>
// and is actually retrievable from the stream, not just "no error
// returned" from Publish.
func TestPublisher_PublishesToTenantSubject(t *testing.T) {
	nc, err := natsgo.Connect(natsURL(t))
	require.NoError(t, err)
	defer nc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	js, err := jetstream.New(nc)
	require.NoError(t, err)
	// Start clean: this stream is shared across test runs against the
	// same dev NATS instance.
	_ = js.DeleteStream(ctx, atsapnats.StreamName)

	publisher, err := atsapnats.NewPublisher(ctx, nc)
	require.NoError(t, err)

	tenantID := "tenant-1"
	ev := corepostgres.OutboxEvent{
		ID: "outbox-1", TenantID: tenantID, EventType: "call.initiated",
		AggregateID: "call-1", Payload: []byte(`{"call_id":"call-1"}`),
	}
	require.NoError(t, publisher.Publish(ctx, ev))

	// Consume it back from the stream to prove real delivery.
	consumer, err := js.CreateOrUpdateConsumer(ctx, atsapnats.StreamName, jetstream.ConsumerConfig{
		FilterSubject: "tenant." + tenantID + ".event.call.initiated",
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)

	msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
	require.NoError(t, err)

	var received jetstream.Msg
	for msg := range msgs.Messages() {
		received = msg
		_ = msg.Ack()
	}
	require.NoError(t, msgs.Error())
	require.NotNil(t, received, "the published event must be retrievable from the stream")
	assert.Equal(t, "tenant.tenant-1.event.call.initiated", received.Subject())
	assert.JSONEq(t, `{"call_id":"call-1"}`, string(received.Data()))
}

// TestPublisher_RepublishOfTheSameRowIsDeduped covers the delivery
// semantics the outbox actually has (D-58): OutboxWorker publishes and
// only then marks the row published, so a crash between the two
// republishes the identical row on the next drain.
//
// Publishing the same OutboxEvent twice must therefore leave one message
// in the stream, not two — the outbox row id is sent as the JetStream
// message id and the server discards the repeat inside the dedupe
// window. Without WithMsgID this test fetches two messages and fails,
// which is the point of asserting the count rather than the content.
//
// This narrows the common case; it does not make delivery exactly-once.
// A republish after the window still arrives, so consumers stay
// responsible for idempotency.
func TestPublisher_RepublishOfTheSameRowIsDeduped(t *testing.T) {
	nc, err := natsgo.Connect(natsURL(t))
	require.NoError(t, err)
	defer nc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	js, err := jetstream.New(nc)
	require.NoError(t, err)
	_ = js.DeleteStream(ctx, atsapnats.StreamName)

	publisher, err := atsapnats.NewPublisher(ctx, nc)
	require.NoError(t, err)

	tenantID := "tenant-dedupe"
	ev := corepostgres.OutboxEvent{
		ID: "outbox-redelivered", TenantID: tenantID, EventType: "call.initiated",
		AggregateID: "call-9", Payload: []byte(`{"call_id":"call-9"}`),
	}

	// The same row drained twice — exactly what a crash between publish
	// and commit produces.
	require.NoError(t, publisher.Publish(ctx, ev))
	require.NoError(t, publisher.Publish(ctx, ev))

	consumer, err := js.CreateOrUpdateConsumer(ctx, atsapnats.StreamName, jetstream.ConsumerConfig{
		FilterSubject: "tenant." + tenantID + ".event.call.initiated",
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)

	// Ask for more than one so a duplicate would be returned and counted
	// rather than silently left behind by a Fetch(1).
	msgs, err := consumer.Fetch(5, jetstream.FetchMaxWait(3*time.Second))
	require.NoError(t, err)

	delivered := 0
	for msg := range msgs.Messages() {
		delivered++
		_ = msg.Ack()
	}
	require.NoError(t, msgs.Error())
	assert.Equal(t, 1, delivered,
		"republishing the same outbox row must be deduped by message id, not delivered twice")
}

// TestPublisher_DistinctRowsAreNotDeduped is the other half: dedupe must
// key on the outbox row id and nothing else, so two genuinely different
// events on the same subject both arrive. Without it, a dedupe scheme
// keyed on subject or payload would pass the test above while silently
// dropping real events.
func TestPublisher_DistinctRowsAreNotDeduped(t *testing.T) {
	nc, err := natsgo.Connect(natsURL(t))
	require.NoError(t, err)
	defer nc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	js, err := jetstream.New(nc)
	require.NoError(t, err)
	_ = js.DeleteStream(ctx, atsapnats.StreamName)

	publisher, err := atsapnats.NewPublisher(ctx, nc)
	require.NoError(t, err)

	tenantID := "tenant-distinct"
	for _, id := range []string{"outbox-a", "outbox-b"} {
		require.NoError(t, publisher.Publish(ctx, corepostgres.OutboxEvent{
			ID: id, TenantID: tenantID, EventType: "call.initiated",
			AggregateID: "call-9", Payload: []byte(`{"call_id":"call-9"}`),
		}))
	}

	consumer, err := js.CreateOrUpdateConsumer(ctx, atsapnats.StreamName, jetstream.ConsumerConfig{
		FilterSubject: "tenant." + tenantID + ".event.call.initiated",
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)

	msgs, err := consumer.Fetch(5, jetstream.FetchMaxWait(3*time.Second))
	require.NoError(t, err)

	delivered := 0
	for msg := range msgs.Messages() {
		delivered++
		_ = msg.Ack()
	}
	require.NoError(t, msgs.Error())
	assert.Equal(t, 2, delivered,
		"two distinct outbox rows carry different message ids and must both be delivered")
}

// TestPublisher_NewPublisher_IdempotentStreamCreation covers calling
// NewPublisher twice against the same NATS instance — the second call
// must find the existing stream, not error trying to recreate it.
func TestPublisher_NewPublisher_IdempotentStreamCreation(t *testing.T) {
	nc, err := natsgo.Connect(natsURL(t))
	require.NoError(t, err)
	defer nc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	js, err := jetstream.New(nc)
	require.NoError(t, err)
	_ = js.DeleteStream(ctx, atsapnats.StreamName)

	_, err = atsapnats.NewPublisher(ctx, nc)
	require.NoError(t, err)

	_, err = atsapnats.NewPublisher(ctx, nc)
	require.NoError(t, err, "creating a Publisher a second time must not error on an already-existing stream")
}
