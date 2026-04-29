package model

// AllModels returns every concrete model type registered in this package.
// Used by AutoMigrate for tests and by introspection tooling.
//
// Production migrations are SQL files under /migrations and should remain the
// source of truth — AutoMigrate is for in-memory test fixtures only.
func AllModels() []any {
	return []any{
		&Operator{},
		&OperatorSession{},
		&Project{},
		&OperatorProjectAccess{},
		&Site{},
		&SiteAgentToken{},
		&Service{},
		&AuditLog{},
	}
}
