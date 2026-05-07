package grpcsvc_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/grpcsvc"
	"github.com/tulip/quicktun/internal/model"
)

func TestAdminGetSystemStatusReturnsCounts(t *testing.T) {
	db := openTestDB(t)
	// 2 operators (1 admin via seed below, plus another)
	op := seedOperator(t, db, "admin@x.com", "p", true)
	seedOperator(t, db, "user@x.com", "p", false)

	// 1 active project, 1 disabled project
	pActive := &model.Project{Slug: "active-p", Name: "Active", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(pActive).Error)
	pDisabled := &model.Project{Slug: "disabled-p", Name: "Disabled", RelayPortRange: "20100-20199", Status: model.ProjectStatusDisabled, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(pDisabled).Error)

	// 2 online sites + 1 offline site (all under pActive)
	now := time.Now().UTC()
	require.NoError(t, db.Create(&model.Site{ProjectID: pActive.ID, Name: "s1", Status: model.SiteStatusOnline, LastSeenAt: &now, Mode: model.SiteModeEndpoint, Backend: model.BackendRathole, LanCidrsJSON: "[]"}).Error)
	require.NoError(t, db.Create(&model.Site{ProjectID: pActive.ID, Name: "s2", Status: model.SiteStatusOnline, LastSeenAt: &now, Mode: model.SiteModeEndpoint, Backend: model.BackendRathole, LanCidrsJSON: "[]"}).Error)
	require.NoError(t, db.Create(&model.Site{ProjectID: pActive.ID, Name: "s3", Status: model.SiteStatusOffline, LastSeenAt: &now, Mode: model.SiteModeEndpoint, Backend: model.BackendRathole, LanCidrsJSON: "[]"}).Error)

	// 3 services on s1
	var s1 model.Site
	require.NoError(t, db.Where("name = ?", "s1").First(&s1).Error)
	for i := 0; i < 3; i++ {
		require.NoError(t, db.Create(&model.Service{SiteID: s1.ID, Name: "svc-" + string(rune('a'+i)), TargetAddr: "127.0.0.1", TargetPort: uint16(8000 + i), Proto: model.ProtoTCP}).Error)
	}

	svc := grpcsvc.NewAdminService(db, nil)
	ctx := auth.WithOperator(context.Background(), op)

	resp, err := svc.GetSystemStatus(ctx, &quicktunv1.GetSystemStatusRequest{})
	require.NoError(t, err)
	require.EqualValues(t, 2, resp.GetOperatorCount())
	require.EqualValues(t, 1, resp.GetProjectCountActive())
	require.EqualValues(t, 1, resp.GetProjectCountDisabled())
	require.EqualValues(t, 2, resp.GetSiteCountOnline())
	require.EqualValues(t, 1, resp.GetSiteCountOffline())
	require.EqualValues(t, 0, resp.GetSiteCountPending())
	require.EqualValues(t, 3, resp.GetServiceCount())
	require.EqualValues(t, 0, resp.GetSupervisorRunningCount()) // nil relay = noop = 0
	require.NotNil(t, resp.GetNow())
}

func TestAdminGetSystemStatusEmptyDB(t *testing.T) {
	db := openTestDB(t)
	op := seedOperator(t, db, "admin@x.com", "p", true)
	svc := grpcsvc.NewAdminService(db, nil)
	ctx := auth.WithOperator(context.Background(), op)

	resp, err := svc.GetSystemStatus(ctx, &quicktunv1.GetSystemStatusRequest{})
	require.NoError(t, err)
	require.EqualValues(t, 1, resp.GetOperatorCount()) // the admin we just seeded
	require.EqualValues(t, 0, resp.GetProjectCountActive())
	require.EqualValues(t, 0, resp.GetProjectCountDisabled())
	require.EqualValues(t, 0, resp.GetSiteCountOnline())
	require.EqualValues(t, 0, resp.GetSiteCountOffline())
	require.EqualValues(t, 0, resp.GetSiteCountPending())
	require.EqualValues(t, 0, resp.GetServiceCount())
	require.Empty(t, resp.GetStaleSites())
}

func TestAdminGetSystemStatusRequiresAdmin(t *testing.T) {
	db := openTestDB(t)
	user := seedOperator(t, db, "user@x.com", "p", false)
	svc := grpcsvc.NewAdminService(db, nil)
	ctx := auth.WithOperator(context.Background(), user)

	_, err := svc.GetSystemStatus(ctx, &quicktunv1.GetSystemStatusRequest{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestAdminGetSystemStatusRequiresAuth(t *testing.T) {
	db := openTestDB(t)
	svc := grpcsvc.NewAdminService(db, nil)

	_, err := svc.GetSystemStatus(context.Background(), &quicktunv1.GetSystemStatusRequest{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())
}

func TestAdminGetSystemStatusReportsStaleSites(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)

	p := &model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p).Error)

	// Site online with last_seen 2 minutes ago — should appear in stale_sites.
	stale := time.Now().UTC().Add(-2 * time.Minute)
	require.NoError(t, db.Create(&model.Site{
		ProjectID: p.ID, Name: "bastion-1", Status: model.SiteStatusOnline,
		LastSeenAt: &stale, Hostname: "bastion-1.lan",
		Mode: model.SiteModeEndpoint, Backend: model.BackendRathole, LanCidrsJSON: "[]",
	}).Error)

	svc := grpcsvc.NewAdminService(db, nil)
	ctx := auth.WithOperator(context.Background(), admin)

	resp, err := svc.GetSystemStatus(ctx, &quicktunv1.GetSystemStatusRequest{})
	require.NoError(t, err)
	require.Len(t, resp.GetStaleSites(), 1)
	got := resp.GetStaleSites()[0]
	require.Equal(t, "projects/p1/sites/bastion-1", got.GetName())
	require.Equal(t, "online", got.GetStatus())
	require.Equal(t, "bastion-1.lan", got.GetHostname())
	require.NotNil(t, got.GetLastSeenAt())
}

func TestAdminGetSystemStatusFiltersFreshSites(t *testing.T) {
	db := openTestDB(t)
	admin := seedOperator(t, db, "admin@x.com", "p", true)

	p := &model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive, Backend: model.BackendRathole, DefaultMode: model.SiteModeEndpoint}
	require.NoError(t, db.Create(p).Error)

	// Site online with last_seen 5 seconds ago — should NOT appear.
	fresh := time.Now().UTC().Add(-5 * time.Second)
	require.NoError(t, db.Create(&model.Site{
		ProjectID: p.ID, Name: "bastion-1", Status: model.SiteStatusOnline,
		LastSeenAt: &fresh, Hostname: "bastion-1.lan",
		Mode: model.SiteModeEndpoint, Backend: model.BackendRathole, LanCidrsJSON: "[]",
	}).Error)

	svc := grpcsvc.NewAdminService(db, nil)
	ctx := auth.WithOperator(context.Background(), admin)

	resp, err := svc.GetSystemStatus(ctx, &quicktunv1.GetSystemStatusRequest{})
	require.NoError(t, err)
	require.Empty(t, resp.GetStaleSites())
}
