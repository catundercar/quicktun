package dao_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
)

func TestServiceCreateAndFind(t *testing.T) {
	db := openWithModels(t)
	p := mkProject(t, db, "p1")
	s, _ := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{
		ProjectID: p.ID, Name: "bastion",
	})
	store := dao.NewServiceDAO(db)
	ctx := context.Background()

	relayPort := uint16(20100)
	svc, err := store.Create(ctx, &model.Service{
		SiteID: s.ID, Name: "ssh",
		TargetAddr: "127.0.0.1", TargetPort: 22,
		Proto: model.ProtoTCP, RelayPort: &relayPort,
	})
	require.NoError(t, err)
	require.NotZero(t, svc.ID)

	got, err := store.FindByName(ctx, s.ID, "ssh")
	require.NoError(t, err)
	require.Equal(t, svc.ID, got.ID)
	require.NotNil(t, got.RelayPort)
	require.Equal(t, uint16(20100), *got.RelayPort)
}

func TestServiceListBySite(t *testing.T) {
	db := openWithModels(t)
	p := mkProject(t, db, "p1")
	s1, _ := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "b1"})
	s2, _ := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "b2"})
	store := dao.NewServiceDAO(db)
	ctx := context.Background()

	_, _ = store.Create(ctx, &model.Service{SiteID: s1.ID, Name: "ssh", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP})
	_, _ = store.Create(ctx, &model.Service{SiteID: s1.ID, Name: "rdp", TargetAddr: "192.168.10.50", TargetPort: 3389, Proto: model.ProtoTCP})
	_, _ = store.Create(ctx, &model.Service{SiteID: s2.ID, Name: "ssh", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP})

	got, err := store.ListBySite(ctx, s1.ID, 100, "")
	require.NoError(t, err)
	require.Len(t, got, 2)
}

func TestServiceDelete(t *testing.T) {
	db := openWithModels(t)
	p := mkProject(t, db, "p1")
	s, _ := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "b"})
	store := dao.NewServiceDAO(db)
	ctx := context.Background()

	svc, _ := store.Create(ctx, &model.Service{SiteID: s.ID, Name: "ssh", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP})
	require.NoError(t, store.Delete(ctx, svc.ID))
	_, err := store.FindByName(ctx, s.ID, "ssh")
	require.True(t, dao.IsNotFound(err))
}

func TestAllocateRelayPortAssignsLowestFree(t *testing.T) {
	db := openWithModels(t)
	p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p", Name: "P", RelayPortRange: "20000-20003",
	})
	s, _ := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "b"})
	store := dao.NewServiceDAO(db)
	ctx := context.Background()

	// 20000 is reserved for the rathole control port — first service-allocatable
	// port is 20001. Pre-seed 20001 and 20003 to force allocation to return 20002.
	rp1 := uint16(20001)
	rp2 := uint16(20003)
	_, _ = store.Create(ctx, &model.Service{SiteID: s.ID, Name: "a", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP, RelayPort: &rp1})
	_, _ = store.Create(ctx, &model.Service{SiteID: s.ID, Name: "b", TargetAddr: "127.0.0.1", TargetPort: 23, Proto: model.ProtoTCP, RelayPort: &rp2})

	port, err := store.AllocateRelayPort(ctx, p)
	require.NoError(t, err)
	require.Equal(t, uint16(20002), port)
}

func TestAllocateRelayPortSkipsControlPort(t *testing.T) {
	// Brand-new project: no allocations yet. The first port handed out must
	// skip minP (reserved for rathole control) and return minP+1.
	db := openWithModels(t)
	p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p", Name: "P", RelayPortRange: "20000-20099",
	})
	store := dao.NewServiceDAO(db)
	port, err := store.AllocateRelayPort(context.Background(), p)
	require.NoError(t, err)
	require.Equal(t, uint16(20001), port)
}

func TestAllocateRelayPortExhausted(t *testing.T) {
	db := openWithModels(t)
	// Range 20000-20002: 20000 is the control port, leaving 20001 and 20002
	// for services. Filling both must exhaust the range.
	p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p", Name: "P", RelayPortRange: "20000-20002",
	})
	s, _ := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "b"})
	store := dao.NewServiceDAO(db)
	ctx := context.Background()

	a := uint16(20001)
	b := uint16(20002)
	_, _ = store.Create(ctx, &model.Service{SiteID: s.ID, Name: "x", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP, RelayPort: &a})
	_, _ = store.Create(ctx, &model.Service{SiteID: s.ID, Name: "y", TargetAddr: "127.0.0.1", TargetPort: 23, Proto: model.ProtoTCP, RelayPort: &b})

	_, err := store.AllocateRelayPort(ctx, p)
	require.Error(t, err)
	require.ErrorIs(t, err, dao.ErrPortRangeExhausted)
}

func TestAllocateRelayPortBadRange(t *testing.T) {
	db := openWithModels(t)
	p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p", Name: "P", RelayPortRange: "garbage",
	})
	store := dao.NewServiceDAO(db)
	_, err := store.AllocateRelayPort(context.Background(), p)
	require.Error(t, err)
}
