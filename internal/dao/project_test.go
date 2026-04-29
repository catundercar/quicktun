package dao_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
)

func TestProjectCreateAndFindBySlug(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewProjectDAO(db)
	ctx := context.Background()

	p, err := store.Create(ctx, &model.Project{
		Slug:           "clinic-network",
		Name:           "Clinic Network",
		DefaultMode:    model.SiteModeEndpoint,
		Backend:        model.BackendRathole,
		RelayPortRange: "20000-20999",
		Status:         model.ProjectStatusActive,
	})
	require.NoError(t, err)
	require.NotZero(t, p.ID)

	got, err := store.FindBySlug(ctx, "clinic-network")
	require.NoError(t, err)
	require.Equal(t, p.ID, got.ID)
	require.Equal(t, "Clinic Network", got.Name)
}

func TestProjectFindBySlugNotFound(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewProjectDAO(db)
	_, err := store.FindBySlug(context.Background(), "nope")
	require.Error(t, err)
	require.True(t, dao.IsNotFound(err))
}

func TestProjectList(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewProjectDAO(db)
	ctx := context.Background()

	_, _ = store.Create(ctx, &model.Project{Slug: "aa", Name: "A", RelayPortRange: "20000-20099"})
	_, _ = store.Create(ctx, &model.Project{Slug: "bb", Name: "B", RelayPortRange: "20100-20199"})
	_, _ = store.Create(ctx, &model.Project{Slug: "cc", Name: "C", RelayPortRange: "20200-20299"})

	got, err := store.List(ctx, 100, "")
	require.NoError(t, err)
	require.Len(t, got, 3)
}

func TestProjectListPagination(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewProjectDAO(db)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		c := byte('a' + i)
		slug := string([]byte{c, c, c}) // 3-char slug to satisfy ValidateSlug-style minimums
		_, err := store.Create(ctx, &model.Project{Slug: slug, Name: slug, RelayPortRange: "20000-20099"})
		require.NoError(t, err)
	}

	page1, err := store.List(ctx, 2, "")
	require.NoError(t, err)
	require.Len(t, page1, 2)

	// Use last id as page token.
	page2, err := store.List(ctx, 2, dao.NextProjectPageToken(page1))
	require.NoError(t, err)
	require.Len(t, page2, 2)
	require.NotEqual(t, page1[0].ID, page2[0].ID)
}

func TestProjectUpdate(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewProjectDAO(db)
	ctx := context.Background()

	p, _ := store.Create(ctx, &model.Project{Slug: "uu", Name: "U1", RelayPortRange: "20000-20099"})
	p.Name = "U2"
	p.Status = model.ProjectStatusDisabled
	require.NoError(t, store.Update(ctx, p))

	got, _ := store.FindBySlug(ctx, "uu")
	require.Equal(t, "U2", got.Name)
	require.Equal(t, model.ProjectStatusDisabled, got.Status)
}

func TestProjectSoftDelete(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewProjectDAO(db)
	ctx := context.Background()

	p, _ := store.Create(ctx, &model.Project{Slug: "dd", Name: "D", RelayPortRange: "20000-20099"})
	require.NoError(t, store.Delete(ctx, p.ID))

	_, err := store.FindBySlug(ctx, "dd")
	require.True(t, dao.IsNotFound(err))
}

func TestProjectCountSites(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewProjectDAO(db)
	ctx := context.Background()

	p, _ := store.Create(ctx, &model.Project{Slug: "pp", Name: "P", RelayPortRange: "20000-20099"})

	got, err := store.CountSites(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, int64(0), got)

	require.NoError(t, db.Create(&model.Site{ProjectID: p.ID, Name: "s1"}).Error)
	require.NoError(t, db.Create(&model.Site{ProjectID: p.ID, Name: "s2"}).Error)

	got, err = store.CountSites(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), got)
}
