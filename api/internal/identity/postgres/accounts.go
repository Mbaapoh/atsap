package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// This file implements the revision-2 ports (LLD-02 §5.4, D-61–D-66).
//
// The tables here, apart from principals and identity_providers, carry no
// tenant_id and no tenant RLS: they describe a person or the installation
// and are read before any tenant is known. archtest confines every query
// on them to this package (design D3). Tenant-visible facts about a
// person leave only through RLS-scoped memberships.

// --- users -------------------------------------------------------------

// CreateUser inserts a person and, if set, their password credential, in
// one transaction.
func (s *Store) CreateUser(ctx context.Context, u domain.User) error {
	tx, err := s.pool.Unwrap().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin user insert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `INSERT INTO users (id, email, display_name, status, created_at) VALUES ($1, $2, $3, $4, $5)`,
		u.ID.String(), u.Email, u.DisplayName, string(u.Status), u.CreatedAt)
	if isUniqueViolation(err) {
		return ports.ErrEmailTaken
	}
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	if u.Password != nil {
		if err := upsertPassword(ctx, tx, u.ID, *u.Password); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

const userColumns = `u.id, u.email, u.display_name, u.status, u.created_at,
	c.secret, c.must_change, c.changed_at
	FROM users u LEFT JOIN user_credentials c ON c.user_id = u.id AND c.kind = 'password'`

// GetUser reads a person with their password credential, if any.
func (s *Store) GetUser(ctx context.Context, id domain.UserID) (domain.User, error) {
	return s.queryUser(ctx, `SELECT `+userColumns+` WHERE u.id = $1`, id.String())
}

// GetUserByEmail reads a person by normalized email.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	return s.queryUser(ctx, `SELECT `+userColumns+` WHERE lower(u.email) = lower($1)`, email)
}

func (s *Store) queryUser(ctx context.Context, query string, arg any) (domain.User, error) {
	var (
		u          domain.User
		rawID      string
		status     string
		secret     *string
		mustChange *bool
		changedAt  *time.Time
	)
	err := s.pool.Unwrap().QueryRow(ctx, query, arg).Scan(&rawID, &u.Email, &u.DisplayName, &status, &u.CreatedAt,
		&secret, &mustChange, &changedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("query user: %w", err)
	}
	if u.ID, err = domain.ParseUserID(rawID); err != nil {
		return domain.User{}, err
	}
	u.Status = domain.UserStatus(status)
	if secret != nil {
		u.Password = &domain.PasswordCredential{Hash: *secret, MustChange: *mustChange, ChangedAt: *changedAt}
	}
	return u, nil
}

// SetPassword replaces the person's password credential.
func (s *Store) SetPassword(ctx context.Context, id domain.UserID, cred domain.PasswordCredential) error {
	tx, err := s.pool.Unwrap().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin password update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := upsertPassword(ctx, tx, id, cred); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func upsertPassword(ctx context.Context, tx pgx.Tx, id domain.UserID, cred domain.PasswordCredential) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO user_credentials (user_id, kind, secret, must_change, changed_at)
		VALUES ($1, 'password', $2, $3, $4)
		ON CONFLICT (user_id) WHERE kind = 'password'
		DO UPDATE SET secret = EXCLUDED.secret, must_change = EXCLUDED.must_change, changed_at = EXCLUDED.changed_at
	`, id.String(), cred.Hash, cred.MustChange, cred.ChangedAt)
	if err != nil {
		return fmt.Errorf("store password: %w", err)
	}
	return nil
}

// SetUserStatus disables or restores a person.
func (s *Store) SetUserStatus(ctx context.Context, id domain.UserID, status domain.UserStatus) error {
	tag, err := s.pool.Unwrap().Exec(ctx, `UPDATE users SET status = $2 WHERE id = $1`, id.String(), string(status))
	if err != nil {
		return fmt.Errorf("update user status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// ListMemberships reads a person's memberships across tenants under the
// self-only policy (app.user_id): the transaction can see that person's
// principals and their bindings, and nothing else of any tenant.
func (s *Store) ListMemberships(ctx context.Context, id domain.UserID) ([]domain.Membership, error) {
	var out []domain.Membership
	err := s.withUser(ctx, id, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.tenant_id, t.name, p.status,
			       COALESCE(array_agg(b.role ORDER BY b.role) FILTER (WHERE b.role IS NOT NULL), '{}')
			  FROM principals p
			  JOIN tenants t ON t.id = p.tenant_id
			  LEFT JOIN role_bindings b ON b.principal_id = p.id
			 WHERE p.user_id = $1 AND t.status = 'ACTIVE'
			 GROUP BY p.id, p.tenant_id, t.name, p.status
			 ORDER BY t.name, p.tenant_id
		`, id.String())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				m                     domain.Membership
				rawP, rawT, rawStatus string
			)
			if err := rows.Scan(&rawP, &rawT, &m.TenantName, &rawStatus, &m.Roles); err != nil {
				return err
			}
			if m.PrincipalID, err = shareddomain.ParsePrincipalID(rawP); err != nil {
				return err
			}
			if m.TenantID, err = shareddomain.ParseTenantID(rawT); err != nil {
				return err
			}
			m.Status = domain.PrincipalStatus(rawStatus)
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	return out, nil
}

// withUser runs fn in a transaction that sees only the given person's
// own memberships (the principals_self and role_bindings_self policies).
func (s *Store) withUser(ctx context.Context, id domain.UserID, fn func(context.Context, pgx.Tx) error) error {
	tx, err := s.pool.Unwrap().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.user_id', $1, true)`, id.String()); err != nil {
		return err
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// --- liveness ------------------------------------------------------------

// LiveState reads, in one tenant-scoped transaction, everything
// ValidateToken re-checks per request: the membership, its tenant, the
// person's status and must-change flag, the membership's bindings and,
// when asked, the session (design D4).
func (s *Store) LiveState(ctx context.Context, tenantID shareddomain.TenantID, principalID shareddomain.PrincipalID, session domain.SessionID) (ports.LiveState, error) {
	var st ports.LiveState
	err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		p, err := scanPrincipal(tx.QueryRow(ctx, `SELECT `+principalColumns+` WHERE p.id = $1`, principalID.String()))
		if err != nil {
			return err
		}
		st.Principal = p

		var tStatus, tRawID string
		if err := tx.QueryRow(ctx, `SELECT id, name, status, residency_zone, created_at FROM tenants WHERE id = $1`,
			tenantID.String()).Scan(&tRawID, &st.Tenant.Name, &tStatus, &st.Tenant.ResidencyZone, &st.Tenant.CreatedAt); err != nil {
			return err
		}
		st.Tenant.ID = tenantID
		st.Tenant.Status = domain.TenantStatus(tStatus)

		var uStatus string
		var mustChange *bool
		if err := tx.QueryRow(ctx, `SELECT u.status, c.must_change FROM users u
			LEFT JOIN user_credentials c ON c.user_id = u.id AND c.kind = 'password' WHERE u.id = $1`,
			p.UserID.String()).Scan(&uStatus, &mustChange); err != nil {
			return err
		}
		st.UserStatus = domain.UserStatus(uStatus)
		st.MustChange = mustChange != nil && *mustChange

		rows, err := tx.Query(ctx, `SELECT role, scope, created_at FROM role_bindings WHERE principal_id = $1 ORDER BY role, scope`,
			principalID.String())
		if err != nil {
			return err
		}
		for rows.Next() {
			b := domain.RoleBinding{TenantID: tenantID, PrincipalID: principalID}
			if err := rows.Scan(&b.Role, &b.Scope, &b.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			st.Bindings = append(st.Bindings, b)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		if !session.IsZero() {
			sess, err := scanSession(tx.QueryRow(ctx, `SELECT `+sessionColumns+` WHERE id = $1`, session.String()))
			if err != nil {
				return err
			}
			st.Session = &sess
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.LiveState{}, ports.ErrNotFound
	}
	if err != nil {
		return ports.LiveState{}, fmt.Errorf("read live state: %w", err)
	}
	return st, nil
}

// --- sessions -------------------------------------------------------------

const sessionColumns = `id, user_id, principal_id, refresh_digest, COALESCE(previous_digest, ''),
	idle_expires_at, absolute_expires_at, revoked_at, COALESCE(revoke_reason, ''), created_at FROM sessions`

func scanSession(row pgx.Row) (domain.Session, error) {
	var (
		s              domain.Session
		rawID, rawUser string
		rawPrincipal   *string
	)
	if err := row.Scan(&rawID, &rawUser, &rawPrincipal, &s.RefreshDigest, &s.PreviousDigest,
		&s.IdleExpiresAt, &s.AbsoluteExpiresAt, &s.RevokedAt, &s.RevokeReason, &s.CreatedAt); err != nil {
		return domain.Session{}, err
	}
	var err error
	if s.ID, err = domain.ParseSessionID(rawID); err != nil {
		return domain.Session{}, err
	}
	if s.UserID, err = domain.ParseUserID(rawUser); err != nil {
		return domain.Session{}, err
	}
	if rawPrincipal != nil {
		pid, err := shareddomain.ParsePrincipalID(*rawPrincipal)
		if err != nil {
			return domain.Session{}, err
		}
		s.PrincipalID = &pid
	}
	return s, nil
}

func principalArg(p *shareddomain.PrincipalID) *string {
	if p == nil {
		return nil
	}
	v := p.String()
	return &v
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// CreateSession inserts a session.
func (s *Store) CreateSession(ctx context.Context, sess domain.Session) error {
	_, err := s.pool.Unwrap().Exec(ctx, `
		INSERT INTO sessions (id, user_id, principal_id, refresh_digest, previous_digest,
			idle_expires_at, absolute_expires_at, revoked_at, revoke_reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, sess.ID.String(), sess.UserID.String(), principalArg(sess.PrincipalID), sess.RefreshDigest,
		nullable(sess.PreviousDigest), sess.IdleExpiresAt, sess.AbsoluteExpiresAt, sess.RevokedAt,
		nullable(sess.RevokeReason), sess.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// GetSession reads a session by id.
func (s *Store) GetSession(ctx context.Context, id domain.SessionID) (domain.Session, error) {
	sess, err := scanSession(s.pool.Unwrap().QueryRow(ctx, `SELECT `+sessionColumns+` WHERE id = $1`, id.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("query session: %w", err)
	}
	return sess, nil
}

// UpdateSessionByDigest locks the session holding digest (current or
// previous generation) with SELECT … FOR UPDATE, applies fn, and stores
// the result, even when fn also returns an error — a replayed secret is
// refused and its revocation must still persist.
func (s *Store) UpdateSessionByDigest(ctx context.Context, digest string, fn func(domain.Session) (domain.Session, error)) (domain.Session, error) {
	tx, err := s.pool.Unwrap().Begin(ctx)
	if err != nil {
		return domain.Session{}, fmt.Errorf("begin session update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	cur, err := scanSession(tx.QueryRow(ctx, `SELECT `+sessionColumns+`
		WHERE refresh_digest = $1 OR previous_digest = $1 FOR UPDATE`, digest))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("lock session: %w", err)
	}
	next, fnErr := fn(cur)
	if _, err := tx.Exec(ctx, `
		UPDATE sessions SET principal_id = $2, refresh_digest = $3, previous_digest = $4,
			idle_expires_at = $5, revoked_at = $6, revoke_reason = $7
		WHERE id = $1
	`, next.ID.String(), principalArg(next.PrincipalID), next.RefreshDigest, nullable(next.PreviousDigest),
		next.IdleExpiresAt, next.RevokedAt, nullable(next.RevokeReason)); err != nil {
		return domain.Session{}, fmt.Errorf("store session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Session{}, fmt.Errorf("commit session: %w", err)
	}
	return next, fnErr
}

// RevokeUserSessions ends every live session of a person except keep.
func (s *Store) RevokeUserSessions(ctx context.Context, user domain.UserID, keep domain.SessionID, at time.Time, reason string) (int, error) {
	var keepArg *string
	if !keep.IsZero() {
		v := keep.String()
		keepArg = &v
	}
	tag, err := s.pool.Unwrap().Exec(ctx, `
		UPDATE sessions SET revoked_at = $2, revoke_reason = $3
		WHERE user_id = $1 AND revoked_at IS NULL AND ($4::uuid IS NULL OR id <> $4::uuid)
	`, user.String(), at, reason, keepArg)
	if err != nil {
		return 0, fmt.Errorf("revoke sessions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// RevokeSession ends one session; ending an ended session is a no-op.
func (s *Store) RevokeSession(ctx context.Context, id domain.SessionID, at time.Time, reason string) error {
	_, err := s.pool.Unwrap().Exec(ctx, `
		UPDATE sessions SET revoked_at = $2, revoke_reason = $3 WHERE id = $1 AND revoked_at IS NULL
	`, id.String(), at, reason)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// --- reset and invitation tickets -------------------------------------------

// CreateTicket stores a ticket's digest.
func (s *Store) CreateTicket(ctx context.Context, t ports.ResetTicket) error {
	_, err := s.pool.Unwrap().Exec(ctx, `
		INSERT INTO password_reset_tickets (user_id, digest, purpose, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, t.UserID.String(), t.Digest, string(t.Purpose), t.ExpiresAt, t.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert ticket: %w", err)
	}
	return nil
}

// CountTickets counts a person's tickets of a purpose created since.
func (s *Store) CountTickets(ctx context.Context, user domain.UserID, purpose domain.TicketPurpose, since time.Time) (int, error) {
	var n int
	err := s.pool.Unwrap().QueryRow(ctx, `
		SELECT count(*) FROM password_reset_tickets WHERE user_id = $1 AND purpose = $2 AND created_at >= $3
	`, user.String(), string(purpose), since).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count tickets: %w", err)
	}
	return n, nil
}

// RedeemTicket consumes a ticket, sets the password and ends every
// session, atomically: a reset either fully happens or does not.
func (s *Store) RedeemTicket(ctx context.Context, digest string, cred domain.PasswordCredential, at time.Time) (domain.UserID, domain.TicketPurpose, error) {
	tx, err := s.pool.Unwrap().Begin(ctx)
	if err != nil {
		return domain.UserID{}, "", fmt.Errorf("begin redeem: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var rawUser, purpose string
	err = tx.QueryRow(ctx, `
		UPDATE password_reset_tickets SET used_at = $2
		WHERE digest = $1 AND used_at IS NULL AND expires_at > $2
		RETURNING user_id, purpose
	`, digest, at).Scan(&rawUser, &purpose)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.UserID{}, "", ports.ErrNotFound
	}
	if err != nil {
		return domain.UserID{}, "", fmt.Errorf("redeem ticket: %w", err)
	}
	user, err := domain.ParseUserID(rawUser)
	if err != nil {
		return domain.UserID{}, "", err
	}
	if err := upsertPassword(ctx, tx, user, cred); err != nil {
		return domain.UserID{}, "", err
	}
	// Every outstanding ticket of the person dies with this one.
	if _, err := tx.Exec(ctx, `UPDATE password_reset_tickets SET used_at = $2 WHERE user_id = $1 AND used_at IS NULL`,
		user.String(), at); err != nil {
		return domain.UserID{}, "", fmt.Errorf("retire tickets: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = $2, revoke_reason = $3
		WHERE user_id = $1 AND revoked_at IS NULL`, user.String(), at, domain.RevokePasswordReset); err != nil {
		return domain.UserID{}, "", fmt.Errorf("revoke sessions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.UserID{}, "", fmt.Errorf("commit redeem: %w", err)
	}
	return user, domain.TicketPurpose(purpose), nil
}

// --- identity providers (SSO-ready, D-63) ----------------------------------

// CreateProvider inserts a provider under its tenant's RLS.
func (s *Store) CreateProvider(ctx context.Context, p domain.IdentityProvider) error {
	return s.pool.WithTenant(ctx, p.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO identity_providers (id, tenant_id, kind, display_name, issuer, mode)
			VALUES ($1, $2, $3, $4, $5, $6)`, p.ID.String(), p.TenantID.String(), string(p.Kind), p.DisplayName, p.Issuer, string(p.Mode))
		if err != nil {
			return fmt.Errorf("insert provider: %w", err)
		}
		return nil
	})
}

// ClaimDomain records a claim; the domain is the primary key, so a second
// provider's claim is refused by the database itself.
func (s *Store) ClaimDomain(ctx context.Context, c domain.DomainClaim) error {
	_, err := s.pool.Unwrap().Exec(ctx, `INSERT INTO identity_provider_domains (domain, provider_id, verified_at)
		VALUES (lower($1), $2, $3)`, c.Domain, c.ProviderID.String(), c.VerifiedAt)
	if isUniqueViolation(err) {
		return ports.ErrDomainClaimed
	}
	if err != nil {
		return fmt.Errorf("claim domain: %w", err)
	}
	return nil
}

// VerifyDomain marks a claim verified.
func (s *Store) VerifyDomain(ctx context.Context, domainName string, at time.Time) error {
	tag, err := s.pool.Unwrap().Exec(ctx, `UPDATE identity_provider_domains SET verified_at = $2 WHERE domain = lower($1)`, domainName, at)
	if err != nil {
		return fmt.Errorf("verify domain: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// ProviderForDomain reads a verified claim and its provider. The provider
// row is tenant-scoped; this read happens before any tenant is known, so
// it runs under the discovery policy, which exposes only providers that
// hold a verified claim.
func (s *Store) ProviderForDomain(ctx context.Context, domainName string) (domain.DomainClaim, domain.IdentityProvider, error) {
	var (
		claim                  domain.DomainClaim
		p                      domain.IdentityProvider
		rawP, rawT, kind, mode string
	)
	tx, err := s.pool.Unwrap().Begin(ctx)
	if err != nil {
		return claim, p, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.discovery', 'on', true)`); err != nil {
		return claim, p, err
	}
	err = tx.QueryRow(ctx, `
		SELECT d.domain, d.verified_at, p.id, p.tenant_id, p.kind, p.display_name, p.issuer, p.mode
		  FROM identity_provider_domains d JOIN identity_providers p ON p.id = d.provider_id
		 WHERE d.domain = lower($1) AND d.verified_at IS NOT NULL
	`, domainName).Scan(&claim.Domain, &claim.VerifiedAt, &rawP, &rawT, &kind, &p.DisplayName, &p.Issuer, &mode)
	if errors.Is(err, pgx.ErrNoRows) {
		return claim, p, ports.ErrNotFound
	}
	if err != nil {
		return claim, p, fmt.Errorf("provider for domain: %w", err)
	}
	pid, err := domain.ParseIdentityProviderID(rawP)
	if err != nil {
		return claim, p, err
	}
	p.ID, claim.ProviderID = pid, pid
	if p.TenantID, err = shareddomain.ParseTenantID(rawT); err != nil {
		return claim, p, err
	}
	p.Kind, p.Mode = domain.ProviderKind(kind), domain.ProviderMode(mode)
	return claim, p, nil
}

// LinkExternalIdentity links (provider, subject) to a person.
func (s *Store) LinkExternalIdentity(ctx context.Context, e domain.ExternalIdentity) error {
	_, err := s.pool.Unwrap().Exec(ctx, `INSERT INTO external_identities (provider_id, subject, user_id, linked_at)
		VALUES ($1, $2, $3, $4)`, e.ProviderID.String(), e.Subject, e.UserID.String(), e.LinkedAt)
	if isUniqueViolation(err) {
		return ports.ErrAlreadyLinked
	}
	if err != nil {
		return fmt.Errorf("link external identity: %w", err)
	}
	return nil
}

// UserForExternalIdentity resolves (provider, subject) to a person.
func (s *Store) UserForExternalIdentity(ctx context.Context, provider domain.IdentityProviderID, subject string) (domain.UserID, error) {
	var raw string
	err := s.pool.Unwrap().QueryRow(ctx, `SELECT user_id FROM external_identities WHERE provider_id = $1 AND subject = $2`,
		provider.String(), subject).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.UserID{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.UserID{}, fmt.Errorf("resolve external identity: %w", err)
	}
	return domain.ParseUserID(raw)
}

// --- auth events ---------------------------------------------------------------

// AppendAuthEvent writes one history entry. There is no update or delete
// counterpart, here or on the port.
func (s *Store) AppendAuthEvent(ctx context.Context, e domain.AuthEvent) error {
	var user *string
	if e.UserID != nil {
		v := e.UserID.String()
		user = &v
	}
	var addr *string
	if ip := net.ParseIP(e.SourceAddress); ip != nil {
		v := ip.String()
		addr = &v
	}
	_, err := s.pool.Unwrap().Exec(ctx, `INSERT INTO auth_events (user_id, kind, source_address, created_at)
		VALUES ($1, $2, $3, $4)`, user, string(e.Kind), addr, e.At)
	if err != nil {
		return fmt.Errorf("append auth event: %w", err)
	}
	return nil
}

// ListAuthEvents reads a person's most recent history, newest first.
func (s *Store) ListAuthEvents(ctx context.Context, user domain.UserID, limit int) ([]domain.AuthEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Unwrap().Query(ctx, `SELECT kind, COALESCE(host(source_address), ''), created_at
		FROM auth_events WHERE user_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, user.String(), limit)
	if err != nil {
		return nil, fmt.Errorf("list auth events: %w", err)
	}
	defer rows.Close()
	var out []domain.AuthEvent
	for rows.Next() {
		u := user
		e := domain.AuthEvent{UserID: &u}
		var kind string
		if err := rows.Scan(&kind, &e.SourceAddress, &e.At); err != nil {
			return nil, err
		}
		e.Kind = domain.AuthEventKind(kind)
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- login throttle -------------------------------------------------------------

// throttleRetention is how long an idle throttle row is kept; older rows
// are pruned on write, so no background job is needed (design D7).
const throttleRetention = 24 * time.Hour

// BlockedFor returns the longest remaining block across keys.
func (s *Store) BlockedFor(ctx context.Context, keys []string, now time.Time) (time.Duration, error) {
	var until *time.Time
	err := s.pool.Unwrap().QueryRow(ctx, `SELECT max(blocked_until) FROM login_throttle
		WHERE key_digest = ANY($1) AND blocked_until > $2`, keys, now).Scan(&until)
	if err != nil {
		return 0, fmt.Errorf("read throttle: %w", err)
	}
	if until == nil {
		return 0, nil
	}
	return until.Sub(now), nil
}

// RecordFailure counts a failure on every key and sets each key's block
// from the policy, then prunes idle rows.
func (s *Store) RecordFailure(ctx context.Context, keys []string, now time.Time, policy domain.ThrottlePolicy) error {
	tx, err := s.pool.Unwrap().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin throttle: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	runStart := now.Add(-policy.Window)
	if policy.Window <= 0 {
		runStart = time.Time{}
	}
	for _, k := range keys {
		var failures int
		// A failure long after the previous one starts a fresh run.
		if err := tx.QueryRow(ctx, `
			INSERT INTO login_throttle (key_digest, failures, updated_at) VALUES ($1, 1, $2)
			ON CONFLICT (key_digest) DO UPDATE SET
				failures = CASE WHEN login_throttle.updated_at < $3 THEN 1 ELSE login_throttle.failures + 1 END,
				updated_at = $2
			RETURNING failures
		`, k, now, runStart).Scan(&failures); err != nil {
			return fmt.Errorf("count failure: %w", err)
		}
		if d := policy.Delay(failures); d > 0 {
			if _, err := tx.Exec(ctx, `UPDATE login_throttle SET blocked_until = $2 WHERE key_digest = $1`, k, now.Add(d)); err != nil {
				return fmt.Errorf("block key: %w", err)
			}
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM login_throttle WHERE updated_at < $1`, now.Add(-throttleRetention)); err != nil {
		return fmt.Errorf("prune throttle: %w", err)
	}
	return tx.Commit(ctx)
}

// Clear resets a key after a success.
func (s *Store) Clear(ctx context.Context, key string) error {
	if _, err := s.pool.Unwrap().Exec(ctx, `DELETE FROM login_throttle WHERE key_digest = $1`, key); err != nil {
		return fmt.Errorf("clear throttle: %w", err)
	}
	return nil
}
