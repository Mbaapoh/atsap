// Package acl is the Asterisk Anti-Corruption Layer: it absorbs Asterisk
// channel/bridge churn and exposes only Call/Participant behavior to the
// rest of telephony-core (docs/hld/01-architecture.md §2; D-15-D-19).
package acl

import "sync"

// Correlation is what one Asterisk channel ID maps to: the domain
// Tenant, Call, and Participant it currently backs.
type Correlation struct {
	TenantID      string
	CallID        string
	ParticipantID string
}

// CorrelationRegistry maps Asterisk channel IDs to the Call/Participant
// they back. It is in-memory and per-process — a process crash mid-call
// loses this mapping for calls in flight, an accepted platform-wide
// limitation (docs/DECISIONS.md "Known and accepted limitations"; LLD-01
// §9), not something this type works around.
//
// Safe for concurrent use: register/lookup/remove may be called from
// multiple goroutines (the ARI event loop and any concurrent command
// handler) without external locking.
type CorrelationRegistry struct {
	mu        sync.RWMutex
	byChannel map[string]Correlation
}

// NewCorrelationRegistry returns an empty registry.
func NewCorrelationRegistry() *CorrelationRegistry {
	return &CorrelationRegistry{
		byChannel: make(map[string]Correlation),
	}
}

// Register associates channelID with the given Call/Participant.
// Registering an already-registered channelID overwrites the previous
// association (used when a channel var is re-confirmed after a
// reconnect).
func (r *CorrelationRegistry) Register(channelID string, corr Correlation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byChannel[channelID] = corr
}

// Lookup returns the Correlation for channelID, and whether it was
// found.
func (r *CorrelationRegistry) Lookup(channelID string) (Correlation, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	corr, ok := r.byChannel[channelID]
	return corr, ok
}

// RegisterCorrelation is Register expressed in plain strings so callers
// outside this package (the application layer, via ports.CorrelationRegistrar)
// can register a correlation at origination time without importing acl —
// domain/application must depend only on domain and ports
// (docs/hld/01-architecture.md §1.2), never directly on an adapter package.
func (r *CorrelationRegistry) RegisterCorrelation(channelID, tenantID, callID, participantID string) {
	r.Register(channelID, Correlation{TenantID: tenantID, CallID: callID, ParticipantID: participantID})
}

// Remove drops channelID's association, once its channel is destroyed.
func (r *CorrelationRegistry) Remove(channelID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byChannel, channelID)
}

// Len reports how many channels are currently tracked. Used by tests and
// diagnostics.
func (r *CorrelationRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byChannel)
}
