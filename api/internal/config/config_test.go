package config

import (
	"strings"
	"testing"
	"time"
)

// setRequired sets every env var Load() requires to a valid non-empty
// value, so a test can override just the one(s) it cares about.
func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("ARI_PASSWORD", "ari-secret")
	t.Setenv("AMI_PASSWORD", "ami-secret")
	t.Setenv("DATABASE_URL", "postgres://atsapbx_app:pw@postgres:5432/atsapbx?sslmode=disable")
	t.Setenv("DATABASE_WORKER_URL", "postgres://atsap_outbox_worker:pw@postgres:5432/atsapbx?sslmode=disable")
	t.Setenv("NATS_URL", "nats://nats:4222")
	t.Setenv("ATSAPBX_JWT_KEY", testKeyHex)
}

// testKeyHex is 64 hex chars = a 32-byte Ed25519 seed, clearly a test
// fixture (LLD-02 §5.2 dev-token guardrails: never a real key).
const testKeyHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func TestLoad_Defaults(t *testing.T) {
	setRequired(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	want := Config{
		HTTPAddr:          ":8080",
		LogLevel:          "info",
		ARIURL:            "http://asterisk:8088/ari",
		ARIUsername:       "voipapp",
		ARIPassword:       "ari-secret",
		ARIAppName:        "voip-app",
		AMIAddr:           "asterisk:5038",
		AMIUsername:       "voipapp",
		AMIPassword:       "ami-secret",
		DatabaseURL:       "postgres://atsapbx_app:pw@postgres:5432/atsapbx?sslmode=disable",
		DatabaseWorkerURL: "postgres://atsap_outbox_worker:pw@postgres:5432/atsapbx?sslmode=disable",
		NATSURL:           "nats://nats:4222",
		JWTPrivateKeyHex:  testKeyHex,
		// SIP realm and transports default to PJSIP's own defaults, so an
		// unconfigured platform agrees with an unconfigured engine rather
		// than silently generating credentials it cannot authenticate.
		SIPRealm:           "asterisk",
		SIPTransport:       "transport-udp",
		SIPWebRTCTransport: "transport-wss",
		// No public registrar host until the installer names one; the
		// SIP defaults for port and transport.
		SIPPublicPort:      5060,
		SIPPublicTransport: "udp",
		// Links default to the dev console; mail is off until an SMTP
		// server is named (identity-accounts-and-sessions D10).
		ConsoleURL: "http://localhost:8080",
		SMTPFrom:   "AtsaPBX <no-reply@localhost>",
		// Every trunk is checked twice a minute (D-74).
		TrunkProbeInterval: 30 * time.Second,
	}
	if cfg != want {
		t.Errorf("Load() = %+v, want %+v", cfg, want)
	}
}

func TestLoad_Overrides(t *testing.T) {
	setRequired(t)
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("ARI_URL", "http://example:8088/ari")
	t.Setenv("ARI_USERNAME", "custom-user")
	t.Setenv("ARI_APP_NAME", "custom-app")
	t.Setenv("AMI_ADDR", "example:5038")
	t.Setenv("AMI_USERNAME", "custom-ami-user")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.HTTPAddr != ":9090" || cfg.LogLevel != "debug" || cfg.ARIURL != "http://example:8088/ari" ||
		cfg.ARIUsername != "custom-user" || cfg.ARIAppName != "custom-app" || cfg.AMIAddr != "example:5038" ||
		cfg.AMIUsername != "custom-ami-user" {
		t.Errorf("Load() = %+v, overrides not applied", cfg)
	}
}

func TestLoad_MissingARIPassword(t *testing.T) {
	setRequired(t)
	t.Setenv("ARI_PASSWORD", "")

	_, err := Load()
	requireErrorContains(t, err, "ARI_PASSWORD")
}

func TestLoad_MissingAMIPassword(t *testing.T) {
	setRequired(t)
	t.Setenv("AMI_PASSWORD", "")

	_, err := Load()
	requireErrorContains(t, err, "AMI_PASSWORD")
}

func TestLoad_MissingDatabaseURL(t *testing.T) {
	setRequired(t)
	t.Setenv("DATABASE_URL", "")

	_, err := Load()
	requireErrorContains(t, err, "DATABASE_URL")
}

func TestLoad_MissingDatabaseWorkerURL(t *testing.T) {
	setRequired(t)
	t.Setenv("DATABASE_WORKER_URL", "")

	_, err := Load()
	requireErrorContains(t, err, "DATABASE_WORKER_URL")
}

func TestLoad_MissingNATSURL(t *testing.T) {
	setRequired(t)
	t.Setenv("NATS_URL", "")

	_, err := Load()
	requireErrorContains(t, err, "NATS_URL")
}

func requireErrorContains(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("Load() expected an error mentioning %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Errorf("Load() error = %q, want it to mention %q", err.Error(), substr)
	}
}

func TestGetEnv(t *testing.T) {
	t.Setenv("GETENV_TEST_SET", "value")
	t.Setenv("GETENV_TEST_EMPTY", "")

	if got := getEnv("GETENV_TEST_SET", "fallback"); got != "value" {
		t.Errorf("getEnv(set) = %q, want %q", got, "value")
	}
	if got := getEnv("GETENV_TEST_EMPTY", "fallback"); got != "fallback" {
		t.Errorf("getEnv(empty) = %q, want fallback %q", got, "fallback")
	}
	if got := getEnv("GETENV_TEST_UNSET", "fallback"); got != "fallback" {
		t.Errorf("getEnv(unset) = %q, want fallback %q", got, "fallback")
	}
}

func TestLoad_SIPPublicRegistrar(t *testing.T) {
	setRequired(t)
	t.Setenv("SIP_PUBLIC_HOST", "pbx.example.com")
	t.Setenv("SIP_PUBLIC_PORT", "5080")
	t.Setenv("SIP_PUBLIC_TRANSPORT", "tls")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.SIPPublicHost != "pbx.example.com" || cfg.SIPPublicPort != 5080 || cfg.SIPPublicTransport != "tls" {
		t.Errorf("got %q %d %q", cfg.SIPPublicHost, cfg.SIPPublicPort, cfg.SIPPublicTransport)
	}
}

func TestLoad_SIPPublicRegistrarRejectsBadValues(t *testing.T) {
	for _, tc := range []struct{ key, value, want string }{
		{"SIP_PUBLIC_PORT", "0", "SIP_PUBLIC_PORT"},
		{"SIP_PUBLIC_PORT", "65536", "SIP_PUBLIC_PORT"},
		{"SIP_PUBLIC_PORT", "sip", "SIP_PUBLIC_PORT"},
		{"SIP_PUBLIC_TRANSPORT", "wss", "SIP_PUBLIC_TRANSPORT"},
		{"SIP_PUBLIC_HOST", "sip:pbx.example.com", "SIP_PUBLIC_HOST"},
		{"SIP_PUBLIC_HOST", "pbx.example.com:5060", "SIP_PUBLIC_HOST"},
		{"SIP_PUBLIC_HOST", "user@pbx.example.com", "SIP_PUBLIC_HOST"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			setRequired(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Load() error = %v, want one naming %s", err, tc.want)
			}
		})
	}
}

// pbx-trunk-health-probing: the check interval is a duration of at least
// 5 s, or 0 to turn scheduled checks off.
func TestLoad_TrunkProbeInterval(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{"1m", time.Minute, true},
		{"5s", 5 * time.Second, true},
		{"0", 0, true},
		{"1s", 0, false},
		{"-30s", 0, false},
		{"often", 0, false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			setRequired(t)
			t.Setenv("ATSAPBX_TRUNK_PROBE_INTERVAL", tc.value)
			cfg, err := Load()
			if !tc.ok {
				if err == nil || !strings.Contains(err.Error(), "ATSAPBX_TRUNK_PROBE_INTERVAL") {
					t.Errorf("Load() error = %v, want one naming ATSAPBX_TRUNK_PROBE_INTERVAL", err)
				}
				return
			}
			if err != nil || cfg.TrunkProbeInterval != tc.want {
				t.Errorf("Load() = %v, %v; want %v", cfg.TrunkProbeInterval, err, tc.want)
			}
		})
	}
}
