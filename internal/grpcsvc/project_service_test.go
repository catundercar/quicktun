package grpcsvc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/audit"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/grpcsvc"
	"github.com/tulip/quicktun/internal/model"
)

func newProjectService(t *testing.T, db *gorm.DB) *grpcsvc.ProjectService {
	return grpcsvc.NewProjectService(dao.NewProjectDAO(db), audit.NewWriter(db))
}

// Authenticated context for tests: an admin operator.
func adminCtx(t *testing.T, db *gorm.DB) context.Context {
	t.Helper()
	op := seedOperator(t, db, "admin@x.com", "p", true)
	return auth.WithOperator(context.Background(), op)
}

func TestGetProjectByName(t *testing.T) {
	db := openTestDB(t)
	dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "clinic-network", Name: "Clinic", RelayPortRange: "20000-20099",
	})
	svc := newProjectService(t, db)

	resp, err := svc.GetProject(adminCtx(t, db), &quicktunv1.GetProjectRequest{
		Name: "projects/clinic-network",
	})
	require.NoError(t, err)
	require.Equal(t, "projects/clinic-network", resp.Name)
	require.Equal(t, "Clinic", resp.DisplayName)
}

func TestGetProjectNotFound(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)

	_, err := svc.GetProject(adminCtx(t, db), &quicktunv1.GetProjectRequest{
		Name: "projects/missing",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestGetProjectInvalidName(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)

	_, err := svc.GetProject(adminCtx(t, db), &quicktunv1.GetProjectRequest{
		Name: "not-a-resource-name",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetProjectRequiresAuth(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)

	_, err := svc.GetProject(context.Background(), &quicktunv1.GetProjectRequest{
		Name: "projects/clinic-network",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())
}

func TestListProjectsAdminSeesAll(t *testing.T) {
	db := openTestDB(t)
	store := dao.NewProjectDAO(db)
	store.Create(context.Background(), &model.Project{Slug: "aa", Name: "A", RelayPortRange: "20000-20099"})
	store.Create(context.Background(), &model.Project{Slug: "bb", Name: "B", RelayPortRange: "20100-20199"})
	svc := newProjectService(t, db)

	resp, err := svc.ListProjects(adminCtx(t, db), &quicktunv1.ListProjectsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Projects, 2)
}

func TestListProjectsNonAdminSeesOnlyAccessible(t *testing.T) {
	db := openTestDB(t)
	store := dao.NewProjectDAO(db)
	pa, _ := store.Create(context.Background(), &model.Project{Slug: "aa", Name: "A", RelayPortRange: "20000-20099"})
	store.Create(context.Background(), &model.Project{Slug: "bb", Name: "B", RelayPortRange: "20100-20199"})
	op := seedOperator(t, db, "user@x.com", "p", false) // not admin
	require.NoError(t, db.Create(&model.OperatorProjectAccess{
		OperatorID: op.ID, ProjectID: pa.ID, Role: model.ProjectRoleOperator,
	}).Error)
	svc := newProjectService(t, db)

	ctx := auth.WithOperator(context.Background(), op)
	resp, err := svc.ListProjects(ctx, &quicktunv1.ListProjectsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Projects, 1)
	require.Equal(t, "projects/aa", resp.Projects[0].Name)
}
