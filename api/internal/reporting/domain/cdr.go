// Package domain holds reporting's read models. It performs no I/O.
package domain

import (
	"time"

	shareddomain "atsap-api/internal/shared/domain"
)

// Filter narrows a call-record query. Zero values mean any; the service
// narrows PrincipalID for an agent before it reaches the store.
type Filter struct {
	From        time.Time
	To          time.Time
	Direction   string
	ExtensionID *shareddomain.ExtensionID
	PrincipalID *shareddomain.PrincipalID
	Number      string
}

// Participant is one party on a call, attributed to a platform extension
// and owner when known. It never carries an engine endpoint.
type Participant struct {
	ID              shareddomain.ParticipantID
	Role            string
	ExtensionID     *shareddomain.ExtensionID
	PrincipalID     *shareddomain.PrincipalID
	BillableSeconds int
	UsageSeconds    int
}

// CallRecord is one terminated call (a CDR) with its participants.
type CallRecord struct {
	CallID            shareddomain.CallID
	Direction         string
	SourceNumber      string
	DestNumber        string
	StartedAt         time.Time
	AnsweredAt        *time.Time
	EndedAt           *time.Time
	TerminationReason string
	Participants      []Participant
}
