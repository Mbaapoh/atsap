//go:build integration

package application_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identityports "atsap-api/internal/identity/ports"
	"atsap-api/internal/pbx/ports"
)

// pbx-sip-registrar-settings task 2.2: the registrar address is read with
// the extension-read permission, and an installation without a public
// host says so.

func TestSipRegistrar_ReturnsTheConfiguredAddress(t *testing.T) {
	h := newHarness(t)
	svc := h.svc.WithRegistrar(ports.SipRegistrar{Host: "pbx.example.com", Port: 5060, Transport: "udp"})
	r, err := svc.SipRegistrar(h.ctx, h.tenant)
	require.NoError(t, err)
	assert.Equal(t, ports.SipRegistrar{Configured: true, Host: "pbx.example.com", Port: 5060, Transport: "udp"}, r)
	assert.Equal(t, []string{"extension.read on " + h.tenant.String()}, h.authz.calls, "authorized as reading extensions")
}

func TestSipRegistrar_NotConfiguredWithoutAHost(t *testing.T) {
	h := newHarness(t)
	svc := h.svc.WithRegistrar(ports.SipRegistrar{Port: 5060, Transport: "udp"})
	r, err := svc.SipRegistrar(h.ctx, h.tenant)
	require.NoError(t, err)
	assert.Equal(t, ports.SipRegistrar{}, r, "no guessed address, no stray port or transport")
}

func TestSipRegistrar_RefusedWithoutPermission(t *testing.T) {
	h := newHarness(t)
	h.authz.deny = identityports.ErrPermissionDenied
	svc := h.svc.WithRegistrar(ports.SipRegistrar{Host: "pbx.example.com", Port: 5060, Transport: "udp"})
	r, err := svc.SipRegistrar(h.ctx, h.tenant)
	require.ErrorIs(t, err, identityports.ErrPermissionDenied)
	assert.Equal(t, ports.SipRegistrar{}, r)
}
