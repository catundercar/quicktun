package model

import "time"

// AuditLog records control-plane operator actions and notable system events.
// Append-only; updates and deletes should never happen at the application layer.
//
// ProjectID is nullable for cross-project actions. OperatorID is nullable for
// system-originated entries (e.g., scheduled cleanup).
type AuditLog struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Ts         time.Time `gorm:"index;not null" json:"ts"`
	ProjectID  *uint64   `gorm:"index" json:"project_id,omitempty"`
	OperatorID *uint64   `gorm:"index" json:"operator_id,omitempty"`
	Action     string    `gorm:"index;not null;size:64" json:"action"`
	Target     string    `gorm:"not null;size:255" json:"target"`
	SourceIP   string    `gorm:"not null;size:45" json:"source_ip"`
	ExtraJSON  string    `gorm:"type:text;not null" json:"extra_json"`
}
