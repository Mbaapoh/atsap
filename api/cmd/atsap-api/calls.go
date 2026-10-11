package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	pbxports "atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
	telephonydomain "atsap-api/internal/telephony/domain"
	telephonyports "atsap-api/internal/telephony/ports"
)

// callStatusAdapter satisfies pbx-core's CallStatus over telephony-core's
// call store (pbx-call-placement design D3).
//
// pbx-core could import telephony's types — the direction is legal — but
// it needs four facts about a call, not the Call aggregate, and keeping
// the aggregate out of pbx stops call internals spreading into the dial
// plan. Origination is NOT adapted here: building InitiateCallCommand is
// pbx/application's alone, because that is the only place permitted to
// set BypassScreening (LLD-03 §10.1, archtest).
type callStatusAdapter struct {
	calls telephonyports.CallStore
}

var _ pbxports.CallStatus = callStatusAdapter{}

func (a callStatusAdapter) Status(ctx context.Context, tenantID shareddomain.TenantID, callID shareddomain.CallID) (pbxports.CallOutcome, error) {
	call, err := a.calls.GetCall(ctx, tenantID, callID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pbxports.CallOutcome{}, fmt.Errorf("call %s: %w", callID, pbxports.ErrNotFound)
	}
	if err != nil {
		return pbxports.CallOutcome{}, err
	}
	return pbxports.CallOutcome{
		StartedAt:  call.StartedAt,
		AnsweredAt: call.AnsweredAt,
		Ended:      call.State == telephonydomain.CallTerminated,
		Reason:     call.TerminationReason,
	}, nil
}
