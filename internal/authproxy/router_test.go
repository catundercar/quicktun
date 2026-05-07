package authproxy_test

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/authproxy"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
)

func openRouterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:authproxy_router_" + sanitize(t.Name()) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(model.AllModels()...))
	t.Cleanup(func() {
		s, _ := db.DB()
		s.Close()
	})
	return db
}

func sanitize(s string) string {
	return strings.NewReplacer("/", "_", " ", "_").Replace(s)
}

// seedProjectSiteToken creates an active project, a site under it, and an
// agent token. Returns project, site, and the raw bearer token.
func seedProjectSiteToken(t *testing.T, db *gorm.DB, ttl time.Duration) (*model.Project, *model.Site, string) {
	t.Helper()
	ctx := context.Background()
	p, err := dao.NewProjectDAO(db).Create(ctx, &model.Project{
		Slug: "proj", Name: "proj", RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)
	s, err := dao.NewSiteDAO(db).Create(ctx, &model.Site{
		ProjectID: p.ID, Name: "bastion-1",
	})
	require.NoError(t, err)
	_, raw, err := dao.NewSiteAgentTokenDAO(db).Issue(ctx, s.ID, ttl)
	require.NoError(t, err)
	return p, s, raw
}

func TestRouterValidTokenReturnsLoopback(t *testing.T) {
	db := openRouterTestDB(t)
	_, _, raw := seedProjectSiteToken(t, db, 5*time.Minute)

	r := authproxy.NewRouter(db)
	addr, err := r.Route(context.Background(), raw, "relay:443")
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:20000", addr)
}

func TestRouterEmptyTokenRejected(t *testing.T) {
	db := openRouterTestDB(t)
	r := authproxy.NewRouter(db)
	_, err := r.Route(context.Background(), "", "relay:443")
	require.ErrorIs(t, err, authproxy.ErrUnauthenticated)
}

func TestRouterInvalidTokenRejected(t *testing.T) {
	db := openRouterTestDB(t)
	_, _, _ = seedProjectSiteToken(t, db, 5*time.Minute)

	r := authproxy.NewRouter(db)
	// Random hex string that is not the issued token.
	bogus := hex.EncodeToString([]byte("not-a-real-token-xxxxxxxxxxxxxxxx"))
	_, err := r.Route(context.Background(), bogus, "relay:443")
	require.ErrorIs(t, err, authproxy.ErrUnauthenticated)
}

func TestRouterExpiredTokenRejected(t *testing.T) {
	db := openRouterTestDB(t)
	_, _, raw := seedProjectSiteToken(t, db, 5*time.Minute)

	// Force expiry into the past.
	past := time.Now().UTC().Add(-1 * time.Hour)
	require.NoError(t, db.Model(&model.SiteAgentToken{}).
		Where("site_id IS NOT NULL").
		Update("expires_at", &past).Error)

	r := authproxy.NewRouter(db)
	_, err := r.Route(context.Background(), raw, "relay:443")
	require.ErrorIs(t, err, authproxy.ErrUnauthenticated)
}

func TestRouterDisabledProjectRejected(t *testing.T) {
	db := openRouterTestDB(t)
	p, _, raw := seedProjectSiteToken(t, db, 5*time.Minute)

	require.NoError(t, db.Model(&model.Project{}).
		Where("id = ?", p.ID).
		Update("status", model.ProjectStatusDisabled).Error)

	r := authproxy.NewRouter(db)
	_, err := r.Route(context.Background(), raw, "relay:443")
	require.ErrorIs(t, err, authproxy.ErrUnauthenticated)
	require.False(t, errors.Is(err, authproxy.ErrInternal),
		"disabled project must surface as Unauthenticated, not Internal")
}

func TestRouterMissingSiteRejected(t *testing.T) {
	db := openRouterTestDB(t)
	_, s, raw := seedProjectSiteToken(t, db, 5*time.Minute)

	// Hard-delete the site so the post-validation lookup misses.
	require.NoError(t, db.Unscoped().Delete(&model.Site{}, s.ID).Error)

	r := authproxy.NewRouter(db)
	_, err := r.Route(context.Background(), raw, "relay:443")
	require.ErrorIs(t, err, authproxy.ErrUnauthenticated)
}

func TestRouterBadPortRangeReturnsInternal(t *testing.T) {
	db := openRouterTestDB(t)
	p, _, raw := seedProjectSiteToken(t, db, 5*time.Minute)

	// Corrupt the port range directly (bypassing DAO validation).
	require.NoError(t, db.Model(&model.Project{}).
		Where("id = ?", p.ID).
		Update("relay_port_range", "garbage").Error)

	r := authproxy.NewRouter(db)
	_, err := r.Route(context.Background(), raw, "relay:443")
	require.Error(t, err)
	require.True(t, errors.Is(err, authproxy.ErrInternal),
		"expected ErrInternal, got: %v", err)
}

func TestRouterAgentTokenIgnoresTarget(t *testing.T) {
	// Site agent tokens route to the project's minP regardless of CONNECT
	// target — the agent path doesn't care what host:port the operator typed.
	db := openRouterTestDB(t)
	_, _, raw := seedProjectSiteToken(t, db, 5*time.Minute)

	r := authproxy.NewRouter(db)
	addr, err := r.Route(context.Background(), raw, "anything:1234")
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:20000", addr)
}

// seedOperatorService bootstraps a complete graph for operator-token tests:
// project → site → service (with relay_port assigned) → operator → session →
// access-grant. Returns the operator, raw session token, and the relay_port
// assigned to the service.
func seedOperatorService(t *testing.T, db *gorm.DB, withAccess bool) (*model.Operator, string, uint16) {
	t.Helper()
	ctx := context.Background()

	// Project + site (no agent token needed for operator tests).
	p, err := dao.NewProjectDAO(db).Create(ctx, &model.Project{
		Slug: "op-proj", Name: "op-proj", RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)
	s, err := dao.NewSiteDAO(db).Create(ctx, &model.Site{
		ProjectID: p.ID, Name: "op-site",
	})
	require.NoError(t, err)

	// Service with an explicit relay_port within the project range.
	relayPort := uint16(20042)
	require.NoError(t, db.Create(&model.Service{
		SiteID: s.ID, Name: "ssh",
		TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP,
		RelayPort: &relayPort,
	}).Error)

	// Operator + session + (optional) access grant.
	op, err := dao.NewOperatorDAO(db).Create(ctx, "op@example.com", "h", false)
	require.NoError(t, err)
	_, raw, err := dao.NewSessionDAO(db).Issue(ctx, op.ID, time.Hour, "ua", "ip")
	require.NoError(t, err)

	if withAccess {
		require.NoError(t, db.Create(&model.OperatorProjectAccess{
			OperatorID: op.ID, ProjectID: p.ID, Role: model.ProjectRoleOperator,
		}).Error)
	}

	return op, raw, relayPort
}

func TestRouterOperatorTokenForwardsToServicePort(t *testing.T) {
	db := openRouterTestDB(t)
	_, raw, relayPort := seedOperatorService(t, db, true)

	r := authproxy.NewRouter(db)
	target := "127.0.0.1:" + strconv.Itoa(int(relayPort))
	addr, err := r.Route(context.Background(), raw, target)
	require.NoError(t, err)
	require.Equal(t, target, addr)
}

func TestRouterOperatorTokenRejectsWithoutAccess(t *testing.T) {
	db := openRouterTestDB(t)
	_, raw, relayPort := seedOperatorService(t, db, false)

	r := authproxy.NewRouter(db)
	target := "127.0.0.1:" + strconv.Itoa(int(relayPort))
	_, err := r.Route(context.Background(), raw, target)
	require.ErrorIs(t, err, authproxy.ErrUnauthenticated)
}

func TestRouterOperatorTokenRejectsNonLoopbackTarget(t *testing.T) {
	db := openRouterTestDB(t)
	_, raw, _ := seedOperatorService(t, db, true)

	r := authproxy.NewRouter(db)
	_, err := r.Route(context.Background(), raw, "evil.example.com:80")
	require.ErrorIs(t, err, authproxy.ErrUnauthenticated)
}

func TestRouterOperatorTokenRejectsUnknownPort(t *testing.T) {
	db := openRouterTestDB(t)
	_, raw, _ := seedOperatorService(t, db, true)

	r := authproxy.NewRouter(db)
	// 30000 is not allocated to any service.
	_, err := r.Route(context.Background(), raw, "127.0.0.1:30000")
	require.ErrorIs(t, err, authproxy.ErrUnauthenticated)
}

func TestRouterOperatorTokenRejectsDisabledProject(t *testing.T) {
	db := openRouterTestDB(t)
	_, raw, relayPort := seedOperatorService(t, db, true)

	require.NoError(t, db.Model(&model.Project{}).
		Where("slug = ?", "op-proj").
		Update("status", model.ProjectStatusDisabled).Error)

	r := authproxy.NewRouter(db)
	target := "127.0.0.1:" + strconv.Itoa(int(relayPort))
	_, err := r.Route(context.Background(), raw, target)
	require.ErrorIs(t, err, authproxy.ErrUnauthenticated)
}

// TestRouterOperatorAdminBypassesAccessCheck verifies that an admin operator
// can CONNECT to a service port WITHOUT an operator_project_access row.
func TestRouterOperatorAdminBypassesAccessCheck(t *testing.T) {
	db := openRouterTestDB(t)
	ctx := context.Background()

	// Build project → site → service (same pattern as seedOperatorService).
	p, err := dao.NewProjectDAO(db).Create(ctx, &model.Project{
		Slug: "admin-proj", Name: "admin-proj", RelayPortRange: "21000-21099",
	})
	require.NoError(t, err)
	s, err := dao.NewSiteDAO(db).Create(ctx, &model.Site{
		ProjectID: p.ID, Name: "admin-site",
	})
	require.NoError(t, err)
	relayPort := uint16(21042)
	require.NoError(t, db.Create(&model.Service{
		SiteID: s.ID, Name: "ssh",
		TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP,
		RelayPort: &relayPort,
	}).Error)

	// Create an ADMIN operator + session. Do NOT create an
	// operator_project_access row — that is the whole point of this test.
	adminOp, err := dao.NewOperatorDAO(db).Create(ctx, "admin@example.com", "h", true)
	require.NoError(t, err)
	_, raw, err := dao.NewSessionDAO(db).Issue(ctx, adminOp.ID, time.Hour, "ua", "ip")
	require.NoError(t, err)

	r := authproxy.NewRouter(db)
	target := "127.0.0.1:" + strconv.Itoa(int(relayPort))
	addr, err := r.Route(ctx, raw, target)
	require.NoError(t, err, "admin operator must bypass access-check without an operator_project_access row")
	require.Equal(t, target, addr)
}

func TestRouterOperatorTokenRejectsExpiredSession(t *testing.T) {
	db := openRouterTestDB(t)
	op, raw, relayPort := seedOperatorService(t, db, true)

	past := time.Now().UTC().Add(-1 * time.Hour)
	require.NoError(t, db.Model(&model.OperatorSession{}).
		Where("operator_id = ?", op.ID).
		Update("expires_at", past).Error)

	r := authproxy.NewRouter(db)
	target := "127.0.0.1:" + strconv.Itoa(int(relayPort))
	_, err := r.Route(context.Background(), raw, target)
	require.ErrorIs(t, err, authproxy.ErrUnauthenticated)
}
