package application

import (
	"context"

	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/telephony/ports"
)

// alwaysPermitLicense satisfies ports.LicenseManager until LLD-08 lands
// the real licensing bounded context behind this same port. Delete this
// file (and its wiring in cmd/atsap-api) when that adapter is wired in —
// do not extend it with real logic here.
type alwaysPermitLicense struct{}

// NewAlwaysPermitLicense returns the LLD-01 stub LicenseManager.
func NewAlwaysPermitLicense() ports.LicenseManager {
	return alwaysPermitLicense{}
}

func (alwaysPermitLicense) ValidateCapacity(context.Context, shareddomain.TenantID, int) (ports.CapacityVerdict, error) {
	return ports.CapacityVerdict{Permitted: true}, nil
}
