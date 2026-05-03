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
