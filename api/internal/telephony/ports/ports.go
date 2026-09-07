// Package ports defines telephony-core's hexagonal port interfaces —
// exactly the shapes fixed in docs/hld/04-bounded-contexts.md §1. This
// package has no implementation: application (inbound) and acl/postgres
// (outbound) implement these interfaces; domain and application depend
// only on this package, never on a concrete adapter (docs/hld/01-architecture.md §1.2).
package ports

import (
	"context"

	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/shared/event"
	"atsap-api/internal/telephony/domain"
)

// InitiateCallCommand carries what's needed to originate a call.
type InitiateCallCommand struct {
	TenantID     shareddomain.TenantID
	Direction    domain.Direction
	SourceNumber string
	DestNumber   string
	// SourceEndpointURI and DestEndpointURI identify the PJSIP endpoints
	// to originate/answer, e.g. "PJSIP/1000". telephony-core is
	// transport-agnostic beyond this string; pbx-core resolves real
	// numbers to endpoints in later work (LLD-01 §1).
	SourceEndpointURI string
	DestEndpointURI   string
}

// TransferCommand is not implemented in this change (LLD-01 §1
// out-of-scope table) — the type exists so CallService's shape matches
// docs/hld/04-bounded-contexts.md §1 now, without a later breaking change.
type TransferCommand struct {
	CallID             shareddomain.CallID
	ParticipantID      shareddomain.ParticipantID
	NewDestEndpointURI string
}

// CallService is telephony-core's inbound port: the application-layer
// API other contexts and the ConnectRPC handlers call.
type CallService interface {
	InitiateCall(ctx context.Context, cmd InitiateCallCommand) (shareddomain.CallID, error)
	AnswerParticipant(ctx context.Context, callID shareddomain.CallID, participantID shareddomain.ParticipantID) error
	HoldParticipant(ctx context.Context, callID shareddomain.CallID, participantID shareddomain.ParticipantID) error
	TransferParticipant(ctx context.Context, cmd TransferCommand) error
	HangupCall(ctx context.Context, callID shareddomain.CallID, reason string) error
}

// ChannelRef identifies an Asterisk channel to the ACL only. It never
// crosses into domain, application-layer return values exposed to other
// contexts, or any API/event payload (docs/hld/01-architecture.md §1.2
// rule 3; spec: "No infrastructure channel identifier is ever exposed").
type ChannelRef string

// BridgeID identifies an Asterisk bridge, ACL-internal only.
type BridgeID string

// SnoopRequest configures an audio snoop channel (ai-pipeline, not used
// in this change — see MediaGateway.StartSnoop).
type SnoopRequest struct {
	Direction string
}

// SnoopRef identifies a snoop channel, ACL-internal only.
type SnoopRef string

// OriginateRequest carries what the ACL needs to place one leg.
type OriginateRequest struct {
	EndpointURI string
	CallerID    string
	ChannelID   string
	Variables   map[string]string
	// TimeoutSeconds bounds how long Asterisk alerts before giving up
	// (PRD §11.1 Presenting; docs/hld/03-domain-model.md §2.1). Zero
	// means Asterisk's own default.
	TimeoutSeconds int
}

// MediaGateway is telephony-core's outbound port onto the media engine,
// today implemented by the Asterisk ACL — exactly
// docs/hld/04-bounded-contexts.md §1's interface. StartSnoop is
// declared but not implemented in this change (returns an error);
// ai-pipeline (LLD-05) is its first caller.
//
// Engine capability contract (DECISIONS D-41): this interface is written
// in domain capabilities, never engine verbs, so a future engine can
// implement it without reshaping. Any implementation must additionally
// satisfy all four clauses, verified by the mediatest conformance suite
// (api/internal/telephony/mediatest):
//
//  1. Caller-supplied correlation: Originate must accept an
//     orchestrator-chosen identifier (OriginateRequest.ChannelID plus
//     Variables) and return it on every subsequent event for that leg.
//     Matching by dialled-number-plus-timestamp is forbidden (D-19) —
//     an engine without token support fails the contract at review.
//  2. Fixed event vocabulary: the engine translates its native events
//     into ParticipantAnswered/ParticipantLeft-shaped signals inside its
//     own acl/<engine> package. New event kinds are never added here to
//     suit an engine.
//  3. Metering from events alone: per-second per-participant usage and
//     CDR-grade detail must be derivable from answer/hangup/transfer
//     events. An engine that cannot report these cannot bill.
//  4. Degraded paths per primitive: anything the engine cannot do
//     (e.g. no audio tap) degrades by configuration — never by
//     orchestrator branching. See the capability checklist in
//     docs/hld/01-architecture.md §2.
type MediaGateway interface {
	Originate(ctx context.Context, req OriginateRequest) (ChannelRef, error)
	CreateBridge(ctx context.Context, bridgeType string) (BridgeID, error)
	AddChannelToBridge(ctx context.Context, bridgeID BridgeID, channelRef ChannelRef) error
	DestroyBridge(ctx context.Context, bridgeID BridgeID) error
	StartPlayback(ctx context.Context, channelRef ChannelRef, mediaURI string) error
	StartSnoop(ctx context.Context, channelRef ChannelRef, snoopReq SnoopRequest) (SnoopRef, error)
	DestroyChannel(ctx context.Context, channelRef ChannelRef) error
}

// CallStore is telephony-core's outbound persistence port.
type CallStore interface {
	// SaveCall persists call and enqueues events to the transactional
	// outbox in the SAME database transaction (docs/hld/01-architecture.md
	// §3.2) — this is what "transactional outbox" means: the domain
	// write and the outbox write commit or roll back together. The
	// outbox worker (a separate background component behind
	// EventPublisher below, not CallService) publishes queued events to
	// NATS asynchronously afterward.
	SaveCall(ctx context.Context, call *domain.Call, events []event.DomainEvent) error
	GetCall(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.CallID) (*domain.Call, error)
	AddChannelHistory(ctx context.Context, tenantID shareddomain.TenantID, participantID shareddomain.ParticipantID, channelRef ChannelRef, bridgeID BridgeID, eventType string) error
	RecordUsageTicks(ctx context.Context, ticks []domain.UsageTick) error
}

// CapacityVerdict is LicenseManager's answer to a capacity check.
type CapacityVerdict struct {
	Permitted bool
	Reason    string
}

// LicenseManager is the Screening-state capacity port. This change wires
// it to an always-permit stub (application/stub_license.go); LLD-02
// replaces the adapter behind this same interface.
type LicenseManager interface {
	ValidateCapacity(ctx context.Context, tenantID shareddomain.TenantID, requestedChannels int) (CapacityVerdict, error)
}

// ComplianceVerdict is ComplianceEngine's answer to a compliance check.
type ComplianceVerdict struct {
	Permitted bool
	Reason    string
}

// ComplianceEngine is the Screening-state compliance port. This change
// wires it to an always-permit stub (application/stub_compliance.go);
// a later change replaces the adapter behind this same interface.
type ComplianceEngine interface {
	Evaluate(ctx context.Context, tenantID shareddomain.TenantID, destNumber string) (ComplianceVerdict, error)
}

// CorrelationRegistrar is the narrow slice of the ACL's correlation
// registry the application layer needs when originating a participant:
// registering which Asterisk channel now backs it (D-19). Satisfied by
// *acl.CorrelationRegistry, wired in by the composition root (cmd/) —
// application depends on this port, never on the acl package itself
// (docs/hld/01-architecture.md §1.2).
type CorrelationRegistrar interface {
	RegisterCorrelation(channelID, tenantID, callID, participantID string)
}

// Publishing events to NATS is not a telephony-core port: CallService
// only ever calls CallStore.SaveCall, which enqueues events to the
// outbox transactionally (see CallStore's doc comment above). The
// worker that reads the outbox and publishes to NATS asynchronously is
// generic infrastructure serving every future bounded context's outbox
// rows alike, not telephony-specific — its contract (OutboxWorker,
// OutboxPublisher) lives in internal/postgres, not here.
