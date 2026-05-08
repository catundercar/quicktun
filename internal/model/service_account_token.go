package model

import (
	"time"

	"gorm.io/gorm"
)

// ServiceAccountToken is a long-lived bearer token bound to a specific
// operator. Designed for CI/CD automation where login-derived sessions
// (with TTL) are inconvenient. ExpiresAt is nullable — when nil the token
// never expires until explicitly revoked.
//
// Only the SHA-256 hash of the token is persisted; the raw value is shown
// to the admin once at issue time and never recoverable.
type ServiceAccountToken struct {
	ID          uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	OperatorID  uint64         `gorm:"index;not null" json:"operator_id"`
	TokenHash   string         `gorm:"uniqueIndex;not null;size:128" json:"-"`
	Description string         `gorm:"not null;default:''" json:"description"`
	ExpiresAt   *time.Time     `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time     `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time     `json:"revoked_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName fixes the GORM-derived plural form to match migrations/0003.
func (ServiceAccountToken) TableName() string { return "service_account_tokens" }
