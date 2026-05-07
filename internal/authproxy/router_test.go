package authproxy_test

import (
	"context"
	"encoding/hex"
	"errors"
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
	addr, err := r.Route(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:20000", addr)
}

func TestRouterEmptyTokenRejected(t *testing.T) {
	db := openRouterTestDB(t)
	r := authproxy.NewRouter(db)
	_, err := r.Route(context.Background(), "")
	require.ErrorIs(t, err, authproxy.ErrUnauthenticated)
}

func TestRouterInvalidTokenRejected(t *testing.T) {
	db := openRouterTestDB(t)
	_, _, _ = seedProjectSiteToken(t, db, 5*time.Minute)

	r := authproxy.NewRouter(db)
	// Random hex string that is not the issued token.
	bogus := hex.EncodeToString([]byte("not-a-real-token-xxxxxxxxxxxxxxxx"))
	_, err := r.Route(context.Background(), bogus)
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
	_, err := r.Route(context.Background(), raw)
	require.ErrorIs(t, err, authproxy.ErrUnauthenticated)
}

func TestRouterDisabledProjectRejected(t *testing.T) {
	db := openRouterTestDB(t)
	p, _, raw := seedProjectSiteToken(t, db, 5*time.Minute)

	require.NoError(t, db.Model(&model.Project{}).
		Where("id = ?", p.ID).
		Update("status", model.ProjectStatusDisabled).Error)

	r := authproxy.NewRouter(db)
	_, err := r.Route(context.Background(), raw)
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
	_, err := r.Route(context.Background(), raw)
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
	_, err := r.Route(context.Background(), raw)
	require.Error(t, err)
	require.True(t, errors.Is(err, authproxy.ErrInternal),
		"expected ErrInternal, got: %v", err)
}
