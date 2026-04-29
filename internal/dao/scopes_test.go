package dao_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
)

func openWithModels(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:scopes_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(model.AllModels()...))
	t.Cleanup(func() {
		s, _ := db.DB()
		s.Close()
	})
	return db
}

func TestScopeProject(t *testing.T) {
	db := openWithModels(t)

	p1 := model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099"}
	p2 := model.Project{Slug: "p2", Name: "P2", RelayPortRange: "20100-20199"}
	require.NoError(t, db.Create(&p1).Error)
	require.NoError(t, db.Create(&p2).Error)
	require.NoError(t, db.Create(&model.Site{ProjectID: p1.ID, Name: "a"}).Error)
	require.NoError(t, db.Create(&model.Site{ProjectID: p1.ID, Name: "b"}).Error)
	require.NoError(t, db.Create(&model.Site{ProjectID: p2.ID, Name: "c"}).Error)

	var sites []model.Site
	require.NoError(t, db.Scopes(dao.ScopeProject(p1.ID)).Find(&sites).Error)
	require.Len(t, sites, 2)
}

func TestScopeOperatorProjects(t *testing.T) {
	db := openWithModels(t)

	op := model.Operator{Email: "o@x.com", PasswordHash: "x"}
	require.NoError(t, db.Create(&op).Error)
	p1 := model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099"}
	p2 := model.Project{Slug: "p2", Name: "P2", RelayPortRange: "20100-20199"}
	p3 := model.Project{Slug: "p3", Name: "P3", RelayPortRange: "20200-20299"}
	require.NoError(t, db.Create(&p1).Error)
	require.NoError(t, db.Create(&p2).Error)
	require.NoError(t, db.Create(&p3).Error)
	require.NoError(t, db.Create(&model.OperatorProjectAccess{OperatorID: op.ID, ProjectID: p1.ID, Role: model.ProjectRoleOperator}).Error)
	require.NoError(t, db.Create(&model.OperatorProjectAccess{OperatorID: op.ID, ProjectID: p2.ID, Role: model.ProjectRoleViewer}).Error)
	// op has no access to p3
	require.NoError(t, db.Create(&model.Site{ProjectID: p1.ID, Name: "a"}).Error)
	require.NoError(t, db.Create(&model.Site{ProjectID: p2.ID, Name: "b"}).Error)
	require.NoError(t, db.Create(&model.Site{ProjectID: p3.ID, Name: "c"}).Error)

	var sites []model.Site
	require.NoError(t, db.Scopes(dao.ScopeOperatorProjects(op.ID)).Find(&sites).Error)
	require.Len(t, sites, 2)
}
