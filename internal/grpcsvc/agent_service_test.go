package grpcsvc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/grpcsvc"
	"github.com/tulip/quicktun/internal/model"
)

const testRelayHost = "relay.test"

// newAgentService constructs an AgentService against db with the test relay
// host. Tests inject the agent principal via auth.WithAgentPrincipal directly
// (the AgentInterceptor path is exercised in internal/auth tests).
func newAgentService(t *testing.T, db *gorm.DB) *grpcsvc.AgentService {
	t.Helper()
	return grpcsvc.NewAgentService(
		dao.NewProjectDAO(db),
		dao.NewSiteDAO(db),
		dao.NewServiceDAO(db),
		zap.NewNop(),
		testRelayHost,
	)
}

// mkAgentFixtures creates a project, site, and zero-or-more services.
// Each entry in svcs becomes a service with the given (name, relayPort, target).
// A nil relayPort entry means the service is unallocated (relay_port IS NULL).
func mkAgentFixtures(t *testing.T, db *gorm.DB, projSlug, siteName string) (*model.Project, *model.Site) {
	t.Helper()
	ctx := context.Background()
	p, err := dao.NewProjectDAO(db).Create(ctx, &model.Project{
		Slug: projSlug, Name: projSlug, RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)
	s, err := dao.NewSiteDAO(db).Create(ctx, &model.Site{
		ProjectID: p.ID, Name: siteName,
	})
	require.NoError(t, err)
	return p, s
}

func mkService(t *testing.T, db *gorm.DB, siteID uint64, name string, relayPort *uint16, targetAddr string, targetPort uint16) *model.Service {
	t.Helper()
	svc, err := dao.NewServiceDAO(db).Create(context.Background(), &model.Service{
		SiteID:     siteID,
		Name:       name,
		TargetAddr: targetAddr,
		TargetPort: targetPort,
		Proto:      model.ProtoTCP,
		RelayPort:  relayPort,
	})
	require.NoError(t, err)
	return svc
}

func u16(v uint16) *uint16 { return &v }

func agentCtx(p *model.Project, s *model.Site) context.Context {
	return auth.WithAgentPrincipal(context.Background(), &auth.AgentPrincipal{
		SiteID:    s.ID,
		ProjectID: p.ID,
	})
}

func TestBootstrapReturnsExpectedTunnels(t *testing.T) {
	db := openTestDB(t)
	p, s := mkAgentFixtures(t, db, "proj", "bastion-1")
	mkService(t, db, s.ID, "ssh", u16(20001), "127.0.0.1", 22)
	mkService(t, db, s.ID, "web", u16(20002), "10.0.0.5", 80)

	svc := newAgentService(t, db)
	resp, err := svc.Bootstrap(agentCtx(p, s), &quicktunv1.BootstrapRequest{
		Hostname:     "bastion-host",
		Os:           "linux",
		AgentVersion: "0.1.0",
	})
	require.NoError(t, err)
	require.Len(t, resp.Tunnels, 2)
	// rathole control port == project's RelayPortRange[0] == 20000.
	require.Equal(t, testRelayHost+":20000", resp.RatholeControlAddr)
	require.Equal(t, "proj", resp.ProjectSlug)
	require.Equal(t, "bastion-1", resp.SiteSlug)
	require.Equal(t, "projects/proj/sites/bastion-1", resp.SiteName)
	require.NotEmpty(t, resp.ConfigVersion)
	require.Equal(t, int32(15), resp.HeartbeatSeconds)

	// Confirm last_seen_at and hostname were persisted.
	got, err := dao.NewSiteDAO(db).FindByID(context.Background(), s.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastSeenAt)
	require.Equal(t, "bastion-host", got.Hostname)
	require.Equal(t, "linux", got.OS)
	require.Equal(t, "0.1.0", got.AgentVersion)
}

func TestBootstrapServiceWithoutRelayPortIsSkipped(t *testing.T) {
	db := openTestDB(t)
	p, s := mkAgentFixtures(t, db, "proj", "bastion-1")
	mkService(t, db, s.ID, "ssh", u16(20001), "127.0.0.1", 22)
	mkService(t, db, s.ID, "unallocated", nil, "127.0.0.1", 9000)

	svc := newAgentService(t, db)
	resp, err := svc.Bootstrap(agentCtx(p, s), &quicktunv1.BootstrapRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Tunnels, 1)
	require.Equal(t, "ssh", resp.Tunnels[0].ServiceSlug)
}

func TestHeartbeatRebootstrapOnConfigChange(t *testing.T) {
	db := openTestDB(t)
	p, s := mkAgentFixtures(t, db, "proj", "bastion-1")
	mkService(t, db, s.ID, "ssh", u16(20001), "127.0.0.1", 22)

	svc := newAgentService(t, db)
	boot, err := svc.Bootstrap(agentCtx(p, s), &quicktunv1.BootstrapRequest{})
	require.NoError(t, err)
	v1 := boot.ConfigVersion
	require.NotEmpty(t, v1)

	// Add another service so the server-side hash diverges from v1.
	mkService(t, db, s.ID, "web", u16(20002), "10.0.0.5", 80)

	hb, err := svc.Heartbeat(agentCtx(p, s), &quicktunv1.HeartbeatRequest{
		ConfigVersion: v1,
	})
	require.NoError(t, err)
	require.True(t, hb.ShouldRebootstrap)
}

func TestHeartbeatNoRebootstrapWhenStable(t *testing.T) {
	db := openTestDB(t)
	p, s := mkAgentFixtures(t, db, "proj", "bastion-1")
	mkService(t, db, s.ID, "ssh", u16(20001), "127.0.0.1", 22)

	svc := newAgentService(t, db)
	boot, err := svc.Bootstrap(agentCtx(p, s), &quicktunv1.BootstrapRequest{})
	require.NoError(t, err)
	v1 := boot.ConfigVersion

	hb, err := svc.Heartbeat(agentCtx(p, s), &quicktunv1.HeartbeatRequest{
		ConfigVersion: v1,
	})
	require.NoError(t, err)
	require.False(t, hb.ShouldRebootstrap)
	require.NotNil(t, hb.ServerTime)
}

func TestHeartbeatUpdatesLastSeen(t *testing.T) {
	db := openTestDB(t)
	p, s := mkAgentFixtures(t, db, "proj", "bastion-1")

	pre, err := dao.NewSiteDAO(db).FindByID(context.Background(), s.ID)
	require.NoError(t, err)
	require.Nil(t, pre.LastSeenAt) // freshly created site has no heartbeat yet

	svc := newAgentService(t, db)
	_, err = svc.Heartbeat(agentCtx(p, s), &quicktunv1.HeartbeatRequest{
		Hostname:     "bastion-host",
		LanCidrs:     []string{"10.0.0.0/24"},
		AgentVersion: "0.1.0",
	})
	require.NoError(t, err)

	post, err := dao.NewSiteDAO(db).FindByID(context.Background(), s.ID)
	require.NoError(t, err)
	require.NotNil(t, post.LastSeenAt)
	require.Equal(t, "bastion-host", post.Hostname)
	require.Equal(t, "0.1.0", post.AgentVersion)
	require.Contains(t, post.LanCidrsJSON, "10.0.0.0/24")
	require.Equal(t, model.SiteStatusOnline, post.Status)
}

// TestBootstrapRejectsDisabledProject verifies that an agent calling
// Bootstrap against a disabled project receives FailedPrecondition and that
// the site's status is NOT flipped to online.
func TestBootstrapRejectsDisabledProject(t *testing.T) {
	db := openTestDB(t)
	p, s := mkAgentFixtures(t, db, "proj", "bastion-1")
	require.NoError(t, db.Model(&model.Project{}).
		Where("id = ?", p.ID).
		Update("status", model.ProjectStatusDisabled).Error)

	svc := newAgentService(t, db)
	_, err := svc.Bootstrap(agentCtx(p, s), &quicktunv1.BootstrapRequest{
		Hostname: "bastion-host",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.FailedPrecondition, st.Code())

	// Status must remain pending — no online flip on a disabled project.
	got, err := dao.NewSiteDAO(db).FindByID(context.Background(), s.ID)
	require.NoError(t, err)
	require.Equal(t, model.SiteStatusPending, got.Status)
}

// TestHeartbeatRejectsDisabledProject verifies a successful Bootstrap followed
// by a project disable causes Heartbeat to return FailedPrecondition without
// re-flipping site status to online.
func TestHeartbeatRejectsDisabledProject(t *testing.T) {
	db := openTestDB(t)
	p, s := mkAgentFixtures(t, db, "proj", "bastion-1")

	svc := newAgentService(t, db)
	// Successful bootstrap first to confirm the happy path puts the site online.
	_, err := svc.Bootstrap(agentCtx(p, s), &quicktunv1.BootstrapRequest{Hostname: "host"})
	require.NoError(t, err)

	// Now disable the project and verify Heartbeat is rejected.
	require.NoError(t, db.Model(&model.Project{}).
		Where("id = ?", p.ID).
		Update("status", model.ProjectStatusDisabled).Error)

	_, err = svc.Heartbeat(agentCtx(p, s), &quicktunv1.HeartbeatRequest{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.FailedPrecondition, st.Code())
}
