package asterisk

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
)

// ingressLock serialises rebuilds of the carrier-ingress row: two trunk
// writes in different tenants each read every trunk's addresses, and
// without it the later commit could overwrite the earlier one's union
// with a snapshot that lacks it.
const ingressLock = 0x617473617062786e // "atsapbxn"

// ProjectIngress writes the engine's carrier-ingress endpoint, its AOR,
// and its identify row: the union of every trunk's carrier addresses and
// inbound ranges, across tenants (D-77). The endpoint carries no tenant;
// the platform attributes each call. With no address anywhere there is
// no identify row, and the ingress recognises nothing.
func (p *Projector) ProjectIngress(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(ingressLock)); err != nil {
		return fmt.Errorf("lock carrier ingress: %w", err)
	}
	match, ok, err := ingressMatch(ctx, tx)
	if err != nil {
		return err
	}
	id := domain.CarrierIngressEndpoint
	if _, err := tx.Exec(ctx, `INSERT INTO ps_aors (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, id); err != nil {
		return fmt.Errorf("project carrier ingress aor: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO ps_endpoints (id, transport, aors, auth, outbound_auth, context, disallow, allow, direct_media, set_var)
		VALUES ($1, $2, $1, NULL, NULL, $3, $4, $5, 'no', NULL)
		ON CONFLICT (id) DO UPDATE SET
			transport = EXCLUDED.transport, aors = EXCLUDED.aors, auth = NULL, outbound_auth = NULL,
			context = EXCLUDED.context, disallow = EXCLUDED.disallow, allow = EXCLUDED.allow,
			direct_media = EXCLUDED.direct_media, set_var = NULL
	`, id, p.cfg.SIPTransport, stasisContext, disallowedCodecs, allowedCodecs); err != nil {
		return fmt.Errorf("project carrier ingress endpoint: %w", err)
	}
	if !ok {
		if _, err := tx.Exec(ctx, `DELETE FROM ps_endpoint_id_ips WHERE id = $1`, id); err != nil {
			return fmt.Errorf("deproject carrier ingress identify: %w", err)
		}
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO ps_endpoint_id_ips (id, endpoint, "match") VALUES ($1, $1, $2)
		ON CONFLICT (id) DO UPDATE SET endpoint = EXCLUDED.endpoint, "match" = EXCLUDED."match"
	`, id, match); err != nil {
		return fmt.Errorf("project carrier ingress identify: %w", err)
	}
	return nil
}

// EnsureIngress is what the app does at startup: the ingress exists and
// matches every trunk, and no per-trunk identify row from before D-77
// remains to compete with it.
func (p *Projector) EnsureIngress(ctx context.Context) error {
	return pgx.BeginFunc(ctx, p.pool.Unwrap(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM ps_endpoint_id_ips WHERE id LIKE 't\_%'`); err != nil {
			return fmt.Errorf("remove per-trunk identify rows: %w", err)
		}
		return p.ProjectIngress(ctx, tx)
	})
}

// DiffIngress names what of the carrier-ingress projection disagrees with
// every trunk's addresses: "missing", or the fields that differ.
func (p *Projector) DiffIngress(ctx context.Context) ([]string, error) {
	var drift []string
	err := pgx.BeginFunc(ctx, p.pool.Unwrap(), func(tx pgx.Tx) error {
		want, wantRow, err := ingressMatch(ctx, tx)
		if err != nil {
			return err
		}
		var transport, dialplanContext, allow string
		var setVar *string
		err = tx.QueryRow(ctx, `SELECT transport, context, allow, set_var FROM ps_endpoints WHERE id = $1`, domain.CarrierIngressEndpoint).
			Scan(&transport, &dialplanContext, &allow, &setVar)
		if errors.Is(err, pgx.ErrNoRows) {
			drift = []string{"missing"}
			return nil
		}
		if err != nil {
			return fmt.Errorf("read carrier ingress: %w", err)
		}
		if transport != p.cfg.SIPTransport {
			drift = append(drift, "transport")
		}
		if dialplanContext != stasisContext {
			drift = append(drift, "context")
		}
		if allow != allowedCodecs {
			drift = append(drift, "allow")
		}
		if setVar != nil && *setVar != "" {
			drift = append(drift, "set_var") // the ingress must never name a tenant
		}
		var got *string
		err = tx.QueryRow(ctx, `SELECT "match" FROM ps_endpoint_id_ips WHERE id = $1`, domain.CarrierIngressEndpoint).Scan(&got)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if wantRow {
				drift = append(drift, "identify")
			}
		case err != nil:
			return fmt.Errorf("read carrier ingress identify: %w", err)
		case !wantRow || got == nil || *got != want:
			drift = append(drift, "identify")
		}
		return nil
	})
	return drift, err
}

// ingressMatch reads every trunk's addresses through the one cross-tenant
// read built for it (migration 0016) and folds them into an identify
// match.
func ingressMatch(ctx context.Context, tx pgx.Tx) (string, bool, error) {
	rows, err := tx.Query(ctx, `SELECT resolved, inbound FROM carrier_ingress_sources()`)
	if err != nil {
		return "", false, fmt.Errorf("read carrier addresses: %w", err)
	}
	defer rows.Close()
	var addrs []netip.Addr
	var ranges []netip.Prefix
	for rows.Next() {
		var a []netip.Addr
		var r []netip.Prefix
		if err := rows.Scan(&a, &r); err != nil {
			return "", false, fmt.Errorf("scan carrier addresses: %w", err)
		}
		addrs = append(addrs, a...)
		ranges = append(ranges, r...)
	}
	if err := rows.Err(); err != nil {
		return "", false, fmt.Errorf("read carrier addresses: %w", err)
	}
	match, ok := domain.IdentifyMatch(addrs, ranges)
	return match, ok, nil
}
