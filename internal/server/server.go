// Package server boots the quicktun control-plane gRPC server and the
// grpc-gateway HTTP/JSON sidecar. It glues together dao, auth, and
// grpcsvc into a runnable service.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/audit"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/grpcsvc"
)

// Config bundles construction parameters for Server.
type Config struct {
	DB         *gorm.DB
	Logger     *zap.Logger
	GRPCListen string
	HTTPListen string
	SessionTTL time.Duration
}

// Server runs the gRPC server and grpc-gateway HTTP server side-by-side.
type Server struct {
	cfg        Config
	grpcServer *grpc.Server
	httpServer *http.Server
}

// New builds a Server but does not yet bind any listeners.
func New(cfg Config) (*Server, error) {
	if cfg.DB == nil {
		return nil, errors.New("server: cfg.DB is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 8 * time.Hour
	}

	ops := dao.NewOperatorDAO(cfg.DB)
	sessions := dao.NewSessionDAO(cfg.DB)
	authSvc := grpcsvc.NewAuthService(ops, sessions, cfg.SessionTTL)

	intc := auth.NewUnaryInterceptor(sessions, "/quicktun.v1.AuthService/Login")
	gs := grpc.NewServer(grpc.UnaryInterceptor(intc))
	quicktunv1.RegisterAuthServiceServer(gs, authSvc)

	auditWriter := audit.NewWriter(cfg.DB)
	projectSvc := grpcsvc.NewProjectService(dao.NewProjectDAO(cfg.DB), auditWriter)
	quicktunv1.RegisterProjectServiceServer(gs, projectSvc)

	return &Server{cfg: cfg, grpcServer: gs}, nil
}

// Run binds both listeners and serves until ctx is cancelled or one of the
// servers errors. On shutdown it stops both servers and returns the first
// non-nil error.
func (s *Server) Run(ctx context.Context) error {
	grpcLn, err := net.Listen("tcp", s.cfg.GRPCListen)
	if err != nil {
		return fmt.Errorf("server: listen grpc %q: %w", s.cfg.GRPCListen, err)
	}

	gatewayMux := runtime.NewServeMux(runtime.WithErrorHandler(gatewayErrorHandler))
	dialOpts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := quicktunv1.RegisterAuthServiceHandlerFromEndpoint(ctx, gatewayMux, s.cfg.GRPCListen, dialOpts); err != nil {
		grpcLn.Close()
		return fmt.Errorf("server: register gateway: %w", err)
	}
	if err := quicktunv1.RegisterProjectServiceHandlerFromEndpoint(ctx, gatewayMux, s.cfg.GRPCListen, dialOpts); err != nil {
		grpcLn.Close()
		return fmt.Errorf("server: register project gateway: %w", err)
	}

	s.httpServer = &http.Server{
		Addr:              s.cfg.HTTPListen,
		Handler:           recoverHandler(s.cfg.Logger, gatewayMux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() {
		s.cfg.Logger.Info("grpc listening", zap.String("addr", grpcLn.Addr().String()))
		if err := s.grpcServer.Serve(grpcLn); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errCh <- fmt.Errorf("grpc serve: %w", err)
		} else {
			errCh <- nil
		}
	}()
	go func() {
		s.cfg.Logger.Info("http (gateway) listening", zap.String("addr", s.cfg.HTTPListen))
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http serve: %w", err)
		} else {
			errCh <- nil
		}
	}()

	var firstErr error
	select {
	case <-ctx.Done():
	case e := <-errCh:
		firstErr = e
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.httpServer.Shutdown(shutdownCtx); err != nil && firstErr == nil {
		firstErr = err
	}
	s.grpcServer.GracefulStop()

	// Drain remaining channel to avoid goroutine leak.
	go func() { <-errCh }()
	return firstErr
}

// gatewayErrorHandler is a grpc-gateway error handler that augments the
// default JSON error body with a human-readable "status" field containing the
// gRPC code name (e.g. "UNAUTHENTICATED"), ensuring API clients and tests can
// identify the error type by string rather than numeric code.
func gatewayErrorHandler(ctx context.Context, mux *runtime.ServeMux, marshaler runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
	s := status.Convert(err)
	httpStatus := runtime.HTTPStatusFromCode(s.Code())

	body := map[string]any{
		"code":    int32(s.Code()),
		"status":  s.Code().String(), // e.g. "Unauthenticated"
		"message": s.Message(),
	}

	w.Header().Set("Content-Type", "application/json")
	if s.Code() == codes.Unauthenticated {
		// RFC 9110 §11.6.1: WWW-Authenticate must name an auth scheme.
		w.Header().Set("WWW-Authenticate", `Bearer realm="quicktun"`)
	}
	w.WriteHeader(httpStatus)

	if b, encErr := json.Marshal(body); encErr == nil {
		w.Write(b) //nolint:errcheck
	}
}

func recoverHandler(lg *zap.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				lg.Error("http panic", zap.Any("recover", rec), zap.ByteString("stack", debug.Stack()))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		h.ServeHTTP(w, r)
	})
}
