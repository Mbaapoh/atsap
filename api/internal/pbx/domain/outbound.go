package domain

import (
	"errors"
	"fmt"
	"strings"
)

// OutboundClass is how far an extension may dial. It is a configuration
// control, not a detection one: a compromised extension cannot spend
// beyond the class an administrator gave it (LLD-03 §10.2).
type OutboundClass string

const (
	// OutboundInternal reaches extensions only.
	OutboundInternal OutboundClass = "INTERNAL"
	// OutboundNational reaches extensions and numbers dialled in national
	// form (no leading "+"). It is the default for a new extension.
	OutboundNational OutboundClass = "NATIONAL"
	// OutboundInternational reaches every destination, including numbers
	// dialled in international form with a leading "+".
	OutboundInternational OutboundClass = "INTERNATIONAL"
)

// ErrOutboundClassUnknown is returned for a class outside the three.
var ErrOutboundClassUnknown = errors.New("outbound class is not one of the supported values")

// NormalizeOutboundClass supplies the safe default for an omitted value.
// A new extension is national, never international, so a compromise
// cannot immediately dial premium international numbers.
func NormalizeOutboundClass(c OutboundClass) OutboundClass {
	if c == "" {
		return OutboundNational
	}
	return c
}

// Valid reports whether c is one of the three classes.
func (c OutboundClass) Valid() bool {
	switch c {
	case OutboundInternal, OutboundNational, OutboundInternational:
		return true
	}
	return false
}

// Rank orders the classes by reach. It is only meaningful for valid
// classes.
func (c OutboundClass) Rank() int {
	switch c {
	case OutboundInternal:
		return 0
	case OutboundNational:
		return 1
	case OutboundInternational:
		return 2
	}
	return -1
}

// Allows reports whether an extension holding c may dial a destination of
// class target. An unknown value allows nothing.
func (c OutboundClass) Allows(target OutboundClass) bool {
	c = NormalizeOutboundClass(c)
	if !c.Valid() || !target.Valid() {
		return false
	}
	return c.Rank() >= target.Rank()
}

// ClassifyDialledDestination classifies a trunk-bound destination from its
// dialled form: an optional leading "+" is the international signal, as it
// is in E.164. Extensions are classified by the dial plan, not here.
func ClassifyDialledDestination(dialled string) OutboundClass {
	if strings.HasPrefix(dialled, "+") {
		return OutboundInternational
	}
	return OutboundNational
}

// ValidateOutboundClass checks the stored value.
func ValidateOutboundClass(c OutboundClass) error {
	if !c.Valid() {
		return fmt.Errorf("%w: %q", ErrOutboundClassUnknown, string(c))
	}
	return nil
}
