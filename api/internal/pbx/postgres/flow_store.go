package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// FlowStore persists IVR flows and their versions.
type FlowStore struct{}

// NewFlowStore returns the store.
func NewFlowStore() *FlowStore { return &FlowStore{} }

var _ ports.FlowStore = (*FlowStore)(nil)

func (s *FlowStore) Create(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, f domain.Flow, v domain.FlowVersion) error {
	_, err := tx.Exec(ctx, `INSERT INTO ivr_flows (id, tenant_id, number, name, active_version, created_at, updated_at)
		VALUES ($1,$2,$3,$4,1,$5,$5)`, f.ID.String(), tenantID.String(), f.Number, f.Name, f.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return ports.ErrInboundNumberTaken
		}
		return fmt.Errorf("create flow: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO ivr_flow_versions
		(tenant_id, flow_id, version, open_extension_id, closed_extension_id, business_start_minute, business_end_minute, prompt, menu, menu_timeout_seconds, no_input_extension_id, invalid_extension_id, published_at, published_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		tenantID.String(), f.ID.String(), v.Version, nullableExtension(v.Config.OpenExtensionID),
		nullableExtension(v.Config.ClosedExtensionID), v.Config.BusinessStartMinute, v.Config.BusinessEndMinute,
		v.Config.Prompt, menuJSON(v.Config.Menu), v.Config.MenuTimeoutSeconds,
		nullableExtension(v.Config.NoInputExtensionID), nullableExtension(v.Config.InvalidExtensionID),
		v.PublishedAt, v.PublishedBy.String())
	if err != nil {
		return fmt.Errorf("create flow version: %w", err)
	}
	return nil
}

func (s *FlowStore) Get(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, id shareddomain.FlowID) (domain.Flow, domain.FlowVersion, error) {
	var (
		f                   domain.Flow
		rawID, number, name string
		active              int
		created, updated    time.Time
	)
	err := tx.QueryRow(ctx, `SELECT id, number, name, active_version, created_at, updated_at
		FROM ivr_flows WHERE tenant_id=$1 AND id=$2`, tenantID.String(), id.String()).
		Scan(&rawID, &number, &name, &active, &created, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, domain.FlowVersion{}, ports.ErrNotFound
	}
	if err != nil {
		return f, domain.FlowVersion{}, fmt.Errorf("get flow: %w", err)
	}
	f = domain.Flow{ID: id, TenantID: tenantID, Number: number, Name: name, ActiveVersion: active, CreatedAt: created, UpdatedAt: updated}
	v, err := s.version(ctx, tx, tenantID, id, active)
	return f, v, err
}

func (s *FlowStore) FindByNumber(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, number string) (domain.Flow, domain.FlowVersion, error) {
	var rawID string
	err := tx.QueryRow(ctx, `SELECT id FROM ivr_flows WHERE tenant_id=$1 AND number=$2`, tenantID.String(), number).Scan(&rawID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Flow{}, domain.FlowVersion{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.Flow{}, domain.FlowVersion{}, fmt.Errorf("find flow: %w", err)
	}
	id, err := shareddomain.ParseFlowID(rawID)
	if err != nil {
		return domain.Flow{}, domain.FlowVersion{}, err
	}
	return s.Get(ctx, tx, tenantID, id)
}

func (s *FlowStore) List(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID) ([]ports.FlowView, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM ivr_flows WHERE tenant_id=$1 ORDER BY number`, tenantID.String())
	if err != nil {
		return nil, err
	}
	var ids []shareddomain.FlowID
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		id, err := shareddomain.ParseFlowID(raw)
		if err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	out := make([]ports.FlowView, 0, len(ids))
	for _, id := range ids {
		f, v, err := s.Get(ctx, tx, tenantID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, flowView(f, v))
	}
	return out, nil
}

func (s *FlowStore) Publish(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, id shareddomain.FlowID, config domain.FlowConfig, by shareddomain.PrincipalID, at time.Time) (int, error) {
	var next int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM ivr_flow_versions WHERE tenant_id=$1 AND flow_id=$2`,
		tenantID.String(), id.String()).Scan(&next); err != nil {
		return 0, err
	}
	_, err := tx.Exec(ctx, `INSERT INTO ivr_flow_versions
		(tenant_id, flow_id, version, open_extension_id, closed_extension_id, business_start_minute, business_end_minute, prompt, menu, menu_timeout_seconds, no_input_extension_id, invalid_extension_id, published_at, published_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		tenantID.String(), id.String(), next, nullableExtension(config.OpenExtensionID), nullableExtension(config.ClosedExtensionID),
		config.BusinessStartMinute, config.BusinessEndMinute, config.Prompt, menuJSON(config.Menu),
		config.MenuTimeoutSeconds, nullableExtension(config.NoInputExtensionID), nullableExtension(config.InvalidExtensionID),
		at, by.String())
	if err != nil {
		return 0, fmt.Errorf("publish flow: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE ivr_flows SET active_version=$2, updated_at=$3 WHERE tenant_id=$1 AND id=$4`,
		tenantID.String(), next, at, id.String()); err != nil {
		return 0, err
	}
	return next, nil
}

func (s *FlowStore) Rollback(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, id shareddomain.FlowID, version int) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ivr_flow_versions WHERE tenant_id=$1 AND flow_id=$2 AND version=$3)`,
		tenantID.String(), id.String(), version).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ports.ErrNotFound
	}
	_, err := tx.Exec(ctx, `UPDATE ivr_flows SET active_version=$2, updated_at=NOW() WHERE tenant_id=$1 AND id=$3`,
		tenantID.String(), version, id.String())
	return err
}

func (s *FlowStore) Versions(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, id shareddomain.FlowID) ([]ports.FlowVersionView, error) {
	rows, err := tx.Query(ctx, `SELECT version, open_extension_id, closed_extension_id, business_start_minute, business_end_minute, prompt, published_at, published_by
		FROM ivr_flow_versions WHERE tenant_id=$1 AND flow_id=$2 ORDER BY version DESC`, tenantID.String(), id.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ports.FlowVersionView
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *FlowStore) Delete(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, id shareddomain.FlowID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM ivr_flows WHERE tenant_id=$1 AND id=$2`, tenantID.String(), id.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

func (s *FlowStore) ListHolidays(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT day::text, name FROM ivr_holidays WHERE tenant_id=$1 ORDER BY day`, tenantID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var day, name string
		if err := rows.Scan(&day, &name); err != nil {
			return nil, err
		}
		out[day] = name
	}
	return out, rows.Err()
}

func (s *FlowStore) AddHoliday(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, day, name string) error {
	_, err := tx.Exec(ctx, `INSERT INTO ivr_holidays (tenant_id, day, name) VALUES ($1,$2,$3)
		ON CONFLICT (tenant_id, day) DO UPDATE SET name=EXCLUDED.name`, tenantID.String(), day, name)
	return err
}

func (s *FlowStore) DeleteHoliday(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, day string) error {
	_, err := tx.Exec(ctx, `DELETE FROM ivr_holidays WHERE tenant_id=$1 AND day=$2`, tenantID.String(), day)
	return err
}

func (s *FlowStore) IsHoliday(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, day string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ivr_holidays WHERE tenant_id=$1 AND day=$2)`, tenantID.String(), day).Scan(&exists)
	return exists, err
}

func (s *FlowStore) version(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, id shareddomain.FlowID, version int) (domain.FlowVersion, error) {
	row := tx.QueryRow(ctx, `SELECT version, open_extension_id, closed_extension_id, business_start_minute, business_end_minute, prompt,
		menu, menu_timeout_seconds, no_input_extension_id, invalid_extension_id, published_at, published_by
		FROM ivr_flow_versions WHERE tenant_id=$1 AND flow_id=$2 AND version=$3`, tenantID.String(), id.String(), version)
	vv, err := scanVersion(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.FlowVersion{}, ports.ErrNotFound
		}
		return domain.FlowVersion{}, err
	}
	return domain.FlowVersion{FlowID: id, Version: vv.Version, Config: vv.Config, PublishedAt: vv.PublishedAt, PublishedBy: vv.PublishedBy}, nil
}

func scanVersion(row pgx.Row) (ports.FlowVersionView, error) {
	var (
		v                              ports.FlowVersionView
		open, closed, noInput, invalid *string
		menuRaw                        []byte
	)
	if err := row.Scan(&v.Version, &open, &closed, &v.Config.BusinessStartMinute, &v.Config.BusinessEndMinute,
		&v.Config.Prompt, &menuRaw, &v.Config.MenuTimeoutSeconds, &noInput, &invalid,
		&v.PublishedAt, &v.PublishedBy); err != nil {
		return v, err
	}
	if open != nil {
		id, err := shareddomain.ParseExtensionID(*open)
		if err != nil {
			return v, err
		}
		v.Config.OpenExtensionID = &id
	}
	if closed != nil {
		id, err := shareddomain.ParseExtensionID(*closed)
		if err != nil {
			return v, err
		}
		v.Config.ClosedExtensionID = &id
	}
	if noInput != nil {
		id, err := shareddomain.ParseExtensionID(*noInput)
		if err != nil {
			return v, err
		}
		v.Config.NoInputExtensionID = &id
	}
	if invalid != nil {
		id, err := shareddomain.ParseExtensionID(*invalid)
		if err != nil {
			return v, err
		}
		v.Config.InvalidExtensionID = &id
	}
	v.Config.Menu = map[string]shareddomain.ExtensionID{}
	var raw map[string]string
	if len(menuRaw) > 0 {
		if err := json.Unmarshal(menuRaw, &raw); err != nil {
			return v, err
		}
	}
	for digit, id := range raw {
		ext, err := shareddomain.ParseExtensionID(id)
		if err != nil {
			return v, err
		}
		v.Config.Menu[digit] = ext
	}
	return v, nil
}

func menuJSON(menu map[string]shareddomain.ExtensionID) []byte {
	raw := map[string]string{}
	for digit, ext := range menu {
		raw[digit] = ext.String()
	}
	b, _ := json.Marshal(raw)
	return b
}

func flowView(f domain.Flow, v domain.FlowVersion) ports.FlowView {
	return ports.FlowView{ID: f.ID, Number: f.Number, Name: f.Name, ActiveVersion: f.ActiveVersion, Config: v.Config}
}

func nullableExtension(id *shareddomain.ExtensionID) *string {
	if id == nil {
		return nil
	}
	v := id.String()
	return &v
}
