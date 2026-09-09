// Package postgres persists the installation's licence.
//
// Every query here uses parameter placeholders (OWASP A03, LLD-08 §10).
//
// The store deliberately does no interpretation. It reads and writes
// bytes and timestamps; whether those bytes are a licence anyone should
// believe is the domain's decision, made by verifying the signature on
// load (D-53). A store that returned a parsed entitlement would have
// made that decision already, in the one layer that cannot check it.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"atsap-api/internal/licensing/ports"
)

// Store reads and writes licensing_state.
type Store struct {
	pool *pgxpool.Pool
}

var _ ports.Store = (*Store)(nil)

// NewStore returns a Store over pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Load returns the stored licence, or ports.ErrNoLicense when the
// installation has never been activated.
//
// The claim columns are not selected. They exist for display and
// support, and selecting them here would put them one assignment away
// from an entitlement decision — the exposure D-53 closed. What comes
// back is the signed payload, its signature, and the confirmation
// timestamps that drive the grace period.
func (s *Store) Load(ctx context.Context) (ports.StoredLicense, error) {
	var (
		lic   ports.StoredLicense
		grace *time.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT instance_id, signed_payload, signature, last_confirmed_at, grace_started_at
		  FROM licensing_state
		 LIMIT 1`,
	).Scan(&lic.InstanceID, &lic.SignedPayload, &lic.Signature, &lic.LastConfirmedAt, &grace)

	if errors.Is(err, pgx.ErrNoRows) {
		// Not a failure: a fresh installation is in Setup until a token
		// is applied (D-52).
		return ports.StoredLicense{}, ports.ErrNoLicense
	}
	if err != nil {
		return ports.StoredLicense{}, fmt.Errorf("licensing: load licence: %w", err)
	}
	lic.GraceStartedAt = grace
	return lic, nil
}

// Save replaces the stored licence.
//
// lic.Display is written alongside the payload purely so an
// administrator or a support engineer can read the row. It is a cache
// with no authority (D-53), and Load never reads it back — so editing
// those columns in the database changes no verdict, which the
// integration test in internal/postgres proves directly.
func (s *Store) Save(ctx context.Context, lic ports.StoredLicense) error {
	fingerprint, err := json.Marshal(lic.Display.Fingerprint)
	if err != nil {
		return fmt.Errorf("licensing: encode display fingerprint: %w", err)
	}

	// Replace the licence rather than upsert on instance_id.
	//
	// An installation has one licence (T-1), and applying a new one
	// SUPERSEDES the old rather than joining it — including when the new
	// licence carries a different instance identity, which is exactly the
	// case an upsert keyed on instance_id turns into a second row.
	//
	// Two rows are worse than they look. Load reads "the licence" with
	// LIMIT 1, so it would pick arbitrarily, and a service holding one
	// key reading a row signed by another reports a perfectly good
	// licence as TAMPERED — silently, and presenting as a security event.
	// Observed on 2026-09-09; licensing_state_singleton now makes the
	// second row impossible, and this write is what keeps it so.
	//
	// One transaction, because a window with no licence at all would read
	// as Setup and refuse calls.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("licensing: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM licensing_state`); err != nil {
		return fmt.Errorf("licensing: clear previous licence: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO licensing_state (
			instance_id, signed_payload, signature, fingerprint,
			edition, capacity, max_tenants, max_extensions,
			entitlement_status, last_confirmed_at, grace_started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		lic.InstanceID, lic.SignedPayload, lic.Signature, fingerprint,
		lic.Display.Edition, lic.Display.Capacity, lic.Display.MaxTenants, lic.Display.MaxExtensions,
		lic.Display.Status, lic.LastConfirmedAt, lic.GraceStartedAt,
	); err != nil {
		return fmt.Errorf("licensing: save licence: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("licensing: commit licence: %w", err)
	}
	return nil
}

// TouchConfirmed records a successful entitlement confirmation.
//
// It updates two timestamps and nothing else: confirmation says the
// licence is still ours, never what it grants. Rewriting the payload
// here would let a confirmation response change an entitlement, which
// is a licence issued over the wire without a signature.
func (s *Store) TouchConfirmed(ctx context.Context, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE licensing_state
		   SET last_confirmed_at = $1, grace_started_at = NULL, entitlement_status = 'VALID'`,
		at.UTC())
	if err != nil {
		return fmt.Errorf("licensing: record confirmation: %w", err)
	}
	return nil
}
