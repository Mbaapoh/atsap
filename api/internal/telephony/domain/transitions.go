package domain

import (
	"errors"
	"fmt"
	"time"

	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/shared/event"
)

// Sentinel errors. ErrCallTerminated is returned by every mutating method
// once a Call is Terminated — the spec requires a Terminated record to be
// immutable (no further state, timestamp, or billable-duration change).
var (
	ErrCallTerminated      = errors.New("call is terminated: no further transitions allowed")
	ErrInvalidTransition   = errors.New("invalid call state transition")
	ErrParticipantNotFound = errors.New("participant not found on call")
)

// NewCall creates a Call in the Initiated state with no participants.
// Callers should also emit the returned InitiatedEvent through the
// outbox in the same transaction as the initial persist.
func NewCall(id shareddomain.CallID, tenantID shareddomain.TenantID, direction Direction, sourceNumber, destNumber string, startedAt time.Time) *Call {
	return &Call{
		ID:           id,
		TenantID:     tenantID,
		Direction:    direction,
		State:        CallInitiated,
		SourceNumber: sourceNumber,
		DestNumber:   destNumber,
		StartedAt:    startedAt,
	}
}

// InitiatedEvent returns the event for a freshly created Call.
func (c *Call) InitiatedEvent() event.DomainEvent {
	return CallInitiatedEvent{CallID: c.ID.String()}
}

// AddParticipant adds p to the Call in the Invited state. It does not
// itself emit an event: ParticipantJoinedEvent fires when a participant
// actually enters the bridge (ConnectParticipant), not when invited.
func (c *Call) AddParticipant(p *CallParticipant) error {
	if c.State == CallTerminated {
		return ErrCallTerminated
	}
	p.State = ParticipantInvited
	c.Participants = append(c.Participants, p)
	return nil
}

// TransitionToScreening moves a Call from Initiated to Screening.
func (c *Call) TransitionToScreening() error {
	if c.State == CallTerminated {
		return ErrCallTerminated
	}
	if c.State != CallInitiated {
		return fmt.Errorf("%w: screening requires Initiated, got %s", ErrInvalidTransition, c.State)
	}
	c.State = CallScreening
	return nil
}

// ApplyScreeningVerdict moves a Call from Screening to Routing if both
// verdicts permit it, or terminates the call otherwise, naming which
// verdict rejected it (spec: "Screening evaluates capacity and
// compliance before Routing").
func (c *Call) ApplyScreeningVerdict(capacityPermitted, compliancePermitted bool, when time.Time) ([]event.DomainEvent, error) {
	if c.State == CallTerminated {
		return nil, ErrCallTerminated
	}
	if c.State != CallScreening {
		return nil, fmt.Errorf("%w: screening verdict requires Screening, got %s", ErrInvalidTransition, c.State)
	}
	if !capacityPermitted || !compliancePermitted {
		return c.Terminate(screeningRejectionReason(capacityPermitted, compliancePermitted), when)
	}
	c.State = CallRouting
	return nil, nil
}

func screeningRejectionReason(capacityPermitted, compliancePermitted bool) string {
	switch {
	case !capacityPermitted && !compliancePermitted:
		return "capacity and compliance verdicts both rejected the call"
	case !capacityPermitted:
		return "capacity verdict rejected the call"
	default:
		return "compliance verdict rejected the call"
	}
}

// TransitionToPresenting moves a Call from Routing to Presenting (a
// destination has been selected and is being alerted).
func (c *Call) TransitionToPresenting() error {
	if c.State == CallTerminated {
		return ErrCallTerminated
	}
	if c.State != CallRouting {
		return fmt.Errorf("%w: presenting requires Routing, got %s", ErrInvalidTransition, c.State)
	}
	c.State = CallPresenting
	return nil
}

// ConnectParticipant marks a participant Connected (entered the bridge).
// If this brings the Call's connected-participant count to 2 or more
// while the Call is Presenting, the Call transitions to Active (spec:
// "Active requires two or more connected participants").
func (c *Call) ConnectParticipant(participantID shareddomain.ParticipantID, when time.Time) ([]event.DomainEvent, error) {
	if c.State == CallTerminated {
		return nil, ErrCallTerminated
	}
	p := c.findParticipant(participantID)
	if p == nil {
		return nil, ErrParticipantNotFound
	}

	p.State = ParticipantConnected
	p.AnsweredAt = &when

	events := []event.DomainEvent{ParticipantJoinedEvent{CallID: c.ID.String(), ParticipantID: p.ID.String()}}

	if c.State == CallPresenting && c.connectedCount() >= 2 {
		c.State = CallActive
		c.AnsweredAt = &when
		events = append(events, CallActiveEvent{CallID: c.ID.String()})
	}
	return events, nil
}

// DisconnectParticipant marks a participant Disconnected (left the
// bridge). If this was the last Connected participant on an Active Call,
// the Call moves to Terminating; callers finalize with Terminate once
// teardown (bridge/channel destruction) is confirmed by the ACL.
// (Degraded is not checked here: nothing in this package transitions a
// Call into Degraded yet — see CallDegraded's doc comment — so adding
// that branch now would be untestable dead code; LLD-04/07 adds it
// alongside the RTCP-XR sampling that makes Degraded reachable.)
func (c *Call) DisconnectParticipant(participantID shareddomain.ParticipantID, when time.Time) ([]event.DomainEvent, error) {
	if c.State == CallTerminated {
		return nil, ErrCallTerminated
	}
	p := c.findParticipant(participantID)
	if p == nil {
		return nil, ErrParticipantNotFound
	}

	p.State = ParticipantDisconnected
	p.LeftAt = &when

	events := []event.DomainEvent{ParticipantLeftEvent{CallID: c.ID.String(), ParticipantID: p.ID.String()}}

	if c.connectedCount() == 0 && c.State == CallActive {
		c.State = CallTerminating
	}
	return events, nil
}

// Terminate ends the Call, from any non-Terminated state: normal
// teardown from Terminating, or a direct termination from an earlier
// state that never reached Active (a rejected Screening verdict, an
// unanswered Presenting timeout, a caller hangup during Routing) — see
// PRD §11.1's state diagram, which allows both. The record becomes
// immutable once this returns successfully.
func (c *Call) Terminate(reason string, when time.Time) ([]event.DomainEvent, error) {
	if c.State == CallTerminated {
		return nil, ErrCallTerminated
	}
	c.State = CallTerminated
	c.EndedAt = &when
	c.TerminationReason = reason
	return []event.DomainEvent{CallTerminatedEvent{CallID: c.ID.String(), Reason: reason}}, nil
}
