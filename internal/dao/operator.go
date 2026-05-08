package dao

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/model"
)

// OperatorDAO encapsulates queries against the operators table.
type OperatorDAO struct{ db *gorm.DB }

// NewOperatorDAO constructs an OperatorDAO bound to db.
func NewOperatorDAO(db *gorm.DB) *OperatorDAO { return &OperatorDAO{db: db} }

// Create inserts a new operator. passwordHash must already be bcrypt-encoded;
// callers (CLI / handlers) hash before calling.
func (d *OperatorDAO) Create(ctx context.Context, email, passwordHash string, isAdmin bool) (*model.Operator, error) {
	op := &model.Operator{Email: email, PasswordHash: passwordHash, IsAdmin: isAdmin}
	if err := d.db.WithContext(ctx).Create(op).Error; err != nil {
		return nil, fmt.Errorf("dao: create operator: %w", err)
	}
	return op, nil
}

// FindByEmail returns the live (non-soft-deleted) operator with the given email.
func (d *OperatorDAO) FindByEmail(ctx context.Context, email string) (*model.Operator, error) {
	var op model.Operator
	if err := d.db.WithContext(ctx).Where("email = ?", email).First(&op).Error; err != nil {
		return nil, fmt.Errorf("dao: find operator by email: %w", err)
	}
	return &op, nil
}

// FindByID returns the live operator at id.
func (d *OperatorDAO) FindByID(ctx context.Context, id uint64) (*model.Operator, error) {
	var op model.Operator
	if err := d.db.WithContext(ctx).First(&op, id).Error; err != nil {
		return nil, fmt.Errorf("dao: find operator by id: %w", err)
	}
	return &op, nil
}

// List returns up to pageSize operators ordered by id ASC, starting after
// pageToken (which encodes the last seen operator ID). pageSize <= 0 means
// 50; > 1000 is clamped to 1000. An empty pageToken starts from the first row.
func (d *OperatorDAO) List(ctx context.Context, pageSize int, pageToken string) ([]model.Operator, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	q := d.db.WithContext(ctx).Order("id ASC").Limit(pageSize)
	if pageToken != "" {
		afterID, err := strconv.ParseUint(pageToken, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPageToken, err)
		}
		q = q.Where("id > ?", afterID)
	}
	var out []model.Operator
	if err := q.Find(&out).Error; err != nil {
		return nil, fmt.Errorf("dao: list operators: %w", err)
	}
	return out, nil
}

// NextOperatorPageToken returns the page token to fetch the page AFTER the
// given page. Returns "" when the page is empty.
func NextOperatorPageToken(page []model.Operator) string {
	if len(page) == 0 {
		return ""
	}
	return strconv.FormatUint(page[len(page)-1].ID, 10)
}

// UpdateIsAdmin flips the is_admin flag on the operator at id. Idempotent.
func (d *OperatorDAO) UpdateIsAdmin(ctx context.Context, id uint64, isAdmin bool) error {
	res := d.db.WithContext(ctx).Model(&model.Operator{}).
		Where("id = ?", id).
		Update("is_admin", isAdmin)
	if res.Error != nil {
		return fmt.Errorf("dao: update operator is_admin: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("dao: update operator is_admin: %w", gorm.ErrRecordNotFound)
	}
	return nil
}

// UpdatePassword overwrites password_hash for the operator at id. The hash
// must already be bcrypt-encoded; callers (CLI / handlers) hash before calling.
func (d *OperatorDAO) UpdatePassword(ctx context.Context, id uint64, hashedPassword string) error {
	res := d.db.WithContext(ctx).Model(&model.Operator{}).
		Where("id = ?", id).
		Update("password_hash", hashedPassword)
	if res.Error != nil {
		return fmt.Errorf("dao: update operator password: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("dao: update operator password: %w", gorm.ErrRecordNotFound)
	}
	return nil
}

// Delete soft-deletes the operator at id. Idempotent: returns nil when the
// row is already gone (Save's RowsAffected==0 case is treated as success so
// repeated DELETE calls don't error).
func (d *OperatorDAO) Delete(ctx context.Context, id uint64) error {
	res := d.db.WithContext(ctx).Delete(&model.Operator{}, id)
	if res.Error != nil {
		return fmt.Errorf("dao: delete operator: %w", res.Error)
	}
	return nil
}

// CountAdmins returns the number of live operators with is_admin = true.
// Used to refuse demoting / deleting the last admin.
func (d *OperatorDAO) CountAdmins(ctx context.Context) (int64, error) {
	var n int64
	err := d.db.WithContext(ctx).Model(&model.Operator{}).
		Where("is_admin = ?", true).
		Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("dao: count admins: %w", err)
	}
	return n, nil
}

// OperatorProjectAccessDAO encapsulates queries against the
// operator_project_access join table. Grants are upsert (insert or update
// the role on a live row); revokes soft-delete and free the (operator,
// project) tuple for re-grant.
type OperatorProjectAccessDAO struct{ db *gorm.DB }

// NewOperatorProjectAccessDAO constructs an OperatorProjectAccessDAO.
func NewOperatorProjectAccessDAO(db *gorm.DB) *OperatorProjectAccessDAO {
	return &OperatorProjectAccessDAO{db: db}
}

// List returns all live access grants for operatorID, joined back to the
// project's slug for human-readable output. The composite uniqueness on
// (operator_id, project_id) ensures no duplicates.
type OperatorProjectAccessRow struct {
	model.OperatorProjectAccess
	ProjectSlug string `gorm:"column:project_slug"`
}

// List returns all live access grants for operatorID, including the joined
// project slug so callers can render results without a second round-trip.
func (d *OperatorProjectAccessDAO) List(ctx context.Context, operatorID uint64) ([]OperatorProjectAccessRow, error) {
	var rows []OperatorProjectAccessRow
	err := d.db.WithContext(ctx).
		Table("operator_project_access").
		Select("operator_project_access.*, projects.slug AS project_slug").
		Joins("JOIN projects ON projects.id = operator_project_access.project_id AND projects.deleted_at IS NULL").
		Where("operator_project_access.operator_id = ? AND operator_project_access.deleted_at IS NULL", operatorID).
		Order("operator_project_access.id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("dao: list operator project access: %w", err)
	}
	return rows, nil
}

// Grant upserts an access grant for (operatorID, projectID). If a live row
// already exists, its role is updated; otherwise a new row is inserted.
// Returns the resulting row.
func (d *OperatorProjectAccessDAO) Grant(ctx context.Context, operatorID, projectID uint64, role model.ProjectRole) (*model.OperatorProjectAccess, error) {
	var existing model.OperatorProjectAccess
	err := d.db.WithContext(ctx).
		Where("operator_id = ? AND project_id = ?", operatorID, projectID).
		First(&existing).Error
	if err == nil {
		if existing.Role != role {
			existing.Role = role
			if e := d.db.WithContext(ctx).Save(&existing).Error; e != nil {
				return nil, fmt.Errorf("dao: update access role: %w", e)
			}
		}
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("dao: lookup access: %w", err)
	}
	row := &model.OperatorProjectAccess{
		OperatorID: operatorID,
		ProjectID:  projectID,
		Role:       role,
	}
	if err := d.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, fmt.Errorf("dao: create access: %w", err)
	}
	return row, nil
}

// Revoke soft-deletes the (operatorID, projectID) access grant. Idempotent:
// missing rows are not an error.
func (d *OperatorProjectAccessDAO) Revoke(ctx context.Context, operatorID, projectID uint64) error {
	res := d.db.WithContext(ctx).
		Where("operator_id = ? AND project_id = ?", operatorID, projectID).
		Delete(&model.OperatorProjectAccess{})
	if res.Error != nil {
		return fmt.Errorf("dao: revoke access: %w", res.Error)
	}
	return nil
}

// SessionDAO encapsulates session create / validate / revoke.
type SessionDAO struct{ db *gorm.DB }

// compile-time assertion that *SessionDAO satisfies auth.Validator.
var _ auth.Validator = (*SessionDAO)(nil)

// NewSessionDAO constructs a SessionDAO bound to db.
func NewSessionDAO(db *gorm.DB) *SessionDAO { return &SessionDAO{db: db} }

// Issue creates a new session for operatorID with the given TTL and metadata.
// Returns the persisted session record and the raw bearer token (which is
// only ever returned here — callers must surface it to the client immediately;
// the DB only stores the hash).
func (d *SessionDAO) Issue(ctx context.Context, operatorID uint64, ttl time.Duration, userAgent, sourceIP string) (*model.OperatorSession, string, error) {
	raw, hash, err := auth.IssueToken()
	if err != nil {
		return nil, "", err
	}
	now := time.Now().UTC()
	rec := &model.OperatorSession{
		OperatorID: operatorID,
		TokenHash:  hash,
		IssuedAt:   now,
		ExpiresAt:  now.Add(ttl),
		UserAgent:  userAgent,
		SourceIP:   sourceIP,
	}
	if err := d.db.WithContext(ctx).Create(rec).Error; err != nil {
		return nil, "", fmt.Errorf("dao: create session: %w", err)
	}
	return rec, raw, nil
}

// Validate looks up the session for rawToken, verifies it has not expired
// and has not been revoked, and returns the owning operator.
func (d *SessionDAO) Validate(ctx context.Context, rawToken string) (*model.Operator, error) {
	hash := auth.HashToken(rawToken)
	var sess model.OperatorSession
	err := d.db.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", hash, time.Now().UTC()).
		First(&sess).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("dao: session: invalid or expired token")
		}
		return nil, fmt.Errorf("dao: session lookup: %w", err)
	}
	var op model.Operator
	if err := d.db.WithContext(ctx).First(&op, sess.OperatorID).Error; err != nil {
		return nil, fmt.Errorf("dao: session operator lookup: %w", err)
	}
	return &op, nil
}

// ValidateSessionRaw hashes raw and looks up an operator_session row whose
// token_hash matches, has not been revoked, and whose expires_at is in the
// future. Returns the owning operator's ID and IsAdmin flag, or
// gorm.ErrRecordNotFound (wrapped) if no such session exists.
//
// Mirrors SiteAgentTokenDAO.ValidateRaw. Used by the auth-proxy's operator
// session path; the gRPC interceptor uses Validate (which also returns the
// Operator row) instead.
func (d *SessionDAO) ValidateSessionRaw(ctx context.Context, raw string) (uint64, bool, error) {
	hash := auth.HashToken(raw)

	// Single query: join operator_sessions → operators so we get is_admin
	// without a second round-trip.
	type result struct {
		OperatorID uint64
		IsAdmin    bool
	}
	var row result
	err := d.db.WithContext(ctx).
		Model(&model.OperatorSession{}).
		Select("operator_sessions.operator_id, operators.is_admin").
		Joins("JOIN operators ON operators.id = operator_sessions.operator_id AND operators.deleted_at IS NULL").
		Where("operator_sessions.token_hash = ? AND operator_sessions.revoked_at IS NULL AND operator_sessions.expires_at > ?", hash, time.Now().UTC()).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, false, fmt.Errorf("dao: session: invalid or expired token: %w", err)
		}
		return 0, false, fmt.Errorf("dao: session lookup: %w", err)
	}
	return row.OperatorID, row.IsAdmin, nil
}

// Revoke marks the session as revoked. Idempotent.
func (d *SessionDAO) Revoke(ctx context.Context, sessionID uint64) error {
	now := time.Now().UTC()
	res := d.db.WithContext(ctx).Model(&model.OperatorSession{}).
		Where("id = ? AND revoked_at IS NULL", sessionID).
		Update("revoked_at", &now)
	if res.Error != nil {
		return fmt.Errorf("dao: revoke session: %w", res.Error)
	}
	return nil
}

// RevokeByToken is a convenience for self-logout — find the session by token
// hash and revoke it.
func (d *SessionDAO) RevokeByToken(ctx context.Context, rawToken string) error {
	hash := auth.HashToken(rawToken)
	now := time.Now().UTC()
	res := d.db.WithContext(ctx).Model(&model.OperatorSession{}).
		Where("token_hash = ? AND revoked_at IS NULL", hash).
		Update("revoked_at", &now)
	if res.Error != nil {
		return fmt.Errorf("dao: revoke by token: %w", res.Error)
	}
	return nil
}
