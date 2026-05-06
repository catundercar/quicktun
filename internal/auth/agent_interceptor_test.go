package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:agent_intc_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(model.AllModels()...))
	t.Cleanup(func() {
		s, _ := db.DB()
		s.Close()
	})
	return db
}

func mkSiteWithToken(t *testing.T, db *gorm.DB, ttl time.Duration) (*model.Site, string) {
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
	return s, raw
}

func TestAgentInterceptorAcceptsValidToken(t *testing.T) {
	db := openTestDB(t)
	site, raw := mkSiteWithToken(t, db, 5*time.Minute)
	intc := auth.AgentInterceptor(db)

	var seen *auth.AgentPrincipal
	var called bool
	handler := func(ctx context.Context, req any) (any, error) {
		called = true
		p, ok := auth.AgentFromContext(ctx)
		require.True(t, ok)
		seen = p
		return "ok", nil
	}

	md := metadata.Pairs("authorization", "Bearer "+raw)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	resp, err := intc(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicktun.v1.AgentService/Bootstrap"},
		handler)
	require.NoError(t, err)
	require.Equal(t, "ok", resp)
	require.True(t, called)
	require.NotNil(t, seen)
	require.Equal(t, site.ID, seen.SiteID)
	require.Equal(t, site.ProjectID, seen.ProjectID)
}

func TestAgentInterceptorRejectsBadToken(t *testing.T) {
	db := openTestDB(t)
	_, _ = mkSiteWithToken(t, db, 5*time.Minute)
	intc := auth.AgentInterceptor(db)

	handler := func(ctx context.Context, req any) (any, error) {
		t.Fatalf("handler should not be called")
		return nil, nil
	}
	md := metadata.Pairs("authorization", "Bearer wrong-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := intc(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicktun.v1.AgentService/Bootstrap"},
		handler)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Unauthenticated, st.Code())
}

func TestAgentInterceptorRejectsExpiredToken(t *testing.T) {
	db := openTestDB(t)
	_, raw := mkSiteWithToken(t, db, 5*time.Minute)
	intc := auth.AgentInterceptor(db)

	// Force expire_time into the past.
	past := time.Now().Add(-1 * time.Hour)
	require.NoError(t, db.Model(&model.SiteAgentToken{}).
		Where("token_hash = ?", auth.HashToken(raw)).
		Update("expires_at", &past).Error)

	handler := func(ctx context.Context, req any) (any, error) {
		t.Fatalf("handler should not be called")
		return nil, nil
	}
	md := metadata.Pairs("authorization", "Bearer "+raw)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := intc(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicktun.v1.AgentService/Heartbeat"},
		handler)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())
}

func TestAgentInterceptorPassesThroughNonAgentMethods(t *testing.T) {
	db := openTestDB(t)
	intc := auth.AgentInterceptor(db)

	called := false
	handler := func(ctx context.Context, req any) (any, error) {
		called = true
		return "ok", nil
	}

	// No metadata, non-agent method: must still be called (operator
	// interceptor handles this path, not us).
	resp, err := intc(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicktun.v1.ProjectService/Foo"},
		handler)
	require.NoError(t, err)
	require.Equal(t, "ok", resp)
	require.True(t, called)
}
