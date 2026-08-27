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

	// ARI connection settings (Asterisk REST Interface).
	ARIURL      string
	ARIUsername string
	ARIPassword string
	ARIAppName  string // Stasis application name the dialplan routes calls into.

	// AMI connection settings (Asterisk Manager Interface).
	AMIAddr     string
	AMIUsername string
	AMIPassword string
}

// Load reads configuration from environment variables, applying sane
// defaults for local development. It returns an error if a required
// value is missing or malformed.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:    getEnv("HTTP_ADDR", ":8080"),
		ARIURL:      getEnv("ARI_URL", "http://asterisk:8088/ari"),
		ARIUsername: getEnv("ARI_USERNAME", "voipapp"),
		ARIPassword: getEnv("ARI_PASSWORD", ""),
		ARIAppName:  getEnv("ARI_APP_NAME", "voip-app"),
		AMIAddr:     getEnv("AMI_ADDR", "asterisk:5038"),
		AMIUsername: getEnv("AMI_USERNAME", "voipapp"),
		AMIPassword: getEnv("AMI_PASSWORD", ""),
	}

	if cfg.ARIPassword == "" {
		return cfg, fmt.Errorf("ARI_PASSWORD is required")
	}
	if cfg.AMIPassword == "" {
		return cfg, fmt.Errorf("AMI_PASSWORD is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
