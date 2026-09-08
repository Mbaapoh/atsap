// Package asterisk is pbx-core's Anti-Corruption Layer onto the media
// engine's own configuration state.
//
// It is the ONLY package in the codebase permitted to name a PJSIP
// Realtime table (ps_endpoints, ps_auths, ps_aors, ps_contacts) or the
// identifier those tables key on (DECISIONS D-41, D-47). Nothing in
// pbx/domain, pbx/application or pbx/rpc may reference them, and
// golangci-lint's depguard enforces that.
//
// The direction of translation is one-way and narrow: a domain
// Extension goes in, engine rows come out. Nothing engine-shaped comes
// back — RegistrationStatus returns a domain status, never a contact
// row — so a second engine could implement ports.EndpointProjector
// without reshaping anything above this package.
package asterisk

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

// Engine-side constants. These are Asterisk's vocabulary and they stop
// here.
const (
	// stasisContext is the dialplan context that hands a call straight to
	// the Go application (core/conf/extensions.conf). Every projected
	// endpoint enters there: pbx-core does not generate dialplan, so an
	// endpoint's only job is to reach Stasis (D-47).
	stasisContext = "stasis-in"

	// Codec policy. Narrow on purpose — a codec the platform cannot
	// transcode is worse than one it refuses.
	disallowedCodecs = "all"
	allowedCodecs    = "ulaw,alaw"

	// One contact per extension: a fresh REGISTER replaces a stale one
	// rather than being refused, which matters because a device's address
	// changes on every network move.
	maxContacts    = 1
	removeExisting = "yes"
)

// Config carries the engine-side values that are deployment choices
// rather than domain facts.
type Config struct {
	// Realm the SIP digest is computed over. It MUST match the realm used
	// when the credential digest was generated — the HA1 is computed over
	// it, so a mismatch produces a credential that can never authenticate
	// (LLD-03 §7.3).
	Realm string

	// SIPTransport and WebRTCTransport name transports defined in the
	// engine's own configuration.
	//
	// WebRTCTransport requires a WSS transport that core/conf does not
	// define yet — it lands with the engine configuration task in this
	// same change. Until it does, projecting a WEBRTC extension produces
	// an endpoint pointing at a transport that does not exist, so the
	// wiring must not offer WebRTC before that config ships.
	SIPTransport    string
	WebRTCTransport string
}

// Projector implements ports.EndpointProjector against PJSIP Realtime.
//
// It holds a pool as well as taking transactions, because its two kinds
// of operation genuinely differ. Writes MUST join the caller's
// transaction so the domain row and the projection commit together
// (design D1). A status read is not part of any domain write — it
// answers "what is the engine seeing right now" — so it runs on its own
// connection and takes no tx.
//
// The projection tables carry no RLS, so the pool needs no tenant
// context. That is also why every write path is gated upstream by a
// domain operation RLS authorized (design D9): nothing here can tell
// one tenant's endpoint from another's.
type Projector struct {
	cfg  Config
	pool *corepostgres.Pool
}

var (
	_ ports.EndpointProjector    = (*Projector)(nil)
	_ ports.ProjectionReconciler = (*Projector)(nil)
)

// NewProjector returns a Projector writing endpoints for cfg's realm and
// transports, reading registration state through pool.
func NewProjector(cfg Config, pool *corepostgres.Pool) *Projector {
	return &Projector{cfg: cfg, pool: pool}
}

// transportFor maps a domain device type onto an engine transport name.
func (p *Projector) transportFor(d domain.DeviceType) string {
	if d == domain.DeviceWebRTC {
		return p.cfg.WebRTCTransport
	}
	return p.cfg.SIPTransport
}

// ProjectExtension makes ext registrable by a device, inside the
// caller's transaction.
//
// Every statement is an upsert, so this is the same code path for a
// create and for an update: an extension whose display name changed and
// one that never existed both end with the engine holding exactly the
// rows the domain says it should. That matters because the alternative —
// branching on whether the row exists — is a race against any concurrent
// write.
//
// Written aor → auth → endpoint, the order of dependency. There are no
// foreign keys between them (the engine's schema has none), so the order
// is for a human reading the log, not for the database.
func (p *Projector) ProjectExtension(ctx context.Context, tx pgx.Tx, ext domain.Extension) error {
	id := domain.EndpointIdentifier(ext.ID)

	if _, err := tx.Exec(ctx, `
		INSERT INTO ps_aors (id, max_contacts, remove_existing)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET
			max_contacts = EXCLUDED.max_contacts,
			remove_existing = EXCLUDED.remove_existing
	`, id, maxContacts, removeExisting); err != nil {
		return fmt.Errorf("project aor: %w", err)
	}

	// auth_type=md5 with md5_cred and NO password: SIP digest requires the
	// server to hold the plaintext or the HA1, and holding the HA1 means
	// no recoverable secret is ever at rest (LLD-03 §7.3). The password
	// column is left NULL deliberately and asserted empty by test.
	//
	// The username is the endpoint identifier, not a separate value:
	// Asterisk matches an inbound REGISTER by From-user against the
	// endpoint id, so a different username would never be presented.
	if _, err := tx.Exec(ctx, `
		INSERT INTO ps_auths (id, auth_type, username, md5_cred, realm)
		VALUES ($1, 'md5', $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET
			auth_type = EXCLUDED.auth_type,
			username  = EXCLUDED.username,
			md5_cred  = EXCLUDED.md5_cred,
			realm     = EXCLUDED.realm
	`, id, id, ext.SecretDigest, p.cfg.Realm); err != nil {
		return fmt.Errorf("project auth: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO ps_endpoints
			(id, transport, aors, auth, context, disallow, allow, direct_media)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'no')
		ON CONFLICT (id) DO UPDATE SET
			transport    = EXCLUDED.transport,
			aors         = EXCLUDED.aors,
			auth         = EXCLUDED.auth,
			context      = EXCLUDED.context,
			disallow     = EXCLUDED.disallow,
			allow        = EXCLUDED.allow,
			direct_media = EXCLUDED.direct_media
	`, id, p.transportFor(ext.DeviceType), id, id, stasisContext,
		disallowedCodecs, allowedCodecs); err != nil {
		return fmt.Errorf("project endpoint: %w", err)
	}

	return nil
}

// RemoveExtension deprovisions the extension inside the caller's
// transaction.
//
// Contacts are deliberately NOT deleted here. They are the engine's own
// rows, written on registration and pruned by it on expiry; removing the
// endpoint is what stops a device re-registering, and reaching into the
// engine's bookkeeping would be this ACL exceeding its remit.
//
// The caller MUST have already performed a domain delete that RLS
// authorized. These tables have no row-level security, so this method
// would happily remove another tenant's endpoint if handed its id — the
// gate is ExtensionStore.Delete failing closed (design D9), not anything
// here.
func (p *Projector) RemoveExtension(ctx context.Context, tx pgx.Tx, extID shareddomain.ExtensionID) error {
	id := domain.EndpointIdentifier(extID)

	for _, stmt := range []struct{ table, sql string }{
		{"ps_endpoints", `DELETE FROM ps_endpoints WHERE id = $1`},
		{"ps_auths", `DELETE FROM ps_auths WHERE id = $1`},
		{"ps_aors", `DELETE FROM ps_aors WHERE id = $1`},
	} {
		if _, err := tx.Exec(ctx, stmt.sql, id); err != nil {
			return fmt.Errorf("deproject %s: %w", stmt.table, err)
		}
	}
	return nil
}

// RegistrationStatus reports which of ids currently have a device
// registered, as one query rather than one per extension.
//
// Registration is read from the engine's own contact table: a contact
// exists, and its expiry is in the future. Asterisk prunes expired
// contacts lazily, so an expiry check is required — trusting the row's
// presence alone would report a phone that has been unplugged for a week
// as registered.
//
// Ids with no current contact are simply absent from the returned map;
// the caller supplies the NOT_REGISTERED default rather than this method
// guessing at extensions it was not asked about.
func (p *Projector) RegistrationStatus(ctx context.Context, ids []shareddomain.ExtensionID) (map[shareddomain.ExtensionID]domain.RegistrationStatus, error) {
	status := make(map[shareddomain.ExtensionID]domain.RegistrationStatus, len(ids))
	if len(ids) == 0 {
		return status, nil
	}

	// Endpoint identifiers are derived, so the mapping back to an
	// ExtensionID is a local lookup rather than a second query.
	byIdentifier := make(map[string]shareddomain.ExtensionID, len(ids))
	identifiers := make([]string, 0, len(ids))
	for _, id := range ids {
		identifier := domain.EndpointIdentifier(id)
		byIdentifier[identifier] = id
		identifiers = append(identifiers, identifier)
	}

	rows, err := p.pool.Unwrap().Query(ctx, `
		SELECT DISTINCT endpoint
		FROM ps_contacts
		WHERE endpoint = ANY($1) AND expiration_time > $2
	`, identifiers, time.Now().Unix())
	if err != nil {
		return nil, fmt.Errorf("read registration status: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var endpoint string
		if err := rows.Scan(&endpoint); err != nil {
			return nil, fmt.Errorf("scan registration status: %w", err)
		}
		if extID, ok := byIdentifier[endpoint]; ok {
			status[extID] = domain.StatusRegistered
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read registration status: %w", err)
	}
	return status, nil
}

// --- reconciliation --------------------------------------------------

// ListProjected reports every endpoint the engine currently holds.
func (p *Projector) ListProjected(ctx context.Context) (ports.ProjectionInventory, error) {
	rows, err := p.pool.Unwrap().Query(ctx, `SELECT id FROM ps_endpoints ORDER BY id`)
	if err != nil {
		return ports.ProjectionInventory{}, fmt.Errorf("list projected endpoints: %w", err)
	}
	defer rows.Close()

	var inv ports.ProjectionInventory
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return ports.ProjectionInventory{}, fmt.Errorf("scan projected endpoint: %w", err)
		}
		if extID, ok := domain.ExtensionIDFromIdentifier(id); ok {
			inv.Extensions = append(inv.Extensions, extID)
			continue
		}
		inv.Unattributable = append(inv.Unattributable, id)
	}
	if err := rows.Err(); err != nil {
		return ports.ProjectionInventory{}, fmt.Errorf("list projected endpoints: %w", err)
	}
	return inv, nil
}

// DiffExtension reports which projected fields disagree with what ext
// says they should be, naming each one.
//
// Presence alone is not agreement: a row edited by hand — a changed
// context, a blanked credential, a different transport — leaves the
// extension listed in the console and broken on the phone. The comparison
// re-derives what the projection ought to be and compares field by field,
// so the report says what differs rather than merely that something does.
//
// An absent row is reported as "missing" rather than as every field
// differing, because one line an operator can act on beats eight they
// have to read.
func (p *Projector) DiffExtension(ctx context.Context, ext domain.Extension) ([]string, error) {
	id := domain.EndpointIdentifier(ext.ID)

	var (
		transport, aors, auth, context_, allow string
		authType, username, digest, realm      string
		maxContactsGot                         int
	)

	err := p.pool.Unwrap().QueryRow(ctx, `
		SELECT e.transport, e.aors, e.auth, e.context, e.allow,
		       a.auth_type, a.username, a.md5_cred, a.realm,
		       r.max_contacts
		FROM ps_endpoints e
		JOIN ps_auths a ON a.id = e.id
		JOIN ps_aors  r ON r.id = e.id
		WHERE e.id = $1
	`, id).Scan(&transport, &aors, &auth, &context_, &allow,
		&authType, &username, &digest, &realm, &maxContactsGot)
	if errors.Is(err, pgx.ErrNoRows) {
		return []string{"missing"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read projected extension: %w", err)
	}

	var drift []string
	expect := func(field, want, got string) {
		if want != got {
			drift = append(drift, field)
		}
	}
	expect("transport", p.transportFor(ext.DeviceType), transport)
	expect("aors", id, aors)
	expect("auth", id, auth)
	expect("context", stasisContext, context_)
	expect("allow", allowedCodecs, allow)
	expect("auth_type", "md5", authType)
	expect("username", id, username)
	expect("md5_cred", ext.SecretDigest, digest)
	expect("realm", p.cfg.Realm, realm)
	if maxContactsGot != maxContacts {
		drift = append(drift, "max_contacts")
	}
	return drift, nil
}
