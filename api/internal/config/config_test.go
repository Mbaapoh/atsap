package config

import (
	"strings"
	"testing"
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
}

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
		SeedDevTenant:     false,
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
	t.Setenv("ATSAPBX_SEED_DEV_TENANT", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.HTTPAddr != ":9090" || cfg.LogLevel != "debug" || cfg.ARIURL != "http://example:8088/ari" ||
		cfg.ARIUsername != "custom-user" || cfg.ARIAppName != "custom-app" || cfg.AMIAddr != "example:5038" ||
		cfg.AMIUsername != "custom-ami-user" || !cfg.SeedDevTenant {
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

func TestLoad_SeedDevTenant_DefaultsFalse(t *testing.T) {
	setRequired(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.SeedDevTenant {
		t.Error("SeedDevTenant should default to false")
	}
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
