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

func TestCreateProjectSuccess(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)

	resp, err := svc.CreateProject(adminCtx(t, db), &quicktunv1.CreateProjectRequest{
		ProjectId: "clinic-network",
		Project: &quicktunv1.Project{
			DisplayName:    "Clinic Network",
			RelayPortRange: "20000-20999",
			DefaultMode:    quicktunv1.SiteMode_SITE_MODE_ENDPOINT,
			Backend:        quicktunv1.Backend_BACKEND_RATHOLE,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "projects/clinic-network", resp.Name)
	require.Equal(t, "clinic-network", resp.ProjectId)
	require.Equal(t, "Clinic Network", resp.DisplayName)
	require.Equal(t, quicktunv1.ProjectStatus_PROJECT_STATUS_ACTIVE, resp.Status)

	// Verify audit log entry written.
	var audits []model.AuditLog
	require.NoError(t, db.Find(&audits).Error)
	require.Len(t, audits, 1)
	require.Equal(t, "project.create", audits[0].Action)
	require.Equal(t, "projects/clinic-network", audits[0].Target)
}

func TestCreateProjectRejectsBadSlug(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)

	_, err := svc.CreateProject(adminCtx(t, db), &quicktunv1.CreateProjectRequest{
		ProjectId: "Bad Slug!",
		Project:   &quicktunv1.Project{DisplayName: "X", RelayPortRange: "20000-20099"},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestCreateProjectRejectsMissingFields(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)

	_, err := svc.CreateProject(adminCtx(t, db), &quicktunv1.CreateProjectRequest{
		ProjectId: "x-y-z",
		Project:   &quicktunv1.Project{}, // missing display_name and relay_port_range
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestCreateProjectRejectsDuplicate(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)
	ctx := adminCtx(t, db)

	req := &quicktunv1.CreateProjectRequest{
		ProjectId: "dup-slug",
		Project: &quicktunv1.Project{
			DisplayName: "First", RelayPortRange: "20000-20099",
		},
	}
	_, err := svc.CreateProject(ctx, req)
	require.NoError(t, err)

	_, err = svc.CreateProject(ctx, req)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.AlreadyExists, st.Code())
}

func TestCreateProjectRequiresAdmin(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)
	op := seedOperator(t, db, "non-admin@x.com", "p", false)
	ctx := auth.WithOperator(context.Background(), op)

	_, err := svc.CreateProject(ctx, &quicktunv1.CreateProjectRequest{
		ProjectId: "any-slug",
		Project:   &quicktunv1.Project{DisplayName: "X", RelayPortRange: "20000-20099"},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}
