package auth

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/tulip/quicktun/internal/model"
)

// Validator validates a raw bearer token and returns the owning operator.
// Implemented by *dao.SessionDAO; receiving an interface here keeps the
// interceptor unit-testable without a database.
type Validator interface {
	Validate(ctx context.Context, rawToken string) (*model.Operator, error)
}

// ctxKey is unexported to prevent collisions with other packages' context keys.
type ctxKey int

const (
	operatorKey ctxKey = iota
	rawTokenKey
)

// WithOperator attaches op to ctx so handlers can read it via OperatorFromContext.
func WithOperator(ctx context.Context, op *model.Operator) context.Context {
	return context.WithValue(ctx, operatorKey, op)
}

// OperatorFromContext returns the authenticated operator attached by
// NewUnaryInterceptor. Returns nil if not authenticated (e.g. in the Login
// path, which is unauthenticated).
func OperatorFromContext(ctx context.Context) *model.Operator {
	op, _ := ctx.Value(operatorKey).(*model.Operator)
	return op
}

// WithRawToken attaches the raw bearer token to ctx so handlers like Logout
// can revoke it without re-parsing metadata. Set by the interceptor.
func WithRawToken(ctx context.Context, raw string) context.Context {
	return context.WithValue(ctx, rawTokenKey, raw)
}

// RawTokenFromContext returns the raw bearer token previously attached by
// WithRawToken. Returns "" if no token is in context.
func RawTokenFromContext(ctx context.Context) string {
	v, _ := ctx.Value(rawTokenKey).(string)
	return v
}

// NewUnaryInterceptor returns a grpc.UnaryServerInterceptor that:
//   - lets requests for any FullMethod listed in `unauth` proceed without auth
//   - extracts a Bearer token from the "authorization" gRPC metadata header
//   - validates it via Validator and attaches operator + raw token to ctx
//   - returns codes.Unauthenticated on missing or invalid token
func NewUnaryInterceptor(v Validator, unauth ...string) grpc.UnaryServerInterceptor {
	allowlist := make(map[string]struct{}, len(unauth))
	for _, m := range unauth {
		allowlist[m] = struct{}{}
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if _, ok := allowlist[info.FullMethod]; ok {
			return handler(ctx, req)
		}
		// AgentService is authenticated by AgentInterceptor (site agent
		// tokens, not operator sessions). Pass through so that interceptor
		// can do its own check.
		if strings.HasPrefix(info.FullMethod, "/quicktun.v1.AgentService/") {
			return handler(ctx, req)
		}
		token := extractBearer(ctx)
		if token == "" {
			return nil, status.Error(codes.Unauthenticated, "missing bearer token")
		}
		op, err := v.Validate(ctx, token)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}
		ctx = WithOperator(ctx, op)
		ctx = WithRawToken(ctx, token)
		return handler(ctx, req)
	}
}

func extractBearer(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return ""
	}
	const prefix = "Bearer "
	v := values[0]
	if !strings.HasPrefix(v, prefix) {
		return ""
	}
	return strings.TrimSpace(v[len(prefix):])
}
