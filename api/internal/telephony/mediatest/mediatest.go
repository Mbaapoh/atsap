// Package mediatest is the executable form of the MediaGateway
// capability contract (DECISIONS D-41; ports.MediaGateway doc comment):
// ordered scenarios any engine adapter must satisfy to prove fit.
//
// Scenarios take an already-constructed ports.MediaGateway — each adapter
// wires its own test-double transport in its own (AST-exempt) package
// test and calls Run/RunFailure. This package imports ports only, never
// an adapter, so it stays green under the ACL-boundary gate
// (docs/hld/01-architecture.md §6) no matter how many engines exist.
//
// What the suite asserts is deliberately port-level, never engine
// behavior: call order and handle threading, D-19 token round-trip,
// timeout honored, errors propagated (never swallowed, never
// success-with-empty-handle). Engine specifics (exact status codes,
// retry policy, idempotency of destroy) stay in the adapter's own
// tests — asserting them here would bake one engine's habits into the
// contract every other engine must then imitate.
package mediatest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/telephony/ports"
)

// WireObserver reports what the most recent Originate call placed on the
// wire, in engine-neutral terms. The token and timeout scenarios cannot
// run without one: silent skips would hollow the suite out, so Run
// fails fast on a nil observer instead.
type WireObserver interface {
	LastOriginate() (channelID string, variables map[string]string, timeout string)
}

// originateFixture is the single originate shape every scenario uses, so
// the token assertions below always check the same values.
func originateFixture() ports.OriginateRequest {
	return ports.OriginateRequest{
		EndpointURI:    "PJSIP/1000",
		CallerID:       "Test <1000>",
		ChannelID:      "atsa-part-conformance-0001",
		Variables:      map[string]string{"ATSA_PARTICIPANT_ID": "conformance-0001"},
		TimeoutSeconds: 20,
	}
}

// Run executes every happy-path contract scenario against gw, whose
// transport must succeed. See RunFailure for the error half.
func Run(t *testing.T, gw ports.MediaGateway, obs WireObserver) {
	t.Helper()
	require.NotNil(t, obs, "wire observer is required: token/timeout scenarios cannot run blind")

	ctx := context.Background()

	t.Run("lifecycle/originate-bridge-playback-destroy", func(t *testing.T) {
		req := originateFixture()
		ref, err := gw.Originate(ctx, req)
		require.NoError(t, err)
		require.NotEmpty(t, ref, "originate must return a usable channel handle, never empty success")

		bridgeID, err := gw.CreateBridge(ctx, "mixing")
		require.NoError(t, err)
		require.NotEmpty(t, bridgeID)

		require.NoError(t, gw.AddChannelToBridge(ctx, bridgeID, ref))
		require.NoError(t, gw.StartPlayback(ctx, ref, "sound:hello-world"))
		require.NoError(t, gw.DestroyChannel(ctx, ref))
		require.NoError(t, gw.DestroyBridge(ctx, bridgeID))
	})

	t.Run("originate/correlation-token-round-trip", func(t *testing.T) {
		req := originateFixture()
		if _, err := gw.Originate(ctx, req); err != nil {
			t.Fatalf("originate: %v", err)
		}
		channelID, variables, _ := obs.LastOriginate()
		assert.Equal(t, req.ChannelID, channelID, "D-19: orchestrator-supplied channel ID must reach the wire unchanged")
		assert.Equal(t, req.Variables, variables, "D-19: correlation variables must reach the wire unchanged")
	})

	t.Run("originate/timeout-honored", func(t *testing.T) {
		req := originateFixture()
		if _, err := gw.Originate(ctx, req); err != nil {
			t.Fatalf("originate: %v", err)
		}
		_, _, timeout := obs.LastOriginate()
		assert.Equal(t, "20", timeout, "explicit alerting timeout must reach the engine")
		// NOTE: TimeoutSeconds==0 (engine default) is deliberately NOT
		// asserted here — its wire encoding is engine-specific and stays
		// in the adapter's own tests.
	})

	t.Run("snoop/unimplemented-until-lld-10", func(t *testing.T) {
		_, err := gw.StartSnoop(ctx, "chan-1", ports.SnoopRequest{Direction: "in"})
		require.Error(t, err, "StartSnoop is declared but unimplemented in this change; LLD-10 inverts this scenario when ai-pipeline lands")
	})
}

// RunFailure executes the error-propagation half against gw, whose
// transport must fail every call. Contract: transport failures surface
// as non-nil errors — never swallowed, never success-with-empty-handle.
func RunFailure(t *testing.T, gw ports.MediaGateway) {
	t.Helper()
	ctx := context.Background()
	req := originateFixture()

	t.Run("originate/failure-propagates", func(t *testing.T) {
		ref, err := gw.Originate(ctx, req)
		require.Error(t, err)
		assert.Empty(t, ref, "failed originate must not return a handle")
	})

	t.Run("bridge/failure-propagates", func(t *testing.T) {
		bridgeID, err := gw.CreateBridge(ctx, "mixing")
		require.Error(t, err)
		assert.Empty(t, bridgeID)
	})

	t.Run("playback/failure-propagates", func(t *testing.T) {
		assert.Error(t, gw.StartPlayback(ctx, "chan-1", "sound:hello-world"))
	})

	t.Run("destroy/failure-propagates", func(t *testing.T) {
		assert.Error(t, gw.DestroyChannel(ctx, "chan-1"))
		assert.Error(t, gw.DestroyBridge(ctx, "bridge-1"))
	})
}
