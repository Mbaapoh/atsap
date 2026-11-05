package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	shareddomain "atsap-api/internal/shared/domain"
)

// FlowConfig is one published IVR revision: which extension to ring when
// open, which when closed, and the UTC business window. A prompt is
// reserved for the DTMF-menu slice; this slice routes on time.
type FlowConfig struct {
	OpenExtensionID     *shareddomain.ExtensionID
	ClosedExtensionID   *shareddomain.ExtensionID
	BusinessStartMinute int
	BusinessEndMinute   int
	Prompt              string
}

// Flow is a tenant's flow on one DID.
type Flow struct {
	ID            shareddomain.FlowID
	TenantID      shareddomain.TenantID
	Number        string
	Name          string
	ActiveVersion int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// FlowVersion is one published revision.
type FlowVersion struct {
	FlowID      shareddomain.FlowID
	Version     int
	Config      FlowConfig
	PublishedAt time.Time
	PublishedBy shareddomain.PrincipalID
}

// FlowRoute is the interpreter's answer: where to send the call.
type FlowRoute struct {
	ExtensionID *shareddomain.ExtensionID
	// Closed says the closed branch was chosen (holiday or outside hours).
	Closed bool
}

// Flow errors.
var (
	ErrFlowNameEmpty          = errors.New("flow name is required")
	ErrFlowNumberInvalid      = errors.New("flow number must be 1-32 digits")
	ErrFlowDestinationMissing = errors.New("flow needs at least an open destination")
	ErrFlowHoursInvalid       = errors.New("flow business hours must be minutes between 0 and 1439")
)

// ValidateFlowConfig checks a revision.
func (c FlowConfig) Validate() error {
	if c.OpenExtensionID == nil && c.ClosedExtensionID == nil {
		return ErrFlowDestinationMissing
	}
	if c.BusinessStartMinute < 0 || c.BusinessStartMinute >= 24*60 ||
		c.BusinessEndMinute < 0 || c.BusinessEndMinute >= 24*60 {
		return ErrFlowHoursInvalid
	}
	if len(c.Prompt) > 255 {
		return fmt.Errorf("flow prompt is too long")
	}
	return nil
}

// ValidateFlow checks the flow's own fields.
func (f Flow) Validate() error {
	if strings.TrimSpace(f.Name) == "" {
		return ErrFlowNameEmpty
	}
	if len(f.Name) > 100 {
		return fmt.Errorf("flow name is too long")
	}
	body := strings.TrimPrefix(f.Number, "+")
	if body == "" || len(body) > 32 {
		return ErrFlowNumberInvalid
	}
	for _, r := range body {
		if r < '0' || r > '9' {
			return ErrFlowNumberInvalid
		}
	}
	return nil
}

// RouteFlow chooses the open or closed destination. A holiday is always
// closed; start == end means open all day. Outside the window uses the
// closed destination when one is set; otherwise the flow has no route.
func RouteFlow(c FlowConfig, at time.Time, holiday bool) (FlowRoute, bool) {
	closed := holiday
	if !closed && c.BusinessStartMinute != c.BusinessEndMinute {
		minute := at.UTC().Hour()*60 + at.UTC().Minute()
		if c.BusinessStartMinute < c.BusinessEndMinute {
			closed = minute < c.BusinessStartMinute || minute >= c.BusinessEndMinute
		} else {
			closed = minute >= c.BusinessEndMinute && minute < c.BusinessStartMinute
		}
	}
	if closed {
		if c.ClosedExtensionID != nil {
			return FlowRoute{ExtensionID: c.ClosedExtensionID, Closed: true}, true
		}
		return FlowRoute{Closed: true}, false
	}
	if c.OpenExtensionID == nil {
		return FlowRoute{}, false
	}
	return FlowRoute{ExtensionID: c.OpenExtensionID}, true
}

// NormalizeFlowNumber strips a leading + and trims.
func NormalizeFlowNumber(number string) string {
	return strings.TrimPrefix(strings.TrimSpace(number), "+")
}
