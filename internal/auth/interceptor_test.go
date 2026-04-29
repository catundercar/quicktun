package auth_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/model"
)

type stubValidator struct{ op *model.Operator; err error }

func (s *stubValidator) Validate(ctx context.Context, raw string) (*model.Operator, error) {
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
