package grpcsvc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
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

func TestCreateServiceSuccess(t *testing.T) {
	db := openTestDB(t)
	mkProjAndSite(t, db, "p1", "bastion")
	svc := newServiceService(t, db)

	resp, err := svc.CreateService(adminCtx(t, db), &quicktunv1.CreateServiceRequest{
		Parent:    "projects/p1/sites/bastion",
		ServiceId: "ssh",
		Service: &quicktunv1.Service{
			DisplayName: "SSH", TargetAddr: "127.0.0.1", TargetPort: 22,
			Proto: quicktunv1.Proto_PROTO_TCP,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "projects/p1/sites/bastion/services/ssh", resp.Name)
	require.NotZero(t, resp.RelayPort)
	require.GreaterOrEqual(t, resp.RelayPort, uint32(20000))
	require.LessOrEqual(t, resp.RelayPort, uint32(20099))

	var audits []model.AuditLog
	require.NoError(t, db.Where("action = ?", "service.create").Find(&audits).Error)
	require.Len(t, audits, 1)
}

func TestCreateServiceRejectsDuplicate(t *testing.T) {
	db := openTestDB(t)
	mkProjAndSite(t, db, "p1", "bastion")
	svc := newServiceService(t, db)
	ctx := adminCtx(t, db)

	req := &quicktunv1.CreateServiceRequest{
		Parent: "projects/p1/sites/bastion", ServiceId: "ssh",
		Service: &quicktunv1.Service{
			DisplayName: "SSH", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: quicktunv1.Proto_PROTO_TCP,
		},
	}
	_, err := svc.CreateService(ctx, req)
	require.NoError(t, err)
	_, err = svc.CreateService(ctx, req)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.AlreadyExists, st.Code())
}

func TestCreateServiceRejectsBadTarget(t *testing.T) {
	db := openTestDB(t)
	mkProjAndSite(t, db, "p1", "bastion")
	svc := newServiceService(t, db)

	_, err := svc.CreateService(adminCtx(t, db), &quicktunv1.CreateServiceRequest{
		Parent: "projects/p1/sites/bastion", ServiceId: "x",
		Service: &quicktunv1.Service{
			DisplayName: "X",
		},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestCreateServicePortRangeExhausted(t *testing.T) {
	db := openTestDB(t)
	p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "tiny", Name: "T", RelayPortRange: "20500-20500",
	})
	_, err := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "b"})
	require.NoError(t, err)
	svc := newServiceService(t, db)
	ctx := adminCtx(t, db)

	_, err = svc.CreateService(ctx, &quicktunv1.CreateServiceRequest{
		Parent: "projects/tiny/sites/b", ServiceId: "a",
		Service: &quicktunv1.Service{DisplayName: "A", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: quicktunv1.Proto_PROTO_TCP},
	})
	require.NoError(t, err)
	_, err = svc.CreateService(ctx, &quicktunv1.CreateServiceRequest{
		Parent: "projects/tiny/sites/b", ServiceId: "b",
		Service: &quicktunv1.Service{DisplayName: "B", TargetAddr: "127.0.0.1", TargetPort: 23, Proto: quicktunv1.Proto_PROTO_TCP},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.ResourceExhausted, st.Code())
}

func TestCreateServiceRequiresAdmin(t *testing.T) {
	db := openTestDB(t)
	p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "bastion"})
	op := seedOperator(t, db, "u@x.com", "p", false)
	require.NoError(t, db.Create(&model.OperatorProjectAccess{
		OperatorID: op.ID, ProjectID: p.ID, Role: model.ProjectRoleOperator,
	}).Error)
	svc := newServiceService(t, db)

	ctx := auth.WithOperator(context.Background(), op)
	_, err := svc.CreateService(ctx, &quicktunv1.CreateServiceRequest{
		Parent: "projects/p1/sites/bastion", ServiceId: "ssh",
		Service: &quicktunv1.Service{DisplayName: "SSH", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: quicktunv1.Proto_PROTO_TCP},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestUpdateServiceTarget(t *testing.T) {
	db := openTestDB(t)
	mkSvc(t, db, "p1", "bastion", "ssh")
	svc := newServiceService(t, db)

	resp, err := svc.UpdateService(adminCtx(t, db), &quicktunv1.UpdateServiceRequest{
		Service: &quicktunv1.Service{
			Name:       "projects/p1/sites/bastion/services/ssh",
			TargetAddr: "192.168.10.50",
			TargetPort: 2222,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"target_addr", "target_port"}},
	})
	require.NoError(t, err)
	require.Equal(t, "192.168.10.50", resp.TargetAddr)
	require.Equal(t, uint32(2222), resp.TargetPort)
}

func TestUpdateServiceRequiresMask(t *testing.T) {
	db := openTestDB(t)
	mkSvc(t, db, "p1", "bastion", "ssh")
	svc := newServiceService(t, db)

	_, err := svc.UpdateService(adminCtx(t, db), &quicktunv1.UpdateServiceRequest{
		Service: &quicktunv1.Service{Name: "projects/p1/sites/bastion/services/ssh", TargetAddr: "x"},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUpdateServiceRejectsRelayPort(t *testing.T) {
	db := openTestDB(t)
	mkSvc(t, db, "p1", "bastion", "ssh")
	svc := newServiceService(t, db)

	_, err := svc.UpdateService(adminCtx(t, db), &quicktunv1.UpdateServiceRequest{
		Service: &quicktunv1.Service{
			Name: "projects/p1/sites/bastion/services/ssh", RelayPort: 30000,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"relay_port"}},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestDeleteServiceSuccess(t *testing.T) {
	db := openTestDB(t)
	mkSvc(t, db, "p1", "bastion", "ssh")
	svc := newServiceService(t, db)
	ctx := adminCtx(t, db)

	_, err := svc.DeleteService(ctx, &quicktunv1.DeleteServiceRequest{
		Name: "projects/p1/sites/bastion/services/ssh",
	})
	require.NoError(t, err)

	_, err = svc.GetService(ctx, &quicktunv1.GetServiceRequest{
		Name: "projects/p1/sites/bastion/services/ssh",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestDeleteServiceRequiresAdmin(t *testing.T) {
	db := openTestDB(t)
	p, site, _ := mkSvc(t, db, "p1", "bastion", "ssh")
	op := seedOperator(t, db, "u@x.com", "p", false)
	require.NoError(t, db.Create(&model.OperatorProjectAccess{
		OperatorID: op.ID, ProjectID: p.ID, Role: model.ProjectRoleViewer,
	}).Error)
	_ = site
	svc := newServiceService(t, db)

	ctx := auth.WithOperator(context.Background(), op)
	_, err := svc.DeleteService(ctx, &quicktunv1.DeleteServiceRequest{
		Name: "projects/p1/sites/bastion/services/ssh",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}
