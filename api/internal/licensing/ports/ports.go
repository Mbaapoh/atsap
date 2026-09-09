// Package ports declares licensing's contracts: the full LicenseManager
// this context provides (HLD 04 §4), and the persistence it needs.
//
// It does NOT replace telephony/ports.LicenseManager. That one is the
// consumer's narrow view — the two methods telephony-core actually calls
// — and this one is the provider's full contract. Merging them would
// require licensing to import telephony/ports for its verdict type,
// making a Tier-0 context depend on a Tier-0 peer, which HLD 04 §10.1
// denies. cmd/atsap-api adapts between them (LLD-08 §3.1).
package ports

import (
	"context"
	"errors"
	"time"

	"atsap-api/internal/licensing/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

// LicenseManager is HLD 04 §4's contract, as licensing provides it.
type LicenseManager interface {
	// ValidateCapacity reserves requestedChannels for callID against the
	// entitlement in force.
	//
	// callID is why this signature differs from HLD 04 §4's literal
	// shape, and the difference is load-bearing: a reservation keyed by
	// call is what lets ReleaseCapacity be idempotent rather than a
	// blind decrement (D-58). Reserving twice for one call takes one
	// channel; releasing twice frees one.
	ValidateCapacity(ctx context.Context, tenantID shareddomain.TenantID, callID string, requestedChannels int) (domain.CapacityVerdict, error)

	// ReleaseCapacity returns callID's reservation. Releasing a call
	// that holds none — refused, already released, or a second
	// termination signal — is a no-op, not an error (D-58).
	ReleaseCapacity(ctx context.Context, callID string) error

	// ApplyLicenseKey verifies and stores a licence supplied in the
	// compact form domain.SplitToken decodes.
	//
	// Idempotent: applying the same token again succeeds, changes
	// nothing, and does not refresh the last-confirmation time — startup
	// intake runs on every boot, so re-application must never restart
	// the grace clock (D-58).
	ApplyLicenseKey(ctx context.Context, token string) error

	// VerifyDailyEntitlement performs the scheduled confirmation and
	// reports the resulting state. Never called during call setup
	// (AC-06.3).
	VerifyDailyEntitlement(ctx context.Context) (domain.State, error)

	// Entitlement reports what the installation may currently do,
	// including the extension and tenant caps this context publishes but
	// does not enforce (D-49, D-51). identity and pbx-core read these
	// through ports they own; licensing never calls them.
	Entitlement(ctx context.Context) (domain.Entitlement, error)
}

// Store is the persistence licensing needs.
//
// It returns the SIGNED PAYLOAD, never a parsed entitlement (D-53). The
// store's job is to hand back bytes and let the domain decide whether to
// believe them; a store that returned domain.Entitlement would have
// decided already, and the claim columns would be back in the trust path
// they were removed from.
type Store interface {
	// Load returns the stored licence. ErrNoLicense when none has been
	// applied — the Setup state, which is not an error condition and not
	// the degraded floor (D-52).
	Load(ctx context.Context) (StoredLicense, error)

	// Save replaces the stored licence.
	Save(ctx context.Context, lic StoredLicense) error

	// TouchConfirmed records a successful entitlement confirmation. It
	// is separate from Save because confirmation moves a timestamp and
	// must never rewrite the licence itself.
	TouchConfirmed(ctx context.Context, at time.Time) error
}

// StoredLicense is a licence as it sits in the database: the payload and
// signature that are the entitlement of record, plus the confirmation
// timestamps that drive the grace period.
type StoredLicense struct {
	InstanceID      string
	SignedPayload   []byte
	Signature       []byte
	LastConfirmedAt time.Time
	GraceStartedAt  *time.Time

	// Display is the human-readable copy of what SignedPayload says,
	// written so an administrator or a support engineer can read the row
	// without a tool. It has no authority whatsoever (D-53).
	//
	// Note the asymmetry, which is the point: Save writes Display, and
	// Load NEVER populates it. There is therefore no path from a claim
	// column back into an entitlement decision — not a rule someone must
	// remember, but a value that simply is not there to be read. Editing
	// those columns in the database changes nothing, and the integration
	// test in internal/postgres proves it.
	Display DisplayClaims
}

// DisplayClaims is the cache written to licensing_state's readable
// columns. Populated by the application layer from an already-verified
// token, never by the store, and never read back.
type DisplayClaims struct {
	Edition       string
	Capacity      int
	MaxTenants    int
	MaxExtensions int
	Fingerprint   []string
	Status        string
}

// ErrNoLicense means no licence has ever been applied: the Setup state
// (D-52). It is a normal condition, not a failure — a fresh installation
// reports it on every load until a token is applied, and the caller
// answers with domain.SetupEntitlement rather than a capacity floor.
var ErrNoLicense = errors.New("licensing: no licence has been applied")
