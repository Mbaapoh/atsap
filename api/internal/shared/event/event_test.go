package event_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"atsap-api/internal/shared/event"
)

// fakeEvent is a minimal DomainEvent used only to test the envelope.
type fakeEvent struct{}

func (fakeEvent) EventType() string { return "fake.happened" }

func TestNewEnvelope(t *testing.T) {
	occurredAt := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	env := event.NewEnvelope("tenant-1", "aggregate-1", occurredAt, fakeEvent{})

	assert.Equal(t, "fake.happened", env.Type)
	assert.Equal(t, "tenant-1", env.TenantID)
	assert.Equal(t, "aggregate-1", env.AggregateID)
	assert.True(t, env.OccurredAt.Equal(occurredAt))
	assert.Equal(t, fakeEvent{}, env.Payload)
}
