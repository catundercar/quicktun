package grpcsvc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"gorm.io/gorm"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/audit"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/grpcsvc"
	"github.com/tulip/quicktun/internal/model"
)

// recordingRelay is a RelayManager stub that records every method call.
// Reused across project/site/service tests via the shared grpcsvc_test package.
type recordingRelay struct {
	added     []uint64
	removed   []uint64
	refreshed []uint64
}

func (r *recordingRelay) AddProject(_ context.Context, id uint64) error {
	r.added = append(r.added, id)
	return nil
}
func (r *recordingRelay) RemoveProject(_ context.Context, id uint64) error {
	r.removed = append(r.removed, id)
	return nil
}
func (r *recordingRelay) Refresh(_ context.Context, id uint64) error {
	r.refreshed = append(r.refreshed, id)
	return nil
}

func newProjectService(t *testing.T, db *gorm.DB) *grpcsvc.ProjectService {
	return grpcsvc.NewProjectService(dao.NewProjectDAO(db), audit.NewWriter(db), zap.NewNop(), nil)
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

	_, err := svc.CreateProject(ctx, &quicktunv1.CreateProjectRequest{
		ProjectId: "dup-slug",
		Project: &quicktunv1.Project{
			DisplayName: "First", RelayPortRange: "20000-20099",
		},
	})
	require.NoError(t, err)

	// Use a non-overlapping range so the overlap check passes and we reach
	// the slug uniqueness constraint in the DB.
	_, err = svc.CreateProject(ctx, &quicktunv1.CreateProjectRequest{
		ProjectId: "dup-slug",
		Project: &quicktunv1.Project{
			DisplayName: "Second", RelayPortRange: "30000-30099",
		},
	})
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

func TestUpdateProjectDisplayName(t *testing.T) {
	db := openTestDB(t)
	dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "u-p", Name: "Old", RelayPortRange: "20000-20099",
	})
	svc := newProjectService(t, db)

	resp, err := svc.UpdateProject(adminCtx(t, db), &quicktunv1.UpdateProjectRequest{
		Project: &quicktunv1.Project{
			Name:        "projects/u-p",
			DisplayName: "New",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"display_name"}},
	})
	require.NoError(t, err)
	require.Equal(t, "New", resp.DisplayName)
	require.Equal(t, "20000-20099", resp.RelayPortRange) // unchanged
}

func TestUpdateProjectStatus(t *testing.T) {
	db := openTestDB(t)
	dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "u-s", Name: "S", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive,
	})
	svc := newProjectService(t, db)

	resp, err := svc.UpdateProject(adminCtx(t, db), &quicktunv1.UpdateProjectRequest{
		Project: &quicktunv1.Project{
			Name:   "projects/u-s",
			Status: quicktunv1.ProjectStatus_PROJECT_STATUS_DISABLED,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
	})
	require.NoError(t, err)
	require.Equal(t, quicktunv1.ProjectStatus_PROJECT_STATUS_DISABLED, resp.Status)
}

func TestUpdateProjectRequiresMask(t *testing.T) {
	db := openTestDB(t)
	dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "u-m", Name: "M", RelayPortRange: "20000-20099",
	})
	svc := newProjectService(t, db)

	_, err := svc.UpdateProject(adminCtx(t, db), &quicktunv1.UpdateProjectRequest{
		Project: &quicktunv1.Project{Name: "projects/u-m", DisplayName: "X"},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUpdateProjectRequiresAdmin(t *testing.T) {
	db := openTestDB(t)
	dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "u-a", Name: "A", RelayPortRange: "20000-20099",
	})
	svc := newProjectService(t, db)
	op := seedOperator(t, db, "user@x.com", "p", false)
	ctx := auth.WithOperator(context.Background(), op)

	_, err := svc.UpdateProject(ctx, &quicktunv1.UpdateProjectRequest{
		Project:    &quicktunv1.Project{Name: "projects/u-a", DisplayName: "X"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"display_name"}},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestDeleteProjectSuccess(t *testing.T) {
	db := openTestDB(t)
	dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "d-p", Name: "D", RelayPortRange: "20000-20099",
	})
	svc := newProjectService(t, db)
	ctx := adminCtx(t, db)

	_, err := svc.DeleteProject(ctx, &quicktunv1.DeleteProjectRequest{
		Name: "projects/d-p",
	})
	require.NoError(t, err)

	_, err = svc.GetProject(ctx, &quicktunv1.GetProjectRequest{
		Name: "projects/d-p",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestDeleteProjectRefusesIfHasSites(t *testing.T) {
	db := openTestDB(t)
	store := dao.NewProjectDAO(db)
	p, _ := store.Create(context.Background(), &model.Project{
		Slug: "d-s", Name: "S", RelayPortRange: "20000-20099",
	})
	require.NoError(t, db.Create(&model.Site{ProjectID: p.ID, Name: "child"}).Error)
	svc := newProjectService(t, db)

	_, err := svc.DeleteProject(adminCtx(t, db), &quicktunv1.DeleteProjectRequest{
		Name: "projects/d-s",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestProjectServiceCallsRelayManager(t *testing.T) {
	db := openTestDB(t)
	rec := &recordingRelay{}
	svc := grpcsvc.NewProjectService(dao.NewProjectDAO(db), audit.NewWriter(db), zap.NewNop(), rec)
	ctx := adminCtx(t, db)

	// Create -> AddProject
	created, err := svc.CreateProject(ctx, &quicktunv1.CreateProjectRequest{
		ProjectId: "rec-p",
		Project: &quicktunv1.Project{
			DisplayName:    "Rec P",
			RelayPortRange: "20000-20099",
		},
	})
	require.NoError(t, err)
	require.Len(t, rec.added, 1)
	require.Empty(t, rec.refreshed)
	require.Empty(t, rec.removed)

	// Update -> Refresh
	_, err = svc.UpdateProject(ctx, &quicktunv1.UpdateProjectRequest{
		Project: &quicktunv1.Project{
			Name: created.Name, DisplayName: "Rec P v2",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"display_name"}},
	})
	require.NoError(t, err)
	require.Len(t, rec.refreshed, 1)

	// Delete -> RemoveProject
	_, err = svc.DeleteProject(ctx, &quicktunv1.DeleteProjectRequest{Name: created.Name})
	require.NoError(t, err)
	require.Len(t, rec.removed, 1)
}

func TestCreateProjectRejectsOverlappingRange(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)
	ctx := adminCtx(t, db)

	_, err := svc.CreateProject(ctx, &quicktunv1.CreateProjectRequest{
		ProjectId: "p1",
		Project: &quicktunv1.Project{
			DisplayName: "P1", RelayPortRange: "20000-20099",
		},
	})
	require.NoError(t, err)

	_, err = svc.CreateProject(ctx, &quicktunv1.CreateProjectRequest{
		ProjectId: "p2",
		Project: &quicktunv1.Project{
			DisplayName: "P2", RelayPortRange: "20050-20149",
		},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestDeleteProjectForceWithSites(t *testing.T) {
	db := openTestDB(t)
	store := dao.NewProjectDAO(db)
	p, _ := store.Create(context.Background(), &model.Project{
		Slug: "d-f", Name: "F", RelayPortRange: "20000-20099",
	})
	require.NoError(t, db.Create(&model.Site{ProjectID: p.ID, Name: "child"}).Error)
	svc := newProjectService(t, db)

	_, err := svc.DeleteProject(adminCtx(t, db), &quicktunv1.DeleteProjectRequest{
		Name:  "projects/d-f",
		Force: true,
	})
	require.NoError(t, err)
}

func TestUpdateProjectDisabledTearsDownSupervisor(t *testing.T) {
	db := openTestDB(t)
	rec := &recordingRelay{}
	svc := grpcsvc.NewProjectService(dao.NewProjectDAO(db), audit.NewWriter(db), zap.NewNop(), rec)
	ctx := adminCtx(t, db)

	created, err := svc.CreateProject(ctx, &quicktunv1.CreateProjectRequest{
		ProjectId: "p1",
		Project: &quicktunv1.Project{
			DisplayName: "P1", RelayPortRange: "20000-20099",
		},
	})
	require.NoError(t, err)
	require.Len(t, rec.added, 1)

	// Disable — must call RemoveProject, not Refresh.
	_, err = svc.UpdateProject(ctx, &quicktunv1.UpdateProjectRequest{
		Project: &quicktunv1.Project{
			Name:   created.Name,
			Status: quicktunv1.ProjectStatus_PROJECT_STATUS_DISABLED,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
	})
	require.NoError(t, err)
	require.Len(t, rec.removed, 1, "disabled project must trigger RemoveProject")
	require.Empty(t, rec.refreshed, "disabled project must NOT trigger Refresh")

	// Re-enable — must call Refresh, not RemoveProject again.
	_, err = svc.UpdateProject(ctx, &quicktunv1.UpdateProjectRequest{
		Project: &quicktunv1.Project{
			Name:   created.Name,
			Status: quicktunv1.ProjectStatus_PROJECT_STATUS_ACTIVE,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rec.refreshed), 1, "re-enabled project must trigger Refresh")
	require.Len(t, rec.removed, 1, "re-enabling must NOT call RemoveProject again")
}
