package grpcsvc

import (
	"context"
	"errors"
	"strconv"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/audit"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
)

// ServiceAccountService implements quicktunv1.ServiceAccountServiceServer.
// All RPCs are admin-only — SA tokens grant operator-equivalent access, so
// only admins may issue them. Self-management ("can an operator mint an
// SA token bound to themselves?") is deliberately deferred to a later
// phase; for now the surface is simple and easy to audit.
type ServiceAccountService struct {
	quicktunv1.UnimplementedServiceAccountServiceServer
	tokens    *dao.ServiceAccountTokenDAO
	operators *dao.OperatorDAO
	audit     *audit.Writer
	lg        *zap.Logger
}

// NewServiceAccountService constructs a ServiceAccountService. lg may be
// nil (a no-op logger is substituted). audit may be nil for tests; the
// helper short-circuits writes when nil.
func NewServiceAccountService(
	tokens *dao.ServiceAccountTokenDAO,
	operators *dao.OperatorDAO,
	auditWriter *audit.Writer,
	lg *zap.Logger,
) *ServiceAccountService {
	if lg == nil {
		lg = zap.NewNop()
	}
	return &ServiceAccountService{
		tokens:    tokens,
		operators: operators,
		audit:     auditWriter,
		lg:        lg,
	}
}

func (s *ServiceAccountService) requireAdmin(ctx context.Context) (*model.Operator, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if !op.IsAdmin {
		return nil, status.Error(codes.PermissionDenied, "admin role required")
	}
	return op, nil
}

func (s *ServiceAccountService) writeAudit(ctx context.Context, e audit.Entry) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Log(ctx, e); err != nil {
		s.lg.Warn("audit write failed", zap.String("action", e.Action), zap.Error(err))
	}
}

// CreateServiceAccountToken issues a fresh SA token for the operator
// named in req.Operator. The raw token is returned exactly once; the
// server stores only the SHA-256 hash.
func (s *ServiceAccountService) CreateServiceAccountToken(ctx context.Context, req *quicktunv1.CreateServiceAccountTokenRequest) (*quicktunv1.ServiceAccountTokenWithRaw, error) {
	if _, err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}
	opID, err := parseOperatorName(req.GetOperator())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := s.operators.FindByID(ctx, opID); err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "operator not found")
		}
		return nil, status.Error(codes.Internal, "operator lookup failed")
	}

	ttl := time.Duration(req.GetTtlSeconds()) * time.Second
	rec, raw, err := s.tokens.Issue(ctx, opID, req.GetDescription(), ttl)
	if err != nil {
		return nil, status.Error(codes.Internal, "issue sa token failed")
	}
	s.writeAudit(ctx, audit.Entry{
		Action: "sa_token.create",
		Target: formatSATokenName(rec.ID),
		Extra: map[string]any{
			"operator":    req.GetOperator(),
			"description": req.GetDescription(),
			"ttl_seconds": req.GetTtlSeconds(),
		},
	})
	return &quicktunv1.ServiceAccountTokenWithRaw{
		Token: saTokenToProto(rec),
		Raw:   raw,
	}, nil
}

// ListServiceAccountTokens returns metadata for every SA token bound to
// the operator named in req.Operator (active + revoked).
func (s *ServiceAccountService) ListServiceAccountTokens(ctx context.Context, req *quicktunv1.ListServiceAccountTokensRequest) (*quicktunv1.ListServiceAccountTokensResponse, error) {
	if _, err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}
	opID, err := parseOperatorName(req.GetOperator())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := s.operators.FindByID(ctx, opID); err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "operator not found")
		}
		return nil, status.Error(codes.Internal, "operator lookup failed")
	}
	rows, err := s.tokens.ListByOperator(ctx, opID)
	if err != nil {
		return nil, status.Error(codes.Internal, "list sa tokens failed")
	}
	out := &quicktunv1.ListServiceAccountTokensResponse{
		Tokens: make([]*quicktunv1.ServiceAccountToken, len(rows)),
	}
	for i := range rows {
		out.Tokens[i] = saTokenToProto(&rows[i])
	}
	return out, nil
}

// RevokeServiceAccountToken marks the SA token at id as revoked.
// Idempotent: a missing or already-revoked token returns OK.
func (s *ServiceAccountService) RevokeServiceAccountToken(ctx context.Context, req *quicktunv1.RevokeServiceAccountTokenRequest) (*emptypb.Empty, error) {
	if _, err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}
	id := req.GetId()
	if id == 0 {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	// Best-effort lookup so we can record the operator name in audit; it's
	// fine if this fails (idempotent revoke for already-deleted tokens).
	rec, lookupErr := s.tokens.FindByID(ctx, id)
	if lookupErr != nil && !errors.Is(lookupErr, dao.ErrInvalidPageToken) && !dao.IsNotFound(lookupErr) {
		return nil, status.Error(codes.Internal, "lookup sa token failed")
	}
	if err := s.tokens.Revoke(ctx, id); err != nil {
		return nil, status.Error(codes.Internal, "revoke sa token failed")
	}
	extra := map[string]any{}
	if rec != nil {
		extra["operator"] = formatOperatorName(rec.OperatorID)
		extra["description"] = rec.Description
	}
	s.writeAudit(ctx, audit.Entry{
		Action: "sa_token.revoke",
		Target: formatSATokenName(id),
		Extra:  extra,
	})
	return &emptypb.Empty{}, nil
}

// formatSATokenName renders an SA-token id into a stable resource name
// for audit. There is no SA-token gRPC GET endpoint, so we don't strictly
// need a resource format, but using a uniform "serviceAccountTokens/{id}"
// form keeps audit log targets consistent with the rest of the API.
func formatSATokenName(id uint64) string {
	return "serviceAccountTokens/" + strconv.FormatUint(id, 10)
}

// saTokenToProto converts a *model.ServiceAccountToken to its proto view,
// substituting nil for unset optional timestamps so JSON output omits them.
func saTokenToProto(rec *model.ServiceAccountToken) *quicktunv1.ServiceAccountToken {
	out := &quicktunv1.ServiceAccountToken{
		Id:          rec.ID,
		Operator:    formatOperatorName(rec.OperatorID),
		Description: rec.Description,
		CreateTime:  timestamppb.New(rec.CreatedAt),
	}
	if rec.ExpiresAt != nil {
		out.ExpireTime = timestamppb.New(*rec.ExpiresAt)
	}
	if rec.LastUsedAt != nil {
		out.LastUsedTime = timestamppb.New(*rec.LastUsedAt)
	}
	if rec.RevokedAt != nil {
		out.RevokeTime = timestamppb.New(*rec.RevokedAt)
	}
	return out
}
