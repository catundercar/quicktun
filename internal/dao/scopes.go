package dao

import "gorm.io/gorm"

// ScopeProject restricts a query to a single project_id. Used by handlers
// after the project has been resolved from the resource name.
//
//	db.Scopes(dao.ScopeProject(p.ID)).Find(&sites)
func ScopeProject(projectID uint64) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("project_id = ?", projectID)
	}
}

// ScopeOperatorProjects restricts a query to projects the given operator has
// any access grant for. Use this on List handlers where the operator is the
// caller and the controller wants automatic multi-tenancy filtering.
//
//	db.Scopes(dao.ScopeOperatorProjects(opID)).Find(&sites)
func ScopeOperatorProjects(operatorID uint64) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		sub := db.Session(&gorm.Session{NewDB: true}).
			Table("operator_project_access").
			Select("project_id").
			Where("operator_id = ? AND deleted_at IS NULL", operatorID)
		return db.Where("project_id IN (?)", sub)
	}
}
