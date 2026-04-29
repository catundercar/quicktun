package model

// ProjectStatus enumerates lifecycle states of a Project.
type ProjectStatus string

const (
	ProjectStatusActive   ProjectStatus = "active"
	ProjectStatusDisabled ProjectStatus = "disabled"
)

// Backend selects which relay implementation drives this project.
type Backend string

const (
	BackendRathole Backend = "rathole"
	BackendNetbird Backend = "netbird" // Phase 2
)

// SiteMode controls how a Site exposes services. Phase 1 only ships endpoint.
type SiteMode string

const (
	SiteModeEndpoint SiteMode = "endpoint"
	SiteModeSubnet   SiteMode = "subnet" // Phase 2
)

// ProjectRole grants a level of access to an operator within a project.
type ProjectRole string

const (
	ProjectRoleOwner    ProjectRole = "owner"
	ProjectRoleOperator ProjectRole = "operator"
	ProjectRoleViewer   ProjectRole = "viewer"
)

// Project is a tenancy boundary inside the single-instance control plane.
// Each project gets its own rathole-server process and its own port range.
type Project struct {
	Base
	Slug           string        `gorm:"uniqueIndex:idx_projects_slug_active,where:deleted_at IS NULL;not null;size:64" json:"slug"`
	Name           string        `gorm:"not null;size:255" json:"name"`
	DefaultMode    SiteMode      `gorm:"not null;default:'endpoint';size:32" json:"default_mode"`
	Backend        Backend       `gorm:"not null;default:'rathole';size:32" json:"backend"`
	RelayPortRange string        `gorm:"not null;size:32" json:"relay_port_range"`
	Status         ProjectStatus `gorm:"not null;default:'active';size:32" json:"status"`

	// Sites []Site `gorm:"foreignKey:ProjectID" json:"-"` // TODO: re-enable in Task 4
	AccessGrants []OperatorProjectAccess `gorm:"foreignKey:ProjectID" json:"-"`
}

// OperatorProjectAccess is the many-to-many between operators and projects.
// Composite uniqueness on (operator_id, project_id) is enforced at DB level.
type OperatorProjectAccess struct {
	Base
	OperatorID uint64      `gorm:"uniqueIndex:uk_operator_project_active,where:deleted_at IS NULL;not null" json:"operator_id"`
	ProjectID  uint64      `gorm:"uniqueIndex:uk_operator_project_active,where:deleted_at IS NULL;not null" json:"project_id"`
	Role       ProjectRole `gorm:"not null;default:'operator';size:32" json:"role"`
}

// TableName overrides GORM's default pluralization. The SQL migration uses
// the singular form for this join table.
func (OperatorProjectAccess) TableName() string { return "operator_project_access" }
