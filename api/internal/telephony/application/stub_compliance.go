package application

import (
	"context"

	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/telephony/ports"
)

// alwaysPermitCompliance satisfies ports.ComplianceEngine until a later
// change lands the real compliance bounded context behind this same
// port. Delete this file (and its wiring in cmd/atsap-api) when that
// adapter is wired in — do not extend it with real logic here.
type alwaysPermitCompliance struct{}

// NewAlwaysPermitCompliance returns the LLD-01 stub ComplianceEngine.
func NewAlwaysPermitCompliance() ports.ComplianceEngine {
	return alwaysPermitCompliance{}
}

func (alwaysPermitCompliance) Evaluate(context.Context, shareddomain.TenantID, string) (ports.ComplianceVerdict, error) {
	return ports.ComplianceVerdict{Permitted: true}, nil
}
