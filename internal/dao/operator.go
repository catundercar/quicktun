package dao

import (
	"context"
	"errors"
	"fmt"
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
// future. Returns the owning operator's ID, or gorm.ErrRecordNotFound (wrapped)
// if no such session exists.
//
// Mirrors SiteAgentTokenDAO.ValidateRaw. Used by the auth-proxy's operator
// session path; the gRPC interceptor uses Validate (which also returns the
// Operator row) instead.
func (d *SessionDAO) ValidateSessionRaw(ctx context.Context, raw string) (uint64, error) {
	hash := auth.HashToken(raw)
	var sess model.OperatorSession
	err := d.db.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", hash, time.Now().UTC()).
		First(&sess).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, fmt.Errorf("dao: session: invalid or expired token: %w", err)
		}
		return 0, fmt.Errorf("dao: session lookup: %w", err)
	}
	return sess.OperatorID, nil
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
