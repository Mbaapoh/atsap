package domain

// The five domain events this walking skeleton publishes (proposal.md
// "What Changes"). Each implements shared/event.DomainEvent. EventType
// returns the dotted name used as the outbox event_type and, combined
// with the tenant, the NATS subject (internal/nats).

// CallInitiatedEvent fires when a Call is created.
type CallInitiatedEvent struct {
	CallID string
}

func (CallInitiatedEvent) EventType() string { return "call.initiated" }

// CallActiveEvent fires when a Call reaches Active (2+ participants
// Connected).
type CallActiveEvent struct {
	CallID string
}

func (CallActiveEvent) EventType() string { return "call.active" }

// CallTerminatedEvent fires when a Call reaches Terminated.
type CallTerminatedEvent struct {
	CallID string
	Reason string
}

func (CallTerminatedEvent) EventType() string { return "call.terminated" }

// ParticipantJoinedEvent fires when a participant enters the bridge
// (transitions to Connected).
type ParticipantJoinedEvent struct {
	CallID        string
	ParticipantID string
}

func (ParticipantJoinedEvent) EventType() string { return "participant.joined" }

// ParticipantLeftEvent fires when a participant leaves the bridge
// (transitions to Disconnected).
type ParticipantLeftEvent struct {
	CallID        string
	ParticipantID string
}

func (ParticipantLeftEvent) EventType() string { return "participant.left" }
