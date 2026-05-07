package auth

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"github.com/tulip/quicktun/internal/metrics"
)

// MetricsInterceptor returns a grpc.UnaryServerInterceptor that records each
// RPC's full method name + status code into m. Safe to call with m == nil
// (the interceptor becomes a pass-through).
//
// The interceptor is intentionally placed near the head of the chain so it
// observes errors injected by downstream interceptors (auth, agent auth)
// — including the codes.Unauthenticated path. This makes the resulting
// quicktun_server_requests_total{code="Unauthenticated"} a useful signal
// for credential brute-force attempts.
func MetricsInterceptor(m *metrics.ServerMetrics) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		if m != nil {
			code := status.Code(err).String()
			m.ObserveRequest(info.FullMethod, code, time.Since(start).Seconds())
		}
		return resp, err
	}
}
