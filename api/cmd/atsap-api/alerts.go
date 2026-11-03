package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"atsap-api/internal/config"
	identitydomain "atsap-api/internal/identity/domain"
	identityports "atsap-api/internal/identity/ports"
	identitypostgres "atsap-api/internal/identity/postgres"
	"atsap-api/internal/mail"
	"atsap-api/internal/pbx/domain"
	pbxports "atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// carrierAlertMailer implements the BR-08 alert (D-80): one email per
// failure transition to the tenant's active administrators and the
// installation operators named in ATSAPBX_CARRIER_ALERT_EMAILS.
// principalLister is the slice of identity the alert needs; the store
// implements it, and a test can supply a page.
type principalLister interface {
	ListPrincipals(ctx context.Context, tenant shareddomain.TenantID, limit, offset int) ([]identitydomain.Principal, error)
}

type carrierAlertMailer struct {
	principals principalLister
	mailer     identityports.Mailer
	extra      []string
	logger     *slog.Logger
}

var _ pbxports.CarrierAlerter = (*carrierAlertMailer)(nil)

// newCarrierAlerter builds the mail alert when SMTP is configured. A nil
// alerter is valid: health is still observed and recorded, and the failure
// is logged once here rather than on every alert.
func newCarrierAlerter(cfg config.Config, principals *identitypostgres.Store, logger *slog.Logger) (pbxports.CarrierAlerter, error) {
	if cfg.SMTPAddr == "" {
		logger.Warn("ATSAPBX_SMTP_ADDR is not set: carrier-failure alerts will not be sent")
		return nil, nil
	}
	sender, err := mail.New(mail.Config{
		Addr: cfg.SMTPAddr, From: cfg.SMTPFrom, User: cfg.SMTPUser, Password: cfg.SMTPPassword,
		Insecure: cfg.SMTPInsecure,
	})
	if err != nil {
		return nil, fmt.Errorf("carrier alert smtp: %w", err)
	}
	return &carrierAlertMailer{principals: principals, mailer: sender, extra: cfg.CarrierAlertEmails, logger: logger}, nil
}

func (a *carrierAlertMailer) CarrierDown(ctx context.Context, alert pbxports.CarrierAlert) error {
	recipients := map[string]struct{}{}
	for _, address := range a.extra {
		recipients[address] = struct{}{}
	}
	// One page at a time; the tenant's membership is small in Phase A.
	for offset := 0; ; offset += 200 {
		page, err := a.principals.ListPrincipals(ctx, alert.TenantID, 200, offset)
		if err != nil {
			return fmt.Errorf("list tenant administrators: %w", err)
		}
		for _, p := range page {
			if p.Status == identitydomain.PrincipalActive && p.Role == "TENANT_ADMIN" && p.Email != "" {
				recipients[p.Email] = struct{}{}
			}
		}
		if len(page) < 200 {
			break
		}
	}
	if len(recipients) == 0 {
		a.logger.Warn("pbx: carrier alert has no recipients", "trunk_id", alert.TrunkID, "tenant_id", alert.TenantID)
		return nil
	}

	subject := fmt.Sprintf("AtsaPBX: carrier %s is %s", alert.TrunkName, strings.ToLower(string(alert.Health)))
	body := fmt.Sprintf(`AtsaPBX carrier alert

Trunk:  %s (%s)
Tenant: %s
State:  %s
Reason: %s
At:     %s

Calls will prefer another trunk while this one is degraded, and skip it
while it is unhealthy. This alert is sent once per failure; recovery is
silent. The console shows the live state and time.
`, alert.TrunkName, alert.Host, alert.TenantID, alert.Health, reasonWords(alert.Reason), alert.At.UTC().Format(time.RFC3339))

	var errs []error
	for address := range recipients {
		if err := a.mailer.Send(ctx, address, subject, body); err != nil {
			errs = append(errs, fmt.Errorf("send to %s: %w", address, err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("carrier alert for trunk %s: %w", alert.TrunkID, err)
	}
	return nil
}

func reasonWords(r domain.HealthReason) string {
	switch r {
	case domain.ReasonAnswering:
		return "answering"
	case domain.ReasonSlowResponse:
		return "slow response"
	case domain.ReasonHostNotFound:
		return "host not found"
	case domain.ReasonNoResponse:
		return "no response"
	case domain.ReasonCarrierError:
		return "carrier error"
	case domain.ReasonAddressNotAllowed:
		return "address not allowed"
	case domain.ReasonCallSetupFailures:
		return "too many call setup failures"
	default:
		return string(r)
	}
}
