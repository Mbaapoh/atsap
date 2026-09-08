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
)

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
	List(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, page Page) ([]domain.Extension, error)
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

	// RegistrationStatus reports which of ids currently have a device
	// registered. It takes a slice because the extensions list screen
	// needs a page of statuses, and a per-row call would make one screen
	// N queries against engine state. Ids absent from the result are
	// reported as not registered by the caller, never guessed at here.
	RegistrationStatus(ctx context.Context, ids []shareddomain.ExtensionID) (map[shareddomain.ExtensionID]domain.RegistrationStatus, error)
}
