package main

import (
	"context"
	"fmt"
	"log/slog"

	licensingapp "atsap-api/internal/licensing/application"
	licensingdomain "atsap-api/internal/licensing/domain"
	licensingpg "atsap-api/internal/licensing/postgres"
	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
	telephonyports "atsap-api/internal/telephony/ports"
)

// licenseAdapter satisfies telephony-core's narrow LicenseManager using
// licensing's full one.
//
// This file exists because the two contexts each define their own
// CapacityVerdict, and neither may import the other: identity and
// licensing are Tier-0 peers, and HLD 04 §10.1 gives licensing "may
// depend on: nothing" (LLD-08 §3.1). Merging the two interfaces would
// force exactly the import the matrix denies.
//
// So the translation lives here, in the composition root — the one place
// permitted to know both sides. It is four lines of field copying, which
// is the correct price for keeping a Tier-0 context free of a peer
// dependency, and it is why "just share the type" is not a
// simplification but a boundary violation.
type licenseAdapter struct {
	inner *licensingapp.Service
}

var _ telephonyports.LicenseManager = (*licenseAdapter)(nil)

func (a *licenseAdapter) ValidateCapacity(ctx context.Context, tenantID shareddomain.TenantID, callID string, channels int) (telephonyports.CapacityVerdict, error) {
	v, err := a.inner.ValidateCapacity(ctx, tenantID, callID, channels)
	if err != nil {
		return telephonyports.CapacityVerdict{}, err
	}
	return telephonyports.CapacityVerdict{
		Permitted: v.Permitted,
		// The reason crosses as a string. telephony-core records it on
		// the terminated call and emits it in telemetry; it never
		// interprets it, which is what keeps licensing's vocabulary out
		// of the call domain.
		Reason: string(v.Reason),
	}, nil
}

func (a *licenseAdapter) ReleaseCapacity(ctx context.Context, callID string) error {
	return a.inner.ReleaseCapacity(ctx, callID)
}

// newLicenseManager builds the licensing service and applies a licence
// supplied by configuration, if there is one.
//
// A token in ATSAPBX_LICENSE_TOKEN is how a compose stack, a Helm chart
// or a CI job activates an installation without a portal round trip
// (D-52 §10.5). Applying it is idempotent, which matters here more than
// anywhere: this runs on EVERY boot, and a re-application that refreshed
// the confirmation timestamp would restart the 7-day grace clock each
// time the process restarted, making the offline window unbounded
// (D-58).
//
// A bad token is fatal at startup, deliberately. An operator who set the
// variable meant to activate this installation; carrying on in Setup
// with no call path and a line in the log is the failure mode where
// somebody discovers it from a customer.
func newLicenseManager(ctx context.Context, pool *corepostgres.Pool, token string, logger *slog.Logger) (*licenseAdapter, error) {
	svc := licensingapp.NewService(
		licensingpg.NewStore(pool.Unwrap()),
		licensingdomain.TrustedKeys(),
		logger,
	)

	if token != "" {
		if err := svc.ApplyLicenseKey(ctx, token); err != nil {
			return nil, fmt.Errorf("apply ATSAPBX_LICENSE_TOKEN: %w", err)
		}
		logger.Info("licensing: licence applied from configuration")
	}

	e, err := svc.Entitlement(ctx)
	if err != nil {
		return nil, fmt.Errorf("read entitlement: %w", err)
	}
	// Said plainly at startup, because "no calls work" is otherwise a
	// mystery: a fresh installation is in Setup until a licence is
	// applied, and that is a state rather than a fault (D-52).
	logger.Info("licensing: entitlement in force",
		"state", string(e.State), "channels", e.Channels,
		"max_extensions", e.MaxExtensions, "max_tenants", e.MaxTenants)

	return &licenseAdapter{inner: svc}, nil
}
