// Package ports defines pbx-core's hexagonal port interfaces: the
// inbound ConfigService the console and partners drive, plus the
// outbound storage and projection ports its implementation needs. This
// package has no implementation — application implements the inbound
// port, postgres and acl/asterisk implement the outbound ones, and
// neither domain nor application depends on a concrete adapter
// (docs/hld/01-architecture.md §1.2).
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

// Errors every implementation reports in common, so callers branch on
// cause rather than on message text.
//
// ErrNotFound is deliberately what a caller gets for an extension in
// another tenant as well as for one that exists nowhere. Distinguishing
// them would confirm the existence of another tenant's resource, which
// is reconnaissance (INV-10).
var (
	ErrNotFound     = errors.New("not found")
	ErrNumberTaken  = errors.New("extension number already exists in this tenant")
	ErrInvalidInput = errors.New("invalid input")

	// ErrEntitlementExceeded is the installation lacking the licence to
	// create another extension, as distinct from the caller lacking the
	// permission. Never folded into a permission error, for the reason
	// identity gives for its own (LLD-02 §4a.1, AC-06.14).
	ErrEntitlementExceeded = errors.New("licence entitlement exceeded")
)

// ExtensionCap supplies the installation's extension cap — the mirror of
// identity's TenantCap (D-51, license-provisioning-enforcement design
// D1).
//
// pbx-core owns this port and the composition root satisfies it, so the
// cap is handed in and pbx-core never imports licensing. It carries a
// number and nothing else: no edition, no capability flags, which keeps
// the commercial catalogue out of a telephony context (design D3).
//
// Zero means unlimited. An error fails creation closed.
type ExtensionCap interface {
	MaxExtensions(ctx context.Context) (int, error)
}

// ExtensionCapFunc adapts a function to ExtensionCap.
type ExtensionCapFunc func(ctx context.Context) (int, error)

// MaxExtensions calls f.
func (f ExtensionCapFunc) MaxExtensions(ctx context.Context) (int, error) { return f(ctx) }

// CreateExtensionCommand is what a caller supplies to create an
// extension. It carries no credential fields: the platform generates
// both the auth username and the secret, and a caller-supplied value is
// refused rather than honoured (LLD-03 §10.3).
type CreateExtensionCommand struct {
	TenantID    shareddomain.TenantID
	Number      string
	DisplayName string
	DeviceType  domain.DeviceType
}

// UpdateExtensionCommand changes the mutable fields of an extension.
// Number is immutable: changing it would silently orphan every device
// provisioned against it, so a renumber is a delete plus a create, made
// visible to the operator.
type UpdateExtensionCommand struct {
	TenantID    shareddomain.TenantID
	ID          shareddomain.ExtensionID
	DisplayName string
	DeviceType  domain.DeviceType
}

// ExtensionView is what a caller sees. It carries no SecretDigest and no
// engine identifier — deliberately a different type from
// domain.Extension so that neither can be returned by accident
// (PRD principle 4, LLD-03 §7.1).
type ExtensionView struct {
	ID           shareddomain.ExtensionID
	Number       string
	DisplayName  string
	AuthUsername string
	DeviceType   domain.DeviceType
	Registration domain.RegistrationStatus
}

// CreatedExtension is the create/regenerate result. Secret is the
// generated plaintext, returned exactly once and never retrievable
// afterwards — the same contract IssueApiKey uses. Never log it, never
// audit it, never persist it.
type CreatedExtension struct {
	Extension ExtensionView
	Secret    string
}

// Page bounds a listing.
type Page struct {
	Limit  int
	Offset int
}

// CreateTrunkCommand is what a caller supplies to connect a carrier.
//
// AuthUsername and Secret are both optional: neither means an
// IP-authenticated carrier, a secret without a username is refused
// (design D7). There is deliberately no health field — health is
// observed, never configured.
type CreateTrunkCommand struct {
	TenantID     shareddomain.TenantID
	Name         string
	Host         string
	Port         int
	AuthUsername string
	// Secret is the carrier password. It is used once, for projection, and
	// is never returned, logged or audited. Never log this struct.
	Secret       string
	ChannelLimit int
	Priority     int
}

// UpdateTrunkCommand replaces a trunk's configuration.
//
// An empty Secret keeps the stored one; a non-empty Secret replaces it.
// An empty AuthUsername turns the trunk into an IP-authenticated carrier
// and drops any stored secret with it, since a secret without a
// username cannot be presented. Health is not updatable.
type UpdateTrunkCommand struct {
	TenantID     shareddomain.TenantID
	ID           shareddomain.TrunkID
	Name         string
	Host         string
	Port         int
	AuthUsername string
	Secret       string
	ChannelLimit int
	Priority     int
}

// TrunkView is what a caller sees: never the secret, never an engine
// identifier. HasSecret says whether one is stored, which is all a
// console needs to render a password field as set.
type TrunkView struct {
	ID           shareddomain.TrunkID
	Name         string
	Host         string
	Port         int
	AuthUsername string
	HasSecret    bool
	ChannelLimit int
	Priority     int
	Health       domain.TrunkHealth
}

// CreateRouteCommand is what a caller supplies to add an outbound route.
type CreateRouteCommand struct {
	TenantID      shareddomain.TenantID
	PrefixPattern string
	TrunkID       shareddomain.TrunkID
	CostPerMinute int64
	Priority      int
}

// RouteView is what a caller sees of a route.
type RouteView struct {
	ID            shareddomain.RouteID
	PrefixPattern string
	TrunkID       shareddomain.TrunkID
	CostPerMinute int64
	Priority      int
}

// ConfigService is pbx-core's inbound port: the configuration API the
// console drives and a partner may call directly. Every method
// authorizes before acting and operates only within the caller's tenant.
type ConfigService interface {
	CreateExtension(ctx context.Context, cmd CreateExtensionCommand) (CreatedExtension, error)
	UpdateExtension(ctx context.Context, cmd UpdateExtensionCommand) error
	DeleteExtension(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.ExtensionID) error
	ListExtensions(ctx context.Context, tenantID shareddomain.TenantID, page Page) ([]ExtensionView, error)
	GetExtension(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.ExtensionID) (ExtensionView, error)
	RegenerateSecret(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.ExtensionID) (CreatedExtension, error)

	// Trunk and route operations mirror the extension ones (LLD-03 §4.4).
	CreateTrunk(ctx context.Context, cmd CreateTrunkCommand) (TrunkView, error)
	UpdateTrunk(ctx context.Context, cmd UpdateTrunkCommand) (TrunkView, error)
	DeleteTrunk(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.TrunkID) error
	ListTrunks(ctx context.Context, tenantID shareddomain.TenantID, page Page) ([]TrunkView, error)
	CreateRoute(ctx context.Context, cmd CreateRouteCommand) (RouteView, error)
	DeleteRoute(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.RouteID) error
	ListRoutes(ctx context.Context, tenantID shareddomain.TenantID, page Page) ([]RouteView, error)
}

// RoutePlanner is exactly HLD 04 §2: no methods added, no signatures
// changed. Port only — never on the wire (LLD-03 §6): a client asking
// which trunk would be picked, separately from placing the call, is an
// oracle whose answer can change between the two.
//
// ReportRouteHealth carries no tenant argument, because HLD 04 §2 fixes
// the signature; the tenant is the authenticated caller's, read from ctx,
// as every other pbx-core operation reads its actor.
type RoutePlanner interface {
	SelectOutboundRoute(ctx context.Context, tenantID shareddomain.TenantID, destination string) ([]domain.TrunkRoute, error)
	ReportRouteHealth(ctx context.Context, trunkID shareddomain.TrunkID, statusCode int, latency time.Duration) error
}

// TrunkStore is the outbound storage port for trunks. Like
// ExtensionStore, every method takes the caller's transaction, and
// tenancy is enforced by its RLS context.
type TrunkStore interface {
	Insert(ctx context.Context, tx pgx.Tx, trunk domain.Trunk) error
	Update(ctx context.Context, tx pgx.Tx, trunk domain.Trunk) error
	// Delete MUST report ErrNotFound when it removed nothing: RemoveTrunk
	// runs on tables with no RLS, and is gated by this (design D9 of the
	// extensions change, unchanged).
	Delete(ctx context.Context, tx pgx.Tx, id shareddomain.TrunkID) error
	Get(ctx context.Context, tx pgx.Tx, id shareddomain.TrunkID) (domain.Trunk, error)
	List(ctx context.Context, tx pgx.Tx, page Page) ([]domain.Trunk, error)
	// SetHealth records an observed health. It is the only writer of
	// health, and nothing on the configuration path calls it.
	SetHealth(ctx context.Context, tx pgx.Tx, id shareddomain.TrunkID, health domain.TrunkHealth) error
}

// RouteStore is the outbound storage port for routes.
type RouteStore interface {
	Insert(ctx context.Context, tx pgx.Tx, route domain.CarrierRoute) error
	// Delete reports ErrNotFound when it removed nothing.
	Delete(ctx context.Context, tx pgx.Tx, id shareddomain.RouteID) error
	Get(ctx context.Context, tx pgx.Tx, id shareddomain.RouteID) (domain.CarrierRoute, error)
	List(ctx context.Context, tx pgx.Tx, page Page) ([]domain.CarrierRoute, error)
	// DeleteByTrunk removes every route sending to trunkID and returns
	// them, so a trunk deletion can audit what it took with it.
	DeleteByTrunk(ctx context.Context, tx pgx.Tx, trunkID shareddomain.TrunkID) ([]domain.CarrierRoute, error)
}

// ExtensionStore is the outbound storage port.
//
// Every method takes the caller's transaction rather than opening its
// own. The domain row and its engine projection must commit or fail
// together (design D1), which is only expressible if both sides are
// handed the same tx. A store that could commit independently would
// reintroduce exactly the split-brain that choosing PJSIP Realtime over
// generate-and-reload was meant to remove.
type ExtensionStore interface {
	Insert(ctx context.Context, tx pgx.Tx, ext domain.Extension) error
	Update(ctx context.Context, tx pgx.Tx, ext domain.Extension) error

	// Delete removes the extension and MUST report ErrNotFound when it
	// removed nothing, rather than treating a no-op as success.
	//
	// The projection tables carry no RLS, so a caller that proceeds to
	// RemoveExtension on the strength of a "successful" delete that RLS
	// actually blocked would deprovision another tenant's endpoint
	// (design D9). Every projector call must be gated by a domain
	// operation that RLS authorized; this is where that gate lives.
	Delete(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, id shareddomain.ExtensionID) error

	Get(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, id shareddomain.ExtensionID) (domain.Extension, error)

	// CountForCreate counts the tenant's extensions after serializing
	// against every other creation in the same tenant until tx ends. A
	// plain count at READ COMMITTED would let two concurrent creations
	// both see room for one more and both commit.
	CountForCreate(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID) (int, error)
	List(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, page Page) ([]domain.Extension, error)
}

// ProjectionInventory is what the engine currently holds, expressed in
// domain terms wherever possible.
type ProjectionInventory struct {
	// Extensions are projected rows whose identifier maps back to an
	// extension.
	Extensions []shareddomain.ExtensionID
	// Trunks are projected rows whose identifier maps back to a trunk.
	Trunks []shareddomain.TrunkID
	// Unattributable are rows this ACL did not write and cannot explain —
	// hand-written, or left by a future object kind. Reported, never
	// repaired: deleting a row nobody can account for is how a reconciler
	// turns a mystery into an outage.
	Unattributable []string
}

// ProjectionReconciler is the diagnostic half of the engine port: what
// does the engine actually hold, and where does it disagree with us.
//
// Separate from EndpointProjector because the audiences differ. Writing a
// projection is something the ordinary use cases do; interrogating one is
// something only the reconcile command does, and keeping the two apart
// stops a handler acquiring the ability to go rummaging in engine state.
type ProjectionReconciler interface {
	// ListProjected reports every endpoint the engine currently holds.
	ListProjected(ctx context.Context) (ProjectionInventory, error)

	// DiffExtension names the projected fields that disagree with ext, or
	// the single field "missing" when the engine holds nothing for it.
	// Presence alone is not agreement: a hand-edited row leaves the
	// extension listed in the console and broken on the phone.
	DiffExtension(ctx context.Context, ext domain.Extension) ([]string, error)

	// DiffTrunk is DiffExtension for a trunk. The secret itself cannot be
	// compared — the platform does not hold it — so a missing secret is
	// reported as "secret", which no repair can restore.
	DiffTrunk(ctx context.Context, trunk domain.Trunk) ([]string, error)
}

// EndpointProjector is the outbound port onto the media engine's own
// configuration state — implemented by pbx/acl/asterisk, which is the
// only package permitted to name an engine table or identifier (D-41,
// D-47).
//
// The interface is written in domain terms: it takes an Extension and
// says "make the engine aware of this", never "write these three rows".
// A second engine could implement it without reshaping the port.
type EndpointProjector interface {
	// ProjectExtension makes ext registrable by a device. It runs inside
	// tx, so it cannot succeed while the domain write fails.
	ProjectExtension(ctx context.Context, tx pgx.Tx, ext domain.Extension) error

	// RemoveExtension deprovisions ext. Also inside tx.
	RemoveExtension(ctx context.Context, tx pgx.Tx, id shareddomain.ExtensionID) error

	// ProjectTrunk makes trunk usable as an outbound carrier, inside tx.
	//
	// secret is the carrier password and is written only to the engine's
	// auth state (LLD-03 §7.4). nil keeps whatever secret is already
	// projected, which is how an update that supplies no new secret
	// leaves it unchanged; a trunk with no AuthUsername has no auth state
	// at all.
	ProjectTrunk(ctx context.Context, tx pgx.Tx, trunk domain.Trunk, secret []byte) error

	// RemoveTrunk deprovisions a trunk. Also inside tx, and only after a
	// domain delete RLS authorized.
	RemoveTrunk(ctx context.Context, tx pgx.Tx, id shareddomain.TrunkID) error

	// RegistrationStatus reports which of ids currently have a device
	// registered. It takes a slice because the extensions list screen
	// needs a page of statuses, and a per-row call would make one screen
	// N queries against engine state. Ids absent from the result are
	// reported as not registered by the caller, never guessed at here.
	RegistrationStatus(ctx context.Context, ids []shareddomain.ExtensionID) (map[shareddomain.ExtensionID]domain.RegistrationStatus, error)
}
