package acl

import (
	"context"
	"encoding/json"
	"log/slog"

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
	logger   *slog.Logger
}

// NewEventLoop returns an EventLoop dispatching through registry to sink.
func NewEventLoop(registry *CorrelationRegistry, sink EventSink, logger *slog.Logger) *EventLoop {
	return &EventLoop{registry: registry, sink: sink, logger: logger}
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
		State string `json:"state"`
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
		l.logger.Warn("acl: StasisStart for unregistered channel", "channel_id", payload.Channel.ID)
		return
	}

	if err := l.sink.ParticipantAnswered(ctx, corr.TenantID, corr.CallID, corr.ParticipantID, payload.Channel.ID); err != nil {
		l.logger.Error("acl: participant-answered handler failed", "error", err,
			"call_id", corr.CallID, "participant_id", corr.ParticipantID)
	}
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
