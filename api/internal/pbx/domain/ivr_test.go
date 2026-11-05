package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

func TestFlowValidation(t *testing.T) {
	open := shareddomain.NewExtensionID()
	cfg := domain.FlowConfig{OpenExtensionID: &open, BusinessStartMinute: 480, BusinessEndMinute: 1080}
	require.NoError(t, cfg.Validate())
	bad := cfg
	bad.BusinessStartMinute = 1440
	assert.ErrorIs(t, bad.Validate(), domain.ErrFlowHoursInvalid)
	assert.ErrorIs(t, (domain.FlowConfig{}).Validate(), domain.ErrFlowDestinationMissing)

	f := domain.Flow{Number: "4420", Name: "Main"}
	require.NoError(t, f.Validate())
	f.Number = "44x"
	assert.ErrorIs(t, f.Validate(), domain.ErrFlowNumberInvalid)
	f.Number = "4420"
	f.Name = ""
	assert.ErrorIs(t, f.Validate(), domain.ErrFlowNameEmpty)
}

func TestRouteFlow(t *testing.T) {
	open, closed := shareddomain.NewExtensionID(), shareddomain.NewExtensionID()
	cfg := domain.FlowConfig{OpenExtensionID: &open, ClosedExtensionID: &closed,
		BusinessStartMinute: 8 * 60, BusinessEndMinute: 18 * 60}
	at := func(h int) time.Time { return time.Date(2026, 11, 4, h, 0, 0, 0, time.UTC) }

	route, ok := domain.RouteFlow(cfg, at(9), false)
	require.True(t, ok)
	assert.Equal(t, open, *route.ExtensionID)
	assert.False(t, route.Closed)

	route, ok = domain.RouteFlow(cfg, at(19), false)
	require.True(t, ok)
	assert.Equal(t, closed, *route.ExtensionID)
	assert.True(t, route.Closed)

	route, ok = domain.RouteFlow(cfg, at(9), true)
	require.True(t, ok)
	assert.Equal(t, closed, *route.ExtensionID)

	noClosed := domain.FlowConfig{OpenExtensionID: &open, BusinessStartMinute: 480, BusinessEndMinute: 1080}
	_, ok = domain.RouteFlow(noClosed, at(19), false)
	assert.False(t, ok, "outside hours with no closed destination has no route")

	allDay := domain.FlowConfig{OpenExtensionID: &open, BusinessStartMinute: 0, BusinessEndMinute: 0}
	route, ok = domain.RouteFlow(allDay, at(3), false)
	require.True(t, ok)
	assert.Equal(t, open, *route.ExtensionID)
	assert.Equal(t, "4420", domain.NormalizeFlowNumber("+4420"))
}

func TestFlowValidationEdges(t *testing.T) {
	open := shareddomain.NewExtensionID()
	cfg := domain.FlowConfig{OpenExtensionID: &open, Prompt: "x"}
	require.NoError(t, cfg.Validate())
	cfg.Prompt = string(make([]byte, 256))
	assert.Error(t, cfg.Validate())

	f := domain.Flow{Number: "4420", Name: "Main"}
	require.NoError(t, f.Validate())
	f.Name = string(make([]byte, 101))
	assert.Error(t, f.Validate())
	f.Name = "Main"
	f.Number = ""
	assert.ErrorIs(t, f.Validate(), domain.ErrFlowNumberInvalid)
	f.Number = string(make([]byte, 33))
	assert.ErrorIs(t, f.Validate(), domain.ErrFlowNumberInvalid)
}

func TestRouteFlowOvernightAndMissingOpen(t *testing.T) {
	closed := shareddomain.NewExtensionID()
	cfg := domain.FlowConfig{ClosedExtensionID: &closed, BusinessStartMinute: 22 * 60, BusinessEndMinute: 6 * 60}
	at := func(h int) time.Time { return time.Date(2026, 11, 4, h, 0, 0, 0, time.UTC) }
	route, ok := domain.RouteFlow(cfg, at(12), false)
	require.True(t, ok)
	assert.True(t, route.Closed)
	_, ok = domain.RouteFlow(cfg, at(23), false)
	assert.False(t, ok, "inside the overnight window with no open destination has no route")
	assert.Equal(t, "4420", domain.NormalizeFlowNumber("  +4420 "))
}
