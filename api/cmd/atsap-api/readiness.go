package main

import (
	"context"

	"atsap-api/internal/server"
)

// eventsHealth is the ARI client's view of its event stream.
type eventsHealth interface{ EventsHealth() error }

// callEventsChecker makes this instance ready only while it receives call
// events from the engine: its stream is connected and its Stasis
// application has not been taken over by another process (D-73). Without
// it, an instance that hears no call events still reported ready, and
// calls stuck at Presenting.
func callEventsChecker(events eventsHealth) server.Checker {
	return server.Checker{
		Name:  "call-events",
		Check: func(context.Context) error { return events.EventsHealth() },
	}
}
