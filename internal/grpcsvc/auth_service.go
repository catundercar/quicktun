// Package grpcsvc contains gRPC service implementations.
package grpcsvc

import (
	"context"
	"time"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
)

// AuthService implements quicktunv1.AuthServiceServer.
type AuthService struct {
	quicktunv1.UnimplementedAuthServiceServer
	ops      *dao.OperatorDAO
	sessions *dao.SessionDAO
	ttl      time.Duration
}

// NewAuthService constructs an AuthService.
func NewAuthService(ops *dao.OperatorDAO, sessions *dao.SessionDAO, sessionTTL time.Duration) *AuthService {
	return &AuthService{ops: ops, sessions: sessions, ttl: sessionTTL}
}

// Login implements quicktunv1.AuthServiceServer.
func (s *AuthService) Login(ctx context.Context, req *quicktunv1.LoginRequest) (*quicktunv1.LoginResponse, error) {
	if req.GetEmail() == "" || req.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "email and password are required")
	}
	op, err := s.ops.FindByEmail(ctx, req.Email)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.Unauthenticated, "invalid credentials")
		}
		return nil, status.Error(codes.Internal, "lookup failed")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(op.PasswordHash), []byte(req.Password)); err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid credentials")
	}

	ua, ip := callerMetadata(ctx)
	rec, raw, err := s.sessions.Issue(ctx, op.ID, s.ttl, ua, ip)
	if err != nil {
		return nil, status.Error(codes.Internal, "session issue failed")
	}

	return &quicktunv1.LoginResponse{
		AccessToken: raw,
		ExpireTime:  timestamppb.New(rec.ExpiresAt),
		Operator:    operatorToProto(op),
	}, nil
}

// Logout implements quicktunv1.AuthServiceServer. Phase 1 ignores
// LogoutRequest.session_name and revokes the caller's current session.
func (s *AuthService) Logout(ctx context.Context, _ *quicktunv1.LogoutRequest) (*emptypb.Empty, error) {
	if auth.OperatorFromContext(ctx) == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if raw := auth.RawTokenFromContext(ctx); raw != "" {
		if err := s.sessions.RevokeByToken(ctx, raw); err != nil {
			return nil, status.Error(codes.Internal, "revoke failed")
		}
	}
	return &emptypb.Empty{}, nil
}

// WhoAmI implements quicktunv1.AuthServiceServer.
func (s *AuthService) WhoAmI(ctx context.Context, _ *quicktunv1.WhoAmIRequest) (*quicktunv1.WhoAmIResponse, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	return &quicktunv1.WhoAmIResponse{Operator: operatorToProto(op)}, nil
}

func operatorToProto(op *model.Operator) *quicktunv1.Operator {
	return &quicktunv1.Operator{
		Name:       "operators/" + uint64ToString(op.ID),
		OperatorId: uint64ToString(op.ID),
		CreateTime: timestamppb.New(op.CreatedAt),
		Email:      op.Email,
		IsAdmin:    op.IsAdmin,
	}
}

func uint64ToString(v uint64) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = digits[v%10]
		v /= 10
	}
	return string(b[i:])
}

func callerMetadata(ctx context.Context) (userAgent, sourceIP string) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", ""
	}
	if v := md.Get("user-agent"); len(v) > 0 {
		userAgent = v[0]
	}
	if v := md.Get("x-forwarded-for"); len(v) > 0 {
		sourceIP = v[0]
	}
	return userAgent, sourceIP
}
