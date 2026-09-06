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

// MediaGateway is telephony-core's outbound port onto the Asterisk ACL —
// exactly docs/hld/04-bounded-contexts.md §1's interface. StartSnoop is
// declared but not implemented in this change (returns an error);
// ai-pipeline (LLD-05) is its first caller.
type MediaGateway interface {
	Originate(ctx context.Context, req OriginateRequest) (ChannelRef, error)
	CreateBridge(ctx context.Context, bridgeType string) (BridgeID, error)
	AddChannelToBridge(ctx context.Context, bridgeID BridgeID, channelRef ChannelRef) error
	StartPlayback(ctx context.Context, channelRef ChannelRef, mediaURI string) error
	StartSnoop(ctx context.Context, channelRef ChannelRef, snoopReq SnoopRequest) (SnoopRef, error)
	DestroyChannel(ctx context.Context, channelRef ChannelRef) error
}

// CallStore is telephony-core's outbound persistence port.
type CallStore interface {
	SaveCall(ctx context.Context, call *domain.Call) error
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

// EventPublisher is telephony-core's outbound port for domain events,
// implemented by the transactional outbox writer (internal/postgres,
// internal/nats).
type EventPublisher interface {
	Publish(ctx context.Context, tenantID shareddomain.TenantID, aggregateID string, events []event.DomainEvent) error
}
