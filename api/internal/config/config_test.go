package config

import (
	"strings"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("ARI_PASSWORD", "ari-secret")
	t.Setenv("AMI_PASSWORD", "ami-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	want := Config{
		HTTPAddr:    ":8080",
		ARIURL:      "http://asterisk:8088/ari",
		ARIUsername: "voipapp",
		ARIPassword: "ari-secret",
		ARIAppName:  "voip-app",
		AMIAddr:     "asterisk:5038",
		AMIUsername: "voipapp",
		AMIPassword: "ami-secret",
	}
	if cfg != want {
		t.Errorf("Load() = %+v, want %+v", cfg, want)
	}
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("ARI_URL", "http://example:8088/ari")
	t.Setenv("ARI_USERNAME", "custom-user")
	t.Setenv("ARI_PASSWORD", "ari-secret")
	t.Setenv("ARI_APP_NAME", "custom-app")
	t.Setenv("AMI_ADDR", "example:5038")
	t.Setenv("AMI_USERNAME", "custom-ami-user")
	t.Setenv("AMI_PASSWORD", "ami-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	want := Config{
		HTTPAddr:    ":9090",
		ARIURL:      "http://example:8088/ari",
		ARIUsername: "custom-user",
		ARIPassword: "ari-secret",
		ARIAppName:  "custom-app",
		AMIAddr:     "example:5038",
		AMIUsername: "custom-ami-user",
		AMIPassword: "ami-secret",
	}
	if cfg != want {
		t.Errorf("Load() = %+v, want %+v", cfg, want)
	}
}

func TestLoad_MissingARIPassword(t *testing.T) {
	t.Setenv("ARI_PASSWORD", "")
	t.Setenv("AMI_PASSWORD", "ami-secret")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for missing ARI_PASSWORD, got nil")
	}
	if !strings.Contains(err.Error(), "ARI_PASSWORD") {
		t.Errorf("Load() error = %q, want it to mention ARI_PASSWORD", err.Error())
	}
}

func TestLoad_MissingAMIPassword(t *testing.T) {
	t.Setenv("ARI_PASSWORD", "ari-secret")
	t.Setenv("AMI_PASSWORD", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for missing AMI_PASSWORD, got nil")
	}
	if !strings.Contains(err.Error(), "AMI_PASSWORD") {
		t.Errorf("Load() error = %q, want it to mention AMI_PASSWORD", err.Error())
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
