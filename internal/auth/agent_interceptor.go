package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/model"
)

// AgentPrincipal identifies an authenticated site agent on the request ctx.
type AgentPrincipal struct {
	SiteID    uint64
	ProjectID uint64
}

type agentCtxKey struct{}

// AgentFromContext returns the principal if the request was authenticated as
// an agent.
func AgentFromContext(ctx context.Context) (*AgentPrincipal, bool) {
	a, ok := ctx.Value(agentCtxKey{}).(*AgentPrincipal)
	return a, ok
}

// AgentInterceptor authenticates AgentService RPCs via Bearer site agent
// tokens. Methods outside /quicktun.v1.AgentService/* pass through (the
// operator interceptor handles those).
//
// Token matching is sha256_hex(raw) == site_agent_tokens.token_hash. The
// principal's SiteID and ProjectID are attached to the request context.
func AgentInterceptor(db *gorm.DB) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !strings.HasPrefix(info.FullMethod, "/quicktun.v1.AgentService/") {
			return handler(ctx, req)
		}
		raw := extractBearer(ctx)
		if raw == "" {
			return nil, status.Error(codes.Unauthenticated, "missing bearer token")
		}
		hexHash := HashToken(raw)

		var tok model.SiteAgentToken
		err := db.WithContext(ctx).
			Where("token_hash = ?", hexHash).
			First(&tok).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}
		if err != nil {
			return nil, status.Errorf(codes.Internal, "auth lookup: %v", err)
		}
		if tok.ExpiresAt != nil && tok.ExpiresAt.Before(time.Now()) {
			return nil, status.Error(codes.Unauthenticated, "token expired")
		}

		var site model.Site
		if err := db.WithContext(ctx).First(&site, tok.SiteID).Error; err != nil {
			return nil, status.Errorf(codes.Internal, "site lookup: %v", err)
		}

		ctx = context.WithValue(ctx, agentCtxKey{}, &AgentPrincipal{
			SiteID:    site.ID,
			ProjectID: site.ProjectID,
		})
		return handler(ctx, req)
	}
}
