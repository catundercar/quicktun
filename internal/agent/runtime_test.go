package agent_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/agent"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/grpcsvc"
	"github.com/tulip/quicktun/internal/model"
)

// startAgentTestServer spins up a tiny in-process gRPC server that hosts
// only AgentService + AgentInterceptor. Returns its listen address and a
// cleanup function.
func startAgentTestServer(t *testing.T, db *gorm.DB, relayHost string) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := grpc.NewServer(grpc.UnaryInterceptor(auth.AgentInterceptor(db, zap.NewNop())))
	quicktunv1.RegisterAgentServiceServer(srv, grpcsvc.NewAgentService(
		dao.NewProjectDAO(db),
		dao.NewSiteDAO(db),
		dao.NewServiceDAO(db),
		zap.NewNop(),
		relayHost,
	))

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

func newRuntimeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:agent_rt_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(model.AllModels()...))
	t.Cleanup(func() { s, _ := db.DB(); s.Close() })
	return db
}

// TestRuntimeBootstrapAndRender drives a full Runtime against an in-process
// AgentService backed by a real DAO + SQLite DB. We assert that the agent
// successfully rendered rathole-client.toml from the BootstrapResponse, and
// that the embedded token is sha256_hex(rawToken). RatholeBinary is empty
// so no subprocess is spawned.
func TestRuntimeBootstrapAndRender(t *testing.T) {
	db := newRuntimeTestDB(t)
	ctx := context.Background()

	// Seed: project + site + 1 service + 1 agent token.
	p, err := dao.NewProjectDAO(db).Create(ctx, &model.Project{
		Slug: "proj", Name: "proj", RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)
	s, err := dao.NewSiteDAO(db).Create(ctx, &model.Site{
		ProjectID: p.ID, Name: "bastion-1",
	})
	require.NoError(t, err)
	relayPort := uint16(20001)
	_, err = dao.NewServiceDAO(db).Create(ctx, &model.Service{
		SiteID:     s.ID,
		Name:       "ssh",
		TargetAddr: "127.0.0.1",
		TargetPort: 22,
		Proto:      model.ProtoTCP,
		RelayPort:  &relayPort,
	})
	require.NoError(t, err)

	_, rawToken, err := dao.NewSiteAgentTokenDAO(db).Issue(ctx, s.ID, time.Hour)
	require.NoError(t, err)

	addr := startAgentTestServer(t, db, "relay.test")

	stateDir := t.TempDir()
	cfg := &agent.Config{
		ControlEndpoint:  addr,
		Token:            rawToken,
		StateDir:         stateDir,
		RatholeBinary:    "", // render-only; no subprocess spawn
		RatholeArgs:      []string{"--client"},
		TLSInsecure:      true,
		HostnameOverride: "test-bastion",
	}

	rt, err := agent.New(cfg, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close() })

	runCtx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- rt.Run(runCtx) }()

	cfgPath := filepath.Join(stateDir, "rathole-client.toml")

	// Wait up to 5s for the file to appear with content.
	require.Eventually(t, func() bool {
		fi, err := os.Stat(cfgPath)
		return err == nil && fi.Size() > 0
	}, 5*time.Second, 50*time.Millisecond, "rathole-client.toml never appeared")

	// Verify contents: rathole_control_addr and sha256(token) hex.
	body, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	out := string(body)

	require.Contains(t, out, "[client]")
	// Project's relay range starts at 20000 -> control port == 20000.
	require.Contains(t, out, `remote_addr = "relay.test:20000"`)

	sum := sha256.Sum256([]byte(rawToken))
	wantHex := hex.EncodeToString(sum[:])
	require.Contains(t, out, `default_token = "`+wantHex+`"`)

	// Service block with site_slug__service_slug naming.
	require.Contains(t, out, "[client.services.bastion-1__ssh]")
	require.Contains(t, out, `local_addr = "127.0.0.1:22"`)

	// Confirm the server saw our hostname / agent version through Bootstrap.
	got, err := dao.NewSiteDAO(db).FindByID(ctx, s.ID)
	require.NoError(t, err)
	require.Equal(t, "test-bastion", got.Hostname)
	require.Equal(t, model.SiteStatusOnline, got.Status)

	// Tear down: cancel ctx, ensure Run returns promptly.
	cancel()
	select {
	case err := <-runErr:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("runtime.Run did not return after ctx cancel")
	}
}
