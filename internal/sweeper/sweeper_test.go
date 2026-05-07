package sweeper_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/sweeper"
)

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:sweeper_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(model.AllModels()...))
	t.Cleanup(func() {
		s, _ := db.DB()
		s.Close()
	})
	return db
}

func mkProject(t *testing.T, db *gorm.DB) *model.Project {
	t.Helper()
	p, err := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p", Name: "P", RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)
	return p
}

func TestSweeperTickFlipsStale(t *testing.T) {
	db := openDB(t)
	p := mkProject(t, db)
	sites := dao.NewSiteDAO(db)
	ctx := context.Background()

	now := time.Now().UTC()
	fresh := now.Add(-1 * time.Second)
	stale := now.Add(-5 * time.Minute)

	freshSite, err := sites.Create(ctx, &model.Site{
		ProjectID: p.ID, Name: "fresh",
		Status: model.SiteStatusOnline, LastSeenAt: &fresh,
	})
	require.NoError(t, err)
	staleSite, err := sites.Create(ctx, &model.Site{
		ProjectID: p.ID, Name: "stale",
		Status: model.SiteStatusOnline, LastSeenAt: &stale,
	})
	require.NoError(t, err)

	sw := sweeper.New(sites, sweeper.Config{
		Interval:     time.Hour, // unused; we drive Tick directly
		OfflineAfter: 30 * time.Second,
	}, nil, nil)

	require.NoError(t, sw.Tick(ctx))

	got, err := sites.FindByID(ctx, freshSite.ID)
	require.NoError(t, err)
	require.Equal(t, model.SiteStatusOnline, got.Status)

	got, err = sites.FindByID(ctx, staleSite.ID)
	require.NoError(t, err)
	require.Equal(t, model.SiteStatusOffline, got.Status)
}

func TestSweeperTickNoStaleSitesIsQuiet(t *testing.T) {
	db := openDB(t)
	sites := dao.NewSiteDAO(db)

	sw := sweeper.New(sites, sweeper.Config{
		Interval:     time.Hour,
		OfflineAfter: 30 * time.Second,
	}, nil, nil)

	// No sites at all — Tick should still succeed without error.
	require.NoError(t, sw.Tick(context.Background()))
}

func TestSweeperRunDisabledWhenIntervalZero(t *testing.T) {
	db := openDB(t)
	sites := dao.NewSiteDAO(db)

	sw := sweeper.New(sites, sweeper.Config{
		Interval:     0,
		OfflineAfter: 30 * time.Second,
	}, nil, nil)

	done := make(chan struct{})
	go func() {
		sw.Run(context.Background())
		close(done)
	}()
	select {
	case <-done:
		// expected: returns immediately when disabled
	case <-time.After(time.Second):
		t.Fatal("Run did not return immediately when interval=0")
	}
}

func TestSweeperRunDisabledWhenOfflineAfterZero(t *testing.T) {
	db := openDB(t)
	sites := dao.NewSiteDAO(db)

	sw := sweeper.New(sites, sweeper.Config{
		Interval:     30 * time.Second,
		OfflineAfter: 0,
	}, nil, nil)

	done := make(chan struct{})
	go func() {
		sw.Run(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return immediately when offline_after=0")
	}
}

func TestSweeperRunStopsOnContextCancel(t *testing.T) {
	db := openDB(t)
	sites := dao.NewSiteDAO(db)

	sw := sweeper.New(sites, sweeper.Config{
		Interval:     50 * time.Millisecond,
		OfflineAfter: 30 * time.Second,
	}, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sw.Run(ctx)
		close(done)
	}()

	// Let at least one tick fire.
	time.Sleep(120 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s after ctx cancel")
	}
}
