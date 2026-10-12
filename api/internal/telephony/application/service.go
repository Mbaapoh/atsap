// Package application implements telephony-core's CallService: the
// orchestrator wiring domain transitions, ports, and (via
// ports.CorrelationRegistrar and the acl.EventSink shape it satisfies
// structurally) the Asterisk ACL together. This package imports only
// domain and ports, never acl directly (docs/hld/01-architecture.md
// §1.2) — the composition root (cmd/, task 9.2) wires the concrete ACL
// adapters in.
package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/shared/event"
	"atsap-api/internal/telephony/domain"
	"atsap-api/internal/telephony/ports"
)

// ErrNotImplemented is returned by CallService methods this change does
// not implement: hold, transfer, and explicit answer-by-command (both
// legs are originated by us in this change, so participants connect via
// ParticipantAnswered reacting to real ARI events, not an external
// command). See LLD-01 §1's out-of-scope table.
var ErrNotImplemented = errors.New("not implemented in this change")

// ErrCallNotFound is returned when a CallID has no in-flight Call.
// Terminated calls are removed from the in-flight set (see removeCall);
// querying a call's final state is GetCall's (task 8.2) job, backed by
// CallStore, not this in-memory set.
var ErrCallNotFound = errors.New("call not found")

// Service implements ports.CallService and structurally satisfies
// acl.EventSink (ParticipantAnswered/ParticipantLeft) without importing
// acl — see the package doc.
//
// In-flight calls live in memory (calls, bridges), not re-read from
// CallStore on every event: this is a deliberate choice for this walking
// skeleton, not an oversight — round-tripping Postgres for every single
// ARI event on the hot call-setup/teardown path is unnecessary work, and
// RLS tenant-scoping (SET LOCAL app.tenant_id) requires a tenant context
// this event-driven path does not otherwise carry (correlation only
// carries string IDs — see ports.CorrelationRegistrar). CallStore is
// still the durable, authoritative record: every meaningful transition
// is persisted via saveCall before this method returns.
type Service struct {
	mediaGateway ports.MediaGateway
	callStore    ports.CallStore
	license      ports.LicenseManager
	compliance   ports.ComplianceEngine
	registrar    ports.CorrelationRegistrar
	// inboundRouter resolves arriving calls (pbx-inbound-and-emergency
	// design D2). It defaults to NoInboundRouter, so telephony runs with
	// no pbx-core in the binary (LLD-03 DoD 15); the composition root
	// overrides it via SetInboundRouter where pbx-core is present.
	inboundRouter ports.InboundRouter
	// timeoutSeconds is the alerting timeout passed to MediaGateway.Originate
	// (PRD §11.1 Presenting; docs/hld/03-domain-model.md §2.1 default 20s).
	timeoutSeconds int
	logger         *slog.Logger

	mu      sync.Mutex
	calls   map[string]*domain.Call
	bridges map[string]ports.BridgeID
	// arrived tracks calls that began with an arriving channel rather
	// than origination, keyed by call ID string. The entry maps the
	// caller participant to its engine channel (which, unlike
	// originated legs, has no deterministic address) and records
	// whether the caller has been answered yet.
	arrived map[string]*arrivedCall
}

// arrivedCall is the per-call state for a call that began with an
// arriving channel.
type arrivedCall struct {
	callerParticipant shareddomain.ParticipantID
	callerRef         ports.ChannelRef
	answered          bool
}

// NewService wires a Service from its ports. timeoutSeconds is the
// alerting timeout for originated legs; pass 0 to use Asterisk's own
// default instead of an explicit one.
func NewService(
	mediaGateway ports.MediaGateway,
	callStore ports.CallStore,
	license ports.LicenseManager,
	compliance ports.ComplianceEngine,
	registrar ports.CorrelationRegistrar,
	timeoutSeconds int,
	logger *slog.Logger,
) *Service {
	return &Service{
		mediaGateway:   mediaGateway,
		callStore:      callStore,
		license:        license,
		compliance:     compliance,
		registrar:      registrar,
		inboundRouter:  ports.NoInboundRouter{},
		timeoutSeconds: timeoutSeconds,
		logger:         logger,
		calls:          make(map[string]*domain.Call),
		bridges:        make(map[string]ports.BridgeID),
		arrived:        make(map[string]*arrivedCall),
	}
}

// SetInboundRouter overrides the default NoInboundRouter with pbx-core's
// implementation. Called by the composition root where pbx-core is part
// of the binary; telephony's own suites never call it (LLD-03 DoD 15).
func (s *Service) SetInboundRouter(router ports.InboundRouter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inboundRouter = router
}

func (s *Service) router() ports.InboundRouter {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inboundRouter
}

func (s *Service) arrivedOf(callID shareddomain.CallID) (arrivedCall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.arrived[callID.String()]
	if !ok {
		return arrivedCall{}, false
	}
	return *a, true
}

func (s *Service) markArrivedAnswered(callID shareddomain.CallID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.arrived[callID.String()]; ok {
		a.answered = true
	}
}

// InitiateCall creates a two-party Call (caller, callee — never fixed
// role columns, D-18), screens it (capacity + compliance), and — if
// permitted — originates both legs. Both legs are originated by us in
// this change, so correlation is registered synchronously here
// (originateParticipant), before either channel exists in Asterisk;
// neither leg's StasisStart needs to teach the ACL anything new (LLD-01
// §4.4, EventLoop's doc comment).
func (s *Service) InitiateCall(ctx context.Context, cmd ports.InitiateCallCommand) (shareddomain.CallID, error) {
	now := time.Now().UTC()
	callID := shareddomain.NewCallID()
	call := domain.NewCall(callID, cmd.TenantID, cmd.Direction, cmd.SourceNumber, cmd.DestNumber, now)

	caller := &domain.CallParticipant{
		ID: shareddomain.NewParticipantID(), CallID: callID, TenantID: cmd.TenantID,
		Role: domain.RoleCaller, EndpointURI: cmd.SourceEndpointURI, JoinedAt: now,
	}
	callee := &domain.CallParticipant{
		ID: shareddomain.NewParticipantID(), CallID: callID, TenantID: cmd.TenantID,
		Role: domain.RoleAgent, EndpointURI: cmd.DestEndpointURI, JoinedAt: now,
	}
	if err := call.AddParticipant(caller); err != nil {
		return shareddomain.CallID{}, fmt.Errorf("add caller participant: %w", err)
	}
	if err := call.AddParticipant(callee); err != nil {
		return shareddomain.CallID{}, fmt.Errorf("add callee participant: %w", err)
	}

	s.storeCall(call)
	if err := s.saveCall(ctx, call, call.InitiatedEvent()); err != nil {
		return shareddomain.CallID{}, err
	}

	if err := call.TransitionToScreening(); err != nil {
		return shareddomain.CallID{}, fmt.Errorf("transition to screening: %w", err)
	}

	// INV-01: a bypass-marked call skips Screening entirely. Both consults
	// are skipped — capacity (licensing must never be positioned to block
	// one: spec capacity-enforcement, task 6.3) and compliance
	// (calling-hours and do-not-call rules must not interpose on emergency
	// either). Identification stays pbx-core's job (LLD-03 §4.3, §10.1);
	// cmd.BypassScreening is the exemption point its application layer
	// drives from domain.IsEmergency. No reservation is taken, so
	// teardown's unconditional ReleaseCapacity lands on its documented
	// no-op for unreserved IDs.
	capVerdict := ports.CapacityVerdict{Permitted: true}
	compVerdict := ports.ComplianceVerdict{Permitted: true}
	if cmd.BypassScreening {
		s.logger.Info("screening bypassed for emergency call",
			"call_id", call.ID, "tenant_id", cmd.TenantID)
	} else {
		var err error
		capVerdict, err = s.license.ValidateCapacity(ctx, cmd.TenantID, call.ID.String(), 1)
		if err != nil {
			return shareddomain.CallID{}, fmt.Errorf("validate capacity: %w", err)
		}
		compVerdict, err = s.compliance.Evaluate(ctx, cmd.TenantID, cmd.DestNumber)
		if err != nil {
			return shareddomain.CallID{}, fmt.Errorf("evaluate compliance: %w", err)
		}
	}

	screenEvents, err := call.ApplyScreeningVerdict(capVerdict.Permitted, compVerdict.Permitted, time.Now().UTC())
	if err != nil {
		return shareddomain.CallID{}, fmt.Errorf("apply screening verdict: %w", err)
	}
	if err := s.saveCall(ctx, call, screenEvents...); err != nil {
		return shareddomain.CallID{}, err
	}
	if call.State == domain.CallTerminated {
		// Screened out — not an API-level error; the caller reads the
		// rejection from the persisted call's TerminationReason (or the
		// published event) via GetCall.
		s.removeCall(callID)
		return callID, nil
	}

	if err := call.TransitionToPresenting(); err != nil {
		return shareddomain.CallID{}, fmt.Errorf("transition to presenting: %w", err)
	}
	if err := s.saveCall(ctx, call); err != nil {
		return shareddomain.CallID{}, err
	}

	// Create the bridge before originating anyone: against real
	// Asterisk, the window between a channel's StasisStart and its own
	// SDP-completion deadline is tight (observed directly — see
	// EventLoop's doc comment), and ensureBridge's ARI round trip on the
	// first participant's StasisStart was, by itself, consistently too
	// slow to make it. Creating the bridge here means StasisStart's
	// handler only ever needs one ARI call (AddChannelToBridge), not two.
	if _, err := s.ensureBridge(ctx, callID); err != nil {
		return shareddomain.CallID{}, fmt.Errorf("create bridge: %w", err)
	}

	for _, p := range call.Participants {
		if err := s.originateParticipant(ctx, cmd.TenantID, callID, p); err != nil {
			return shareddomain.CallID{}, fmt.Errorf("originate participant %s: %w", p.ID, err)
		}
	}

	return callID, nil
}

// CallArrived handles a channel that arrived at the engine without
// having been originated by the platform (pbx-inbound-and-emergency
// design D3). It implements the shape acl.EventSink declares for
// uncorrelated StasisStarts (structurally — this package never imports
// acl), alongside ParticipantAnswered/ParticipantLeft.
//
// The arriving channel becomes the call's caller participant, correlated
// under its own channel id. The router decides what the call is; a
// refusal is signalled at the SIP edge (never silence) and the call
// terminates with the reason. Otherwise the call is screened unless
// bypassed, the caller hears ringing while the destination leg is
// originated exactly as today, and the caller is answered and bridged
// when the destination answers (in ParticipantAnswered).
//
// A channel with no tenant variable is rejected NotFound without the
// router ever being consulted (design D1): without a tenant there is
// nothing to resolve and no scope to persist a call in.
func (s *Service) CallArrived(ctx context.Context, tenantIDStr, sourceEndpoint, dialled, callerNumber, channelID string) error {
	tenantID, err := shareddomain.ParseTenantID(tenantIDStr)
	if err != nil {
		s.logger.Warn("arriving call without a tenant variable; refusing",
			"channel_id", channelID, "source_endpoint", sourceEndpoint)
		if rerr := s.mediaGateway.RejectChannel(ctx, ports.ChannelRef(channelID), ports.RefusalNotFound); rerr != nil {
			return fmt.Errorf("reject tenantless arriving channel: %w", rerr)
		}
		return nil
	}

	now := time.Now().UTC()
	callID := shareddomain.NewCallID()
	callerRef := ports.ChannelRef(channelID)

	call := domain.NewCall(callID, tenantID, domain.Inbound, callerNumber, dialled, now)
	caller := &domain.CallParticipant{
		ID: shareddomain.NewParticipantID(), CallID: callID, TenantID: tenantID,
		Role: domain.RoleCaller, EndpointURI: "PJSIP/" + sourceEndpoint, JoinedAt: now,
	}
	if err := call.AddParticipant(caller); err != nil {
		return fmt.Errorf("add arriving caller participant: %w", err)
	}

	s.storeCall(call)
	if err := s.saveCall(ctx, call, call.InitiatedEvent()); err != nil {
		return err
	}

	decision, err := s.router().ResolveInbound(ctx, ports.InboundRequest{
		TenantID: tenantID, CallID: callID,
		SourceEndpoint: sourceEndpoint, Dialled: dialled, CallerNumber: callerNumber,
	})
	if err != nil {
		s.logger.Error("inbound router failed; refusing rather than hanging",
			"error", err, "call_id", callID, "tenant_id", tenantID)
		return s.refuseArrived(ctx, callID, callerRef,
			ports.RefusalUnavailable, "inbound routing failed")
	}
	// The router owns the call's direction (inbound from a trunk,
	// internal between extensions); it is recorded once decided.
	call.Direction = decision.Direction

	if decision.Refuse != ports.RefusalNone {
		return s.refuseArrived(ctx, callID, callerRef,
			decision.Refuse, inboundRefusalReason(decision.Refuse))
	}

	if err := call.TransitionToScreening(); err != nil {
		return fmt.Errorf("transition arrived call to screening: %w", err)
	}
	if err := s.saveCall(ctx, call); err != nil {
		return err
	}

	if !decision.BypassScreening {
		capVerdict, err := s.license.ValidateCapacity(ctx, tenantID, call.ID.String(), 1)
		if err != nil {
			s.logger.Error("validate capacity for arrived call failed; refusing",
				"error", err, "call_id", callID)
			return s.refuseArrived(ctx, callID, callerRef,
				ports.RefusalUnavailable, "capacity check failed")
		}
		if !capVerdict.Permitted {
			return s.refuseArrived(ctx, callID, callerRef,
				ports.RefusalUnavailable, "capacity verdict rejected the call")
		}
		compVerdict, err := s.compliance.Evaluate(ctx, tenantID, dialled)
		if err != nil {
			s.logger.Error("evaluate compliance for arrived call failed; refusing",
				"error", err, "call_id", callID)
			return s.refuseArrived(ctx, callID, callerRef,
				ports.RefusalForbidden, "compliance check failed")
		}
		if !compVerdict.Permitted {
			return s.refuseArrived(ctx, callID, callerRef,
				ports.RefusalForbidden, "compliance verdict rejected the call")
		}
	} else {
		s.logger.Info("screening bypassed for arriving emergency call",
			"call_id", call.ID, "tenant_id", tenantID)
	}

	if _, err := call.ApplyScreeningVerdict(true, true, time.Now().UTC()); err != nil {
		return fmt.Errorf("apply screening verdict to arrived call: %w", err)
	}
	if err := s.saveCall(ctx, call); err != nil {
		return err
	}

	if err := call.TransitionToPresenting(); err != nil {
		return fmt.Errorf("transition arrived call to presenting: %w", err)
	}
	if err := s.saveCall(ctx, call); err != nil {
		return err
	}

	// The bridge exists before either leg needs it, exactly as in
	// InitiateCall: the destination leg's StasisStart must find a bridge
	// to join with a single ARI call.
	if _, err := s.ensureBridge(ctx, callID); err != nil {
		return fmt.Errorf("create bridge for arrived call: %w", err)
	}

	// The caller is correlated under its own channel id (D-19): the id
	// Asterisk assigned, not one we chose.
	s.registrar.RegisterCorrelation(channelID, tenantID.String(), callID.String(), caller.ID.String())
	s.mu.Lock()
	s.arrived[callID.String()] = &arrivedCall{callerParticipant: caller.ID, callerRef: callerRef}
	s.mu.Unlock()

	if err := s.callStore.AddChannelHistory(ctx, tenantID, caller.ID, callerRef, "", "arrived"); err != nil {
		return fmt.Errorf("add channel history for arriving caller: %w", err)
	}

	if err := s.mediaGateway.RingChannel(ctx, callerRef); err != nil {
		return fmt.Errorf("ring arriving caller: %w", err)
	}

	destination := &domain.CallParticipant{
		ID: shareddomain.NewParticipantID(), CallID: callID, TenantID: tenantID,
		Role: domain.RoleAgent, EndpointURI: decision.DestinationURI, JoinedAt: now,
	}
	if err := call.AddParticipant(destination); err != nil {
		return fmt.Errorf("add arrived destination participant: %w", err)
	}
	// The destination must be persisted before origination: the channel
	// history row originateParticipant writes references it, and the
	// database refuses orphans.
	if err := s.saveCall(ctx, call); err != nil {
		return err
	}
	if err := s.originateParticipant(ctx, tenantID, callID, destination); err != nil {
		return fmt.Errorf("originate arrived destination %s: %w", destination.ID, err)
	}

	return nil
}

// refuseArrived rejects an arriving channel at the SIP edge and records
// the call as terminated with the reason. The call must already be
// stored; it is removed from the in-flight set like a screened-out
// originated call.
func (s *Service) refuseArrived(ctx context.Context, callID shareddomain.CallID, callerRef ports.ChannelRef, cause ports.RefusalCause, reason string) error {
	call, ok := s.loadCall(callID)
	if !ok {
		return fmt.Errorf("refuse arrived call %s: %w", callID, ErrCallNotFound)
	}
	if err := s.mediaGateway.RejectChannel(ctx, callerRef, cause); err != nil {
		return fmt.Errorf("reject arrived channel: %w", err)
	}
	terminateEvents, err := call.Terminate(reason, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("terminate refused arrived call: %w", err)
	}
	if err := s.saveCall(ctx, call, terminateEvents...); err != nil {
		return err
	}
	s.removeCall(callID)
	return nil
}

// inboundRefusalReason names why a router-refused arriving call ended,
// for the persisted record. Screening refusals reuse the originated
// path's reason strings.
func inboundRefusalReason(cause ports.RefusalCause) string {
	switch cause {
	case ports.RefusalNotFound:
		return "inbound call refused: destination not found"
	case ports.RefusalUnavailable:
		return "inbound call refused: no capacity"
	case ports.RefusalForbidden:
		return "inbound call refused: compliance rejected the call"
	default:
		return "inbound call refused"
	}
}

// AnswerParticipant, HoldParticipant, and TransferParticipant are not
// implemented in this change (LLD-01 §1's out-of-scope table): both legs
// connect via ParticipantAnswered reacting to real ARI events, not an
// explicit answer command, and hold/transfer are separate later changes
// against the same ports.

func (s *Service) AnswerParticipant(context.Context, shareddomain.CallID, shareddomain.ParticipantID) error {
	return fmt.Errorf("AnswerParticipant: %w", ErrNotImplemented)
}

func (s *Service) HoldParticipant(context.Context, shareddomain.CallID, shareddomain.ParticipantID) error {
	return fmt.Errorf("HoldParticipant: %w", ErrNotImplemented)
}

func (s *Service) TransferParticipant(context.Context, ports.TransferCommand) error {
	return fmt.Errorf("TransferParticipant: %w", ErrNotImplemented)
}

// HangupCall ends a Call: records final usage and disconnects every
// still-Connected participant, then finalizes to Terminated. Idempotent
// against events that arrive afterward — DisconnectParticipant/Terminate
// both reject a Terminated call (domain.ErrCallTerminated), which
// ParticipantLeft logs and treats as a no-op, not a fatal error.
func (s *Service) HangupCall(ctx context.Context, callID shareddomain.CallID, reason string) error {
	call, ok := s.loadCall(callID)
	if !ok {
		return fmt.Errorf("hangup call %s: %w", callID, ErrCallNotFound)
	}
	if call.State == domain.CallTerminated {
		return domain.ErrCallTerminated
	}

	// The legs still on the engine, resolved to channels NOW: before the
	// loop below marks the connected ones Disconnected, and before
	// finalizeTermination forgets an arrived caller's channel (removeCall
	// deletes it — resolving afterwards silently fell back to the wrong
	// channel and left an arrived caller on the line).
	var live []ports.ChannelRef
	for _, p := range call.Participants {
		if p.State != domain.ParticipantDisconnected {
			live = append(live, s.legRef(callID, p.ID))
		}
	}

	now := time.Now().UTC()
	for _, p := range call.Participants {
		if p.State != domain.ParticipantConnected {
			continue
		}
		if err := s.recordUsageForParticipant(ctx, call, p, now); err != nil {
			s.logger.Error("record usage on hangup", "error", err, "call_id", callID, "participant_id", p.ID)
		}
		if _, err := call.DisconnectParticipant(p.ID, now); err != nil {
			s.logger.Error("disconnect participant on hangup", "error", err, "call_id", callID, "participant_id", p.ID)
		}
	}

	if err := s.finalizeTermination(ctx, call, reason); err != nil {
		return err
	}

	// Release every leg on the engine. Destroying the bridge alone left
	// both phones on the line, media still flowing, with the call recorded
	// as Terminated — found by pbx-call-placement's UAT, where a real phone
	// stayed in the call until killed. Done AFTER finalizeTermination, so
	// the ChannelDestroyed events this triggers find the call already gone
	// rather than racing a second termination. A leg the far end has just
	// hung up is already destroyed; that failure is expected and logged.
	s.releaseLegs(ctx, callID, live)
	return nil
}

// participantChannel is the engine channel a participant's leg is
// originated with. Deterministic, so a leg can be addressed by its
// participant alone (D-19 correlation).
// legRef is the engine channel of one participant: the channel an
// arrived caller came in on, or the atsa-part-<id> channel the platform
// originated for every other leg. Resolve it before finalizing a call.
func (s *Service) legRef(callID shareddomain.CallID, participantID shareddomain.ParticipantID) ports.ChannelRef {
	if a, ok := s.arrivedOf(callID); ok && a.callerParticipant == participantID {
		return a.callerRef
	}
	return participantChannel(participantID)
}

// releaseLegs hangs up channels after the call is finalized, so the
// ChannelDestroyed events find it gone rather than racing a second
// termination. A leg the far end already hung up fails; that is
// expected and only logged.
func (s *Service) releaseLegs(ctx context.Context, callID shareddomain.CallID, refs []ports.ChannelRef) {
	for _, ref := range refs {
		if err := s.mediaGateway.DestroyChannel(ctx, ref); err != nil {
			s.logger.Debug("release leg", "error", err, "call_id", callID, "channel", ref)
		}
	}
}

func participantChannel(id shareddomain.ParticipantID) ports.ChannelRef {
	return ports.ChannelRef("atsa-part-" + id.String())
}

// ParticipantAnswered implements the shape acl.EventSink declares
// (structurally — this package never imports acl). Connects the
// participant, bridges its channel into the call's bridge (creating the
// bridge on the first connect), and persists.
//
// For a call that began with an arriving channel, the answering leg is
// the originated destination: answering it also answers the waiting
// caller and bridges both, which is what moves the call to Active by
// the existing two-connected rule (design D3).
func (s *Service) ParticipantAnswered(ctx context.Context, _, callIDStr, participantIDStr, channelID string) error {
	callID, err := shareddomain.ParseCallID(callIDStr)
	if err != nil {
		return fmt.Errorf("parse call id: %w", err)
	}
	participantID, err := shareddomain.ParseParticipantID(participantIDStr)
	if err != nil {
		return fmt.Errorf("parse participant id: %w", err)
	}

	call, ok := s.loadCall(callID)
	if !ok {
		return fmt.Errorf("participant answered for call %s: %w", callID, ErrCallNotFound)
	}

	connectEvents, err := call.ConnectParticipant(participantID, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("connect participant: %w", err)
	}

	bridgeID, err := s.ensureBridge(ctx, callID)
	if err != nil {
		return fmt.Errorf("ensure bridge: %w", err)
	}
	if err := s.mediaGateway.AddChannelToBridge(ctx, bridgeID, ports.ChannelRef(channelID)); err != nil {
		return fmt.Errorf("add channel to bridge: %w", err)
	}

	if a, ok := s.arrivedOf(callID); ok && !a.answered && a.callerParticipant != participantID {
		if err := s.mediaGateway.AnswerChannel(ctx, a.callerRef); err != nil {
			return fmt.Errorf("answer arriving caller: %w", err)
		}
		callerEvents, err := call.ConnectParticipant(a.callerParticipant, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("connect arriving caller: %w", err)
		}
		if err := s.mediaGateway.AddChannelToBridge(ctx, bridgeID, a.callerRef); err != nil {
			return fmt.Errorf("add arriving caller to bridge: %w", err)
		}
		connectEvents = append(connectEvents, callerEvents...)
		s.markArrivedAnswered(callID)
	}

	return s.saveCall(ctx, call, connectEvents...)
}

// ParticipantLeft implements the shape acl.EventSink declares. A
// participant leaving before the call ever reached Active (e.g. an
// unanswered leg torn down after the alerting timeout) terminates the
// whole call directly, matching the domain model's "destination did not
// answer" path rather than DisconnectParticipant's mid-call path.
func (s *Service) ParticipantLeft(ctx context.Context, _, callIDStr, participantIDStr string) error {
	callID, err := shareddomain.ParseCallID(callIDStr)
	if err != nil {
		return fmt.Errorf("parse call id: %w", err)
	}
	participantID, err := shareddomain.ParseParticipantID(participantIDStr)
	if err != nil {
		return fmt.Errorf("parse participant id: %w", err)
	}

	call, ok := s.loadCall(callID)
	if !ok {
		return fmt.Errorf("participant left for call %s: %w", callID, ErrCallNotFound)
	}

	if call.State != domain.CallActive && call.State != domain.CallDegraded {
		// A leg that never reached Active leaves nothing to bill and
		// no bridge worth keeping. For an arrived call, the other leg
		// is still ringing on the engine — the caller hanging up while
		// ringing must release the destination leg (and vice versa),
		// or a phone rings forever for a call that no longer exists.
		// Originated calls keep their existing behavior: both legs
		// are torn down by HangupCall, never here.
		s.releaseArrivedSetupLegs(ctx, call, participantID)
		return s.finalizeTermination(ctx, call, "call setup did not complete")
	}

	target := findParticipant(call, participantID)
	if target == nil {
		return fmt.Errorf("participant %s not found on call %s", participantID, callID)
	}

	now := time.Now().UTC()
	if target.State == domain.ParticipantConnected {
		if err := s.recordUsageForParticipant(ctx, call, target, now); err != nil {
			s.logger.Error("record usage on participant left", "error", err, "call_id", callID, "participant_id", participantID)
		}
	}

	disconnectEvents, err := call.DisconnectParticipant(participantID, now)
	if err != nil {
		return fmt.Errorf("disconnect participant: %w", err)
	}
	if err := s.saveCall(ctx, call, disconnectEvents...); err != nil {
		return err
	}

	if call.State == domain.CallTerminating {
		return s.endForRemaining(ctx, call, now)
	}
	return nil
}

// releaseArrivedSetupLegs releases, for an arrived call that never
// reached Active, every other leg still on the engine: the caller hanging
// up while ringing must release the destination leg (and vice versa), or
// a phone rings forever for a call that no longer exists. Originated
// calls are not arrived calls; HangupCall tears down both their legs.
func (s *Service) releaseArrivedSetupLegs(ctx context.Context, call *domain.Call, left shareddomain.ParticipantID) {
	a, ok := s.arrivedOf(call.ID)
	if !ok {
		return
	}
	for _, p := range call.Participants {
		if p.ID == left || p.State == domain.ParticipantDisconnected {
			continue
		}
		ref := participantChannel(p.ID)
		if a.callerParticipant == p.ID {
			ref = a.callerRef
		}
		if err := s.mediaGateway.DestroyChannel(ctx, ref); err != nil {
			s.logger.Debug("destroy other leg on arrived setup teardown",
				"error", err, "call_id", call.ID, "participant_id", p.ID)
		}
	}
}

// findParticipant returns the call's participant with id, or nil.
func findParticipant(call *domain.Call, id shareddomain.ParticipantID) *domain.CallParticipant {
	for _, p := range call.Participants {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// endForRemaining ends a call whose last departure left fewer than two
// connected: the call is over for everyone still on it. It records their
// usage up to now, marks them left, finalizes, then releases their legs on
// the engine — otherwise the other party sits on a dead bridge in silence,
// accruing usage.
func (s *Service) endForRemaining(ctx context.Context, call *domain.Call, now time.Time) error {
	var remaining []ports.ChannelRef
	var leftEvents []event.DomainEvent
	for _, p := range call.Participants {
		if p.State != domain.ParticipantConnected {
			continue
		}
		if err := s.recordUsageForParticipant(ctx, call, p, now); err != nil {
			s.logger.Error("record usage on call end", "error", err, "call_id", call.ID, "participant_id", p.ID)
		}
		remaining = append(remaining, s.legRef(call.ID, p.ID))
		evs, err := call.DisconnectParticipant(p.ID, now)
		if err != nil {
			return fmt.Errorf("disconnect remaining participant: %w", err)
		}
		leftEvents = append(leftEvents, evs...)
	}
	if len(leftEvents) > 0 {
		if err := s.saveCall(ctx, call, leftEvents...); err != nil {
			return err
		}
	}
	if err := s.finalizeTermination(ctx, call, "normal clearing"); err != nil {
		return err
	}
	s.releaseLegs(ctx, call.ID, remaining)
	return nil
}

// originateParticipant asks the MediaGateway to place one leg, registers
// its correlation (D-19) so the event loop can find its way back to
// tenantID/callID/participantID, and records a channel-history entry.
// bridgeID is intentionally empty here: no bridge exists yet at
// origination time — ensureBridge creates it on the first connect.
func (s *Service) originateParticipant(ctx context.Context, tenantID shareddomain.TenantID, callID shareddomain.CallID, p *domain.CallParticipant) error {
	req := ports.OriginateRequest{
		EndpointURI: p.EndpointURI,
		ChannelID:   string(participantChannel(p.ID)),
		Variables: map[string]string{
			"ATSA_TENANT_ID":      tenantID.String(),
			"ATSA_CALL_ID":        callID.String(),
			"ATSA_PARTICIPANT_ID": p.ID.String(),
		},
		TimeoutSeconds: s.timeoutSeconds,
	}
	channelRef, err := s.mediaGateway.Originate(ctx, req)
	if err != nil {
		return fmt.Errorf("originate: %w", err)
	}

	s.registrar.RegisterCorrelation(string(channelRef), tenantID.String(), callID.String(), p.ID.String())

	if err := s.callStore.AddChannelHistory(ctx, tenantID, p.ID, channelRef, "", "originated"); err != nil {
		return fmt.Errorf("add channel history: %w", err)
	}
	return nil
}

// ensureBridge returns the call's bridge, creating it on first use.
func (s *Service) ensureBridge(ctx context.Context, callID shareddomain.CallID) (ports.BridgeID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id, ok := s.bridges[callID.String()]; ok {
		return id, nil
	}
	bridgeID, err := s.mediaGateway.CreateBridge(ctx, "mixing")
	if err != nil {
		return "", err
	}
	s.bridges[callID.String()] = bridgeID
	return bridgeID, nil
}

// finalizeTermination destroys the call's bridge (if one was ever
// created — a call screened out or never answered has none) and moves
// the domain Call to Terminated. A bridge-destroy failure is logged, not
// returned: the domain record reaching Terminated must not depend on
// infrastructure cleanup succeeding (INV-03's spirit applied to
// teardown, not just to active calls).
func (s *Service) finalizeTermination(ctx context.Context, call *domain.Call, reason string) error {
	s.mu.Lock()
	bridgeID, hasBridge := s.bridges[call.ID.String()]
	delete(s.bridges, call.ID.String())
	s.mu.Unlock()

	if hasBridge {
		if err := s.mediaGateway.DestroyBridge(ctx, bridgeID); err != nil {
			s.logger.Error("destroy bridge on termination", "error", err, "call_id", call.ID, "bridge_id", bridgeID)
		}
	}

	terminateEvents, err := call.Terminate(reason, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("terminate call: %w", err)
	}
	if err := s.saveCall(ctx, call, terminateEvents...); err != nil {
		return err
	}

	// Return the channel this call reserved at Screening.
	//
	// Issued here and nowhere else: finalizeTermination is the single
	// funnel every termination route reaches, and it is already guarded
	// by the CallTerminated check above, so a second termination signal
	// for an already-terminated call never arrives here.
	//
	// The correctness of the count does NOT depend on that guard
	// holding, which is the point of keying the reservation by call
	// (D-58). A release for a call holding nothing is a no-op, so a
	// screened-out call that never reserved frees nothing, and a future
	// termination path added by someone who has not read this comment
	// cannot double-decrement. Relying on the guard alone would fail in
	// the expensive direction: under-counting permits calls that should
	// be refused, which is licence leakage nothing surfaces.
	//
	// A failure here is logged rather than returned: the call has
	// already terminated, its events are persisted, and refusing to
	// finish teardown over a counter would turn a licensing hiccup into
	// a stuck call.
	if err := s.license.ReleaseCapacity(ctx, call.ID.String()); err != nil {
		s.logger.Error("release capacity on termination", "error", err, "call_id", call.ID)
	}

	s.removeCall(call.ID)
	return nil
}

// recordUsageForParticipant persists one batch of per-second usage ticks
// for p's just-ended connected interval [p.AnsweredAt, until). Batched
// at disconnect/termination time, not streamed every second during the
// call — a deliberate scope choice for this walking skeleton (see
// design.md); D-24's seam is satisfied because the ticks themselves are
// still continuous and non-duplicated (domain.GenerateUsageTicks), just
// persisted together rather than one write per elapsed second.
func (s *Service) recordUsageForParticipant(ctx context.Context, call *domain.Call, p *domain.CallParticipant, until time.Time) error {
	if p.AnsweredAt == nil {
		return nil
	}
	ticks := domain.GenerateUsageTicks(p, call.ID, call.TenantID, call.Direction, *p.AnsweredAt, until)
	if len(ticks) == 0 {
		return nil
	}
	return s.callStore.RecordUsageTicks(ctx, ticks)
}

// saveCall persists the call's current state and enqueues any events
// produced by the transition that led here to the transactional outbox,
// atomically (CallStore.SaveCall's contract) — there is no separate
// publish step here; the outbox worker (task 7.2) does that
// asynchronously, independent of this call ever returning.
func (s *Service) saveCall(ctx context.Context, call *domain.Call, events ...event.DomainEvent) error {
	if err := s.callStore.SaveCall(ctx, call, events); err != nil {
		return fmt.Errorf("save call: %w", err)
	}
	return nil
}

func (s *Service) storeCall(call *domain.Call) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[call.ID.String()] = call
}

func (s *Service) loadCall(id shareddomain.CallID) (*domain.Call, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.calls[id.String()]
	return c, ok
}

func (s *Service) removeCall(id shareddomain.CallID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.calls, id.String())
	delete(s.arrived, id.String())
}
