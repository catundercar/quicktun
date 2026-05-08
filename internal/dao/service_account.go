package dao

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/model"
)

// SATokenPrefix is the visible prefix every service-account raw token
// carries. Helps operators distinguish SA tokens from session tokens at
// a glance and lets server-side scrubbers / log filters detect them.
const SATokenPrefix = "qt_sat_"

// ServiceAccountTokenDAO encapsulates issue/validate/list/revoke for the
// service_account_tokens table. Mirrors the shape of SiteAgentTokenDAO.
type ServiceAccountTokenDAO struct{ db *gorm.DB }

// NewServiceAccountTokenDAO constructs a ServiceAccountTokenDAO.
func NewServiceAccountTokenDAO(db *gorm.DB) *ServiceAccountTokenDAO {
	return &ServiceAccountTokenDAO{db: db}
}

// Issue creates a new SA token bound to operatorID. Returns the persisted
// record AND the raw token (shown to the admin once — only the hash is
// stored). ttl <= 0 means "no expiry".
func (d *ServiceAccountTokenDAO) Issue(ctx context.Context, operatorID uint64, description string, ttl time.Duration) (*model.ServiceAccountToken, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, "", fmt.Errorf("dao: sa token random: %w", err)
	}
	raw := SATokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	hash := auth.HashToken(raw)

	var exp *time.Time
	if ttl > 0 {
		t := time.Now().UTC().Add(ttl)
		exp = &t
	}
	rec := &model.ServiceAccountToken{
		OperatorID:  operatorID,
		TokenHash:   hash,
		Description: description,
		ExpiresAt:   exp,
	}
	if err := d.db.WithContext(ctx).Create(rec).Error; err != nil {
		return nil, "", fmt.Errorf("dao: issue sa token: %w", err)
	}
	return rec, raw, nil
}

// ValidateRaw hashes raw and looks up an active SA token. Returns the
// owning operator id, or wraps gorm.ErrRecordNotFound for invalid /
// expired / revoked tokens. Best-effort updates last_used_at on success
// (errors logged to stderr, not returned).
func (d *ServiceAccountTokenDAO) ValidateRaw(ctx context.Context, raw string) (uint64, error) {
	hash := auth.HashToken(raw)
	now := time.Now().UTC()

	var rec model.ServiceAccountToken
	err := d.db.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL", hash).
		Where("expires_at IS NULL OR expires_at > ?", now).
		First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, fmt.Errorf("dao: sa token: invalid or expired: %w", err)
		}
		return 0, fmt.Errorf("dao: sa token lookup: %w", err)
	}

	if uerr := d.db.WithContext(ctx).Model(&model.ServiceAccountToken{}).
		Where("id = ?", rec.ID).Update("last_used_at", &now).Error; uerr != nil {
		// Match SiteAgentTokenDAO's posture: never fail validation on a
		// best-effort metadata update; surface the error to the operator
		// log only.
		fmt.Fprintf(os.Stderr, "dao: sa token last_used_at update: %v\n", uerr)
	}
	return rec.OperatorID, nil
}

// ListByOperator returns all live SA tokens for the given operator,
// ordered by id ASC. Soft-deleted rows are excluded by GORM automatically.
func (d *ServiceAccountTokenDAO) ListByOperator(ctx context.Context, operatorID uint64) ([]model.ServiceAccountToken, error) {
	var rows []model.ServiceAccountToken
	err := d.db.WithContext(ctx).
		Where("operator_id = ?", operatorID).
		Order("id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("dao: list sa tokens: %w", err)
	}
	return rows, nil
}

// FindByID returns the SA token at id (live row only). Used by Revoke to
// resolve operator id for audit. Returns wrapped gorm.ErrRecordNotFound
// when missing.
func (d *ServiceAccountTokenDAO) FindByID(ctx context.Context, id uint64) (*model.ServiceAccountToken, error) {
	var rec model.ServiceAccountToken
	if err := d.db.WithContext(ctx).First(&rec, id).Error; err != nil {
		return nil, fmt.Errorf("dao: find sa token: %w", err)
	}
	return &rec, nil
}

// Revoke marks the SA token at id as revoked. Idempotent: if already
// revoked or missing, returns nil.
func (d *ServiceAccountTokenDAO) Revoke(ctx context.Context, id uint64) error {
	now := time.Now().UTC()
	res := d.db.WithContext(ctx).Model(&model.ServiceAccountToken{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("revoked_at", &now)
	if res.Error != nil {
		return fmt.Errorf("dao: revoke sa token: %w", res.Error)
	}
	return nil
}

// SATokenIDFromString parses a string id (as exposed via API) to uint64.
// Helper kept here to avoid scattering strconv calls across handlers.
func SATokenIDFromString(s string) (uint64, error) {
	return strconv.ParseUint(s, 10, 64)
}
