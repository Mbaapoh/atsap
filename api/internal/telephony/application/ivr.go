package application

import (
	"context"
	"fmt"
	"time"

	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/telephony/domain"
	"atsap-api/internal/telephony/ports"
)

// ivrSession is one caller waiting on a prompt-and-DTMF menu.
type ivrSession struct {
	tenantID          shareddomain.TenantID
	callID            shareddomain.CallID
	callerParticipant shareddomain.ParticipantID
	callerRef         ports.ChannelRef
	menu              ports.IVRMenu
	playbackID        string
	timer             *time.Timer
}

// startIVR answers the caller, starts the prompt and waits for one digit.
// The caller joins the bridge first, so the chosen destination is bridged
// when it answers exactly like any other arriving call.
func (s *Service) startIVR(ctx context.Context, call *domain.Call, menu ports.IVRMenu, callerRef ports.ChannelRef, now time.Time) error {
	tenantID, callID := call.TenantID, call.ID
	a, ok := s.arrivedOf(callID)
	if !ok {
		return fmt.Errorf("start IVR for call %s: %w", callID, ErrCallNotFound)
	}
	if err := s.mediaGateway.AnswerChannel(ctx, callerRef); err != nil {
		return fmt.Errorf("answer IVR caller: %w", err)
	}
	bridgeID, err := s.ensureBridge(ctx, callID)
	if err != nil {
		return fmt.Errorf("ensure IVR bridge: %w", err)
	}
	if err := s.mediaGateway.AddChannelToBridge(ctx, bridgeID, callerRef); err != nil {
		return fmt.Errorf("add IVR caller to bridge: %w", err)
	}
	connectEvents, err := call.ConnectParticipant(a.callerParticipant, now)
	if err != nil {
		return fmt.Errorf("connect IVR caller: %w", err)
	}
	if err := s.saveCall(ctx, call, connectEvents...); err != nil {
		return err
	}
	s.markArrivedAnswered(callID)

	playbackID, err := s.mediaGateway.StartPlayback(ctx, callerRef, menu.Prompt)
	if err != nil {
		_ = s.terminateIVR(context.WithoutCancel(ctx), call, callerRef, "ivr_prompt_failed")
		return fmt.Errorf("start IVR prompt: %w", err)
	}
	session := &ivrSession{
		tenantID: tenantID, callID: callID, callerParticipant: a.callerParticipant,
		callerRef: callerRef, menu: menu, playbackID: playbackID,
	}
	timeout := menu.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	s.mu.Lock()
	s.ivr[string(callerRef)] = session
	s.mu.Unlock()
	timer := time.AfterFunc(timeout, func() {
		s.ivrTimeout(context.WithoutCancel(ctx), string(callerRef))
	})
	s.mu.Lock()
	session.timer = timer
	s.mu.Unlock()
	return nil
}

// IVRDigit routes one digit from a caller in a menu.
func (s *Service) IVRDigit(ctx context.Context, channelID, digit string) error {
	s.mu.Lock()
	session, ok := s.ivr[channelID]
	var timer *time.Timer
	if ok {
		timer = session.timer
		delete(s.ivr, channelID)
	}
	s.mu.Unlock()
	if !ok {
		return nil // not an IVR channel, or a digit after the menu moved on
	}
	if timer != nil {
		timer.Stop()
	}
	_ = s.mediaGateway.StopPlayback(ctx, session.callerRef, session.playbackID)

	dest := session.menu.Invalid
	for _, option := range session.menu.Options {
		if option.Digit == digit {
			d := option.Destination
			dest = &d
			break
		}
	}
	if dest == nil {
		return s.terminateIVR(ctx, s.loadCallOrNil(session.callID), session.callerRef, "ivr_invalid_input")
	}
	return s.routeIVR(ctx, session, *dest)
}

func (s *Service) ivrTimeout(ctx context.Context, channelID string) {
	s.mu.Lock()
	session, ok := s.ivr[channelID]
	if ok {
		delete(s.ivr, channelID)
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	_ = s.mediaGateway.StopPlayback(ctx, session.callerRef, session.playbackID)
	if session.menu.NoInput == nil {
		_ = s.terminateIVR(ctx, s.loadCallOrNil(session.callID), session.callerRef, "ivr_no_input")
		return
	}
	_ = s.routeIVR(ctx, session, *session.menu.NoInput)
}

func (s *Service) routeIVR(ctx context.Context, session *ivrSession, dest ports.IVRDestination) error {
	call := s.loadCallOrNil(session.callID)
	if call == nil {
		return nil
	}
	decision := ports.InboundDecision{
		DestinationURI:         dest.EndpointURI,
		Direction:              call.Direction,
		DestinationExtensionID: dest.ExtensionID,
		DestinationPrincipalID: dest.PrincipalID,
	}
	return s.connectArrived(ctx, call, decision, session.callerRef, time.Now().UTC())
}

func (s *Service) terminateIVR(ctx context.Context, call *domain.Call, callerRef ports.ChannelRef, reason string) error {
	if call == nil {
		return nil
	}
	_ = s.mediaGateway.DestroyChannel(ctx, callerRef)
	return s.finalizeTermination(ctx, call, reason)
}

func (s *Service) loadCallOrNil(id shareddomain.CallID) *domain.Call {
	call, ok := s.loadCall(id)
	if !ok {
		return nil
	}
	return call
}
