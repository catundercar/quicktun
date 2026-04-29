package model

import "time"

// SiteStatus tracks runtime liveness of a site (bastion machine).
type SiteStatus string

const (
	SiteStatusPending SiteStatus = "pending" // record exists, agent has not yet registered
	SiteStatusOnline  SiteStatus = "online"  // recent heartbeat
	SiteStatusOffline SiteStatus = "offline" // missed heartbeats past threshold
)

// Site is one bastion machine inside a project. Phase 1 only ships endpoint mode
// (services explicitly listed). Subnet mode (mesh routing peer) is Phase 2.
type Site struct {
	Base
	ProjectID    uint64     `gorm:"index;uniqueIndex:uk_project_site_name_active,where:deleted_at IS NULL;not null" json:"project_id"`
	Name         string     `gorm:"uniqueIndex:uk_project_site_name_active,where:deleted_at IS NULL;not null;size:64" json:"name"`
	LanCidrsJSON string     `gorm:"type:text;not null" json:"lan_cidrs_json"`
	Mode         SiteMode   `gorm:"not null;default:'endpoint';size:32" json:"mode"`
	Backend      Backend    `gorm:"not null;size:32" json:"backend"` // empty = inherit from project
	Status       SiteStatus `gorm:"not null;default:'pending';size:32" json:"status"`
	LastSeenAt   *time.Time `gorm:"index" json:"last_seen_at,omitempty"`
	Hostname     string     `gorm:"not null;size:255" json:"hostname"`
	OS           string     `gorm:"not null;size:32" json:"os"`
	AgentVersion string     `gorm:"not null;size:32" json:"agent_version"`

	Project    Project        `gorm:"foreignKey:ProjectID" json:"-"`
	// Services []Service `gorm:"foreignKey:SiteID" json:"-"` // TODO: re-enable in Task 5
	AgentToken SiteAgentToken `gorm:"foreignKey:SiteID" json:"-"`
}

// SiteAgentToken is the long-lived credential each site agent uses to call
// the control plane. One per site (uniqueIndex on SiteID).
type SiteAgentToken struct {
	Base
	SiteID     uint64     `gorm:"uniqueIndex:uk_site_agent_site_active,where:deleted_at IS NULL;not null" json:"site_id"`
	TokenHash  string     `gorm:"uniqueIndex:uk_site_agent_token_active,where:deleted_at IS NULL;not null;size:128" json:"-"`
	ExpiresAt  *time.Time `gorm:"index" json:"expires_at,omitempty"`
	LastUsedAt *time.Time `gorm:"index" json:"last_used_at,omitempty"`
}
