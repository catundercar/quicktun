package agent_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/agent"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/grpcsvc"
	"github.com/tulip/quicktun/internal/model"
)

// buildFakeBin compiles the supervisor's testfakebin into a temp path so the
// agent has a real subprocess to spawn for supervisor lifecycle tests.
// Mirrors internal/relay/manager_test.go::buildFakeBin.
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

// stubAgentServer is a minimal AgentServiceServer that lets a test drive
// the runtime's bootstrap → heartbeat → rebootstrap state machine without
// the real DAO + 15-second heartbeat cadence baked into AgentService.
//
// It serves two Bootstrap responses (first → "v1" with one tunnel, second →
// "v2" with two tunnels) and a Heartbeat that returns ShouldRebootstrap
// once per "epoch": the test can trigger a rebootstrap by calling
// triggerRebootstrap. The HeartbeatSeconds is set to 1 so the runtime's
// ticker fires sub-second.
type stubAgentServer struct {
	quicktunv1.UnimplementedAgentServiceServer
	mu sync.Mutex
	// epoch increments each time triggerRebootstrap is called. Heartbeat
	// returns ShouldRebootstrap=true while heartbeatEpoch < epoch, then
	// flips it back to false once the agent rebootstraps.
	epoch          int
	heartbeatEpoch int
	bootstrapCalls int
}

func (s *stubAgentServer) Bootstrap(_ context.Context, _ *quicktunv1.BootstrapRequest) (*quicktunv1.BootstrapResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bootstrapCalls++
	// Mirror the agent's view of the world: epoch 0 has just "ssh"; epoch >=1
	// adds "http" alongside it.
	tunnels := []*quicktunv1.TunnelBinding{
		{ServiceSlug: "ssh", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: "tcp", RelayPort: 20001},
	}
	if s.epoch >= 1 {
		tunnels = append(tunnels, &quicktunv1.TunnelBinding{
			ServiceSlug: "http", TargetAddr: "127.0.0.1", TargetPort: 80, Proto: "tcp", RelayPort: 20002,
		})
	}
	s.heartbeatEpoch = s.epoch
	return &quicktunv1.BootstrapResponse{
		SiteName:           "proj/bastion-1",
		ProjectSlug:        "proj",
		SiteSlug:           "bastion-1",
		RatholeControlAddr: "relay.test:20000",
		Tunnels:            tunnels,
		HeartbeatSeconds:   1, // fast loop for tests
		ConfigVersion:      fmt.Sprintf("v%d", s.epoch),
	}, nil
}

func (s *stubAgentServer) Heartbeat(_ context.Context, _ *quicktunv1.HeartbeatRequest) (*quicktunv1.HeartbeatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &quicktunv1.HeartbeatResponse{
		ShouldRebootstrap: s.heartbeatEpoch != s.epoch,
		ServerTime:        timestamppb.Now(),
	}, nil
}

func (s *stubAgentServer) triggerRebootstrap() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epoch++
}

func (s *stubAgentServer) bootstrapCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bootstrapCalls
}

// startStubAgentServer hosts the stub on 127.0.0.1:<random> with the real
// AgentInterceptor disabled (the stub doesn't read the principal). Returns
// the listen address.
func startStubAgentServer(t *testing.T, stub *stubAgentServer) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	quicktunv1.RegisterAgentServiceServer(srv, stub)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

// TestRuntimeRestartsSupervisorOnRebootstrap exercises the rebootstrap
// path end-to-end: agent spawns the fake rathole subprocess, then a stub
// server flips ShouldRebootstrap=true and returns a Bootstrap response with
// a different tunnel set. The runtime must call applyBootstrap a second
// time, which renders new toml, stops the old supervisor, and starts a new
// one. We assert (a) the rendered toml on disk now contains the new tunnel
// slug, (b) Bootstrap was called exactly twice, and (c) Run returns
// cleanly on cancel — the second supervisor goroutine must terminate.
func TestRuntimeRestartsSupervisorOnRebootstrap(t *testing.T) {
	bin := buildFakeBin(t)
	stub := &stubAgentServer{}
	addr := startStubAgentServer(t, stub)

	stateDir := t.TempDir()
	cfg := &agent.Config{
		ControlEndpoint:  addr,
		Token:            "dummy-token-for-stub-server",
		StateDir:         stateDir,
		RatholeBinary:    bin,
		RatholeArgs:      []string{"--mode=sleep"},
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

	// First render: only "ssh".
	require.Eventually(t, func() bool {
		body, err := os.ReadFile(cfgPath)
		if err != nil {
			return false
		}
		s := string(body)
		return strings.Contains(s, "bastion-1__ssh") && !strings.Contains(s, "bastion-1__http")
	}, 5*time.Second, 50*time.Millisecond, "first render missing ssh tunnel")

	// Trigger rebootstrap: epoch++ so the next Heartbeat returns
	// ShouldRebootstrap=true and the next Bootstrap returns the new
	// tunnel set.
	stub.triggerRebootstrap()

	// Second render: both "ssh" and "http". HeartbeatSeconds=1 in the
	// stub response keeps this fast.
	require.Eventually(t, func() bool {
		body, err := os.ReadFile(cfgPath)
		if err != nil {
			return false
		}
		s := string(body)
		return strings.Contains(s, "bastion-1__ssh") && strings.Contains(s, "bastion-1__http")
	}, 10*time.Second, 100*time.Millisecond, "rebootstrap did not re-render with new tunnel")

	// Bootstrap should have run exactly twice (initial + rebootstrap).
	require.Equal(t, 2, stub.bootstrapCount(), "expected exactly two Bootstrap calls")

	// Tear down: cancel and verify Run returns promptly. If the second
	// supervisor goroutine were leaked, Close() (via t.Cleanup) would
	// hang on the supDone receive — we let that be the implicit assertion.
	cancel()
	select {
	case err := <-runErr:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("runtime.Run did not return after ctx cancel")
	}
}
