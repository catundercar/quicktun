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

func newSiteService(t *testing.T, db *gorm.DB) *grpcsvc.SiteService {
	return grpcsvc.NewSiteService(
		dao.NewProjectDAO(db),
		dao.NewSiteDAO(db),
		dao.NewSiteAgentTokenDAO(db),
		audit.NewWriter(db),
		"test-relay.example.com:443",
		zap.NewNop(),
		nil,
	)
}

func newSiteServiceWithRelay(t *testing.T, db *gorm.DB, relay grpcsvc.RelayManager) *grpcsvc.SiteService {
	return grpcsvc.NewSiteService(
		dao.NewProjectDAO(db),
		dao.NewSiteDAO(db),
		dao.NewSiteAgentTokenDAO(db),
		audit.NewWriter(db),
		"test-relay.example.com:443",
		zap.NewNop(),
		relay,
	)
}

func mkProjAndSite(t *testing.T, db *gorm.DB, projSlug, siteName string) (*model.Project, *model.Site) {
	t.Helper()
	p, err := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: projSlug, Name: projSlug, RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)
	s, err := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{
		ProjectID: p.ID, Name: siteName,
	})
	require.NoError(t, err)
	return p, s
}

func TestGetSiteByName(t *testing.T) {
	db := openTestDB(t)
	mkProjAndSite(t, db, "p1", "bastion")
	svc := newSiteService(t, db)
	ctx := adminCtx(t, db)

	resp, err := svc.GetSite(ctx, &quicktunv1.GetSiteRequest{
		Name: "projects/p1/sites/bastion",
	})
	require.NoError(t, err)
	require.Equal(t, "projects/p1/sites/bastion", resp.Name)
}

func TestGetSiteNotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p", Name: "p", RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)
	svc := newSiteService(t, db)

	_, err = svc.GetSite(adminCtx(t, db), &quicktunv1.GetSiteRequest{
		Name: "projects/p/sites/missing",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestGetSiteProjectMissing(t *testing.T) {
	db := openTestDB(t)
	svc := newSiteService(t, db)

	_, err := svc.GetSite(adminCtx(t, db), &quicktunv1.GetSiteRequest{
		Name: "projects/missing/sites/x",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestGetSiteInvalidName(t *testing.T) {
	db := openTestDB(t)
	svc := newSiteService(t, db)

	_, err := svc.GetSite(adminCtx(t, db), &quicktunv1.GetSiteRequest{
		Name: "garbage",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetSiteRequiresAuth(t *testing.T) {
	db := openTestDB(t)
	svc := newSiteService(t, db)
	_, err := svc.GetSite(context.Background(), &quicktunv1.GetSiteRequest{
		Name: "projects/p/sites/x",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())
}

func TestListSitesAdminSeesAll(t *testing.T) {
	db := openTestDB(t)
	p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	sd := dao.NewSiteDAO(db)
	sd.Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "a"})
	sd.Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "b"})
	svc := newSiteService(t, db)

	resp, err := svc.ListSites(adminCtx(t, db), &quicktunv1.ListSitesRequest{Parent: "projects/p1"})
	require.NoError(t, err)
	require.Len(t, resp.Sites, 2)
}

func TestListSitesNonAdminWithoutAccessDenied(t *testing.T) {
	db := openTestDB(t)
	p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "a"})
	op := seedOperator(t, db, "noaccess@x.com", "p", false)
	svc := newSiteService(t, db)

	ctx := auth.WithOperator(context.Background(), op)
	_, err := svc.ListSites(ctx, &quicktunv1.ListSitesRequest{Parent: "projects/p1"})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestListSitesNonAdminWithAccessSucceeds(t *testing.T) {
	db := openTestDB(t)
	p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "a"})
	op := seedOperator(t, db, "access@x.com", "p", false)
	require.NoError(t, db.Create(&model.OperatorProjectAccess{
		OperatorID: op.ID, ProjectID: p.ID, Role: model.ProjectRoleViewer,
	}).Error)
	svc := newSiteService(t, db)

	ctx := auth.WithOperator(context.Background(), op)
	resp, err := svc.ListSites(ctx, &quicktunv1.ListSitesRequest{Parent: "projects/p1"})
	require.NoError(t, err)
	require.Len(t, resp.Sites, 1)
}

func TestCreateSiteSuccess(t *testing.T) {
	db := openTestDB(t)
	dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	svc := newSiteService(t, db)
	ctx := adminCtx(t, db)

	resp, err := svc.CreateSite(ctx, &quicktunv1.CreateSiteRequest{
		Parent: "projects/p1",
		SiteId: "bastion-1",
		Site:   &quicktunv1.Site{DisplayName: "Bastion 1"},
	})
	require.NoError(t, err)
	require.Equal(t, "projects/p1/sites/bastion-1", resp.Name)
	require.Equal(t, quicktunv1.SiteStatus_SITE_STATUS_PENDING, resp.Status)

	var audits []model.AuditLog
	require.NoError(t, db.Where("action = ?", "site.create").Find(&audits).Error)
	require.Len(t, audits, 1)
}

func TestCreateSiteRejectsDuplicate(t *testing.T) {
	db := openTestDB(t)
	dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	svc := newSiteService(t, db)
	ctx := adminCtx(t, db)

	req := &quicktunv1.CreateSiteRequest{
		Parent: "projects/p1", SiteId: "dup",
		Site:   &quicktunv1.Site{DisplayName: "X"},
	}
	_, err := svc.CreateSite(ctx, req)
	require.NoError(t, err)
	_, err = svc.CreateSite(ctx, req)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.AlreadyExists, st.Code())
}

func TestCreateSiteRejectsBadSlug(t *testing.T) {
	db := openTestDB(t)
	dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	svc := newSiteService(t, db)

	_, err := svc.CreateSite(adminCtx(t, db), &quicktunv1.CreateSiteRequest{
		Parent: "projects/p1", SiteId: "Bad Slug",
		Site:   &quicktunv1.Site{DisplayName: "X"},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestCreateSiteRequiresAdmin(t *testing.T) {
	db := openTestDB(t)
	p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	op := seedOperator(t, db, "u@x.com", "p", false)
	require.NoError(t, db.Create(&model.OperatorProjectAccess{
		OperatorID: op.ID, ProjectID: p.ID, Role: model.ProjectRoleOperator,
	}).Error)
	svc := newSiteService(t, db)

	ctx := auth.WithOperator(context.Background(), op)
	_, err := svc.CreateSite(ctx, &quicktunv1.CreateSiteRequest{
		Parent: "projects/p1", SiteId: "x",
		Site:   &quicktunv1.Site{DisplayName: "X"},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestDeleteSiteSuccess(t *testing.T) {
	db := openTestDB(t)
	mkProjAndSite(t, db, "p1", "d-target")
	svc := newSiteService(t, db)
	ctx := adminCtx(t, db)

	_, err := svc.DeleteSite(ctx, &quicktunv1.DeleteSiteRequest{
		Name: "projects/p1/sites/d-target",
	})
	require.NoError(t, err)

	_, err = svc.GetSite(ctx, &quicktunv1.GetSiteRequest{Name: "projects/p1/sites/d-target"})
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestDeleteSiteRefusesIfHasServices(t *testing.T) {
	db := openTestDB(t)
	_, s := mkProjAndSite(t, db, "p1", "with-svcs")
	require.NoError(t, db.Create(&model.Service{
		SiteID: s.ID, Name: "ssh", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP,
	}).Error)
	svc := newSiteService(t, db)

	_, err := svc.DeleteSite(adminCtx(t, db), &quicktunv1.DeleteSiteRequest{
		Name: "projects/p1/sites/with-svcs",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestDeleteSiteForceWithServices(t *testing.T) {
	db := openTestDB(t)
	_, s := mkProjAndSite(t, db, "p1", "force-target")
	require.NoError(t, db.Create(&model.Service{
		SiteID: s.ID, Name: "ssh", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP,
	}).Error)
	svc := newSiteService(t, db)

	_, err := svc.DeleteSite(adminCtx(t, db), &quicktunv1.DeleteSiteRequest{
		Name: "projects/p1/sites/force-target", Force: true,
	})
	require.NoError(t, err)
}

func TestUpdateSiteDisplayName(t *testing.T) {
	db := openTestDB(t)
	mkProjAndSite(t, db, "p1", "u-target")
	svc := newSiteService(t, db)

	resp, err := svc.UpdateSite(adminCtx(t, db), &quicktunv1.UpdateSiteRequest{
		Site: &quicktunv1.Site{
			Name:        "projects/p1/sites/u-target",
			DisplayName: "New Name",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"display_name"}},
	})
	require.NoError(t, err)
	require.Equal(t, "New Name", resp.DisplayName)
}

func TestUpdateSiteLanCidrs(t *testing.T) {
	db := openTestDB(t)
	mkProjAndSite(t, db, "p1", "lan-target")
	svc := newSiteService(t, db)

	resp, err := svc.UpdateSite(adminCtx(t, db), &quicktunv1.UpdateSiteRequest{
		Site: &quicktunv1.Site{
			Name:     "projects/p1/sites/lan-target",
			LanCidrs: []string{"192.168.10.0/24", "10.0.0.0/8"},
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"lan_cidrs"}},
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"192.168.10.0/24", "10.0.0.0/8"}, resp.LanCidrs)
}

func TestUpdateSiteRequiresMask(t *testing.T) {
	db := openTestDB(t)
	mkProjAndSite(t, db, "p1", "m-target")
	svc := newSiteService(t, db)

	_, err := svc.UpdateSite(adminCtx(t, db), &quicktunv1.UpdateSiteRequest{
		Site: &quicktunv1.Site{Name: "projects/p1/sites/m-target", DisplayName: "X"},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestRotateSiteAgentToken(t *testing.T) {
	db := openTestDB(t)
	mkProjAndSite(t, db, "p1", "rotate-target")
	svc := newSiteService(t, db)
	ctx := adminCtx(t, db)

	resp1, err := svc.RotateSiteAgentToken(ctx, &quicktunv1.RotateSiteAgentTokenRequest{
		Name: "projects/p1/sites/rotate-target",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp1.Token)

	resp2, err := svc.RotateSiteAgentToken(ctx, &quicktunv1.RotateSiteAgentTokenRequest{
		Name: "projects/p1/sites/rotate-target",
	})
	require.NoError(t, err)
	require.NotEqual(t, resp1.Token, resp2.Token)

	tokens := dao.NewSiteAgentTokenDAO(db)
	_, err = tokens.ValidateRaw(context.Background(), resp1.Token)
	require.Error(t, err)
	_, err = tokens.ValidateRaw(context.Background(), resp2.Token)
	require.NoError(t, err)
}

func TestRotateSiteAgentTokenRequiresAdmin(t *testing.T) {
	db := openTestDB(t)
	p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "x"})
	op := seedOperator(t, db, "u@x.com", "p", false)
	require.NoError(t, db.Create(&model.OperatorProjectAccess{
		OperatorID: op.ID, ProjectID: p.ID, Role: model.ProjectRoleOperator,
	}).Error)
	svc := newSiteService(t, db)

	ctx := auth.WithOperator(context.Background(), op)
	_, err := svc.RotateSiteAgentToken(ctx, &quicktunv1.RotateSiteAgentTokenRequest{
		Name: "projects/p1/sites/x",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestGetSiteInstallCommandLinux(t *testing.T) {
	db := openTestDB(t)
	mkProjAndSite(t, db, "p1", "install-target")
	svc := newSiteService(t, db)

	resp, err := svc.GetSiteInstallCommand(adminCtx(t, db), &quicktunv1.GetSiteInstallCommandRequest{
		Name: "projects/p1/sites/install-target",
		Os:   "linux",
	})
	require.NoError(t, err)
	require.Contains(t, resp.Command, "curl")
	require.Contains(t, resp.Command, "QT_TOKEN=")
	require.NotEmpty(t, resp.Token)
	require.Contains(t, resp.Command, "bash")
}

func TestGetSiteInstallCommandWindows(t *testing.T) {
	db := openTestDB(t)
	mkProjAndSite(t, db, "p1", "win-target")
	svc := newSiteService(t, db)

	resp, err := svc.GetSiteInstallCommand(adminCtx(t, db), &quicktunv1.GetSiteInstallCommandRequest{
		Name: "projects/p1/sites/win-target",
		Os:   "windows",
	})
	require.NoError(t, err)
	require.Contains(t, resp.Command, "iwr")
	require.Contains(t, resp.Command, "QT_TOKEN")
}

func TestGetSiteInstallCommandRequiresAdmin(t *testing.T) {
	db := openTestDB(t)
	p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "x"})
	op := seedOperator(t, db, "u@x.com", "p", false)
	require.NoError(t, db.Create(&model.OperatorProjectAccess{
		OperatorID: op.ID, ProjectID: p.ID, Role: model.ProjectRoleViewer,
	}).Error)
	svc := newSiteService(t, db)

	ctx := auth.WithOperator(context.Background(), op)
	_, err := svc.GetSiteInstallCommand(ctx, &quicktunv1.GetSiteInstallCommandRequest{
		Name: "projects/p1/sites/x",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestUpdateSiteRejectsUnspecifiedMode(t *testing.T) {
	db := openTestDB(t)
	mkProjAndSite(t, db, "p1", "u-mode")
	svc := newSiteService(t, db)

	_, err := svc.UpdateSite(adminCtx(t, db), &quicktunv1.UpdateSiteRequest{
		Site: &quicktunv1.Site{
			Name: "projects/p1/sites/u-mode",
			Mode: quicktunv1.SiteMode_SITE_MODE_UNSPECIFIED,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"mode"}},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestSiteServiceCallsRelayRefreshOnMutations(t *testing.T) {
	db := openTestDB(t)
	_, err := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "rp", Name: "RP", RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)

	rec := &recordingRelay{}
	svc := newSiteServiceWithRelay(t, db, rec)
	ctx := adminCtx(t, db)

	// Create -> Refresh
	_, err = svc.CreateSite(ctx, &quicktunv1.CreateSiteRequest{
		Parent: "projects/rp", SiteId: "rs",
		Site: &quicktunv1.Site{DisplayName: "RS"},
	})
	require.NoError(t, err)

	// Update -> Refresh
	_, err = svc.UpdateSite(ctx, &quicktunv1.UpdateSiteRequest{
		Site: &quicktunv1.Site{
			Name: "projects/rp/sites/rs", DisplayName: "RS v2",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"display_name"}},
	})
	require.NoError(t, err)

	// Rotate -> Refresh
	_, err = svc.RotateSiteAgentToken(ctx, &quicktunv1.RotateSiteAgentTokenRequest{
		Name: "projects/rp/sites/rs",
	})
	require.NoError(t, err)

	// Install command -> Refresh
	_, err = svc.GetSiteInstallCommand(ctx, &quicktunv1.GetSiteInstallCommandRequest{
		Name: "projects/rp/sites/rs",
		Os:   "linux",
	})
	require.NoError(t, err)

	// Delete -> Refresh
	_, err = svc.DeleteSite(ctx, &quicktunv1.DeleteSiteRequest{
		Name: "projects/rp/sites/rs",
	})
	require.NoError(t, err)

	require.Len(t, rec.refreshed, 5,
		"create + update + rotate + install + delete should each call Refresh once")
	require.Empty(t, rec.added)
	require.Empty(t, rec.removed)
}

func TestGetSiteInstallCommandRejectsBadOS(t *testing.T) {
	db := openTestDB(t)
	mkProjAndSite(t, db, "p1", "bad-os-target")
	svc := newSiteService(t, db)

	_, err := svc.GetSiteInstallCommand(adminCtx(t, db), &quicktunv1.GetSiteInstallCommandRequest{
		Name: "projects/p1/sites/bad-os-target",
		Os:   "freebsd",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}
