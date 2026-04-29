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
type ctxKey struct{}

var operatorKey ctxKey

// OperatorFromContext returns the authenticated operator attached by
// NewUnaryInterceptor. Returns nil if not authenticated (e.g. in the Login
// path, which is unauthenticated).
func OperatorFromContext(ctx context.Context) *model.Operator {
	op, _ := ctx.Value(operatorKey).(*model.Operator)
	return op
}

// OperatorContextKey returns the unexported context key used by this package
// to store the authenticated operator. Tests and adjacent packages can use it
// with context.WithValue. Production code should prefer NewUnaryInterceptor
// which sets the value, plus OperatorFromContext to read it.
func OperatorContextKey() any { return operatorKey }

// NewUnaryInterceptor returns a grpc.UnaryServerInterceptor that:
//   - lets requests for any FullMethod listed in `unauth` proceed without auth
//   - extracts a Bearer token from the "authorization" gRPC metadata header
//   - validates it via Validator and attaches the operator to ctx on success
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
		token := extractBearer(ctx)
		if token == "" {
			return nil, status.Error(codes.Unauthenticated, "missing bearer token")
		}
		op, err := v.Validate(ctx, token)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}
		ctx = context.WithValue(ctx, operatorKey, op)
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
