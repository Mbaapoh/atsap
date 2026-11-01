// Package ingress attributes calls on the shared carrier ingress (D-77):
// which tenant and trunk a carrier call belongs to, from its transport
// source address and the number dialled.
package ingress

import (
	"context"
	"log/slog"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	telephonyports "atsap-api/internal/telephony/ports"
)

// Attributor implements telephony's InboundAttributor (D-77): a
// call on the shared carrier ingress belongs to the one trunk that
// recognises its transport source address and whose tenant has the
// dialled number. None or several refuse; several are logged for the
// operator, never told to the carrier.
type Attributor struct {
	matcher ports.IngressMatcher
	logger  *slog.Logger
}

var _ telephonyports.InboundAttributor = (*Attributor)(nil)

// NewAttributor wires an attributor over matcher.
func NewAttributor(matcher ports.IngressMatcher, logger *slog.Logger) *Attributor {
	return &Attributor{matcher: matcher, logger: logger}
}

// Attribute answers for the ingress endpoint only; any other endpoint's
// own configuration names its tenant.
func (a *Attributor) Attribute(ctx context.Context, req telephonyports.AttributionRequest) (telephonyports.Attribution, error) {
	if req.SourceEndpoint != domain.CarrierIngressEndpoint {
		return telephonyports.Attribution{}, nil
	}
	refused := telephonyports.Attribution{Applies: true, Refused: true}
	source, ok := domain.ParseRemoteAddr(req.SourceAddress)
	if !ok {
		a.logger.Warn("pbx: carrier call with no readable source address; refusing", "source_address", req.SourceAddress)
		return refused, nil
	}
	matches, err := a.matcher.Match(ctx, source, domain.NormalizeDialled(req.Dialled))
	if err != nil {
		return telephonyports.Attribution{}, err
	}
	match, ok := domain.AttributeIngress(matches)
	if !ok {
		if len(matches) > 1 {
			trunks := make([]string, len(matches))
			for i, m := range matches {
				trunks[i] = m.TrunkID.String()
			}
			// Operator-only: the carrier gets a plain 404.
			a.logger.Warn("pbx: a carrier call's number is configured in several tenants that share its address; refusing",
				"source", source.String(), "trunk_ids", trunks)
		}
		return refused, nil
	}
	return telephonyports.Attribution{
		Applies:        true,
		TenantID:       match.TenantID,
		SourceEndpoint: domain.TrunkEndpointIdentifier(match.TrunkID),
	}, nil
}
