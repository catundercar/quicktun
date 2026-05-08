package dao

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/model"
)

// AuditDAO encapsulates read-only queries against the audit_logs table.
//
// The audit_logs table is append-only: inserts go through audit.Writer (a
// separate type) and updates/deletes never happen at the application layer.
// This DAO is the reader counterpart used by AuditService.ListAuditLogs.
type AuditDAO struct{ db *gorm.DB }

// NewAuditDAO constructs an AuditDAO bound to db.
func NewAuditDAO(db *gorm.DB) *AuditDAO { return &AuditDAO{db: db} }

// AuditListFilter holds optional filters for AuditDAO.List. All fields are
// optional; the zero value means "no filter".
type AuditListFilter struct {
	OperatorEmail string
	ProjectSlug   string
	ActionPrefix  string
	Since         *time.Time
	Until         *time.Time

	// Limit caps the number of rows returned. Callers should pass limit+1 if
	// they want to probe for a "has next page" sentinel. List does not apply
	// any default; pass a non-zero value.
	Limit int

	// AfterID acts as a descending-order cursor: when non-zero, only rows with
	// id < AfterID are returned (i.e. older than the last row of the previous
	// page).
	AfterID uint64
}

// AuditListRow is the joined row returned by List. project_slug and
// operator_email are pulled via LEFT JOIN so cross-project / system entries
// (where the FK is nil) come back with empty strings rather than being
// filtered out.
type AuditListRow struct {
	ID            uint64
	Ts            time.Time
	OperatorEmail string
	SourceIP      string
	Action        string
	Target        string
	ProjectSlug   string
	ExtraJSON     string
}

// List returns audit log entries newest-first (ORDER BY id DESC), filtered by
// f. Up to f.Limit rows are returned.
//
// Ordering is by primary key DESC rather than by ts: id is an autoincrement
// column written at insert time, so it's monotonic with wall-clock arrival
// order and never has ties, which makes the (id < AfterID) cursor unambiguous.
// The ts column is informational and may diverge from insertion order if a
// caller backfills entries with a custom timestamp.
func (d *AuditDAO) List(ctx context.Context, f AuditListFilter) ([]AuditListRow, error) {
	q := d.baseQuery(ctx, f)

	if f.AfterID > 0 {
		q = q.Where("audit_logs.id < ?", f.AfterID)
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}

	var rows []AuditListRow
	if err := q.
		Order("audit_logs.id DESC").
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("dao: list audit logs: %w", err)
	}
	return rows, nil
}

// Count returns the number of rows matching the filter (no cursor / limit
// applied). Callers can surface this as ListAuditLogsResponse.total_size.
func (d *AuditDAO) Count(ctx context.Context, f AuditListFilter) (uint32, error) {
	q := d.baseQuery(ctx, f)
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return 0, fmt.Errorf("dao: count audit logs: %w", err)
	}
	if n < 0 {
		n = 0
	}
	return uint32(n), nil
}

// baseQuery builds the joined SELECT used by List + Count. The same JOINs
// are required regardless of whether the caller wants rows or just a count,
// because operator_email / project_slug filters need them.
func (d *AuditDAO) baseQuery(ctx context.Context, f AuditListFilter) *gorm.DB {
	q := d.db.WithContext(ctx).
		Table("audit_logs").
		Select("audit_logs.id, audit_logs.ts, " +
			"COALESCE(operators.email, '') AS operator_email, " +
			"audit_logs.source_ip, audit_logs.action, audit_logs.target, " +
			"COALESCE(projects.slug, '') AS project_slug, " +
			"audit_logs.extra_json").
		Joins("LEFT JOIN operators ON operators.id = audit_logs.operator_id").
		Joins("LEFT JOIN projects ON projects.id = audit_logs.project_id")

	if f.OperatorEmail != "" {
		q = q.Where("operators.email = ?", f.OperatorEmail)
	}
	if f.ProjectSlug != "" {
		q = q.Where("projects.slug = ?", f.ProjectSlug)
	}
	if f.ActionPrefix != "" {
		// Match by simple LIKE 'prefix%'. Action values are operator-controlled
		// only via fixed verbs in audit.Entry call sites, so % / _ in the
		// prefix would be a code-path bug, not user input — no need to escape.
		q = q.Where("audit_logs.action LIKE ?", f.ActionPrefix+"%")
	}
	if f.Since != nil {
		q = q.Where("audit_logs.ts >= ?", *f.Since)
	}
	if f.Until != nil {
		q = q.Where("audit_logs.ts <= ?", *f.Until)
	}

	return q
}

// Compile-time assurance that the model package is referenced even if the
// query above only uses raw table names — keeps the import meaningful.
var _ = model.AuditLog{}
