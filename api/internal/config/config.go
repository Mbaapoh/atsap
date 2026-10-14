// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	pbxdomain "atsap-api/internal/pbx/domain"
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
	// JWTPrivateKeyHex is the hex-encoded 32-byte Ed25519 seed the token
	// issuer signs with (identity context). Required whenever the process
	// serves IdentityService or bootstraps. Never logged, never echoed.
	JWTPrivateKeyHex string
	// JWTPreviousKeysHex lists previous signing seeds (comma-separated
	// hex), accepted for validation only, so rotating JWTPrivateKeyHex
	// signs nobody out (D-66). Remove an entry once 15 minutes have passed
	// since rotation: no token signed with it can still be unexpired.
	JWTPreviousKeysHex string

	// ConsoleURL is the console's public base URL, which emailed reset and
	// invitation links point at (D-64), e.g. https://pbx.example.com.
	ConsoleURL string
	// SMTPAddr (host:port), SMTPFrom, SMTPUser and SMTPPassword configure
	// outbound mail. With SMTPAddr empty, mail is not sent and a warning
	// is logged instead — administrator reset still works without mail.
	SMTPAddr     string
	SMTPFrom     string
	SMTPUser     string
	SMTPPassword string
	// SMTPInsecure permits a server without STARTTLS. Dev rig only.
	SMTPInsecure bool
	// CookieInsecure drops the Secure attribute from the session cookie,
	// for the plain-HTTP dev rig only. It is logged loudly when set.
	CookieInsecure bool
	// TrustedProxies lists proxy addresses or CIDRs whose X-Forwarded-For
	// is believed when resolving a caller's address for throttling.
	// Empty — the default — believes no one.
	TrustedProxies string

	// SIPRealm is the realm the media engine challenges with, and the
	// realm every extension credential digest is computed over
	// (MD5(username:realm:secret)). It MUST match the engine's own
	// configuration: a mismatch produces credentials that can never
	// authenticate, and the failure appears only at registration time,
	// far from the code that generated them.
	//
	// Changing it invalidates every stored credential, so it is a
	// migration rather than a config edit (LLD-03 §7.3).
	SIPRealm string

	// SIPTransport and SIPWebRTCTransport name transports defined in the
	// engine's configuration. WebRTC needs a WSS transport that lands
	// with the engine-config task; until then a WEBRTC extension projects
	// against a transport that does not exist.
	SIPTransport       string
	SIPWebRTCTransport string

	// SIPPublicHost, SIPPublicPort and SIPPublicTransport are the address
	// devices register to: the installation's public face, which only the
	// installer knows (NAT, DNS, port mapping). An empty host means not
	// configured, and the API says so rather than guessing
	// (pbx-sip-registrar-settings).
	SIPPublicHost      string
	SIPPublicPort      int
	SIPPublicTransport string

	// TrunkProbeInterval is how often every trunk's carrier is checked
	// with SIP OPTIONS (pbx-trunk-health-probing, D-74). Zero turns the
	// schedule off; a trunk is still checked when created or re-addressed.
	TrunkProbeInterval time.Duration

	// LicenseToken is a signed licence in the compact form
	// domain.SplitToken decodes, supplied so a compose stack, a Helm
	// chart or a CI job can activate an installation without a portal
	// round trip (D-52 §10.5, D-54).
	//
	// It carries a TOKEN, never a mode. There is deliberately no
	// "licensing off" or "dev licensing" setting: verification always
	// runs, and what differs between builds is which keys are trusted,
	// injected at build time and empty by default (D-54). A single
	// configuration value that disabled licensing would defeat D-11,
	// D-13, D-14 and D-53 at once, and would be the first thing found
	// and shared.
	//
	// Empty is normal: an installation with no token is in Setup —
	// administration works, there is no call path — until one is applied
	// here or through the console.
	LicenseToken string
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
		JWTPrivateKeyHex:  getEnv("ATSAPBX_JWT_KEY", ""),
		// "asterisk" is PJSIP's own default realm, so the default here
		// matches an unconfigured engine rather than silently disagreeing
		// with one.
		SIPRealm:           getEnv("SIP_REALM", "asterisk"),
		SIPTransport:       getEnv("SIP_TRANSPORT", "transport-udp"),
		SIPWebRTCTransport: getEnv("SIP_WEBRTC_TRANSPORT", "transport-wss"),
		SIPPublicHost:      getEnv("SIP_PUBLIC_HOST", ""),
		SIPPublicTransport: getEnv("SIP_PUBLIC_TRANSPORT", "udp"),
		LicenseToken:       getEnv("ATSAPBX_LICENSE_TOKEN", ""),
		JWTPreviousKeysHex: getEnv("ATSAPBX_JWT_PREVIOUS_KEYS", ""),
		ConsoleURL:         getEnv("ATSAPBX_CONSOLE_URL", "http://localhost:8080"),
		SMTPAddr:           getEnv("ATSAPBX_SMTP_ADDR", ""),
		SMTPFrom:           getEnv("ATSAPBX_SMTP_FROM", "AtsaPBX <no-reply@localhost>"),
		SMTPUser:           getEnv("ATSAPBX_SMTP_USER", ""),
		SMTPPassword:       getEnv("ATSAPBX_SMTP_PASSWORD", ""),
		SMTPInsecure:       getEnv("ATSAPBX_SMTP_INSECURE", "") == "true",
		CookieInsecure:     getEnv("ATSAPBX_COOKIE_INSECURE", "") == "true",
		TrustedProxies:     getEnv("ATSAPBX_TRUSTED_PROXIES", ""),
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
	if cfg.JWTPrivateKeyHex == "" {
		return cfg, fmt.Errorf("ATSAPBX_JWT_KEY is required")
	}
	if err := cfg.loadSIPPublic(); err != nil {
		return cfg, err
	}
	interval, err := time.ParseDuration(getEnv("ATSAPBX_TRUNK_PROBE_INTERVAL", "30s"))
	if err != nil || interval < 0 || (interval > 0 && interval < 5*time.Second) {
		return cfg, fmt.Errorf("ATSAPBX_TRUNK_PROBE_INTERVAL must be a duration of 5s or more, or 0 to turn scheduled checks off")
	}
	cfg.TrunkProbeInterval = interval

	return cfg, nil
}

// loadSIPPublic reads and checks the public registrar settings. A bad
// value fails startup, where it is cheap to see, rather than at a phone.
func (c *Config) loadSIPPublic() error {
	port, err := strconv.Atoi(getEnv("SIP_PUBLIC_PORT", "5060"))
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("SIP_PUBLIC_PORT must be a port number 1-65535")
	}
	c.SIPPublicPort = port
	switch c.SIPPublicTransport {
	case "udp", "tcp", "tls":
	default:
		return fmt.Errorf("SIP_PUBLIC_TRANSPORT must be udp, tcp or tls")
	}
	if c.SIPPublicHost != "" {
		// The same rule as trunk hosts: a hostname or an IP address, with
		// no scheme, port or user part (LLD-03 §10.5).
		if err := pbxdomain.ValidateHost(c.SIPPublicHost); err != nil {
			return fmt.Errorf("SIP_PUBLIC_HOST must be a hostname or an IP address, with no scheme or port")
		}
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
