// Package application implements reporting read use cases and export.
package application

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	identityapp "atsap-api/internal/identity/application"
	identitydomain "atsap-api/internal/identity/domain"
	identityports "atsap-api/internal/identity/ports"
	corepostgres "atsap-api/internal/postgres"
	"atsap-api/internal/reporting/domain"
	"atsap-api/internal/reporting/ports"
)

// Page bounds a report listing.
type Page struct{ Limit, Offset int }

// Service implements the reporting read use cases.
type Service struct {
	pool  *corepostgres.Pool
	store ports.Store
	audit ports.AuditAppender
	now   func() time.Time
}

// NewService returns a reporting service. audit may be nil in tests; an
// export without it is still read-only.
func NewService(pool *corepostgres.Pool, store ports.Store, audit ports.AuditAppender) *Service {
	return &Service{pool: pool, store: store, audit: audit, now: func() time.Time { return time.Now().UTC() }}
}

// ListCallRecords returns terminated calls. An agent (a membership without
// an administrator/supervisor role) is narrowed to their own participant
// rows; everyone else sees the tenant.
func (s *Service) ListCallRecords(ctx context.Context, filter domain.Filter, page Page) ([]domain.CallRecord, error) {
	tc, err := tenantContext(ctx)
	if err != nil {
		return nil, err
	}
	filter, err = scopeFilter(tc, filter)
	if err != nil {
		return nil, err
	}
	return s.store.List(ctx, tc.TenantID, filter, page.Limit, page.Offset)
}

// ExportCallRecords returns a CSV of matching records and audits the
// export naming the person (AC-07.5).
func (s *Service) ExportCallRecords(ctx context.Context, filter domain.Filter) ([]byte, error) {
	tc, err := tenantContext(ctx)
	if err != nil {
		return nil, err
	}
	filter, err = scopeFilter(tc, filter)
	if err != nil {
		return nil, err
	}
	records, err := s.store.Export(ctx, tc.TenantID, filter, 10000)
	if err != nil {
		return nil, err
	}
	if s.audit != nil {
		if err := s.pool.WithTenant(ctx, tc.TenantID, func(ctx context.Context, tx pgx.Tx) error {
			entry := identitydomain.NewAuditEntry(tc.TenantID, uuid.UUID(tc.PrincipalID), identitydomain.ActorPrincipal,
				"report.export", "call_records", "", nil,
				map[string]any{"from": filter.From, "to": filter.To, "direction": filter.Direction, "count": len(records)},
				nil, s.now())
			return s.audit.AppendAuditTx(ctx, tx, entry)
		}); err != nil {
			return nil, fmt.Errorf("audit export: %w", err)
		}
	}
	return csvOf(records)
}

func tenantContext(ctx context.Context) (*identityports.TenantContext, error) {
	tc, ok := identityapp.TenantContextFrom(ctx)
	if !ok || tc == nil {
		return nil, fmt.Errorf("reporting: no authenticated principal in context")
	}
	return tc, nil
}

func scopeFilter(tc *identityports.TenantContext, f domain.Filter) (domain.Filter, error) {
	if tc.PrincipalID.IsZero() {
		return f, fmt.Errorf("reporting: no principal")
	}
	for _, role := range tc.Roles {
		if role == "TENANT_ADMIN" || role == "PLATFORM_ADMIN" || role == "SUPERVISOR" {
			return f, nil
		}
	}
	own := tc.PrincipalID
	f.PrincipalID = &own
	return f, nil
}

func csvOf(records []domain.CallRecord) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"call_id", "direction", "source_number", "dest_number", "started_at", "answered_at",
		"ended_at", "duration_seconds", "termination_reason", "participant_id", "role", "extension_id",
		"principal_id", "billable_seconds", "usage_seconds"})
	for _, r := range records {
		duration := int64(0)
		if r.AnsweredAt != nil && r.EndedAt != nil {
			duration = int64(r.EndedAt.Sub(*r.AnsweredAt) / time.Second)
		}
		if len(r.Participants) == 0 {
			_ = w.Write([]string{r.CallID.String(), r.Direction, r.SourceNumber, r.DestNumber,
				stamp(r.StartedAt), stampPtr(r.AnsweredAt), stampPtr(r.EndedAt), strconv.FormatInt(duration, 10),
				r.TerminationReason, "", "", "", "", "0", "0"})
			continue
		}
		for _, p := range r.Participants {
			ext, principal := "", ""
			if p.ExtensionID != nil {
				ext = p.ExtensionID.String()
			}
			if p.PrincipalID != nil {
				principal = p.PrincipalID.String()
			}
			_ = w.Write([]string{r.CallID.String(), r.Direction, r.SourceNumber, r.DestNumber,
				stamp(r.StartedAt), stampPtr(r.AnsweredAt), stampPtr(r.EndedAt), strconv.FormatInt(duration, 10),
				r.TerminationReason, p.ID.String(), p.Role, ext, principal,
				strconv.Itoa(p.BillableSeconds), strconv.Itoa(p.UsageSeconds)})
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }
func stampPtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return stamp(*t)
}
