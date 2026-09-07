// Package postgres implements the identity context's outbound storage
// ports against the atsapbx database (internal/postgres.Pool),
// tenant-scoped via WithTenant so PostgreSQL Row-Level Security enforces
// isolation on every read and write (docs/hld/01-architecture.md §4).
//
// Every statement here uses parameter placeholders. No SQL in this
// package is built by string concatenation with caller input
// (LLD-02 §10.4, OWASP A03).
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

// Store implements every identity storage port against one pool.
type Store struct {
	pool *corepostgres.Pool
}

var (
	_ ports.TenantStore      = (*Store)(nil)
	_ ports.PrincipalStore   = (*Store)(nil)
	_ ports.RoleBindingStore = (*Store)(nil)
	_ ports.ApiKeyStore      = (*Store)(nil)
	_ ports.AuditStore       = (*Store)(nil)
)

// NewStore returns a Store backed by pool.
func NewStore(pool *corepostgres.Pool) *Store {
	return &Store{pool: pool}
}

// --- tenants -------------------------------------------------------
//
// Tenants are the root of tenancy: the table has no tenant_id and no RLS
// policy, so these three methods use the pool directly rather than
// WithTenant. Every other method in this file is tenant-scoped.

// HasTenants reports whether any tenant exists. Used by the bootstrap
// command to refuse a second run: provisioning must never begin twice,
// or a second administrator could be minted outside the intended path.
// Tenants are not RLS-scoped, so this is a direct pool query.
func (s *Store) HasTenants(ctx context.Context) (bool, error) {
	var exists bool
	if err := s.pool.Unwrap().QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tenants)`).Scan(&exists); err != nil {
		return false, fmt.Errorf("check for existing tenants: %w", err)
	}
	return exists, nil
}

// CreateTenant inserts a tenant.
func (s *Store) CreateTenant(ctx context.Context, tenant domain.Tenant) error {
	_, err := s.pool.Unwrap().Exec(ctx, `
		INSERT INTO tenants (id, name, status, residency_zone)
		VALUES ($1, $2, $3, $4)
	`, tenant.ID.String(), tenant.Name, string(tenant.Status), tenant.ResidencyZone)
	if err != nil {
		return fmt.Errorf("insert tenant: %w", err)
	}
	return nil
}

// GetTenant reads one tenant.
func (s *Store) GetTenant(ctx context.Context, id shareddomain.TenantID) (domain.Tenant, error) {
	var (
		t      domain.Tenant
		rawID  string
		status string
	)
	err := s.pool.Unwrap().QueryRow(ctx, `
		SELECT id, name, status, residency_zone, created_at FROM tenants WHERE id = $1
	`, id.String()).Scan(&rawID, &t.Name, &status, &t.ResidencyZone, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Tenant{}, fmt.Errorf("tenant %s: %w", id, ports.ErrNotFound)
	}
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("query tenant: %w", err)
	}
	t.ID = id
	t.Status = domain.TenantStatus(status)
	return t, nil
}

// SetTenantStatus suspends or reactivates a tenant. It deliberately
// updates only status: residency_zone is immutable once set
// (LLD-02 §10.6), and no method here offers a way to change it.
func (s *Store) SetTenantStatus(ctx context.Context, id shareddomain.TenantID, status domain.TenantStatus) error {
	tag, err := s.pool.Unwrap().Exec(ctx, `
		UPDATE tenants SET status = $2, updated_at = NOW() WHERE id = $1
	`, id.String(), string(status))
	if err != nil {
		return fmt.Errorf("update tenant status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tenant %s: %w", id, ports.ErrNotFound)
	}
	return nil
}

// --- principals ----------------------------------------------------

// CreatePrincipal inserts a principal. A duplicate username within the
// tenant is reported as ports.ErrUsernameTaken rather than a raw
// constraint error, because the caller's response to it is a user-facing
// message, not a retry.
func (s *Store) CreatePrincipal(ctx context.Context, p domain.Principal) error {
	return s.pool.WithTenant(ctx, p.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO principals (id, tenant_id, username, email, password_hash, role, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, p.ID.String(), p.TenantID.String(), p.Username, p.Email, p.PasswordHash, p.Role, string(p.Status))
		if isUniqueViolation(err) {
			return fmt.Errorf("username %q: %w", p.Username, ports.ErrUsernameTaken)
		}
		if err != nil {
			return fmt.Errorf("insert principal: %w", err)
		}
		return nil
	})
}

// GetPrincipal reads one principal within a tenant.
func (s *Store) GetPrincipal(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.PrincipalID) (domain.Principal, error) {
	return s.queryPrincipal(ctx, tenantID,
		`SELECT id, tenant_id, username, email, password_hash, role, status, created_at
		 FROM principals WHERE id = $1`, id.String())
}

// GetPrincipalByUsername resolves a login within a tenant.
func (s *Store) GetPrincipalByUsername(ctx context.Context, tenantID shareddomain.TenantID, username string) (domain.Principal, error) {
	return s.queryPrincipal(ctx, tenantID,
		`SELECT id, tenant_id, username, email, password_hash, role, status, created_at
		 FROM principals WHERE username = $1`, username)
}

func (s *Store) queryPrincipal(ctx context.Context, tenantID shareddomain.TenantID, query string, arg any) (domain.Principal, error) {
	var (
		p                domain.Principal
		rawID, rawTenant string
		status           string
	)
	err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, query, arg).Scan(
			&rawID, &rawTenant, &p.Username, &p.Email, &p.PasswordHash, &p.Role, &status, &p.CreatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Principal{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.Principal{}, fmt.Errorf("query principal: %w", err)
	}

	if p.ID, err = shareddomain.ParsePrincipalID(rawID); err != nil {
		return domain.Principal{}, fmt.Errorf("decode principal id: %w", err)
	}
	if p.TenantID, err = shareddomain.ParseTenantID(rawTenant); err != nil {
		return domain.Principal{}, fmt.Errorf("decode tenant id: %w", err)
	}
	p.Status = domain.PrincipalStatus(status)
	return p, nil
}

// SetPrincipalStatus disables or reactivates a principal.
func (s *Store) SetPrincipalStatus(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.PrincipalID, status domain.PrincipalStatus) error {
	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE principals SET status = $2 WHERE id = $1`, id.String(), string(status))
		if err != nil {
			return fmt.Errorf("update principal status: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("principal %s: %w", id, ports.ErrNotFound)
		}
		return nil
	})
}

// --- role bindings -------------------------------------------------

// CreateRoleBinding grants a role. Re-granting an identical binding is a
// no-op rather than an error: the caller's intent ("this principal holds
// this role") is already satisfied.
func (s *Store) CreateRoleBinding(ctx context.Context, b domain.RoleBinding) error {
	return s.pool.WithTenant(ctx, b.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO role_bindings (principal_id, tenant_id, role, scope)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (principal_id, role, scope) DO NOTHING
		`, b.PrincipalID.String(), b.TenantID.String(), b.Role, b.Scope)
		if err != nil {
			return fmt.Errorf("insert role binding: %w", err)
		}
		return nil
	})
}

// ListRoleBindings returns every binding held by a principal.
func (s *Store) ListRoleBindings(ctx context.Context, tenantID shareddomain.TenantID, principalID shareddomain.PrincipalID) ([]domain.RoleBinding, error) {
	var bindings []domain.RoleBinding
	err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT role, scope, created_at FROM role_bindings
			WHERE principal_id = $1 ORDER BY role, scope
		`, principalID.String())
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			b := domain.RoleBinding{TenantID: tenantID, PrincipalID: principalID}
			if err := rows.Scan(&b.Role, &b.Scope, &b.CreatedAt); err != nil {
				return err
			}
			bindings = append(bindings, b)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list role bindings: %w", err)
	}
	return bindings, nil
}

// DeleteRoleBinding revokes a role.
func (s *Store) DeleteRoleBinding(ctx context.Context, b domain.RoleBinding) error {
	return s.pool.WithTenant(ctx, b.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			DELETE FROM role_bindings WHERE principal_id = $1 AND role = $2 AND scope = $3
		`, b.PrincipalID.String(), b.Role, b.Scope)
		if err != nil {
			return fmt.Errorf("delete role binding: %w", err)
		}
		return nil
	})
}

// --- api keys ------------------------------------------------------

// CreateApiKey stores a key's digest and metadata. The raw key is not a
// parameter here — it never reaches this layer at all.
func (s *Store) CreateApiKey(ctx context.Context, k domain.ApiKey) error {
	return s.pool.WithTenant(ctx, k.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO api_keys (id, tenant_id, principal_id, key_hash, expires_at, revoked_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, k.ID.String(), k.TenantID.String(), k.PrincipalID.String(), k.KeyHash, k.ExpiresAt, k.RevokedAt)
		if err != nil {
			return fmt.Errorf("insert api key: %w", err)
		}
		return nil
	})
}

// GetApiKeyByHash resolves a presented key's digest to its record,
// within the tenant the presented key names.
//
// The tenant is a parameter rather than something this lookup discovers,
// because api_keys is RLS-protected: a query with no tenant context set
// returns zero rows, whatever the digest. The caller gets the tenant
// from the key itself (application.TenantFromAPIKey) and passes it here,
// which keeps this read tenant-scoped like every other one instead of
// needing a BYPASSRLS role over a credentials table.
//
// Naming a tenant proves nothing on its own — the digest comparison is
// what proves possession, and a forged key naming a real tenant still
// fails it.
func (s *Store) GetApiKeyByHash(ctx context.Context, tenantID shareddomain.TenantID, keyHash string) (domain.ApiKey, error) {
	var (
		k                          domain.ApiKey
		rawID, rawTenant, rawPrinc string
	)
	err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT id, tenant_id, principal_id, key_hash, expires_at, revoked_at, created_at
			FROM api_keys WHERE key_hash = $1
		`, keyHash).Scan(&rawID, &rawTenant, &rawPrinc, &k.KeyHash, &k.ExpiresAt, &k.RevokedAt, &k.CreatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ApiKey{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.ApiKey{}, fmt.Errorf("query api key: %w", err)
	}

	if k.ID, err = shareddomain.ParseApiKeyID(rawID); err != nil {
		return domain.ApiKey{}, fmt.Errorf("decode api key id: %w", err)
	}
	if k.TenantID, err = shareddomain.ParseTenantID(rawTenant); err != nil {
		return domain.ApiKey{}, fmt.Errorf("decode tenant id: %w", err)
	}
	if k.PrincipalID, err = shareddomain.ParsePrincipalID(rawPrinc); err != nil {
		return domain.ApiKey{}, fmt.Errorf("decode principal id: %w", err)
	}
	return k, nil
}

// RevokeApiKey marks a key unusable from revokedAt onwards.
func (s *Store) RevokeApiKey(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.ApiKeyID, revokedAt time.Time) error {
	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE api_keys SET revoked_at = $2 WHERE id = $1`, id.String(), revokedAt)
		if err != nil {
			return fmt.Errorf("revoke api key: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("api key %s: %w", id, ports.ErrNotFound)
		}
		return nil
	})
}

// --- audit ---------------------------------------------------------

// AppendAudit writes one audit record. There is no counterpart that
// updates or deletes one, here or on the port — history is append-only
// by construction (AC-01.4).
func (s *Store) AppendAudit(ctx context.Context, e domain.AuditEntry) error {
	before, err := marshalState(e.BeforeState)
	if err != nil {
		return fmt.Errorf("encode audit before state: %w", err)
	}
	after, err := marshalState(e.AfterState)
	if err != nil {
		return fmt.Errorf("encode audit after state: %w", err)
	}

	return s.pool.WithTenant(ctx, e.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO audit_logs
				(tenant_id, actor_id, actor_type, action, resource_type, resource_id, before_state, after_state, ip_address, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, e.TenantID.String(), e.ActorID.String(), string(e.ActorType), e.Action,
			e.ResourceType, e.ResourceID, before, after, nullableIP(e.IPAddress), e.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert audit entry: %w", err)
		}
		return nil
	})
}

// ListAudit returns a tenant's most recent audit records.
func (s *Store) ListAudit(ctx context.Context, tenantID shareddomain.TenantID, limit int) ([]domain.AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	var entries []domain.AuditEntry
	err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, actor_id, actor_type, action, resource_type, resource_id,
			       before_state, after_state, ip_address, created_at
			FROM audit_logs WHERE tenant_id = $1
			ORDER BY created_at DESC, id DESC
			LIMIT $2
		`, tenantID.String(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				e             domain.AuditEntry
				rawActor      string
				actorType     string
				before, after []byte
				ip            *net.IPNet
			)
			if err := rows.Scan(&e.ID, &rawActor, &actorType, &e.Action, &e.ResourceType,
				&e.ResourceID, &before, &after, &ip, &e.CreatedAt); err != nil {
				return err
			}
			actorID, err := uuid.Parse(rawActor)
			if err != nil {
				return fmt.Errorf("decode audit actor id: %w", err)
			}
			e.TenantID = tenantID
			e.ActorID = actorID
			e.ActorType = domain.ActorType(actorType)
			if e.BeforeState, err = unmarshalState(before); err != nil {
				return err
			}
			if e.AfterState, err = unmarshalState(after); err != nil {
				return err
			}
			if ip != nil {
				e.IPAddress = ip.IP
			}
			entries = append(entries, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	return entries, nil
}

func marshalState(state map[string]any) ([]byte, error) {
	if state == nil {
		return nil, nil
	}
	return json.Marshal(state)
}

func unmarshalState(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode audit state: %w", err)
	}
	return out, nil
}

// nullableIP maps a nil IP to a SQL NULL rather than an empty string, so
// "no client address" is recorded as absent instead of as a bogus value.
func nullableIP(ip net.IP) *string {
	if ip == nil {
		return nil
	}
	s := ip.String()
	return &s
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}
