package dao_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
)

func mkProject(t *testing.T, db *gorm.DB, slug string) *model.Project {
	t.Helper()
	p, err := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: slug, Name: slug, RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)
	return p
}

func TestSiteDAOCreateAndFind(t *testing.T) {
	db := openWithModels(t)
	p := mkProject(t, db, "p1")
	store := dao.NewSiteDAO(db)
	ctx := context.Background()

	s, err := store.Create(ctx, &model.Site{
		ProjectID: p.ID, Name: "bastion-1", Mode: model.SiteModeEndpoint,
		Status: model.SiteStatusPending,
	})
	require.NoError(t, err)
	require.NotZero(t, s.ID)

	got, err := store.FindByName(ctx, p.ID, "bastion-1")
	require.NoError(t, err)
	require.Equal(t, s.ID, got.ID)
}

func TestSiteDAOFindNotFound(t *testing.T) {
	db := openWithModels(t)
	p := mkProject(t, db, "p1")
	store := dao.NewSiteDAO(db)
	_, err := store.FindByName(context.Background(), p.ID, "nope")
	require.True(t, dao.IsNotFound(err))
}

func TestSiteDAOListByProject(t *testing.T) {
	db := openWithModels(t)
	p1 := mkProject(t, db, "p1")
	p2 := mkProject(t, db, "p2")
	store := dao.NewSiteDAO(db)
	ctx := context.Background()

	_, _ = store.Create(ctx, &model.Site{ProjectID: p1.ID, Name: "a"})
	_, _ = store.Create(ctx, &model.Site{ProjectID: p1.ID, Name: "b"})
	_, _ = store.Create(ctx, &model.Site{ProjectID: p2.ID, Name: "c"})

	got, err := store.ListByProject(ctx, p1.ID, 100, "")
	require.NoError(t, err)
	require.Len(t, got, 2)
}

func TestSiteDAOInvalidPageToken(t *testing.T) {
	db := openWithModels(t)
	p := mkProject(t, db, "p")
	store := dao.NewSiteDAO(db)
	_, err := store.ListByProject(context.Background(), p.ID, 100, "not-a-number")
	require.Error(t, err)
	require.ErrorIs(t, err, dao.ErrInvalidPageToken)
}

func TestSiteDAOUpdate(t *testing.T) {
	db := openWithModels(t)
	p := mkProject(t, db, "p")
	store := dao.NewSiteDAO(db)
	ctx := context.Background()

	s, _ := store.Create(ctx, &model.Site{ProjectID: p.ID, Name: "u"})
	s.Status = model.SiteStatusOnline
	require.NoError(t, store.Update(ctx, s))

	got, _ := store.FindByName(ctx, p.ID, "u")
	require.Equal(t, model.SiteStatusOnline, got.Status)
}

func TestSiteDAODelete(t *testing.T) {
	db := openWithModels(t)
	p := mkProject(t, db, "p")
	store := dao.NewSiteDAO(db)
	ctx := context.Background()

	s, _ := store.Create(ctx, &model.Site{ProjectID: p.ID, Name: "d"})
	require.NoError(t, store.Delete(ctx, s.ID))
	_, err := store.FindByName(ctx, p.ID, "d")
	require.True(t, dao.IsNotFound(err))
}

func TestSiteDAOCountServices(t *testing.T) {
	db := openWithModels(t)
	p := mkProject(t, db, "p")
	store := dao.NewSiteDAO(db)
	ctx := context.Background()

	s, _ := store.Create(ctx, &model.Site{ProjectID: p.ID, Name: "s"})
	n, err := store.CountServices(ctx, s.ID)
	require.NoError(t, err)
	require.Equal(t, int64(0), n)

	require.NoError(t, db.Create(&model.Service{
		SiteID: s.ID, Name: "ssh", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP,
	}).Error)
	n, err = store.CountServices(ctx, s.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
}

func TestMarkStaleOffline(t *testing.T) {
	db := openWithModels(t)
	p := mkProject(t, db, "p")
	store := dao.NewSiteDAO(db)
	ctx := context.Background()

	now := time.Now().UTC()
	fresh := now.Add(-10 * time.Second)
	stale := now.Add(-5 * time.Minute)

	// Online + fresh heartbeat — must NOT flip.
	freshSite, err := store.Create(ctx, &model.Site{
		ProjectID: p.ID, Name: "fresh",
		Status: model.SiteStatusOnline, LastSeenAt: &fresh,
	})
	require.NoError(t, err)

	// Online + stale heartbeat — MUST flip to offline.
	staleSite, err := store.Create(ctx, &model.Site{
		ProjectID: p.ID, Name: "stale",
		Status: model.SiteStatusOnline, LastSeenAt: &stale,
	})
	require.NoError(t, err)

	// Pending + stale heartbeat — must NOT flip (preserve "never seen yet").
	pendingSite, err := store.Create(ctx, &model.Site{
		ProjectID: p.ID, Name: "pending",
		Status: model.SiteStatusPending, LastSeenAt: &stale,
	})
	require.NoError(t, err)

	// Threshold is 1 minute ago: fresh is after, stale is before.
	threshold := now.Add(-1 * time.Minute)
	n, err := store.MarkStaleOffline(ctx, threshold)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	got, err := store.FindByID(ctx, freshSite.ID)
	require.NoError(t, err)
	require.Equal(t, model.SiteStatusOnline, got.Status)

	got, err = store.FindByID(ctx, staleSite.ID)
	require.NoError(t, err)
	require.Equal(t, model.SiteStatusOffline, got.Status)

	got, err = store.FindByID(ctx, pendingSite.ID)
	require.NoError(t, err)
	require.Equal(t, model.SiteStatusPending, got.Status)
}

func TestSiteAgentTokenIssueAndValidate(t *testing.T) {
	db := openWithModels(t)
	p := mkProject(t, db, "p")
	sites := dao.NewSiteDAO(db)
	tokens := dao.NewSiteAgentTokenDAO(db)
	ctx := context.Background()

	s, _ := sites.Create(ctx, &model.Site{ProjectID: p.ID, Name: "s"})
	rec, raw, err := tokens.Issue(ctx, s.ID, 5*time.Minute)
	require.NoError(t, err)
	require.NotEmpty(t, raw)
	require.NotEmpty(t, rec.TokenHash)

	siteID, err := tokens.ValidateRaw(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, s.ID, siteID)
}

func TestSiteAgentTokenRotateInvalidatesOldToken(t *testing.T) {
	db := openWithModels(t)
	p := mkProject(t, db, "p")
	sites := dao.NewSiteDAO(db)
	tokens := dao.NewSiteAgentTokenDAO(db)
	ctx := context.Background()

	s, _ := sites.Create(ctx, &model.Site{ProjectID: p.ID, Name: "s"})
	_, oldRaw, _ := tokens.Issue(ctx, s.ID, 5*time.Minute)
	_, newRaw, err := tokens.Issue(ctx, s.ID, 5*time.Minute)
	require.NoError(t, err)
	require.NotEqual(t, oldRaw, newRaw)

	// Old token should no longer validate (Issue soft-deletes prior live token).
	_, err = tokens.ValidateRaw(ctx, oldRaw)
	require.Error(t, err)

	siteID, err := tokens.ValidateRaw(ctx, newRaw)
	require.NoError(t, err)
	require.Equal(t, s.ID, siteID)
}
