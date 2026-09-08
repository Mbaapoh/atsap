package acl

import (
	"context"
	"errors"
	"fmt"

	"atsap-api/internal/telephony/acl/ari"
	"atsap-api/internal/telephony/ports"
)

// MediaGatewayAdapter implements ports.MediaGateway against a real
// *ari.Client — the only place in the module that translates between
// port-level types (ports.ChannelRef, ports.BridgeID) and raw ARI calls
// (docs/hld/01-architecture.md §1.2 rule 3).
type MediaGatewayAdapter struct {
	ari *ari.Client
}

var _ ports.MediaGateway = (*MediaGatewayAdapter)(nil)

// NewMediaGatewayAdapter returns an adapter backed by ariClient.
func NewMediaGatewayAdapter(ariClient *ari.Client) *MediaGatewayAdapter {
	return &MediaGatewayAdapter{ari: ariClient}
}

func (a *MediaGatewayAdapter) Originate(ctx context.Context, req ports.OriginateRequest) (ports.ChannelRef, error) {
	id, err := a.ari.Originate(ctx, ari.OriginateRequest{
		Endpoint:       req.EndpointURI,
		CallerID:       req.CallerID,
		ChannelID:      req.ChannelID,
		Variables:      req.Variables,
		TimeoutSeconds: req.TimeoutSeconds,
	})
	if err != nil {
		return "", fmt.Errorf("acl: originate: %w", err)
	}
	return ports.ChannelRef(id), nil
}

func (a *MediaGatewayAdapter) CreateBridge(ctx context.Context, bridgeType string) (ports.BridgeID, error) {
	id, err := a.ari.CreateBridge(ctx, bridgeType)
	if err != nil {
		return "", fmt.Errorf("acl: create bridge: %w", err)
	}
	return ports.BridgeID(id), nil
}

func (a *MediaGatewayAdapter) AddChannelToBridge(ctx context.Context, bridgeID ports.BridgeID, channelRef ports.ChannelRef) error {
	if err := a.ari.AddChannelToBridge(ctx, string(bridgeID), string(channelRef)); err != nil {
		return fmt.Errorf("acl: add channel to bridge: %w", err)
	}
	return nil
}

func (a *MediaGatewayAdapter) DestroyBridge(ctx context.Context, bridgeID ports.BridgeID) error {
	if err := a.ari.DestroyBridge(ctx, string(bridgeID)); err != nil {
		return fmt.Errorf("acl: destroy bridge: %w", err)
	}
	return nil
}

func (a *MediaGatewayAdapter) StartPlayback(ctx context.Context, channelRef ports.ChannelRef, mediaURI string) error {
	_, err := a.ari.Play(ctx, string(channelRef), mediaURI)
	if err != nil {
		return fmt.Errorf("acl: start playback: %w", err)
	}
	return nil
}

// StartSnoop is not implemented in this change — ai-pipeline (LLD-10) is
// its first caller (LLD-01 §4.2).
func (a *MediaGatewayAdapter) StartSnoop(context.Context, ports.ChannelRef, ports.SnoopRequest) (ports.SnoopRef, error) {
	return "", errors.New("acl: StartSnoop not implemented in this change")
}

func (a *MediaGatewayAdapter) DestroyChannel(ctx context.Context, channelRef ports.ChannelRef) error {
	if err := a.ari.HangupChannel(ctx, string(channelRef), "normal"); err != nil {
		return fmt.Errorf("acl: destroy channel: %w", err)
	}
	return nil
}
