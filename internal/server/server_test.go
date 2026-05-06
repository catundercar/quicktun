package server_test

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/server"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:srv_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(model.AllModels()...))
	t.Cleanup(func() { s, _ := db.DB(); s.Close() })
	return db
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	l.Close()
	return addr
}

func TestServerLoginEndToEnd(t *testing.T) {
	db := newDB(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
	_, err := dao.NewOperatorDAO(db).Create(context.Background(), "u@x.com", string(hash), false)
	require.NoError(t, err)

	grpcAddr := freePort(t)
	httpAddr := freePort(t)
	srv, err := server.New(server.Config{
		DB: db, Logger: zap.NewNop(),
		GRPCListen: grpcAddr, HTTPListen: httpAddr,
		RatholeConfigDir: t.TempDir(),
		SessionTTL:       time.Hour,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Log("server did not stop cleanly")
		}
	})

	// Wait for server to listen.
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", grpcAddr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		c.Close()
		return true
	}, 2*time.Second, 25*time.Millisecond)

	// gRPC login.
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := quicktunv1.NewAuthServiceClient(conn)
	resp, err := client.Login(context.Background(), &quicktunv1.LoginRequest{Email: "u@x.com", Password: "pw"})
	require.NoError(t, err)
	require.NotEmpty(t, resp.AccessToken)

	// HTTP gateway whoami with bearer token.
	req, _ := http.NewRequest("GET", "http://"+httpAddr+"/v1/auth:whoami", nil)
	req.Header.Set("Authorization", "Bearer "+resp.AccessToken)
	httpClient := &http.Client{Timeout: 2 * time.Second}
	httpResp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer httpResp.Body.Close()
	require.Equal(t, 200, httpResp.StatusCode)
}

func TestServerWhoAmIRequiresAuth(t *testing.T) {
	db := newDB(t)
	grpcAddr := freePort(t)
	httpAddr := freePort(t)
	srv, err := server.New(server.Config{
		DB: db, Logger: zap.NewNop(),
		GRPCListen: grpcAddr, HTTPListen: httpAddr,
		RatholeConfigDir: t.TempDir(),
		SessionTTL:       time.Hour,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Log("server did not stop cleanly")
		}
	})
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", grpcAddr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		c.Close()
		return true
	}, 2*time.Second, 25*time.Millisecond)

	httpResp, err := http.Get("http://" + httpAddr + "/v1/auth:whoami")
	require.NoError(t, err)
	defer httpResp.Body.Close()
	require.Equal(t, 401, httpResp.StatusCode)

	// Server returns gateway-translated error body.
	if httpResp.Body != nil {
		buf := make([]byte, 256)
		n, _ := httpResp.Body.Read(buf)
		body := strings.ToLower(string(buf[:n]))
		require.Contains(t, body, "unauthenticated")
	}
}

func TestProjectCreateAndListEndToEnd(t *testing.T) {
	db := newDB(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
	_, err := dao.NewOperatorDAO(db).Create(context.Background(), "admin@x.com", string(hash), true)
	require.NoError(t, err)

	grpcAddr := freePort(t)
	httpAddr := freePort(t)
	srv, err := server.New(server.Config{
		DB: db, Logger: zap.NewNop(),
		GRPCListen: grpcAddr, HTTPListen: httpAddr,
		RatholeConfigDir: t.TempDir(),
		SessionTTL:       time.Hour,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", grpcAddr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		c.Close()
		return true
	}, 2*time.Second, 25*time.Millisecond)

	// Login first.
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()
	authClient := quicktunv1.NewAuthServiceClient(conn)
	loginResp, err := authClient.Login(context.Background(), &quicktunv1.LoginRequest{
		Email: "admin@x.com", Password: "pw",
	})
	require.NoError(t, err)

	// Create project with bearer token.
	projClient := quicktunv1.NewProjectServiceClient(conn)
	authedCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+loginResp.AccessToken)
	created, err := projClient.CreateProject(authedCtx, &quicktunv1.CreateProjectRequest{
		ProjectId: "e2e-test",
		Project: &quicktunv1.Project{
			DisplayName:    "E2E",
			RelayPortRange: "20000-20099",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "projects/e2e-test", created.Name)

	// List should include it.
	listed, err := projClient.ListProjects(authedCtx, &quicktunv1.ListProjectsRequest{})
	require.NoError(t, err)
	require.Len(t, listed.Projects, 1)
	require.Equal(t, "projects/e2e-test", listed.Projects[0].Name)
}

func TestSiteCreateEndToEnd(t *testing.T) {
	db := newDB(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
	_, err := dao.NewOperatorDAO(db).Create(context.Background(), "admin@x.com", string(hash), true)
	require.NoError(t, err)
	_, err = dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)

	grpcAddr := freePort(t)
	httpAddr := freePort(t)
	srv, err := server.New(server.Config{
		DB: db, Logger: zap.NewNop(),
		GRPCListen: grpcAddr, HTTPListen: httpAddr,
		RatholeConfigDir: t.TempDir(),
		SessionTTL:       time.Hour,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", grpcAddr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		c.Close()
		return true
	}, 2*time.Second, 25*time.Millisecond)

	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	authClient := quicktunv1.NewAuthServiceClient(conn)
	loginResp, err := authClient.Login(context.Background(), &quicktunv1.LoginRequest{
		Email: "admin@x.com", Password: "pw",
	})
	require.NoError(t, err)

	siteClient := quicktunv1.NewSiteServiceClient(conn)
	authedCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+loginResp.AccessToken)
	created, err := siteClient.CreateSite(authedCtx, &quicktunv1.CreateSiteRequest{
		Parent: "projects/p1",
		SiteId: "e2e-site",
		Site:   &quicktunv1.Site{DisplayName: "E2E"},
	})
	require.NoError(t, err)
	require.Equal(t, "projects/p1/sites/e2e-site", created.Name)

	listed, err := siteClient.ListSites(authedCtx, &quicktunv1.ListSitesRequest{Parent: "projects/p1"})
	require.NoError(t, err)
	require.Len(t, listed.Sites, 1)
}

func TestServiceCreateEndToEnd(t *testing.T) {
	db := newDB(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
	_, err := dao.NewOperatorDAO(db).Create(context.Background(), "admin@x.com", string(hash), true)
	require.NoError(t, err)
	p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	_, err = dao.NewSiteDAO(db).Create(context.Background(), &model.Site{
		ProjectID: p.ID, Name: "bastion",
	})
	require.NoError(t, err)

	grpcAddr := freePort(t)
	httpAddr := freePort(t)
	srv, err := server.New(server.Config{
		DB: db, Logger: zap.NewNop(),
		GRPCListen: grpcAddr, HTTPListen: httpAddr,
		RatholeConfigDir: t.TempDir(),
		SessionTTL:       time.Hour,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", grpcAddr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		c.Close()
		return true
	}, 2*time.Second, 25*time.Millisecond)

	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	authClient := quicktunv1.NewAuthServiceClient(conn)
	loginResp, err := authClient.Login(context.Background(), &quicktunv1.LoginRequest{
		Email: "admin@x.com", Password: "pw",
	})
	require.NoError(t, err)

	svcClient := quicktunv1.NewServiceServiceClient(conn)
	authedCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+loginResp.AccessToken)
	created, err := svcClient.CreateService(authedCtx, &quicktunv1.CreateServiceRequest{
		Parent:    "projects/p1/sites/bastion",
		ServiceId: "ssh",
		Service: &quicktunv1.Service{
			DisplayName: "SSH", TargetAddr: "127.0.0.1", TargetPort: 22,
			Proto: quicktunv1.Proto_PROTO_TCP,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "projects/p1/sites/bastion/services/ssh", created.Name)
	require.NotZero(t, created.RelayPort)

	listed, err := svcClient.ListServices(authedCtx, &quicktunv1.ListServicesRequest{
		Parent: "projects/p1/sites/bastion",
	})
	require.NoError(t, err)
	require.Len(t, listed.Services, 1)
}
