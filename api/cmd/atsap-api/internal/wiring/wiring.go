// Package wiring holds the composition root's small translations between
// contexts that are worth testing on their own. It lives under cmd so
// only the composition root may use it, and outside package main so its
// tests link only what they exercise.
package wiring

import (
	"context"
	"fmt"

	identityports "atsap-api/internal/identity/ports"
	licensingdomain "atsap-api/internal/licensing/domain"
	pbxports "atsap-api/internal/pbx/ports"
	"atsap-api/internal/server"
)

// TenantCap and ExtensionCap read a cap from the entitlement in force.
// A never-activated installation has caps of zero, and zero is the
// spelling of "unlimited", so Setup is reported as each context's own
// not-licensed error instead (D-72). Degraded keeps its caps (AC-06.13).
func TenantCap(e licensingdomain.Entitlement) (int, error) {
	if e.State == licensingdomain.StateSetup {
		return 0, fmt.Errorf("licence state %s: %w", e.State, identityports.ErrNotLicensed)
	}
	return e.MaxTenants, nil
}

// ExtensionCap is TenantCap for pbx-core's extension cap.
func ExtensionCap(e licensingdomain.Entitlement) (int, error) {
	if e.State == licensingdomain.StateSetup {
		return 0, fmt.Errorf("licence state %s: %w", e.State, pbxports.ErrNotLicensed)
	}
	return e.MaxExtensions, nil
}

// EventsHealth is the ARI client's view of its event stream.
type EventsHealth interface{ EventsHealth() error }

// CallEventsChecker makes this instance ready only while it receives call
// events from the engine: its stream is connected and its Stasis
// application has not been taken over by another process (D-73). Without
// it, an instance that heard no call events still reported ready, and
// calls stuck at Presenting.
func CallEventsChecker(events EventsHealth) server.Checker {
	return server.Checker{
		Name:  "call-events",
		Check: func(context.Context) error { return events.EventsHealth() },
	}
}
