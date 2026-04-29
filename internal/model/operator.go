package model

import "time"

// Operator is a control-plane user (you / your ops team).
type Operator struct {
	Base
	Email        string `gorm:"uniqueIndex:idx_operators_email_active,where:deleted_at IS NULL;not null;size:255" json:"email"`
	PasswordHash string `gorm:"not null;size:128" json:"-"`
	IsAdmin      bool   `gorm:"not null;default:false" json:"is_admin"`

	Sessions []OperatorSession `gorm:"foreignKey:OperatorID" json:"-"`
	ProjectAccess []OperatorProjectAccess `gorm:"foreignKey:OperatorID" json:"-"`
}

// OperatorSession is one logged-in bearer token (8h default TTL).
// TokenHash stores SHA-256 of the raw token; the raw value is shown
// to the client only at issue time.
type OperatorSession struct {
	Base
	OperatorID uint64     `gorm:"index;not null" json:"operator_id"`
	TokenHash  string     `gorm:"uniqueIndex:idx_operator_sessions_token_active,where:deleted_at IS NULL;not null;size:128" json:"-"`
	IssuedAt   time.Time  `gorm:"not null" json:"issued_at"`
	ExpiresAt  time.Time  `gorm:"index;not null" json:"expires_at"`
	RevokedAt  *time.Time `gorm:"index" json:"revoked_at,omitempty"`
	UserAgent  string     `gorm:"size:255" json:"user_agent"`
	SourceIP   string     `gorm:"size:45" json:"source_ip"`
}
