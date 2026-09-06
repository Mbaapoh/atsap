package acl_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"atsap-api/internal/telephony/acl"
)

func TestCorrelationRegistry_RegisterAndLookup(t *testing.T) {
	reg := acl.NewCorrelationRegistry()

	_, ok := reg.Lookup("chan1")
	assert.False(t, ok, "unregistered channel should not be found")

	reg.Register("chan1", acl.Correlation{CallID: "call-1", ParticipantID: "participant-1"})

	corr, ok := reg.Lookup("chan1")
	assert.True(t, ok)
	assert.Equal(t, "call-1", corr.CallID)
	assert.Equal(t, "participant-1", corr.ParticipantID)
	assert.Equal(t, 1, reg.Len())
}

func TestCorrelationRegistry_Remove(t *testing.T) {
	reg := acl.NewCorrelationRegistry()
	reg.Register("chan1", acl.Correlation{CallID: "call-1", ParticipantID: "participant-1"})

	reg.Remove("chan1")

	_, ok := reg.Lookup("chan1")
	assert.False(t, ok)
	assert.Equal(t, 0, reg.Len())
}

func TestCorrelationRegistry_ReRegisterOverwrites(t *testing.T) {
	reg := acl.NewCorrelationRegistry()
	reg.Register("chan1", acl.Correlation{CallID: "call-1", ParticipantID: "participant-1"})
	reg.Register("chan1", acl.Correlation{CallID: "call-1", ParticipantID: "participant-2"})

	corr, ok := reg.Lookup("chan1")
	assert.True(t, ok)
	assert.Equal(t, "participant-2", corr.ParticipantID)
}

// TestCorrelationRegistry_ConcurrentAccess exercises concurrent
// registration and lookup under -race, per docs/TESTING.md's rule that
// anything touching the ACL correlation registry is exercised
// concurrently.
func TestCorrelationRegistry_ConcurrentAccess(t *testing.T) {
	reg := acl.NewCorrelationRegistry()
	const n = 100

	var wg sync.WaitGroup
	wg.Add(n * 2)
	for i := range n {
		channelID := fmt.Sprintf("chan%d", i)
		go func() {
			defer wg.Done()
			reg.Register(channelID, acl.Correlation{CallID: "call-1", ParticipantID: channelID})
		}()
		go func() {
			defer wg.Done()
			reg.Lookup(channelID) // may race the register above; must not corrupt state
		}()
	}
	wg.Wait()

	assert.Equal(t, n, reg.Len())
}
