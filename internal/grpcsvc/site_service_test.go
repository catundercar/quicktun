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

func newSiteService(t *testing.T, db *gorm.DB) *grpcsvc.SiteService {
	return grpcsvc.NewSiteService(
		dao.NewProjectDAO(db),
		dao.NewSiteDAO(db),
		dao.NewSiteAgentTokenDAO(db),
		audit.NewWriter(db),
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
