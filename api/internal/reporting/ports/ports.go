// Package ports declares reporting storage and audit ports.
package ports

import (
	"context"

	"github.com/jackc/pgx/v5"

	identitydomain "atsap-api/internal/identity/domain"
	"atsap-api/internal/reporting/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

// Store reads terminated calls for reporting. It is read-only.
type Store interface {
	List(ctx context.Context, tenantID shareddomain.TenantID, filter domain.Filter, limit, offset int) ([]domain.CallRecord, error)
	// Export returns up to maxRows matching records, oldest first.
	Export(ctx context.Context, tenantID shareddomain.TenantID, filter domain.Filter, maxRows int) ([]domain.CallRecord, error)
}

// AuditAppender is identity's audit writer, used for export access
// (AC-07.5). Reporting never writes another context's table directly.
type AuditAppender interface {
	AppendAuditTx(ctx context.Context, tx pgx.Tx, entry identitydomain.AuditEntry) error
}
