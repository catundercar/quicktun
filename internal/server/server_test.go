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
		SessionTTL: time.Hour,
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
		SessionTTL: time.Hour,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx)
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
