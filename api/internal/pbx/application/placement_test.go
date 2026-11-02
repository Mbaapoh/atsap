package application_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"atsap-api/internal/pbx/application"
	telephonyports "atsap-api/internal/telephony/ports"
)

// DefaultPlacement fills the caller-first timeout so a caller placement
// never rings forever (console-click-to-call D1).
func TestDefaultPlacement_CallerTimeout(t *testing.T) {
	p := application.DefaultPlacement(application.Placement{})
	assert.Equal(t, telephonyports.DefaultCallerTimeout, p.CallerTimeout,
		"an unset caller timeout defaults to 30 s")

	p = application.DefaultPlacement(application.Placement{CallerTimeout: 12 * time.Second})
	assert.Equal(t, 12*time.Second, p.CallerTimeout,
		"a configured caller timeout is kept")
}
