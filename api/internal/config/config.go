// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
)

// Config holds all runtime settings for the atsap-api process.
type Config struct {
	// HTTPAddr is where the app's own health/metrics/API server listens.
	HTTPAddr string
	// LogLevel is one of "debug", "info", "warn", "error" (internal/logging).
	LogLevel string

	// ARI connection settings (Asterisk REST Interface).
	ARIURL      string
	ARIUsername string
	ARIPassword string
	ARIAppName  string // Stasis application name the dialplan routes calls into.

	// AMI connection settings (Asterisk Manager Interface).
	AMIAddr     string
	AMIUsername string
	AMIPassword string

	// DatabaseURL is the app's own connection string (atsapbx_app role,
	// atsapbx database) — never the asterisk CDR/CEL database.
	DatabaseURL string
	// DatabaseWorkerURL is the outbox worker's connection string
	// (atsap_outbox_worker role — BYPASSRLS, scoped to outbox only;
	// docs/hld/03-domain-model.md §5). Deliberately a separate URL, not
	// derived from DatabaseURL: the two roles must never be
	// interchangeable, and deriving one from the other by string
	// manipulation invites exactly that mistake.
	DatabaseWorkerURL string
	// NATSURL is the JetStream connection string.
	NATSURL string
	// SeedDevTenant gates the 0002_dev_tenant_seed migration
	// (postgres.MigrateUp's seedDevTenant parameter). Never true in
	// production.
	SeedDevTenant bool
}

// Load reads configuration from environment variables, applying sane
// defaults for local development. It returns an error if a required
// value is missing or malformed.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:          getEnv("HTTP_ADDR", ":8080"),
		LogLevel:          getEnv("LOG_LEVEL", "info"),
		ARIURL:            getEnv("ARI_URL", "http://asterisk:8088/ari"),
		ARIUsername:       getEnv("ARI_USERNAME", "voipapp"),
		ARIPassword:       getEnv("ARI_PASSWORD", ""),
		ARIAppName:        getEnv("ARI_APP_NAME", "voip-app"),
		AMIAddr:           getEnv("AMI_ADDR", "asterisk:5038"),
		AMIUsername:       getEnv("AMI_USERNAME", "voipapp"),
		AMIPassword:       getEnv("AMI_PASSWORD", ""),
		DatabaseURL:       getEnv("DATABASE_URL", ""),
		DatabaseWorkerURL: getEnv("DATABASE_WORKER_URL", ""),
		NATSURL:           getEnv("NATS_URL", ""),
		SeedDevTenant:     getEnv("ATSAPBX_SEED_DEV_TENANT", "false") == "true",
	}

	if cfg.ARIPassword == "" {
		return cfg, fmt.Errorf("ARI_PASSWORD is required")
	}
	if cfg.AMIPassword == "" {
		return cfg, fmt.Errorf("AMI_PASSWORD is required")
	}
	if cfg.DatabaseURL == "" {
		return cfg, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.DatabaseWorkerURL == "" {
		return cfg, fmt.Errorf("DATABASE_WORKER_URL is required")
	}
	if cfg.NATSURL == "" {
		return cfg, fmt.Errorf("NATS_URL is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
