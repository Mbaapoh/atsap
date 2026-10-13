// Package ports defines telephony-core's hexagonal port interfaces —
// exactly the shapes fixed in docs/hld/04-bounded-contexts.md §1. This
// package has no implementation: application (inbound) and acl/postgres
// (outbound) implement these interfaces; domain and application depend
// only on this package, never on a concrete adapter (docs/hld/01-architecture.md §1.2).
package ports

import (
	"context"
	"time"

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
	// BypassScreening skips capacity, compliance and suspension checks
	// (INV-01, LLD-03 §4.3). It is set ONLY by pbx-core's application
	// layer from its own emergency matcher (pbx/domain.IsEmergency): it
	// is not a request field, is never decoded from a wire message, and
	// any RPC handler that sets it is a defect (LLD-03 §10.1). Tests set
	// it directly to exercise telephony's enforcement contract without
	// importing pbx-core, which HLD 04 §10.1 forbids. Default false
	// preserves existing behavior.
	BypassScreening bool
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

// RefusalCause is why an arriving call is refused at the SIP edge
// (pbx-inbound-and-emergency design D2/D3). The zero value connects —
// a decision refusing nothing — so a default-built decision can never
// refuse a call by accident.
type RefusalCause string

const (
	// RefusalNone connects the call. It is the zero value, not a
	// sentinel a caller must remember to clear.
	RefusalNone RefusalCause = ""
	// RefusalNotFound is an unallocated DID or an unroutable dialled
	// number: the engine answers SIP 404 (ARI hangup reason
	// "unallocated").
	RefusalNotFound RefusalCause = "not_found"
	// RefusalUnavailable is exhausted capacity or a full trunk: the
	// engine answers SIP 503 (ARI hangup reason "congestion").
	RefusalUnavailable RefusalCause = "unavailable"
	// RefusalForbidden is a compliance refusal: the engine answers SIP
	// 403 (ARI hangup reason "rejected").
	RefusalForbidden RefusalCause = "forbidden"
)

// InboundRequest describes a channel that arrived at the engine without
// having been originated by the platform: a carrier INVITE, or a
// registered phone dialling into the Stasis context.
type InboundRequest struct {
	TenantID shareddomain.TenantID
	// CallID is assigned by the arriving-call path before the router
	// runs, so pbx-core can reserve a trunk channel keyed by it.
	CallID         shareddomain.CallID
	SourceEndpoint string
	Dialled        string
	CallerNumber   string
}

// InboundDecision is the router's answer for one arriving call.
type InboundDecision struct {
	// Refuse is RefusalNone to connect, or the cause to refuse with.
	Refuse RefusalCause
	// DestinationURI is opaque to telephony-core: the engine address of
	// the leg to originate when connecting.
	DestinationURI string
	Direction      domain.Direction
	// CallerNumber, when set, replaces the arriving channel's caller
	// number as the call's source: a desk phone presents its engine
	// endpoint, and only the router knows the extension behind it. Empty
	// keeps the channel's number (a carrier's caller).
	CallerNumber string
	// CallerName is the caller's display name, shown to the destination.
	CallerName string
	// BypassScreening skips capacity and compliance checks. Assigned only
	// in pbx-core's application layer from its own emergency matcher,
	// under the same provenance rule as InitiateCallCommand's field.
	BypassScreening bool
}

// InboundRouter is telephony-core's outbound port for resolving arriving
// calls, owned by telephony and implemented by pbx-core (LLD-03 §4.2).
// The composition root wires the implementation; telephony ships and
// runs with NoInboundRouter.
type InboundRouter interface {
	ResolveInbound(ctx context.Context, req InboundRequest) (InboundDecision, error)
}

// NoInboundRouter is telephony-core's default router: every arriving
// call is refused as not found. It is what lets telephony run with no
// pbx-core in the binary (LLD-03 DoD 15) — the walking skeleton and the
// licence-transition suites run against exactly this.
type NoInboundRouter struct{}

// ResolveInbound refuses every arriving call as not found.
func (NoInboundRouter) ResolveInbound(_ context.Context, _ InboundRequest) (InboundDecision, error) {
	return InboundDecision{Refuse: RefusalNotFound}, nil
}

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
// ai-pipeline (LLD-10) is its first caller.
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
	// RingChannel sends ringing (SIP 180) on an arriving channel the
	// platform did not originate, while the destination leg is set up.
	RingChannel(ctx context.Context, channelRef ChannelRef) error
	// AnswerChannel answers an arriving channel, connecting its audio.
	AnswerChannel(ctx context.Context, channelRef ChannelRef) error
	// RejectChannel refuses an arriving channel at the SIP edge with
	// the standard response for cause (PRD §12.4): NotFound → 404,
	// Unavailable → 503, Forbidden → 403. Never silence.
	RejectChannel(ctx context.Context, channelRef ChannelRef, cause RefusalCause) error
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

// LicenseManager is the capacity port telephony-core consumes. It is
// the CONSUMER's narrow view — the two methods this context actually
// calls — not the provider's full contract, which lives in
// licensing/ports and has four (LLD-08 §3.1). Keeping them separate is
// what stops licensing importing telephony/ports for CapacityVerdict,
// which HLD 04 §10.1 denies; cmd/atsap-api adapts between the two.
//
// # Why this interface changed, and why that was allowed
//
// LLD-08 §1 originally forbade ANY edit under internal/telephony/. That
// tripwire fired during licensing-capacity-grace, for a reason no
// document anticipated: ReleaseCapacity had zero occurrences in the
// codebase. It was named in HLD 04 §4 and in licensing's port, but this
// interface declared only ValidateCapacity and nothing anywhere
// released. LLD-01 built the half of the seam its always-permit stub
// needed, and a real counter against that code produces a number that
// only rises — until every call in the installation is refused and none
// is ever dropped to correct it.
//
// The two ways to avoid touching telephony-core are both forbidden by
// HLD 04 §10.1, which gives licensing "may depend on: nothing":
// subscribing to call-lifecycle events, or reading telephony-core's
// tables. Shipping a counter without release is a defect, not a
// limitation.
//
// So the rule was narrowed rather than waived: telephony-core may gain
// port methods it owns and their call sites, and nothing else. The seam's
// direction is untouched — this context still calls a port it owns and
// still never imports licensing, which is what the tripwire actually
// existed to protect.
type LicenseManager interface {
	// ValidateCapacity reserves a channel for callID.
	//
	// callID is the second half of the same correction. A reservation
	// keyed by call is what lets ReleaseCapacity be idempotent instead
	// of a blind decrement (D-58), and the failure direction is why it
	// matters: a decrement running twice UNDER-counts, so the
	// installation permits calls it should refuse. That is licence
	// leakage no test notices, because everything continues to work,
	// and nothing surfaces until a partner is running more channels
	// than they bought.
	ValidateCapacity(ctx context.Context, tenantID shareddomain.TenantID, callID string, requestedChannels int) (CapacityVerdict, error)

	// ReleaseCapacity returns callID's reservation.
	//
	// Idempotent by contract: releasing a call that holds nothing —
	// because it was refused at Screening, because it already released,
	// or because a second termination signal arrived — is a no-op and
	// not an error. Implementations must not rely on the caller
	// releasing exactly once.
	ReleaseCapacity(ctx context.Context, callID string) error
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
// rows alike, not telephony-specific — so OutboxPublisher lives in
// internal/shared/ports (D-57) and OutboxWorker in internal/postgres,
// neither of them here.

// CallFilter narrows a call listing. Zero values mean "any".
type CallFilter struct {
	State     domain.CallState
	Direction domain.Direction
	From, To  time.Time // started_at in [From, To); zero = unbounded
}

// CallSummary is one row of a call listing: platform fields only. It has
// no participant, channel, bridge or endpoint field, so no engine
// identifier can be listed (console-apis-phase-a design D5).
type CallSummary struct {
	ID                shareddomain.CallID
	Direction         domain.Direction
	State             domain.CallState
	SourceNumber      string
	DestNumber        string
	StartedAt         time.Time
	AnsweredAt        *time.Time
	EndedAt           *time.Time
	TerminationReason string
}

// CallLister reads a tenant's calls for the call-history screen. Separate
// from CallStore, which the call path depends on: a read-only listing has
// no business widening the interface call setup is written against.
type CallLister interface {
	ListCalls(ctx context.Context, tenantID shareddomain.TenantID, filter CallFilter, limit, offset int) ([]CallSummary, error)
}
