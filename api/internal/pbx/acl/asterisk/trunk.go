package asterisk

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

// trunkAuthType is the engine's plaintext-credential auth type. Used for
// trunks and never for extensions: the engine must PRESENT this secret
// to the carrier, so a one-way digest cannot serve (LLD-03 §7.4).
const trunkAuthType = "userpass"

// contactFor is the static contact the engine sends a trunk's calls to.
// IPv6 literals are bracketed, as a SIP URI requires.
func contactFor(t domain.Trunk) string {
	host := t.Host
	if isIPv6(host) {
		host = "[" + host + "]"
	}
	return "sip:" + host + ":" + strconv.Itoa(t.Port)
}

func isIPv6(host string) bool {
	for i := 0; i < len(host); i++ {
		if host[i] == ':' {
			return true
		}
	}
	return false
}

// ProjectTrunk makes trunk usable as an outbound carrier inside the
// caller's transaction.
//
// Three rows, the same family as an extension's but the reverse
// direction: an extension's `auth` is what a device presents TO us; a
// trunk's `outbound_auth` is what we present TO the carrier. The `auth`
// column stays NULL for a trunk — the carrier is not challenged by us.
//
// The secret goes into ps_auths.password and nowhere else. That is the
// LLD-03 §7.4 exception, stated plainly there: the engine has to present
// it, so it must be able to read it. A nil secret keeps whatever is
// already projected (an update that supplies no new secret); a trunk
// with no AuthUsername has no auth row at all (IP authentication).
func (p *Projector) ProjectTrunk(ctx context.Context, tx pgx.Tx, t domain.Trunk, secret []byte) error {
	id := domain.TrunkEndpointIdentifier(t.ID)

	if _, err := tx.Exec(ctx, `
		INSERT INTO ps_aors (id, contact) VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET contact = EXCLUDED.contact
	`, id, contactFor(t)); err != nil {
		return fmt.Errorf("project trunk aor: %w", err)
	}

	var outboundAuth, fromUser *string
	if t.AuthUsername == "" {
		if _, err := tx.Exec(ctx, `DELETE FROM ps_auths WHERE id = $1`, id); err != nil {
			return fmt.Errorf("deproject trunk auth: %w", err)
		}
	} else {
		var password *string
		if secret != nil {
			s := string(secret)
			password = &s
		}
		// COALESCE keeps the projected secret when none is supplied. The
		// platform holds no copy to re-supply it from, by design.
		if _, err := tx.Exec(ctx, `
			INSERT INTO ps_auths (id, auth_type, username, password) VALUES ($1, $2, $3, $4)
			ON CONFLICT (id) DO UPDATE SET
				auth_type = EXCLUDED.auth_type,
				username  = EXCLUDED.username,
				password  = COALESCE(EXCLUDED.password, ps_auths.password)
		`, id, trunkAuthType, t.AuthUsername, password); err != nil {
			return fmt.Errorf("project trunk auth: %w", err)
		}
		outboundAuth, fromUser = &id, &t.AuthUsername
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO ps_endpoints
			(id, transport, aors, auth, outbound_auth, context, disallow, allow, direct_media, from_user, from_domain, set_var)
		VALUES ($1, $2, $3, NULL, $4, $5, $6, $7, 'no', $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			transport     = EXCLUDED.transport,
			aors          = EXCLUDED.aors,
			auth          = NULL,
			outbound_auth = EXCLUDED.outbound_auth,
			context       = EXCLUDED.context,
			disallow      = EXCLUDED.disallow,
			allow         = EXCLUDED.allow,
			direct_media  = EXCLUDED.direct_media,
			from_user     = EXCLUDED.from_user,
			from_domain   = EXCLUDED.from_domain,
			set_var       = EXCLUDED.set_var
	`, id, p.cfg.SIPTransport, id, outboundAuth, stasisContext,
		disallowedCodecs, allowedCodecs, fromUser, t.Host, setVarFor(t.TenantID)); err != nil {
		return fmt.Errorf("project trunk endpoint: %w", err)
	}

	// Identify-by-IP (design D5): one row matching the carrier's host,
	// so signalling from that address is attributed to this trunk.
	// Asterisk resolves a hostname match itself.
	if _, err := tx.Exec(ctx, `
		INSERT INTO ps_endpoint_id_ips (id, endpoint, "match")
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET
			endpoint = EXCLUDED.endpoint,
			"match"  = EXCLUDED."match"
	`, id, id, t.Host); err != nil {
		return fmt.Errorf("project trunk identify: %w", err)
	}
	return nil
}

// RemoveTrunk deprovisions a trunk inside the caller's transaction. Like
// RemoveExtension it is unguarded by the database — these tables carry
// no RLS — and must only be reached after a domain delete RLS authorized.
func (p *Projector) RemoveTrunk(ctx context.Context, tx pgx.Tx, trunkID shareddomain.TrunkID) error {
	id := domain.TrunkEndpointIdentifier(trunkID)
	for _, stmt := range []struct{ table, sql string }{
		{"ps_endpoints", `DELETE FROM ps_endpoints WHERE id = $1`},
		{"ps_auths", `DELETE FROM ps_auths WHERE id = $1`},
		{"ps_aors", `DELETE FROM ps_aors WHERE id = $1`},
		{"ps_endpoint_id_ips", `DELETE FROM ps_endpoint_id_ips WHERE id = $1`},
	} {
		if _, err := tx.Exec(ctx, stmt.sql, id); err != nil {
			return fmt.Errorf("deproject trunk %s: %w", stmt.table, err)
		}
	}
	return nil
}

// DiffTrunk names the projected fields that disagree with t.
//
// The secret cannot be compared: the platform holds no copy. What can be
// checked is that one is present when the trunk has a credential — a
// blanked or deleted password is reported as "secret", and it is the one
// drift no repair can fix, because nothing on our side can re-supply it.
func (p *Projector) DiffTrunk(ctx context.Context, t domain.Trunk) ([]string, error) {
	id := domain.TrunkEndpointIdentifier(t.ID)

	var (
		transport, aors, dialplanContext, allow, fromDomain string
		setVar                                              *string
		auth, outboundAuth, fromUser, contact               *string
	)
	err := p.pool.Unwrap().QueryRow(ctx, `
		SELECT e.transport, e.aors, e.auth, e.outbound_auth, e.context, e.allow, e.from_user, e.from_domain, e.set_var, r.contact
		FROM ps_endpoints e JOIN ps_aors r ON r.id = e.id
		WHERE e.id = $1
	`, id).Scan(&transport, &aors, &auth, &outboundAuth, &dialplanContext, &allow, &fromUser, &fromDomain, &setVar, &contact)
	if errors.Is(err, pgx.ErrNoRows) {
		return []string{"missing"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read projected trunk: %w", err)
	}

	var drift []string
	expect := func(field, want, got string) {
		if want != got {
			drift = append(drift, field)
		}
	}
	str := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	expect("transport", p.cfg.SIPTransport, transport)
	expect("aors", id, aors)
	expect("auth", "", str(auth))
	expect("context", stasisContext, dialplanContext)
	expect("allow", allowedCodecs, allow)
	expect("from_domain", t.Host, fromDomain)
	expect("contact", contactFor(t), str(contact))
	expect("set_var", setVarFor(t.TenantID), str(setVar))

	var identifyMatch *string
	err = p.pool.Unwrap().QueryRow(ctx, `SELECT "match" FROM ps_endpoint_id_ips WHERE id = $1`, id).Scan(&identifyMatch)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		drift = append(drift, "identify")
	case err != nil:
		return nil, fmt.Errorf("read projected trunk identify: %w", err)
	default:
		expect("identify", t.Host, str(identifyMatch))
	}

	if t.AuthUsername == "" {
		expect("outbound_auth", "", str(outboundAuth))
		expect("from_user", "", str(fromUser))
		var stray bool
		if err := p.pool.Unwrap().QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ps_auths WHERE id = $1)`, id).Scan(&stray); err != nil {
			return nil, fmt.Errorf("read projected trunk auth: %w", err)
		}
		if stray {
			drift = append(drift, "auth_row")
		}
		return drift, nil
	}

	expect("outbound_auth", id, str(outboundAuth))
	expect("from_user", t.AuthUsername, str(fromUser))

	var authType, username, password *string
	err = p.pool.Unwrap().QueryRow(ctx, `SELECT auth_type, username, password FROM ps_auths WHERE id = $1`, id).
		Scan(&authType, &username, &password)
	if errors.Is(err, pgx.ErrNoRows) {
		drift = append(drift, "auth_row")
		if t.CredentialRef != "" {
			drift = append(drift, "secret")
		}
		return drift, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read projected trunk auth: %w", err)
	}
	expect("auth_type", trunkAuthType, str(authType))
	expect("username", t.AuthUsername, str(username))
	if t.CredentialRef != "" && str(password) == "" {
		drift = append(drift, "secret")
	}
	return drift, nil
}
