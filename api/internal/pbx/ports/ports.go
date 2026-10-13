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

	"github.com/google/uuid"
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

	// ErrNotLicensed is an installation with no licence applied: nothing
	// is provisioned before activation (D-72). The cap port reports it,
	// so this context still never reads the licence itself.
	ErrNotLicensed = errors.New("no licence is applied")

	// ErrTrunkCapacity is every trunk selected for a destination being at
	// its channel limit (pbx-core/trunk-routing). A precondition, not a
	// fault: the call may succeed once a channel frees.
	ErrTrunkCapacity = errors.New("no selected trunk has a free channel")

	// ErrCallerNotRegistered is a place-call from an extension with no
	// registered device: there is nothing to ring, so nothing is set up.
	ErrCallerNotRegistered = errors.New("extension has no registered device")

	// ErrCallRefused is a call telephony-core's Screening refused —
	// licensed capacity or compliance. Wrapped with the refusal reason.
	ErrCallRefused = errors.New("call refused")

	// ErrInboundNumberTaken is a DID already assigned in the tenant.
	ErrInboundNumberTaken = errors.New("inbound number already assigned in this tenant")

	// ErrEmergencyNumberProtected is deleting (or removing the
	// emergency status of) 112 or a catalogue number for the tenant's
	// country: the floor no tenant can configure away (design D6).
	ErrEmergencyNumberProtected = errors.New("emergency number is built-in and cannot be removed")
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

// PlaceCallCommand places a call from one of the tenant's extensions to a
// destination as dialled. It carries nothing engine-shaped and nothing
// that could request a Screening bypass: whether a call is an emergency
// is decided by the dial plan alone (INV-01, LLD-03 §10.1).
type PlaceCallCommand struct {
	TenantID    shareddomain.TenantID
	ExtensionID shareddomain.ExtensionID
	Destination string
}

// PlacedCall is PlaceCall's result.
type PlacedCall struct {
	CallID shareddomain.CallID
}

// SipRegistrar is the installation's public SIP registrar address: where
// devices register (pbx-sip-registrar-settings). Configured is false when
// the installer has not named a public host; the other fields are then
// empty rather than guessed.
type SipRegistrar struct {
	Configured bool
	Host       string
	Port       int
	Transport  string
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
	// SipRegistrar returns where the tenant's devices register, to a
	// caller who may read its extensions.
	SipRegistrar(ctx context.Context, tenantID shareddomain.TenantID) (SipRegistrar, error)

	// Trunk and route operations mirror the extension ones (LLD-03 §4.4).
	CreateTrunk(ctx context.Context, cmd CreateTrunkCommand) (TrunkView, error)
	UpdateTrunk(ctx context.Context, cmd UpdateTrunkCommand) (TrunkView, error)
	DeleteTrunk(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.TrunkID) error
	ListTrunks(ctx context.Context, tenantID shareddomain.TenantID, page Page) ([]TrunkView, error)
	CreateRoute(ctx context.Context, cmd CreateRouteCommand) (RouteView, error)
	DeleteRoute(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.RouteID) error
	ListRoutes(ctx context.Context, tenantID shareddomain.TenantID, page Page) ([]RouteView, error)

	// PlaceCall and HangupCall are the dial plan's wire surface (LLD-03 §6).
	PlaceCall(ctx context.Context, cmd PlaceCallCommand) (PlacedCall, error)
	HangupCall(ctx context.Context, tenantID shareddomain.TenantID, callID shareddomain.CallID) error

	// Inbound numbers: the tenant's DIDs, each ringing one extension.
	CreateInboundNumber(ctx context.Context, cmd CreateInboundNumberCommand) (InboundNumberView, error)
	ListInboundNumbers(ctx context.Context, tenantID shareddomain.TenantID) ([]InboundNumberView, error)
	DeleteInboundNumber(ctx context.Context, tenantID shareddomain.TenantID, id uuid.UUID) error

	// Emergency profile: the tenant's emergency country and its own
	// additional numbers (design D6).
	SetEmergencyProfile(ctx context.Context, tenantID shareddomain.TenantID, countryCode string) error
	GetEmergencyProfile(ctx context.Context, tenantID shareddomain.TenantID) (EmergencyProfile, error)
	CreateEmergencyNumber(ctx context.Context, tenantID shareddomain.TenantID, number string) error
	ListEmergencyNumbers(ctx context.Context, tenantID shareddomain.TenantID) ([]EmergencyNumberView, error)
	DeleteEmergencyNumber(ctx context.Context, tenantID shareddomain.TenantID, number string) error
}

// CreateInboundNumberCommand assigns a DID to an extension in the same
// tenant. Number carries an optional leading "+".
type CreateInboundNumberCommand struct {
	TenantID    shareddomain.TenantID
	Number      string
	ExtensionID shareddomain.ExtensionID
}

// InboundNumberView is what a caller sees of a DID: no engine state,
// never another tenant's extension.
type InboundNumberView struct {
	ID          uuid.UUID
	Number      string
	ExtensionID shareddomain.ExtensionID
}

// EmergencyNumberView is one effective emergency number and where it
// comes from: BuiltIn (112 or the country's catalogue, not removable)
// or the tenant's own addition.
type EmergencyNumberView struct {
	Number  string
	BuiltIn bool
}

// EmergencyProfile is the tenant's emergency configuration as the
// console renders it.
type EmergencyProfile struct {
	// CountryCode is the tenant's emergency country, "" when unset.
	CountryCode string
	// CountrySet reports whether a country was ever chosen. InCatalogue
	// reports whether it names a catalogued country. Both false with an
	// empty code means "no country set"; set-but-uncatalogued means
	// "enter your numbers".
	CountrySet  bool
	InCatalogue bool
	Numbers     []EmergencyNumberView
}

// CallOutcome is what pbx-core needs to know about a call it placed.
type CallOutcome struct {
	StartedAt  time.Time
	AnsweredAt *time.Time
	Ended      bool
	// Reason is why the call ended, as telephony-core records it — for a
	// call screened out, the refusal (e.g. capacity). Never engine text.
	Reason string
}

// CallStatus reads a call's outcome. Satisfied at the root over
// telephony-core's call store. ErrNotFound for an unknown call.
type CallStatus interface {
	Status(ctx context.Context, tenantID shareddomain.TenantID, callID shareddomain.CallID) (CallOutcome, error)
}

// EndpointIdentity is what a projected endpoint name attributes back
// to: exactly one of an extension or a trunk.
type EndpointIdentity struct {
	ExtensionID shareddomain.ExtensionID
	TrunkID     shareddomain.TrunkID
	IsTrunk     bool
}

// EndpointAddresser turns domain identities into the engine addresses an
// origination needs. Implemented by pbx/acl/asterisk, the only package
// permitted engine vocabulary (D-41, D-47).
type EndpointAddresser interface {
	ExtensionAddress(id shareddomain.ExtensionID) string
	TrunkAddress(id shareddomain.TrunkID, dialled string) string
	// Identify attributes a projected endpoint name back to the domain
	// identity it was projected from (design D4): the arriving-call
	// path's translation of a source endpoint string, done here in the
	// ACL rather than in application. It reports false for anything
	// this ACL did not project — hand-written rows, future kinds —
	// which the router refuses as unknown.
	Identify(endpoint string) (EndpointIdentity, bool)
}

// CallLegStore persists placed calls (design D4). Every method takes the
// caller's transaction; tenancy is the transaction's RLS context.
type CallLegStore interface {
	// LockTrunk serializes every placement through trunkID until tx ends,
	// so a count of its live legs stays exact for the transaction.
	LockTrunk(ctx context.Context, tx pgx.Tx, trunkID shareddomain.TrunkID) error
	Insert(ctx context.Context, tx pgx.Tx, leg domain.PlacedCallLeg) error
	// AttachCall records the call id origination returned for a leg.
	AttachCall(ctx context.Context, tx pgx.Tx, legID uuid.UUID, callID shareddomain.CallID) error
	// LiveOnTrunk returns the unreleased legs on trunkID.
	LiveOnTrunk(ctx context.Context, tx pgx.Tx, trunkID shareddomain.TrunkID) ([]domain.PlacedCallLeg, error)
	// Release marks a leg's call as ended; releasing twice is a no-op.
	Release(ctx context.Context, tx pgx.Tx, legID uuid.UUID, at time.Time) error
	// Get reports ErrNotFound for another tenant's call or one never placed.
	Get(ctx context.Context, tx pgx.Tx, callID shareddomain.CallID) (domain.PlacedCallLeg, error)
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

// InboundNumberStore is the outbound storage port for DIDs. Every
// method takes the caller's transaction; tenancy is its RLS context.
type InboundNumberStore interface {
	// Insert persists a DID already normalized to digits. A duplicate
	// number in the tenant is ErrInboundNumberTaken; the composite
	// foreign key refuses another tenant's extension at the database.
	Insert(ctx context.Context, tx pgx.Tx, number domain.InboundNumber) error
	// List returns the tenant's DIDs in assignment order.
	List(ctx context.Context, tx pgx.Tx) ([]domain.InboundNumber, error)
	// Delete removes one DID, reporting ErrNotFound when it removed
	// nothing (another tenant's DID included).
	Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
	// FindByNumber resolves a DID for the arriving-call path.
	// ErrNotFound when unassigned. number must already be normalized.
	FindByNumber(ctx context.Context, tx pgx.Tx, number string) (domain.InboundNumber, error)
	// DeleteByExtension removes every DID pointing at extensionID and
	// returns them, so an extension deletion can audit what it took
	// with it (design D7, mirroring trunk/route deletion).
	DeleteByExtension(ctx context.Context, tx pgx.Tx, extensionID shareddomain.ExtensionID) ([]domain.InboundNumber, error)
}

// EmergencyStore is the outbound storage port for the emergency
// profile. Every method takes the caller's transaction; tenancy is its
// RLS context. Only the tenant's OWN additions live in emergency_numbers
// — 112 and the catalogue are code — and only the country code lives in
// emergency_settings.
type EmergencyStore interface {
	// GetCountry returns the tenant's emergency country and whether one
	// was ever set.
	GetCountry(ctx context.Context, tx pgx.Tx) (string, bool, error)
	// SetCountry replaces the tenant's emergency country.
	SetCountry(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, countryCode string) error
	// ListAdditions returns the tenant's own numbers, ascending.
	ListAdditions(ctx context.Context, tx pgx.Tx) ([]string, error)
	// AddAddition records one of the tenant's own numbers. Adding what
	// is already there succeeds idempotently.
	AddAddition(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, number string) error
	// DeleteAddition removes one of the tenant's own numbers,
	// reporting ErrNotFound when it removed nothing.
	DeleteAddition(ctx context.Context, tx pgx.Tx, number string) error
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

	// FindByNumber resolves a tenant-local extension number, the dial
	// plan's internal lookup. ErrNotFound when no extension has it.
	FindByNumber(ctx context.Context, tx pgx.Tx, number string) (domain.Extension, error)
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
