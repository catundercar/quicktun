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

func newServiceService(t *testing.T, db *gorm.DB) *grpcsvc.ServiceService {
	return grpcsvc.NewServiceService(
		dao.NewProjectDAO(db),
		dao.NewSiteDAO(db),
		dao.NewServiceDAO(db),
		audit.NewWriter(db),
	)
}

func mkSvc(t *testing.T, db *gorm.DB, projSlug, siteName, svcName string) (*model.Project, *model.Site, *model.Service) {
	t.Helper()
	p, s := mkProjAndSite(t, db, projSlug, siteName)
	relay := uint16(20100)
	svc, err := dao.NewServiceDAO(db).Create(context.Background(), &model.Service{
		SiteID: s.ID, Name: svcName,
		TargetAddr: "127.0.0.1", TargetPort: 22,
		Proto: model.ProtoTCP, RelayPort: &relay,
	})
	require.NoError(t, err)
	return p, s, svc
}

func TestGetServiceByName(t *testing.T) {
	db := openTestDB(t)
	mkSvc(t, db, "p1", "bastion", "ssh")
	svc := newServiceService(t, db)

	resp, err := svc.GetService(adminCtx(t, db), &quicktunv1.GetServiceRequest{
		Name: "projects/p1/sites/bastion/services/ssh",
	})
	require.NoError(t, err)
	require.Equal(t, "projects/p1/sites/bastion/services/ssh", resp.Name)
	require.Equal(t, uint32(20100), resp.RelayPort)
}

func TestGetServiceNotFound(t *testing.T) {
	db := openTestDB(t)
	mkProjAndSite(t, db, "p1", "bastion")
	svc := newServiceService(t, db)

	_, err := svc.GetService(adminCtx(t, db), &quicktunv1.GetServiceRequest{
		Name: "projects/p1/sites/bastion/services/missing",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestGetServiceInvalidName(t *testing.T) {
	db := openTestDB(t)
	svc := newServiceService(t, db)
	_, err := svc.GetService(adminCtx(t, db), &quicktunv1.GetServiceRequest{Name: "garbage"})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetServiceRequiresAuth(t *testing.T) {
	db := openTestDB(t)
	svc := newServiceService(t, db)
	_, err := svc.GetService(context.Background(), &quicktunv1.GetServiceRequest{
		Name: "projects/p1/sites/b/services/ssh",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())
}

func TestListServicesAdminSeesAll(t *testing.T) {
	db := openTestDB(t)
	p, s := mkProjAndSite(t, db, "p1", "bastion")
	sd := dao.NewServiceDAO(db)
	rp1 := uint16(20100)
	rp2 := uint16(20101)
	sd.Create(context.Background(), &model.Service{SiteID: s.ID, Name: "ssh", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP, RelayPort: &rp1})
	sd.Create(context.Background(), &model.Service{SiteID: s.ID, Name: "rdp", TargetAddr: "192.168.10.50", TargetPort: 3389, Proto: model.ProtoTCP, RelayPort: &rp2})
	_ = p
	svc := newServiceService(t, db)

	resp, err := svc.ListServices(adminCtx(t, db), &quicktunv1.ListServicesRequest{
		Parent: "projects/p1/sites/bastion",
	})
	require.NoError(t, err)
	require.Len(t, resp.Services, 2)
}

func TestListServicesNonAdminWithoutAccessDenied(t *testing.T) {
	db := openTestDB(t)
	mkProjAndSite(t, db, "p1", "bastion")
	op := seedOperator(t, db, "noaccess@x.com", "p", false)
	svc := newServiceService(t, db)

	ctx := auth.WithOperator(context.Background(), op)
	_, err := svc.ListServices(ctx, &quicktunv1.ListServicesRequest{
		Parent: "projects/p1/sites/bastion",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}
