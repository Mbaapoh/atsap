// Package application orchestrates pbx-core's use cases. It depends on
// pbx/domain and pbx/ports only — never on a concrete adapter
// (docs/hld/01-architecture.md §1.2) — and never on the media engine's
// vocabulary, which stops at pbx/acl/asterisk.
package application

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	identityapp "atsap-api/internal/identity/application"
	identitydomain "atsap-api/internal/identity/domain"
	identityports "atsap-api/internal/identity/ports"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

// Actions this service authorizes against. They name what is being done,
// not who may do it — the role bindings decide that (LLD-02 §10.1,
// deny-by-default).
const (
	actionCreateExtension = "extension.create"
	actionUpdateExtension = "extension.update"
	actionDeleteExtension = "extension.delete"
	actionReadExtension   = "extension.read"
	actionRotateSecret    = "extension.rotate_secret"

	auditResourceType = "extension"
)

// Authorizer is the slice of identity this context needs: permission
// checks and audit writes. Narrower than the full IdentityService on
// purpose — pbx-core has no business issuing tokens.
type Authorizer interface {
	AuthorizeAction(ctx context.Context, principal shareddomain.PrincipalID, action, resource string) error
}

// AuditAppender writes an audit record inside a caller-owned
// transaction, so a mutation and the record of it commit together.
// Implemented by identity's store: audit_logs belongs to identity, and
// pbx-core must not write another context's table directly.
type AuditAppender interface {
	AppendAuditTx(ctx context.Context, tx pgx.Tx, entry identitydomain.AuditEntry) error
}

// Service implements ports.ConfigService.
type Service struct {
	pool      *corepostgres.Pool
	store     ports.ExtensionStore
	projector ports.EndpointProjector
	authz     Authorizer
	audit     AuditAppender
	realm     string
	rand      io.Reader
	now       func() time.Time
}

var _ ports.ConfigService = (*Service)(nil)

// NewService wires the extension use cases.
//
// realm must be the SIP realm the engine is configured with: the
// credential digest is computed over it, so a mismatch produces
// credentials that can never authenticate (LLD-03 §7.3).
func NewService(
	pool *corepostgres.Pool,
	store ports.ExtensionStore,
	projector ports.EndpointProjector,
	authz Authorizer,
	audit AuditAppender,
	realm string,
) *Service {
	return &Service{
		pool:      pool,
		store:     store,
		projector: projector,
		authz:     authz,
		audit:     audit,
		realm:     realm,
		rand:      rand.Reader,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

// Actor is who is performing a mutation, for the audit trail.
type Actor struct {
	PrincipalID shareddomain.PrincipalID
}

// CreateExtension provisions an extension and makes it registrable.
//
// The domain row, the engine projection and the audit record are one
// transaction (design D1). If any of the three fails, none of them
// happened: the console cannot show an extension no phone could register
// to, and the engine cannot hold an endpoint the platform has no record
// of.
func (s *Service) CreateExtension(ctx context.Context, cmd ports.CreateExtensionCommand) (ports.CreatedExtension, error) {
	actor, err := actorFrom(ctx)
	if err != nil {
		return ports.CreatedExtension{}, err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionCreateExtension, cmd.TenantID.String()); err != nil {
		return ports.CreatedExtension{}, err
	}

	now := s.now()
	ext := domain.Extension{
		ID:          shareddomain.NewExtensionID(),
		TenantID:    cmd.TenantID,
		Number:      cmd.Number,
		DisplayName: cmd.DisplayName,
		DeviceType:  cmd.DeviceType,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := ext.Validate(); err != nil {
		return ports.CreatedExtension{}, fmt.Errorf("%w: %v", ports.ErrInvalidInput, err)
	}

	// The identifier is derived from the extension id and serves as both
	// the engine object id and the SIP username, so the credential must
	// be computed over it (design D2).
	ext.AuthUsername = domain.EndpointIdentifier(ext.ID)
	secret, digest, err := domain.NewExtensionCredential(ext.AuthUsername, s.realm, s.rand)
	if err != nil {
		return ports.CreatedExtension{}, fmt.Errorf("generate credential: %w", err)
	}
	ext.SecretDigest = digest

	err = s.pool.WithTenant(ctx, cmd.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.store.Insert(ctx, tx, ext); err != nil {
			return err
		}
		if err := s.projector.ProjectExtension(ctx, tx, ext); err != nil {
			return err
		}
		return s.appendAudit(ctx, tx, actor, "extension.created", ext, nil, &ext)
	})
	if err != nil {
		return ports.CreatedExtension{}, err
	}

	// The plaintext is returned here and nowhere else, ever. It is not
	// persisted, not audited, and not retrievable by any later read — the
	// same contract IssueApiKey uses.
	return ports.CreatedExtension{
		Extension: viewOf(ext, domain.StatusNotRegistered),
		Secret:    secret,
	}, nil
}

// UpdateExtension changes an extension's mutable fields. The number is
// not among them: renumbering would silently orphan every device already
// provisioned against it, so it is a delete plus a create, visible to
// the operator.
func (s *Service) UpdateExtension(ctx context.Context, cmd ports.UpdateExtensionCommand) error {
	actor, err := actorFrom(ctx)
	if err != nil {
		return err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionUpdateExtension, cmd.TenantID.String()); err != nil {
		return err
	}

	return s.pool.WithTenant(ctx, cmd.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.store.Get(ctx, tx, cmd.TenantID, cmd.ID)
		if err != nil {
			return err
		}

		after := before
		after.DisplayName = cmd.DisplayName
		after.DeviceType = cmd.DeviceType
		after.UpdatedAt = s.now()
		if err := after.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ports.ErrInvalidInput, err)
		}

		if err := s.store.Update(ctx, tx, after); err != nil {
			return err
		}
		// Re-projected because device type selects the transport: a change
		// the engine never learns about is a change that did not happen.
		if err := s.projector.ProjectExtension(ctx, tx, after); err != nil {
			return err
		}
		return s.appendAudit(ctx, tx, actor, "extension.updated", after, &before, &after)
	})
}

// DeleteExtension removes an extension and deprovisions it.
//
// The store's delete runs first and fails closed on a row it did not
// remove (design D9). That ordering is the security control: the
// projection tables have no RLS, so RemoveExtension would happily delete
// another tenant's endpoint if it were ever reached with a foreign id.
// It is only reached after RLS has authorized the domain delete.
func (s *Service) DeleteExtension(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.ExtensionID) error {
	actor, err := actorFrom(ctx)
	if err != nil {
		return err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionDeleteExtension, tenantID.String()); err != nil {
		return err
	}

	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.store.Get(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if err := s.store.Delete(ctx, tx, tenantID, id); err != nil {
			return err
		}
		if err := s.projector.RemoveExtension(ctx, tx, id); err != nil {
			return err
		}
		return s.appendAudit(ctx, tx, actor, "extension.deleted", before, &before, nil)
	})
}

// GetExtension reads one extension, with its live registration state.
func (s *Service) GetExtension(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.ExtensionID) (ports.ExtensionView, error) {
	actor, err := actorFrom(ctx)
	if err != nil {
		return ports.ExtensionView{}, err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionReadExtension, tenantID.String()); err != nil {
		return ports.ExtensionView{}, err
	}

	var ext domain.Extension
	if err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		ext, err = s.store.Get(ctx, tx, tenantID, id)
		return err
	}); err != nil {
		return ports.ExtensionView{}, err
	}

	status, err := s.projector.RegistrationStatus(ctx, []shareddomain.ExtensionID{ext.ID})
	if err != nil {
		return ports.ExtensionView{}, err
	}
	return viewOf(ext, statusOr(status, ext.ID)), nil
}

// ListExtensions returns one page of the tenant's extensions.
//
// Registration state is fetched for the whole page in one call, not per
// row: a per-row lookup would make one screen N queries against engine
// state.
func (s *Service) ListExtensions(ctx context.Context, tenantID shareddomain.TenantID, page ports.Page) ([]ports.ExtensionView, error) {
	actor, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionReadExtension, tenantID.String()); err != nil {
		return nil, err
	}

	var extensions []domain.Extension
	if err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		extensions, err = s.store.List(ctx, tx, tenantID, page)
		return err
	}); err != nil {
		return nil, err
	}
	if len(extensions) == 0 {
		return nil, nil
	}

	ids := make([]shareddomain.ExtensionID, 0, len(extensions))
	for _, e := range extensions {
		ids = append(ids, e.ID)
	}
	status, err := s.projector.RegistrationStatus(ctx, ids)
	if err != nil {
		return nil, err
	}

	views := make([]ports.ExtensionView, 0, len(extensions))
	for _, e := range extensions {
		views = append(views, viewOf(e, statusOr(status, e.ID)))
	}
	return views, nil
}

// RegenerateSecret issues a new credential for an existing extension.
// The previous secret stops authenticating the moment this commits —
// there is no grace period, because two live credentials for one
// extension is precisely the state a rotation exists to end.
func (s *Service) RegenerateSecret(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.ExtensionID) (ports.CreatedExtension, error) {
	actor, err := actorFrom(ctx)
	if err != nil {
		return ports.CreatedExtension{}, err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionRotateSecret, tenantID.String()); err != nil {
		return ports.CreatedExtension{}, err
	}

	var (
		updated domain.Extension
		secret  string
	)
	err = s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.store.Get(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}

		plaintext, digest, err := domain.NewExtensionCredential(before.AuthUsername, s.realm, s.rand)
		if err != nil {
			return fmt.Errorf("generate credential: %w", err)
		}

		updated = before
		updated.SecretDigest = digest
		updated.UpdatedAt = s.now()
		secret = plaintext

		if err := s.store.Update(ctx, tx, updated); err != nil {
			return err
		}
		if err := s.projector.ProjectExtension(ctx, tx, updated); err != nil {
			return err
		}
		return s.appendAudit(ctx, tx, actor, "extension.secret_rotated", updated, &before, &updated)
	})
	if err != nil {
		return ports.CreatedExtension{}, err
	}

	return ports.CreatedExtension{
		Extension: viewOf(updated, domain.StatusNotRegistered),
		Secret:    secret,
	}, nil
}

// --- helpers ---------------------------------------------------------

// appendAudit records one mutation inside the caller's transaction.
//
// Redaction happens HERE, at construction, not at the log sink: a filter
// applied on the way out only works if every call site remembers to use
// it, and the one that forgets is the one that leaks. auditState is the
// only thing that ever renders an Extension for audit, and it has no
// path to the digest.
func (s *Service) appendAudit(
	ctx context.Context, tx pgx.Tx, actor Actor,
	action string, subject domain.Extension,
	before, after *domain.Extension,
) error {
	entry := identitydomain.NewAuditEntry(
		subject.TenantID,
		uuid.UUID(actor.PrincipalID),
		identitydomain.ActorPrincipal,
		action, auditResourceType, subject.ID.String(),
		auditState(before), auditState(after),
		nil, s.now(),
	)
	return s.audit.AppendAuditTx(ctx, tx, entry)
}

// auditState renders the fields of an extension that may be recorded.
//
// SecretDigest is absent BY CONSTRUCTION — not filtered, absent. There is
// no branch here that could include it, so no future edit re-enables it
// by accident. identity's NewAuditEntry additionally redacts anything
// credential-shaped by key name, so this is belt and braces: the field
// never reaches the constructor, and the constructor would strip it if it
// did (LLD-03 §10.4).
func auditState(e *domain.Extension) map[string]any {
	if e == nil {
		return nil
	}
	return map[string]any{
		"extension_id":  e.ID.String(),
		"number":        e.Number,
		"display_name":  e.DisplayName,
		"auth_username": e.AuthUsername,
		"device_type":   string(e.DeviceType),
	}
}

// viewOf converts a domain extension into what a caller may see. The
// digest never crosses this boundary, and neither does the engine's own
// notion of the extension.
func viewOf(e domain.Extension, status domain.RegistrationStatus) ports.ExtensionView {
	return ports.ExtensionView{
		ID:           e.ID,
		Number:       e.Number,
		DisplayName:  e.DisplayName,
		AuthUsername: e.AuthUsername,
		DeviceType:   e.DeviceType,
		Registration: status,
	}
}

// statusOr supplies the NOT_REGISTERED default. The projector reports
// only what is registered; absence means no current contact, and that
// default belongs here rather than in the ACL, which should not be
// guessing about extensions it was not asked about.
func statusOr(m map[shareddomain.ExtensionID]domain.RegistrationStatus, id shareddomain.ExtensionID) domain.RegistrationStatus {
	if s, ok := m[id]; ok {
		return s
	}
	return domain.StatusNotRegistered
}

// actorFrom reads the authenticated principal from the request context.
// A missing one is a programming error in the wiring, not a permission
// decision, so it fails loudly rather than defaulting to anonymous.
func actorFrom(ctx context.Context) (Actor, error) {
	tc, ok := identityapp.TenantContextFrom(ctx)
	if !ok || tc == nil {
		return Actor{}, fmt.Errorf("%w: no authenticated principal in context", identityports.ErrPermissionDenied)
	}
	return Actor{PrincipalID: tc.PrincipalID}, nil
}
