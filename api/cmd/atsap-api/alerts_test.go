//go:build integration

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identitydomain "atsap-api/internal/identity/domain"
	"atsap-api/internal/pbx/domain"
	pbxports "atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

type fakePrincipals struct{ page []identitydomain.Principal }

func (f fakePrincipals) ListPrincipals(context.Context, shareddomain.TenantID, int, int) ([]identitydomain.Principal, error) {
	return f.page, nil
}

type sentMail struct {
	to, subject, body string
}

type fakeMailer struct {
	sent []sentMail
	fail error
}

func (f *fakeMailer) Send(_ context.Context, to, subject, body string) error {
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, sentMail{to: to, subject: subject, body: body})
	return nil
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func alertFor(t *testing.T) pbxports.CarrierAlert {
	t.Helper()
	return pbxports.CarrierAlert{
		TenantID: shareddomain.NewTenantID(), TrunkID: shareddomain.NewTrunkID(),
		TrunkName: "Primary", Host: "sip.carrier.example",
		Health: domain.HealthDegraded, Reason: domain.ReasonCallSetupFailures,
		At: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC),
	}
}

func TestCarrierAlertRecipientsAndBody(t *testing.T) {
	page := []identitydomain.Principal{
		{Role: "TENANT_ADMIN", Status: identitydomain.PrincipalActive, Email: "admin@example.test"},
		{Role: "AGENT", Status: identitydomain.PrincipalActive, Email: "agent@example.test"},
		{Role: "TENANT_ADMIN", Status: identitydomain.PrincipalDisabled, Email: "off@example.test"},
		{Role: "TENANT_ADMIN", Status: identitydomain.PrincipalActive},
	}
	mail := &fakeMailer{}
	a := &carrierAlertMailer{principals: fakePrincipals{page: page}, mailer: mail,
		extra: []string{"ops@example.test"}, logger: quietLogger()}
	require.NoError(t, a.CarrierDown(context.Background(), alertFor(t)))
	var got []string
	for _, m := range mail.sent {
		got = append(got, m.to)
	}
	assert.ElementsMatch(t, []string{"admin@example.test", "ops@example.test"}, got)
}

func TestCarrierAlertMailFailureIsReturned(t *testing.T) {
	mail := &fakeMailer{fail: errors.New("smtp down")}
	a := &carrierAlertMailer{principals: fakePrincipals{}, mailer: mail,
		extra: []string{"ops@example.test"}, logger: quietLogger()}
	err := a.CarrierDown(context.Background(), alertFor(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "smtp down")
}

func TestCarrierAlertBodyHasNoEngineIdentifier(t *testing.T) {
	mail := &fakeMailer{}
	a := &carrierAlertMailer{principals: fakePrincipals{}, mailer: mail,
		extra: []string{"ops@example.test"}, logger: quietLogger()}
	require.NoError(t, a.CarrierDown(context.Background(), alertFor(t)))
	require.Len(t, mail.sent, 1)
	m := mail.sent[0]
	assert.Contains(t, m.subject, "Primary")
	assert.Contains(t, m.body, "too many call setup failures")
	assert.NotContains(t, m.body, "PJSIP/")
	assert.NotContains(t, m.body, "t_")
}
