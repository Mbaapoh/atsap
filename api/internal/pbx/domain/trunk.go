package domain

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	shareddomain "atsap-api/internal/shared/domain"
)

// TrunkHealth is what the platform has observed about a carrier. It is
// never set by a caller: it changes only through observations — the
// platform's own checks and the outcomes of calls through the trunk — and
// a new trunk is Unknown until its first one (pbx-trunk-health-probing).
type TrunkHealth string

const (
	// HealthUnknown carriers have not been observed yet. They are selected
	// as healthy ones are, so a trunk works as soon as it is created.
	HealthUnknown TrunkHealth = "UNKNOWN"
	// HealthHealthy carriers are selected normally.
	HealthHealthy TrunkHealth = "HEALTHY"
	// HealthDegraded carriers are still selected, but only after every
	// healthy candidate.
	HealthDegraded TrunkHealth = "DEGRADED"
	// HealthUnhealthy carriers are skipped by selection entirely.
	HealthUnhealthy TrunkHealth = "UNHEALTHY"
)

// Valid reports whether h is one of the three observed states.
func (h TrunkHealth) Valid() bool {
	switch h {
	case HealthUnknown, HealthHealthy, HealthDegraded, HealthUnhealthy:
		return true
	}
	return false
}

// HealthReason says why a trunk has the health it has.
type HealthReason string

const (
	// ReasonAnswering is the carrier responding promptly.
	ReasonAnswering HealthReason = "ANSWERING"
	// ReasonSlowResponse is the carrier responding, but slowly.
	ReasonSlowResponse HealthReason = "SLOW_RESPONSE"
	// ReasonHostNotFound is the trunk's host name not resolving.
	ReasonHostNotFound HealthReason = "HOST_NOT_FOUND"
	// ReasonNoResponse is the carrier not responding in time.
	ReasonNoResponse HealthReason = "NO_RESPONSE"
	// ReasonCarrierError is the carrier responding with a server error.
	ReasonCarrierError HealthReason = "CARRIER_ERROR"
)

// Valid reports whether r is one of the defined reasons.
func (r HealthReason) Valid() bool {
	switch r {
	case ReasonAnswering, ReasonSlowResponse, ReasonHostNotFound, ReasonNoResponse, ReasonCarrierError:
		return true
	}
	return false
}

// HealthObservation is one observation of a carrier: the health it
// implies, why, and when it was made.
type HealthObservation struct {
	Health TrunkHealth
	Reason HealthReason
	At     time.Time
}

// Trunk field bounds. Each matches its column width.
const (
	MaxTrunkNameLength    = 100
	MaxHostLength         = 253 // a DNS name's own limit, inside the column's 255
	MaxAuthUsernameLength = 100
	MaxTrunkSecretLength  = 255
	DefaultSIPPort        = 5060
)

// Validation errors, one per field, so a refusal names what to fix
// (spec trunk-routing: "a reason identifying the offending field").
var (
	ErrTrunkNameEmpty        = errors.New("trunk name is required")
	ErrTrunkNameTooLong      = errors.New("trunk name is longer than the maximum length")
	ErrTrunkHostInvalid      = errors.New("trunk host is not a valid hostname or IP address")
	ErrTrunkPortInvalid      = errors.New("trunk port must be between 1 and 65535")
	ErrChannelLimitInvalid   = errors.New("trunk channel limit must be positive")
	ErrPriorityInvalid       = errors.New("priority must not be negative")
	ErrAuthUsernameInvalid   = errors.New("trunk auth username contains a character that is not permitted")
	ErrAuthUsernameTooLong   = errors.New("trunk auth username is longer than the maximum length")
	ErrSecretWithoutUsername = errors.New("trunk secret requires an auth username")
	ErrTrunkSecretInvalid    = errors.New("trunk secret is empty, too long, or contains a control character")
)

// Trunk is a partner-owned SIP carrier connection within one tenant.
//
// AuthUsername is configuration, not a secret, and is returned on reads,
// as every SIP-trunk UI shows it. The secret is not a field: it travels
// once, from the caller to the projector, and the domain row holds only
// CredentialRef (LLD-03 §7.4, design D7). Empty AuthUsername and
// CredentialRef together mean an IP-authenticated carrier.
type Trunk struct {
	ID            shareddomain.TrunkID
	TenantID      shareddomain.TenantID
	Name          string
	Host          string
	Port          int
	AuthUsername  string
	CredentialRef string
	ChannelLimit  int
	Priority      int
	Health        TrunkHealth
	// HealthReason and HealthObservedAt are empty and nil until the trunk
	// is first observed.
	HealthReason     HealthReason
	HealthObservedAt *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Validate checks every caller-supplied field. Health and CredentialRef
// are not caller-supplied and are not checked here.
func (t Trunk) Validate() error {
	if t.Name == "" {
		return ErrTrunkNameEmpty
	}
	if n := utf8.RuneCountInString(t.Name); n > MaxTrunkNameLength {
		return fmt.Errorf("%w: %d > %d", ErrTrunkNameTooLong, n, MaxTrunkNameLength)
	}
	if err := ValidateHost(t.Host); err != nil {
		return err
	}
	if t.Port < 1 || t.Port > 65535 {
		return fmt.Errorf("%w: %d", ErrTrunkPortInvalid, t.Port)
	}
	if t.ChannelLimit <= 0 {
		return fmt.Errorf("%w: %d", ErrChannelLimitInvalid, t.ChannelLimit)
	}
	if t.Priority < 0 {
		return fmt.Errorf("%w: %d", ErrPriorityInvalid, t.Priority)
	}
	return ValidateAuthUsername(t.AuthUsername)
}

// ValidateHost accepts an IP literal or a DNS hostname and nothing else.
//
// Allowlisted by construction, not by expression: the host ends up in
// engine configuration (a contact URI and a From domain), so anything
// that is not provably a name or an address is refused — no scheme, no
// port, no user part, no whitespace (LLD-03 §10.5).
func ValidateHost(host string) error {
	if host == "" || len(host) > MaxHostLength {
		return fmt.Errorf("%w: %q", ErrTrunkHostInvalid, host)
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("%w: %q", ErrTrunkHostInvalid, host)
		}
		for _, r := range label {
			if !isASCIIAlnum(r) && r != '-' {
				return fmt.Errorf("%w: %q", ErrTrunkHostInvalid, host)
			}
		}
	}
	return nil
}

// ValidateAuthUsername accepts an empty username (IP authentication) or
// one drawn from letters, digits and "-_.+". That covers the account
// identifiers carriers issue — numeric DIDs, account codes, e-mail-like
// local parts — while excluding every character with meaning in a SIP
// URI (@ : ; ? and whitespace).
func ValidateAuthUsername(username string) error {
	if len(username) > MaxAuthUsernameLength {
		return fmt.Errorf("%w: %d > %d", ErrAuthUsernameTooLong, len(username), MaxAuthUsernameLength)
	}
	for _, r := range username {
		if !isASCIIAlnum(r) && !strings.ContainsRune("-_.+", r) {
			return fmt.Errorf("%w: %q", ErrAuthUsernameInvalid, r)
		}
	}
	return nil
}

// ValidateTrunkSecret checks a secret supplied for username. An empty
// secret is valid (none supplied); a non-empty one requires a username,
// since digest authentication presents both (design D7).
func ValidateTrunkSecret(secret, username string) error {
	if secret == "" {
		return nil
	}
	if username == "" {
		return ErrSecretWithoutUsername
	}
	if len(secret) > MaxTrunkSecretLength {
		return ErrTrunkSecretInvalid
	}
	for _, r := range secret {
		if r < 0x20 || r == 0x7f {
			return ErrTrunkSecretInvalid
		}
	}
	return nil
}

func isASCIIAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// SlowAnswerThreshold is the health-observation threshold above which a
// trunk's answer is slow (design D8).
const SlowAnswerThreshold = 2 * time.Second

// ObserveHealth maps one observed call outcome onto a trunk health and
// its reason, and reports false when the outcome says nothing about the
// carrier.
//
// A 2xx is the carrier working: Healthy, or Degraded when it took longer
// than SlowAnswerThreshold. No response (0) and 408 are the carrier not
// answering, and any 5xx is the carrier failing: Unhealthy. Every other
// code — 404, 480, 486 and the like — describes the callee, and blaming
// the carrier for a busy line would take a working trunk out of service.
func ObserveHealth(statusCode int, latency time.Duration) (TrunkHealth, HealthReason, bool) {
	switch {
	case statusCode >= 200 && statusCode < 300:
		if latency > SlowAnswerThreshold {
			return HealthDegraded, ReasonSlowResponse, true
		}
		return HealthHealthy, ReasonAnswering, true
	case statusCode == 0, statusCode == 408:
		return HealthUnhealthy, ReasonNoResponse, true
	case statusCode >= 500 && statusCode < 600:
		return HealthUnhealthy, ReasonCarrierError, true
	default:
		return "", "", false
	}
}

// ProbeOutcome is what the platform's check of a carrier found.
type ProbeOutcome int

const (
	// ProbeAnswered is a final SIP response received.
	ProbeAnswered ProbeOutcome = iota
	// ProbeNoResponse is no final response within the check's budget.
	ProbeNoResponse
	// ProbeHostNotFound is the trunk's host not resolving.
	ProbeHostNotFound
)

// ProbeSlowThreshold is the response time above which a carrier that
// answers the platform's check is Degraded. An OPTIONS round trip to a
// working carrier takes tens of milliseconds; a second is trouble.
const ProbeSlowThreshold = time.Second

// ObserveProbe maps one check of a carrier onto a trunk health and its
// reason (pbx-trunk-health-probing design D2). Any final response below
// 500 is the carrier answering — including a 403 or 404 to an
// unauthenticated OPTIONS, which a carrier that is up commonly sends.
func ObserveProbe(outcome ProbeOutcome, statusCode int, rtt time.Duration) (TrunkHealth, HealthReason) {
	switch {
	case outcome == ProbeHostNotFound:
		return HealthUnhealthy, ReasonHostNotFound
	case outcome == ProbeNoResponse:
		return HealthUnhealthy, ReasonNoResponse
	case statusCode >= 500:
		return HealthUnhealthy, ReasonCarrierError
	case rtt > ProbeSlowThreshold:
		return HealthDegraded, ReasonSlowResponse
	default:
		return HealthHealthy, ReasonAnswering
	}
}

// TrunkCredentialRef is the reference a trunk row holds in place of its
// secret: where the secret rests, by the trunk's own identity, never the
// secret. Empty for a trunk with no secret.
func TrunkCredentialRef(id shareddomain.TrunkID) string {
	return "trunk-secret:" + id.String()
}

// trunkPrefix distinguishes a trunk's engine identifier from an
// extension's ("e_"), so the two kinds can never collide in the engine's
// flat namespace and an operator reading engine state can tell them
// apart (LLD-03 §7.1).
const trunkPrefix = "t_"

// TrunkEndpointIdentifier is the single identifier a trunk has in the
// media engine, derived from its UUID and never stored, so it cannot
// drift from the trunk it names. Like EndpointIdentifier it never
// appears in an API response, log line or error a user can see.
func TrunkEndpointIdentifier(id shareddomain.TrunkID) string {
	raw := uuid.UUID(id)
	return trunkPrefix + hex.EncodeToString(raw[:])
}

// TrunkIDFromIdentifier is the inverse of TrunkEndpointIdentifier, for
// reconciliation. It returns false for anything not produced by it.
func TrunkIDFromIdentifier(identifier string) (shareddomain.TrunkID, bool) {
	hexPart, ok := strings.CutPrefix(identifier, trunkPrefix)
	if !ok {
		return shareddomain.TrunkID{}, false
	}
	raw, err := hex.DecodeString(hexPart)
	if err != nil || len(raw) != 16 {
		return shareddomain.TrunkID{}, false
	}
	var u uuid.UUID
	copy(u[:], raw)
	return shareddomain.TrunkID(u), true
}
