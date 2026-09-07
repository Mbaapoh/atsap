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
	// timeoutSeconds is the alerting timeout passed to MediaGateway.Originate
	// (PRD §11.1 Presenting; docs/hld/03-domain-model.md §2.1 default 20s).
	timeoutSeconds int
	logger         *slog.Logger

	mu      sync.Mutex
	calls   map[string]*domain.Call
	bridges map[string]ports.BridgeID
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
		timeoutSeconds: timeoutSeconds,
		logger:         logger,
		calls:          make(map[string]*domain.Call),
		bridges:        make(map[string]ports.BridgeID),
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

	capVerdict, err := s.license.ValidateCapacity(ctx, cmd.TenantID, 1)
	if err != nil {
		return shareddomain.CallID{}, fmt.Errorf("validate capacity: %w", err)
	}
	compVerdict, err := s.compliance.Evaluate(ctx, cmd.TenantID, cmd.DestNumber)
	if err != nil {
		return shareddomain.CallID{}, fmt.Errorf("evaluate compliance: %w", err)
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

	return s.finalizeTermination(ctx, call, reason)
}

// ParticipantAnswered implements the shape acl.EventSink declares
// (structurally — this package never imports acl). Connects the
// participant, bridges its channel into the call's bridge (creating the
// bridge on the first connect), and persists.
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
		return s.finalizeTermination(ctx, call, "call setup did not complete")
	}

	var target *domain.CallParticipant
	for _, p := range call.Participants {
		if p.ID == participantID {
			target = p
			break
		}
	}
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
		return s.finalizeTermination(ctx, call, "normal clearing")
	}
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
		ChannelID:   "atsa-part-" + p.ID.String(),
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
}
