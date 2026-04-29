package model_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/model"
)

func TestProjectCreate(t *testing.T) {
	db := openMemDB(t)

	p := model.Project{
		Slug:           "clinic-network",
		Name:           "Clinic Network",
		DefaultMode:    model.SiteModeEndpoint,
		Backend:        model.BackendRathole,
		RelayPortRange: "20000-20999",
		Status:         model.ProjectStatusActive,
	}
	require.NoError(t, db.Create(&p).Error)
	require.NotZero(t, p.ID)
}

func TestProjectSlugUnique(t *testing.T) {
	db := openMemDB(t)
	require.NoError(t, db.Create(&model.Project{Slug: "x", Name: "X", RelayPortRange: "20000-20099"}).Error)
	err := db.Create(&model.Project{Slug: "x", Name: "X2", RelayPortRange: "20100-20199"}).Error
	require.Error(t, err)
}

func TestOperatorProjectAccess(t *testing.T) {
	db := openMemDB(t)

	op := model.Operator{Email: "ops@x.com", PasswordHash: "x"}
	require.NoError(t, db.Create(&op).Error)

	p := model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)

	access := model.OperatorProjectAccess{
		OperatorID: op.ID,
		ProjectID:  p.ID,
		Role:       model.ProjectRoleOperator,
	}
	require.NoError(t, db.Create(&access).Error)

	// uniqueness: same operator+project combo cannot duplicate
	dup := model.OperatorProjectAccess{OperatorID: op.ID, ProjectID: p.ID, Role: model.ProjectRoleViewer}
	require.Error(t, db.Create(&dup).Error)
}

func TestProjectSlugReusableAfterSoftDelete(t *testing.T) {
	db := openMemDB(t)

	p := model.Project{Slug: "reuse", Name: "X", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)
	require.NoError(t, db.Delete(&p).Error)

	p2 := model.Project{Slug: "reuse", Name: "X2", RelayPortRange: "20100-20199"}
	require.NoError(t, db.Create(&p2).Error)
	require.NotEqual(t, p.ID, p2.ID)
}

func TestOperatorProjectAccessReusableAfterSoftDelete(t *testing.T) {
	db := openMemDB(t)

	op := model.Operator{Email: "ops2@x.com", PasswordHash: "x"}
	require.NoError(t, db.Create(&op).Error)
	p := model.Project{Slug: "p-reuse", Name: "P", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)

	access := model.OperatorProjectAccess{
		OperatorID: op.ID,
		ProjectID:  p.ID,
		Role:       model.ProjectRoleOperator,
	}
	require.NoError(t, db.Create(&access).Error)
	require.NoError(t, db.Delete(&access).Error)

	// Same (operator_id, project_id) pair should be re-creatable after soft-delete.
	access2 := model.OperatorProjectAccess{
		OperatorID: op.ID,
		ProjectID:  p.ID,
		Role:       model.ProjectRoleViewer,
	}
	require.NoError(t, db.Create(&access2).Error)
	require.NotEqual(t, access.ID, access2.ID)
}
