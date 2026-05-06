package relay_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/relay"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:relay_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(model.AllModels()...))
	t.Cleanup(func() { s, _ := db.DB(); s.Close() })
	return db
}

// buildFakeBin compiles the supervisor's testfakebin into a temp path so the
// manager has something runnable to spawn during tests.
func buildFakeBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "fakebin")
	wd, _ := os.Getwd()
	cmd := exec.Command("go", "build", "-o", out, "../supervisor/testfakebin")
	cmd.Dir = wd
	if err := cmd.Run(); err != nil {
		t.Fatalf("build fake bin: %v", err)
	}
	return out
}

func TestManagerStartWritesConfigPerProject(t *testing.T) {
	db := newDB(t)
	bin := buildFakeBin(t)
	cfgDir := t.TempDir()

	pdao := dao.NewProjectDAO(db)
	_, err := pdao.Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)
	_, err = pdao.Create(context.Background(), &model.Project{
		Slug: "p2", Name: "P2", RelayPortRange: "21000-21099",
	})
	require.NoError(t, err)

	mgr := relay.NewManager(db, relay.ManagerConfig{
		Binary:     bin,
		BinaryArgs: []string{"--mode=sleep"},
		ConfigDir:  cfgDir,
	}, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, mgr.Start(ctx))

	require.Eventually(t, func() bool {
		_, e1 := os.Stat(filepath.Join(cfgDir, "p1.toml"))
		_, e2 := os.Stat(filepath.Join(cfgDir, "p2.toml"))
		return e1 == nil && e2 == nil
	}, 2*time.Second, 50*time.Millisecond)

	mgr.Stop()
}

func TestManagerRefreshRewritesConfig(t *testing.T) {
	db := newDB(t)
	bin := buildFakeBin(t)
	cfgDir := t.TempDir()

	p, err := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)
	site, err := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{
		ProjectID: p.ID, Name: "bastion",
	})
	require.NoError(t, err)

	mgr := relay.NewManager(db, relay.ManagerConfig{
		Binary:     bin,
		BinaryArgs: []string{"--mode=sleep"},
		ConfigDir:  cfgDir,
	}, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, mgr.Start(ctx))

	rp := uint16(20022)
	_, err = dao.NewServiceDAO(db).Create(context.Background(), &model.Service{
		SiteID: site.ID, Name: "ssh",
		TargetAddr: "127.0.0.1", TargetPort: 22,
		Proto: model.ProtoTCP, RelayPort: &rp,
	})
	require.NoError(t, err)

	require.NoError(t, mgr.Refresh(ctx, p.ID))

	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(cfgDir, "p1.toml"))
		if err != nil {
			return false
		}
		s := string(data)
		return strings.Contains(s, "bastion__ssh") && strings.Contains(s, "20022")
	}, 2*time.Second, 50*time.Millisecond)

	mgr.Stop()
}

func TestManagerAddRemoveProject(t *testing.T) {
	db := newDB(t)
	bin := buildFakeBin(t)
	cfgDir := t.TempDir()

	mgr := relay.NewManager(db, relay.ManagerConfig{
		Binary:     bin,
		BinaryArgs: []string{"--mode=sleep"},
		ConfigDir:  cfgDir,
	}, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, mgr.Start(ctx))

	p, err := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)

	require.NoError(t, mgr.AddProject(ctx, p.ID))
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(cfgDir, "p1.toml"))
		return err == nil
	}, 2*time.Second, 50*time.Millisecond)

	require.NoError(t, mgr.RemoveProject(ctx, p.ID))
	require.Equal(t, 0, mgr.SupervisorCount())

	mgr.Stop()
}

func TestManagerSkipsWhenBinaryEmpty(t *testing.T) {
	db := newDB(t)
	cfgDir := t.TempDir()

	p, err := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)
	mgr := relay.NewManager(db, relay.ManagerConfig{ConfigDir: cfgDir}, zap.NewNop())
	require.NoError(t, mgr.Start(context.Background()))

	// Even with binary="" the config file is rendered, but no supervisor starts.
	require.NoError(t, mgr.AddProject(context.Background(), p.ID))
	require.Equal(t, 0, mgr.SupervisorCount())
	_, err = os.Stat(filepath.Join(cfgDir, "p1.toml"))
	require.NoError(t, err)
}

// TestManagerRefreshWaitsForOldSupervisor pins down the API contract that
// Refresh tears down the old supervisor and replaces it with exactly one
// new supervisor. It does not directly exercise a port-bind race (the fake
// binary releases its ports immediately) but it exercises the done-channel
// wait path.
func TestManagerRefreshWaitsForOldSupervisor(t *testing.T) {
	db := newDB(t)
	bin := buildFakeBin(t)
	cfgDir := t.TempDir()

	p, err := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)

	mgr := relay.NewManager(db, relay.ManagerConfig{
		Binary: bin, BinaryArgs: []string{"--mode=sleep"}, ConfigDir: cfgDir,
	}, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, mgr.Start(ctx))
	require.NoError(t, mgr.AddProject(ctx, p.ID))
	require.Equal(t, 1, mgr.SupervisorCount())

	// Refresh should complete cleanly and leave exactly one tracked supervisor.
	require.NoError(t, mgr.Refresh(ctx, p.ID))
	require.Equal(t, 1, mgr.SupervisorCount())

	mgr.Stop()
}

func TestManagerStartTolersBadBinary(t *testing.T) {
	db := newDB(t)
	cfgDir := t.TempDir()
	mgr := relay.NewManager(db, relay.ManagerConfig{
		Binary:    "/nonexistent/quicktun-test-rathole",
		ConfigDir: cfgDir,
	}, zap.NewNop())
	require.NoError(t, mgr.Start(context.Background()))
	mgr.Stop()
}

// TestManagerStopRefusesAddAfterClose verifies the closed-flag guard prevents
// orphaned supervisors when AddProject is called after Stop.
func TestManagerStopRefusesAddAfterClose(t *testing.T) {
	db := newDB(t)
	cfgDir := t.TempDir()
	mgr := relay.NewManager(db, relay.ManagerConfig{ConfigDir: cfgDir}, zap.NewNop())
	require.NoError(t, mgr.Start(context.Background()))
	mgr.Stop()
	p, err := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)
	err = mgr.AddProject(context.Background(), p.ID)
	require.Error(t, err)
}
