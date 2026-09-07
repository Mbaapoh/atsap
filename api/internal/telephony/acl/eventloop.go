package acl

import (
	"context"
	"encoding/json"
	"log/slog"

	"atsap-api/internal/telephony/acl/ari"
)

// EventSink receives the ACL's interpretation of raw Asterisk events,
// translated from Asterisk-channel terms into Participant terms. The
// CallService orchestrator implements this; the ACL package itself knows
// nothing about persistence or domain state beyond the Correlation it
// already tracks (docs/hld/01-architecture.md §1.2 — no raw telephony
// concepts leak past this package).
type EventSink interface {
	// ParticipantAnswered is called when a channel backing a participant
	// reaches the Up state (answered). channelID is passed through (not
	// just the Correlation) because bridging a newly answered leg into
	// the call's bridge (MediaGateway.AddChannelToBridge) needs it — the
	// orchestrator, not this package, decides what to do with it.
	ParticipantAnswered(ctx context.Context, corr Correlation, channelID string) error
	// ParticipantLeft is called when a channel backing a participant is
	// destroyed or hung up.
	ParticipantLeft(ctx context.Context, corr Correlation) error
}

// EventLoop dispatches raw ARI events through a CorrelationRegistry
// lookup to an EventSink (docs/hld/01-architecture.md §2; LLD-01 §4.4).
//
// StasisStart is deliberately not handled here: in this change, both
// legs of a call are originated by us (InitiateCall), so correlation is
// registered synchronously right after Originate returns — the
// orchestrator already knows the CallID/ParticipantID before the channel
// exists, it does not need to learn them from StasisStart. StasisStart's
// only other possible job here (answering an alerting channel) is not
// needed either: Asterisk answers an originated channel automatically
// when the destination picks up, reported via ChannelStateChange(Up),
// which this loop does handle. A later change (real inbound calls
// through pbx-core) is what gives StasisStart a job.
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
// api/cmd/atsap-api/main.go already uses for handleARIEvent.
func (l *EventLoop) Handle(ctx context.Context, ev ari.Event) {
	switch ev.Type {
	case "ChannelStateChange":
		l.handleChannelStateChange(ctx, ev)
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

func (l *EventLoop) handleChannelStateChange(ctx context.Context, ev ari.Event) {
	var payload channelPayload
	if err := json.Unmarshal(ev.Raw, &payload); err != nil {
		l.logger.Warn("acl: failed to decode ChannelStateChange", "error", err)
		return
	}
	if payload.Channel.State != "Up" {
		return
	}

	corr, ok := l.registry.Lookup(payload.Channel.ID)
	if !ok {
		l.logger.Warn("acl: ChannelStateChange for unregistered channel", "channel_id", payload.Channel.ID)
		return
	}

	if err := l.sink.ParticipantAnswered(ctx, corr, payload.Channel.ID); err != nil {
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

	if err := l.sink.ParticipantLeft(ctx, corr); err != nil {
		l.logger.Error("acl: participant-left handler failed", "error", err,
			"call_id", corr.CallID, "participant_id", corr.ParticipantID)
	}
}
