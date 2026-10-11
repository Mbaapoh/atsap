package acl

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"atsap-api/internal/telephony/acl/ari"
)

// EventSink receives the ACL's interpretation of raw Asterisk events,
// translated from Asterisk-channel terms into Participant terms. The
// CallService orchestrator implements this — using only plain strings
// (not the acl-defined Correlation type) so application can satisfy this
// interface structurally without ever importing acl
// (docs/hld/01-architecture.md §1.2: application depends only on domain
// and ports).
type EventSink interface {
	// ParticipantAnswered is called when a channel backing a participant
	// reaches the Up state (answered). channelID is passed through
	// because bridging a newly answered leg into the call's bridge
	// (MediaGateway.AddChannelToBridge) needs it — the orchestrator, not
	// this package, decides what to do with it.
	ParticipantAnswered(ctx context.Context, tenantID, callID, participantID, channelID string) error
	// ParticipantLeft is called when a channel backing a participant is
	// destroyed or hung up.
	ParticipantLeft(ctx context.Context, tenantID, callID, participantID string) error
	// CallArrived is called when a channel enters Stasis with no
	// correlation — a call the platform did not originate (a carrier
	// INVITE, or a registered phone dialling). tenantID is the
	// endpoint-configured ATSA_TENANT_ID, empty when the channel
	// carries none; the orchestrator refuses a tenantless channel
	// without consulting its router.
	CallArrived(ctx context.Context, tenantID, sourceEndpoint, dialled, callerNumber, channelID string) error
}

// ChannelVariableReader reads a variable off a live engine channel. It
// is the narrow slice of the ARI client the event loop needs to learn
// an uncorrelated channel's tenant (design D1); *ari.Client satisfies
// it, and tests substitute a map.
type ChannelVariableReader interface {
	GetChannelVariable(ctx context.Context, channelID, name string) (string, error)
}

// EventLoop dispatches raw ARI events through a CorrelationRegistry
// lookup to an EventSink (docs/hld/01-architecture.md §2; LLD-01 §4.4).
//
// ParticipantAnswered fires on StasisStart, not ChannelStateChange(Up).
// Both legs of a call are originated by us (InitiateCall), so
// correlation is registered synchronously right after Originate returns
// — this loop does not need StasisStart to learn the CallID/
// ParticipantID. It needs StasisStart for timing instead: against real
// Asterisk, an ARI-originated channel reaches Up (the underlying dial
// answers) *before* Stasis takes control of it, and Asterisk expects the
// SDP offer/answer cycle for the Stasis-controlled channel to complete
// essentially immediately once that handover happens. Bridging on
// ChannelStateChange(Up) instead was tried first and observed to lose
// that race against real Asterisk: the extra round trip (event decode +
// ensureBridge + AddChannelToBridge) was consistently too slow, and
// Asterisk hung up the channel itself with cause 127 ("Interworking,
// unspecified"), BYE Reason "SDP offer/answer incomplete", before our
// bridge-add call ever landed (task 10.2's e2e test surfaced this — the
// task 5.6 unit test's scripted event sequence never modeled Asterisk's
// actual StasisStart-after-Up ordering or this timing pressure).
// StasisStart fires once per channel and only after Up, so reacting to
// it alone is both simpler and faster than also watching
// ChannelStateChange.
type EventLoop struct {
	registry *CorrelationRegistry
	sink     EventSink
	vars     ChannelVariableReader
	logger   *slog.Logger
}

// NewEventLoop returns an EventLoop dispatching through registry to sink.
func NewEventLoop(registry *CorrelationRegistry, sink EventSink, logger *slog.Logger) *EventLoop {
	return &EventLoop{registry: registry, sink: sink, logger: logger}
}

// SetChannelVariableReader wires the reader uncorrelated StasisStarts
// need for their tenant. The composition root sets the ARI client;
// without one, arriving channels carry no tenant and are refused.
func (l *EventLoop) SetChannelVariableReader(r ChannelVariableReader) {
	l.vars = r
}

// Handle processes one raw ARI event. Its signature matches
// ari.EventHandler once wrapped in a closure over ctx, the same pattern
// api/cmd/atsap-api/main.go uses when it calls eventLoop.Handle from its
// ari.Client.StreamEvents callback.
func (l *EventLoop) Handle(ctx context.Context, ev ari.Event) {
	switch ev.Type {
	case "StasisStart":
		l.handleStasisStart(ctx, ev)
	case "ChannelDestroyed", "ChannelHangupRequest":
		l.handleChannelGone(ctx, ev)
	}
}

type channelPayload struct {
	Channel struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		State string `json:"state"`
		Dialplan struct {
			Exten string `json:"exten"`
		} `json:"dialplan"`
		Caller struct {
			Number string `json:"number"`
		} `json:"caller"`
	} `json:"channel"`
}

func (l *EventLoop) handleStasisStart(ctx context.Context, ev ari.Event) {
	var payload channelPayload
	if err := json.Unmarshal(ev.Raw, &payload); err != nil {
		l.logger.Warn("acl: failed to decode StasisStart", "error", err)
		return
	}

	corr, ok := l.registry.Lookup(payload.Channel.ID)
	if !ok {
		l.handleArrivingChannel(ctx, payload.Channel.ID, payload.Channel.Name,
			payload.Channel.Dialplan.Exten, payload.Channel.Caller.Number)
		return
	}

	if err := l.sink.ParticipantAnswered(ctx, corr.TenantID, corr.CallID, corr.ParticipantID, payload.Channel.ID); err != nil {
		l.logger.Error("acl: participant-answered handler failed", "error", err,
			"call_id", corr.CallID, "participant_id", corr.ParticipantID)
	}
}

// handleArrivingChannel routes a StasisStart no originated leg owns to
// the arriving-call path (design D3): the channel stays unanswered while
// the orchestrator decides what it is. The tenant comes from the
// endpoint's configuration (ATSA_TENANT_ID, design D1) — never from a
// caller-controlled header — read off the live channel; the endpoint
// from the channel name; the dialled number from the dialplan extension;
// the caller number from the caller ID.
func (l *EventLoop) handleArrivingChannel(ctx context.Context, channelID, channelName, dialled, callerNumber string) {
	tenantID := ""
	if l.vars != nil {
		v, err := l.vars.GetChannelVariable(ctx, channelID, "ATSA_TENANT_ID")
		if err != nil {
			l.logger.Warn("acl: cannot read tenant of arriving channel; refusing without routing",
				"error", err, "channel_id", channelID)
		} else {
			tenantID = v
		}
	}
	source := sourceEndpointFromChannelName(channelName)
	if err := l.sink.CallArrived(ctx, tenantID, source, dialled, callerNumber, channelID); err != nil {
		l.logger.Error("acl: arrived-call handler failed", "error", err, "channel_id", channelID)
	}
}

// sourceEndpointFromChannelName recovers the endpoint an arriving
// channel was created on from its Asterisk name ("PJSIP/<endpoint>-<id>")
// by dropping the technology prefix and the unique suffix Asterisk
// appends after the last dash. Anything unparseable passes through:
// the router refuses unknown sources, so a guess here would only move
// the refusal somewhere harder to see.
func sourceEndpointFromChannelName(name string) string {
	s := strings.TrimPrefix(name, "PJSIP/")
	if i := strings.LastIndex(s, "-"); i >= 0 {
		s = s[:i]
	}
	return s
}

func (l *EventLoop) handleChannelGone(ctx context.Context, ev ari.Event) {
	var payload channelPayload
	if err := json.Unmarshal(ev.Raw, &payload); err != nil {
		l.logger.Warn("acl: failed to decode channel-gone event", "error", err)
		return
	}

	corr, ok := l.registry.Lookup(payload.Channel.ID)
	if !ok {
		// Already removed (e.g. ChannelDestroyed following a
		// ChannelHangupRequest for the same channel), or a channel this
		// package doesn't track. Not an error.
		return
	}
	l.registry.Remove(payload.Channel.ID)

	if err := l.sink.ParticipantLeft(ctx, corr.TenantID, corr.CallID, corr.ParticipantID); err != nil {
		l.logger.Error("acl: participant-left handler failed", "error", err,
			"call_id", corr.CallID, "participant_id", corr.ParticipantID)
	}
}
