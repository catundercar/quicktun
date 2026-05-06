package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"
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

// WithAgentPrincipal attaches an AgentPrincipal to ctx. For tests; the
// production path uses AgentInterceptor.
func WithAgentPrincipal(ctx context.Context, p *AgentPrincipal) context.Context {
	return context.WithValue(ctx, agentCtxKey{}, p)
}

// AgentInterceptor authenticates AgentService RPCs via Bearer site agent
// tokens. Methods outside /quicktun.v1.AgentService/* pass through (the
// operator interceptor handles those).
//
// Token validation does the SHA-256 hash + expires_at comparison in SQL
// (`expires_at IS NULL OR expires_at > ?`) and bumps last_used_at. The
// owning Site is then loaded so the principal carries both SiteID and
// ProjectID.
//
// This logic mirrors dao.SiteAgentTokenDAO.ValidateRaw; we cannot import
// the dao package here because dao already depends on auth (HashToken /
// IssueToken), which would create an import cycle.
//
// Errors are scrubbed: the client always sees "invalid or expired token"
// (Unauthenticated) or a generic "internal error" (Internal); the underlying
// detail is logged server-side via lg.
func AgentInterceptor(db *gorm.DB, lg *zap.Logger) grpc.UnaryServerInterceptor {
	if lg == nil {
		lg = zap.NewNop()
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !strings.HasPrefix(info.FullMethod, "/quicktun.v1.AgentService/") {
			return handler(ctx, req)
		}
		raw := extractBearer(ctx)
		if raw == "" {
			return nil, status.Error(codes.Unauthenticated, "missing bearer token")
		}

		var tok model.SiteAgentToken
		err := db.WithContext(ctx).
			Where("token_hash = ?", HashToken(raw)).
			Where("expires_at IS NULL OR expires_at > ?", time.Now().UTC()).
			First(&tok).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}
		if err != nil {
			lg.Warn("agent auth: token lookup failed", zap.Error(err))
			return nil, status.Error(codes.Internal, "internal error")
		}

		var site model.Site
		err = db.WithContext(ctx).First(&site, tok.SiteID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			lg.Warn("agent auth: token resolved to missing site",
				zap.Uint64("site_id", tok.SiteID))
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}
		if err != nil {
			lg.Warn("agent auth: site lookup failed", zap.Error(err))
			return nil, status.Error(codes.Internal, "internal error")
		}

		// Best-effort last_used_at refresh; failures are logged but do not
		// fail the request.
		now := time.Now().UTC()
		if err := db.WithContext(ctx).Model(&model.SiteAgentToken{}).
			Where("id = ?", tok.ID).
			Update("last_used_at", &now).Error; err != nil {
			lg.Warn("agent auth: last_used_at update failed", zap.Error(err))
		}

		ctx = context.WithValue(ctx, agentCtxKey{}, &AgentPrincipal{
			SiteID:    site.ID,
			ProjectID: site.ProjectID,
		})
		return handler(ctx, req)
	}
}
