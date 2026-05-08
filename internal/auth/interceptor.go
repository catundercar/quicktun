package auth

import (
	"context"
	"errors"
	"strings"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/model"
)

// Validator validates a raw bearer token and returns the owning operator.
// Implemented by *dao.SessionDAO; receiving an interface here keeps the
// interceptor unit-testable without a database.
type Validator interface {
	Validate(ctx context.Context, rawToken string) (*model.Operator, error)
}

// SATokenValidator validates a raw service-account bearer token and
// returns the owning operator id. Implemented by
// *dao.ServiceAccountTokenDAO. Returning gorm.ErrRecordNotFound (wrapped)
// signals an unknown / expired / revoked token; any other error is logged
// as an internal failure.
type SATokenValidator interface {
	ValidateRaw(ctx context.Context, raw string) (uint64, error)
}

// OperatorLoader resolves an operator id to a *model.Operator. Implemented
// by *dao.OperatorDAO. Used by the SA-token branch to attach the same
// shape of principal as the session branch.
type OperatorLoader interface {
	FindByID(ctx context.Context, id uint64) (*model.Operator, error)
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

// InterceptorOptions tunes NewUnaryInterceptor. Zero values are safe.
type InterceptorOptions struct {
	// SATokens enables the service-account-token branch. When nil, only
	// session tokens are accepted.
	SATokens SATokenValidator
	// Operators is required when SATokens is set, so the interceptor can
	// load the principal *model.Operator for the SA branch (mirroring the
	// shape attached by the session branch).
	Operators OperatorLoader
	// Logger is optional; nil → no-op.
	Logger *zap.Logger
	// Unauth is the list of FullMethod values that bypass authentication.
	Unauth []string
}

// NewUnaryInterceptor returns a grpc.UnaryServerInterceptor that:
//   - lets requests for any FullMethod listed in `unauth` proceed without auth
//   - extracts a Bearer token from the "authorization" gRPC metadata header
//   - validates it via Validator and attaches operator + raw token to ctx
//   - returns codes.Unauthenticated on missing or invalid token
//
// Variadic `unauth` is preserved for backwards compatibility — older call
// sites pass only the session validator + a few unauth method names. Use
// NewUnaryInterceptorWithOptions for the SA-token-aware form.
func NewUnaryInterceptor(v Validator, unauth ...string) grpc.UnaryServerInterceptor {
	return NewUnaryInterceptorWithOptions(v, InterceptorOptions{Unauth: unauth})
}

// NewUnaryInterceptorWithOptions is the explicit constructor that wires in
// SA-token validation alongside session validation.
func NewUnaryInterceptorWithOptions(v Validator, opts InterceptorOptions) grpc.UnaryServerInterceptor {
	lg := opts.Logger
	if lg == nil {
		lg = zap.NewNop()
	}
	allowlist := make(map[string]struct{}, len(opts.Unauth))
	for _, m := range opts.Unauth {
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

		// Try the session path first — that's the common case (web UI,
		// CLI). Don't log on the failure path; we may still succeed via
		// the SA-token branch.
		op, sessErr := v.Validate(ctx, token)
		if sessErr == nil {
			ctx = WithOperator(ctx, op)
			ctx = WithRawToken(ctx, token)
			return handler(ctx, req)
		}

		// Fall through to the SA-token branch only when both validators
		// are configured — older call sites that pass only a session
		// validator behave exactly as before.
		if opts.SATokens != nil && opts.Operators != nil {
			opID, saErr := opts.SATokens.ValidateRaw(ctx, token)
			if saErr == nil {
				op, lerr := opts.Operators.FindByID(ctx, opID)
				if lerr != nil {
					if errors.Is(lerr, gorm.ErrRecordNotFound) {
						lg.Warn("auth: sa token resolved to missing operator",
							zap.Uint64("operator_id", opID))
						return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
					}
					lg.Warn("auth: sa token operator lookup failed", zap.Error(lerr))
					return nil, status.Error(codes.Internal, "internal error")
				}
				ctx = WithOperator(ctx, op)
				ctx = WithRawToken(ctx, token)
				return handler(ctx, req)
			}
			// Both branches failed: log once at warn level so the operator
			// can correlate auth misses without spamming for each session
			// miss.
			lg.Warn("auth: bearer token rejected by both session and SA paths",
				zap.NamedError("session_err", sessErr),
				zap.NamedError("sa_err", saErr))
		}
		return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
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
