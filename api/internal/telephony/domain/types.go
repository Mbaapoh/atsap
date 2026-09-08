// Package domain implements the telephony-core Call/CallParticipant
// aggregate: pure state machines and usage-tick generation, with zero I/O
// and zero dependency on the Asterisk ACL (docs/DECISIONS.md D-17, D-18,
// D-32; docs/hld/03-domain-model.md).
package domain

import (
	"time"

	shareddomain "atsap-api/internal/shared/domain"
)

// Direction is which side originated a Call.
type Direction string

const (
	Inbound  Direction = "INBOUND"
	Outbound Direction = "OUTBOUND"
	Internal Direction = "INTERNAL"
)

// CallState is a Call's position in its lifecycle (PRD §11.1).
type CallState string

const (
	CallInitiated  CallState = "Initiated"
	CallScreening  CallState = "Screening"
	CallRouting    CallState = "Routing"
	CallPresenting CallState = "Presenting"
	CallActive     CallState = "Active"
	// CallDegraded exists as a value so later work (RTCP-XR sampling,
	// LLD-09) does not need a schema or type change to introduce it.
	// Nothing in this package transitions a Call into it yet.
	CallDegraded    CallState = "Degraded"
	CallTerminating CallState = "Terminating"
	CallTerminated  CallState = "Terminated"
)

// ParticipantState is a CallParticipant's position in its own lifecycle,
// independent of the parent Call's state (PRD §11.1 "Participant
// Independence Rule").
type ParticipantState string

const (
	ParticipantInvited      ParticipantState = "Invited"
	ParticipantRinging      ParticipantState = "Ringing"
	ParticipantConnected    ParticipantState = "Connected"
	ParticipantOnHold       ParticipantState = "OnHold"
	ParticipantTransferring ParticipantState = "Transferring"
	ParticipantDisconnected ParticipantState = "Disconnected"
)

// ParticipantRole identifies why a party is on the Call. Call is always
// modeled as N Participants of these roles, never fixed "caller"/"agent"
// columns (D-18).
type ParticipantRole string

const (
	RoleCaller     ParticipantRole = "CALLER"
	RoleAgent      ParticipantRole = "AGENT"
	RoleIVR        ParticipantRole = "IVR"
	RoleQueue      ParticipantRole = "QUEUE"
	RoleSupervisor ParticipantRole = "SUPERVISOR"
	RoleSpecialist ParticipantRole = "SPECIALIST"
	RoleAI         ParticipantRole = "AI"
)

// Call is the aggregate root: a communication session belonging to a
// tenant, modeled as a collection of Participants rather than fixed
// caller/agent fields (D-18).
type Call struct {
	ID                shareddomain.CallID
	TenantID          shareddomain.TenantID
	Direction         Direction
	State             CallState
	SourceNumber      string
	DestNumber        string
	StartedAt         time.Time
	AnsweredAt        *time.Time
	EndedAt           *time.Time
	TerminationReason string
	Participants      []*CallParticipant
}

// CallParticipant is one party's participation in a Call. Its identity
// and billable duration are independent of any Asterisk channel backing
// it at a given moment (D-17) — the Asterisk ACL, not this package, is
// responsible for that mapping.
type CallParticipant struct {
	ID              shareddomain.ParticipantID
	CallID          shareddomain.CallID
	TenantID        shareddomain.TenantID
	Role            ParticipantRole
	EndpointURI     string
	State           ParticipantState
	JoinedAt        time.Time
	AnsweredAt      *time.Time
	LeftAt          *time.Time
	BillableSeconds int
}

// connectedCount returns how many participants are currently Connected.
func (c *Call) connectedCount() int {
	n := 0
	for _, p := range c.Participants {
		if p.State == ParticipantConnected {
			n++
		}
	}
	return n
}

// findParticipant returns the participant with the given ID, or nil.
func (c *Call) findParticipant(id shareddomain.ParticipantID) *CallParticipant {
	for _, p := range c.Participants {
		if p.ID == id {
			return p
		}
	}
	return nil
}
