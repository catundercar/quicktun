package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/model"
)

type stubValidator struct{ op *model.Operator; err error }

func (s *stubValidator) Validate(ctx context.Context, raw string) (*model.Operator, error) {
	return s.op, s.err
}

type stubSATokens struct {
	opID uint64
	err  error
}

func (s *stubSATokens) ValidateRaw(ctx context.Context, raw string) (uint64, error) {
	return s.opID, s.err
}

type stubOperators struct {
	op  *model.Operator
	err error
}

func (s *stubOperators) FindByID(ctx context.Context, id uint64) (*model.Operator, error) {
	return s.op, s.err
}

func TestInterceptorAllowsLoginUnauth(t *testing.T) {
	intc := auth.NewUnaryInterceptor(&stubValidator{}, "/quicktun.v1.AuthService/Login")
	called := false
	handler := func(ctx context.Context, req any) (any, error) { called = true; return "ok", nil }

	resp, err := intc(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicktun.v1.AuthService/Login"},
		handler)
	require.NoError(t, err)
	require.Equal(t, "ok", resp)
	require.True(t, called)
}

func TestInterceptorRejectsMissingToken(t *testing.T) {
	intc := auth.NewUnaryInterceptor(&stubValidator{}, "/quicktun.v1.AuthService/Login")
	handler := func(ctx context.Context, req any) (any, error) { return nil, nil }

	_, err := intc(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicktun.v1.SomethingElse/Get"},
		handler)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Unauthenticated, st.Code())
}

func TestInterceptorAcceptsValidToken(t *testing.T) {
	op := &model.Operator{Base: model.Base{ID: 42}, Email: "x@y.com"}
	intc := auth.NewUnaryInterceptor(&stubValidator{op: op}, "/quicktun.v1.AuthService/Login")
	var seen *model.Operator
	handler := func(ctx context.Context, req any) (any, error) {
		seen = auth.OperatorFromContext(ctx)
		return "ok", nil
	}

	md := metadata.Pairs("authorization", "Bearer raw-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := intc(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicktun.v1.SiteService/Get"},
		handler)
	require.NoError(t, err)
	require.NotNil(t, seen)
	require.Equal(t, uint64(42), seen.ID)
}

func TestInterceptorRejectsBadToken(t *testing.T) {
	intc := auth.NewUnaryInterceptor(
		&stubValidator{err: status.Error(codes.Unauthenticated, "invalid")},
		"/quicktun.v1.AuthService/Login")
	handler := func(ctx context.Context, req any) (any, error) { return nil, nil }

	md := metadata.Pairs("authorization", "Bearer bad")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := intc(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicktun.v1.SiteService/Get"},
		handler)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())
}

// When the session validator fails but the SA-token validator succeeds,
// the request should be authenticated as the SA-bound operator. This is
// the dual-auth path for CI/CD tokens.
func TestInterceptorAcceptsSAToken(t *testing.T) {
	op := &model.Operator{Base: model.Base{ID: 7}, Email: "ci@x.com"}
	intc := auth.NewUnaryInterceptorWithOptions(
		&stubValidator{err: errors.New("session miss")},
		auth.InterceptorOptions{
			SATokens:  &stubSATokens{opID: 7},
			Operators: &stubOperators{op: op},
			Logger:    zap.NewNop(),
			Unauth:    []string{"/quicktun.v1.AuthService/Login"},
		},
	)
	var seen *model.Operator
	handler := func(ctx context.Context, req any) (any, error) {
		seen = auth.OperatorFromContext(ctx)
		return "ok", nil
	}

	md := metadata.Pairs("authorization", "Bearer qt_sat_xyz")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := intc(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicktun.v1.SiteService/Get"},
		handler)
	require.NoError(t, err)
	require.NotNil(t, seen)
	require.Equal(t, uint64(7), seen.ID)
}

// When both session and SA-token validators fail, the request is rejected
// with Unauthenticated (and an internal warn log is emitted — covered by
// the absence of a panic + the rejection).
func TestInterceptorRejectsWhenBothPathsFail(t *testing.T) {
	intc := auth.NewUnaryInterceptorWithOptions(
		&stubValidator{err: errors.New("session miss")},
		auth.InterceptorOptions{
			SATokens:  &stubSATokens{err: gorm.ErrRecordNotFound},
			Operators: &stubOperators{},
			Logger:    zap.NewNop(),
			Unauth:    []string{"/quicktun.v1.AuthService/Login"},
		},
	)
	handler := func(ctx context.Context, req any) (any, error) { return nil, nil }

	md := metadata.Pairs("authorization", "Bearer junk")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := intc(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicktun.v1.SiteService/Get"},
		handler)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())
}
