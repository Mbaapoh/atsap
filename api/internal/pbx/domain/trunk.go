package domain

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
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

// Failed reports whether h means the carrier is not fully serving calls.
func (h TrunkHealth) Failed() bool {
	return h == HealthDegraded || h == HealthUnhealthy
}

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
	// ReasonAddressNotAllowed is the trunk's host name resolving only to
	// addresses that cannot be a carrier (D-76): nothing was sent.
	ReasonAddressNotAllowed HealthReason = "ADDRESS_NOT_ALLOWED"
	// ReasonCallSetupFailures is a trunk degraded by the rolling call
	// setup failure rate, not by the OPTIONS checks (D-80, HLD T-6).
	ReasonCallSetupFailures HealthReason = "CALL_SETUP_FAILURES"
)

// Valid reports whether r is one of the defined reasons.
func (r HealthReason) Valid() bool {
	switch r {
	case ReasonAnswering, ReasonSlowResponse, ReasonHostNotFound, ReasonNoResponse, ReasonCarrierError, ReasonAddressNotAllowed, ReasonCallSetupFailures:
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
	ErrTrunkHostNotCarrier   = errors.New("trunk host cannot be a carrier: localhost, or a loopback, link-local, unspecified or multicast address")
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
	// HealthySince is when the current run of healthy checks began while
	// the trunk is recovering; nil when it is Healthy, or not recovering
	// (D-80, HLD T-6).
	HealthySince *time.Time
	// ResolvedAddresses are what the carrier is recognised by (D-75): the
	// host itself when it is an IP address, otherwise what the platform
	// last resolved the name to. ResolvedAt is when a name last resolved;
	// nil for an IP host or a name not resolved yet.
	ResolvedAddresses []netip.Addr
	ResolvedAt        *time.Time
	// InboundAddresses are the carrier's published signalling addresses
	// and ranges (D-76): configuration, recognised alongside the host's
	// addresses. Normalised by ParseInboundAddresses.
	InboundAddresses []netip.Prefix
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
	if err := ValidateInboundAddresses(t.InboundAddresses); err != nil {
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

// ValidateHost is the rule for a trunk's host (D-76): a valid hostname
// or IP literal (ValidateHostSyntax) that can also be a carrier — not
// localhost or *.localhost (RFC 6761), nor a loopback, link-local,
// unspecified or multicast literal.
func ValidateHost(host string) error {
	if err := ValidateHostSyntax(host); err != nil {
		return err
	}
	if net.ParseIP(host) != nil {
		if a, err := netip.ParseAddr(host); err != nil || !CarrierAddress(a) {
			return fmt.Errorf("%w: %q", ErrTrunkHostNotCarrier, host)
		}
		return nil
	}
	if name := strings.ToLower(strings.TrimSuffix(host, ".")); name == "localhost" || strings.HasSuffix(name, ".localhost") {
		return fmt.Errorf("%w: %q", ErrTrunkHostNotCarrier, host) // RFC 6761 §6.3
	}
	return nil
}

// ValidateHostSyntax accepts an IP literal or a DNS hostname and nothing
// else. Allowlisted by construction, not by expression: a host ends up in
// engine configuration (a contact URI, a From domain, a registrar
// address), so anything that is not provably a name or an address is
// refused — no scheme, no port, no user part, no whitespace (LLD-03
// §10.5). It says nothing about what the host is for: the installation's
// own registrar address may well be localhost on a development rig.
func ValidateHostSyntax(host string) error {
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
	// ProbeAddressNotAllowed is the trunk's host giving only addresses
	// that cannot be a carrier (D-76); no check was sent.
	ProbeAddressNotAllowed
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
	case outcome == ProbeAddressNotAllowed:
		return HealthUnhealthy, ReasonAddressNotAllowed
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

// HLD T-6 (D-80) health thresholds. They are deliberately constants, not
// configuration: the owner chose T-6 exactly as written.
const (
	// DefaultProbeInterval is how often a carrier is checked.
	DefaultProbeInterval = 10 * time.Second
	// DegradeAfterFailures is the consecutive failed checks that degrade.
	DegradeAfterFailures = 3
	// CallWindow is the rolling window for call-setup outcomes.
	CallWindow = 60 * time.Second
	// MinCallSamples is the smallest window that can degrade a trunk.
	MinCallSamples = 5
	// CallFailureRate is the fraction above which the window degrades.
	CallFailureRate = 0.20
	// RecoveryWindow is how long healthy checks must continue to recover.
	RecoveryWindow = 5 * time.Minute
)

// HealthSource is where one health observation came from.
type HealthSource string

// The sample sources: a platform check, and one call-setup outcome.
const (
	HealthFromProbe HealthSource = "PROBE"
	HealthFromCall  HealthSource = "CALL"
)

// Valid reports whether s is a defined sample source.
func (s HealthSource) Valid() bool {
	return s == HealthFromProbe || s == HealthFromCall
}

// HealthSample is one observation: a check, or one call-setup outcome.
// Healthy is whether it counts as a healthy check for the recovery
// window; a slow answer is not.
type HealthSample struct {
	Source  HealthSource
	Healthy bool
	Reason  HealthReason
	At      time.Time
}

// HealthState is the data the health decision needs: the stored state and
// the rolling counts ending at the latest sample.
type HealthState struct {
	Health                   TrunkHealth
	Reason                   HealthReason
	HealthySince             *time.Time
	ConsecutiveProbeFailures int
	CallTotal                int
	CallFailures             int
}

// HealthDecision is what the stored state should become.
type HealthDecision struct {
	Health       TrunkHealth
	Reason       HealthReason
	HealthySince *time.Time
	// Changed is true only when the health state itself changed; that is
	// the transition that alerts the operator (D-80).
	Changed bool
}

// DecideHealth applies HLD T-6 to one sample. It is pure: the store
// supplies the state and rolling counts, and applies the result.
func DecideHealth(state HealthState, sample HealthSample) HealthDecision {
	current := state.Health
	if current == "" {
		current = HealthUnknown
	}
	next, reason, since := current, state.Reason, state.HealthySince

	callFailed := state.CallTotal >= MinCallSamples &&
		float64(state.CallFailures) > CallFailureRate*float64(state.CallTotal)

	if sample.Source == HealthFromProbe {
		configFailure := sample.Reason == ReasonHostNotFound || sample.Reason == ReasonAddressNotAllowed
		switch {
		case configFailure:
			next, reason, since = HealthUnhealthy, sample.Reason, nil
		case sample.Reason == ReasonSlowResponse:
			next, reason, since = HealthDegraded, sample.Reason, nil
		case !sample.Healthy && state.ConsecutiveProbeFailures >= DegradeAfterFailures:
			next, reason, since = HealthDegraded, sample.Reason, nil
		case sample.Healthy:
			switch current {
			case HealthHealthy, HealthUnknown:
				next, reason, since = HealthHealthy, ReasonAnswering, nil
			default:
				// Recovering from Degraded/Unhealthy: keep the state
				// until the window has run.
				if since == nil {
					at := sample.At
					since = &at
				}
				if sample.At.Sub(*since) >= RecoveryWindow {
					next, reason, since = HealthHealthy, ReasonAnswering, nil
				}
			}
		default:
			// A failed check below the threshold. A healthy trunk stays
			// healthy; an unknown, degraded or unhealthy one keeps its
			// state, and the recovery window restarts.
			if current == HealthHealthy {
				next, reason = HealthHealthy, ReasonAnswering
			}
			since = nil
		}
	}

	// The call window can degrade a trunk on its own, but never overrides
	// a configuration failure that already makes it Unhealthy.
	if callFailed && next != HealthUnhealthy {
		next, reason, since = HealthDegraded, ReasonCallSetupFailures, nil
	}

	return HealthDecision{Health: next, Reason: reason, HealthySince: since, Changed: next != current}
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
