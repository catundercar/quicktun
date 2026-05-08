package grpcsvc

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
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

// OperatorService implements quicktunv1.OperatorServiceServer. Every RPC is
// admin-only (the interceptor authenticates; we additionally check IsAdmin
// in-handler so test-friendly direct calls don't bypass the rule). Safety
// guards refuse self-deletion and refuse demoting / deleting the last admin.
type OperatorService struct {
	quicktunv1.UnimplementedOperatorServiceServer
	operators *dao.OperatorDAO
	access    *dao.OperatorProjectAccessDAO
	projects  *dao.ProjectDAO
	audit     *audit.Writer
	lg        *zap.Logger
}

// NewOperatorService constructs an OperatorService. lg is optional (nil →
// no-op zap.Logger). audit is allowed to be nil for testing — log calls
// short-circuit via the helper below.
func NewOperatorService(
	operators *dao.OperatorDAO,
	access *dao.OperatorProjectAccessDAO,
	projects *dao.ProjectDAO,
	audit *audit.Writer,
	lg *zap.Logger,
) *OperatorService {
	if lg == nil {
		lg = zap.NewNop()
	}
	return &OperatorService{
		operators: operators,
		access:    access,
		projects:  projects,
		audit:     audit,
		lg:        lg,
	}
}

// formatOperatorName returns the canonical resource name for an operator id.
func formatOperatorName(id uint64) string {
	return "operators/" + strconv.FormatUint(id, 10)
}

// parseOperatorName parses "operators/{id}" → id. Accepts numeric ids only;
// rejects the empty string and any non-numeric tail.
func parseOperatorName(name string) (uint64, error) {
	const prefix = "operators/"
	if !strings.HasPrefix(name, prefix) {
		return 0, fmt.Errorf("operator name must be %q-prefixed", prefix)
	}
	tail := strings.TrimPrefix(name, prefix)
	if tail == "" || strings.Contains(tail, "/") {
		return 0, errors.New(`operator name must be "operators/{id}"`)
	}
	id, err := strconv.ParseUint(tail, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("operator id: %w", err)
	}
	return id, nil
}

func (s *OperatorService) requireAdmin(ctx context.Context) (*model.Operator, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if !op.IsAdmin {
		return nil, status.Error(codes.PermissionDenied, "admin role required")
	}
	return op, nil
}

func (s *OperatorService) writeAudit(ctx context.Context, e audit.Entry) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Log(ctx, e); err != nil {
		s.lg.Warn("audit write failed", zap.String("action", e.Action), zap.Error(err))
	}
}

// ListOperators returns the operator collection ordered by id. Admin-only.
func (s *OperatorService) ListOperators(ctx context.Context, req *quicktunv1.ListOperatorsRequest) (*quicktunv1.ListOperatorsResponse, error) {
	if _, err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}
	rows, err := s.operators.List(ctx, int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		if errors.Is(err, dao.ErrInvalidPageToken) {
			return nil, status.Error(codes.InvalidArgument, "invalid page_token")
		}
		return nil, status.Error(codes.Internal, "list operators: "+err.Error())
	}
	out := &quicktunv1.ListOperatorsResponse{
		Operators:     make([]*quicktunv1.Operator, len(rows)),
		NextPageToken: dao.NextOperatorPageToken(rows),
	}
	for i := range rows {
		out.Operators[i] = operatorToProto(&rows[i])
	}
	return out, nil
}

// GetOperator returns one operator by name. Admin-only (callers wanting their
// own profile use AuthService.WhoAmI).
func (s *OperatorService) GetOperator(ctx context.Context, req *quicktunv1.GetOperatorRequest) (*quicktunv1.Operator, error) {
	if _, err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}
	id, err := parseOperatorName(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	op, err := s.operators.FindByID(ctx, id)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "operator not found")
		}
		return nil, status.Error(codes.Internal, "lookup failed")
	}
	return operatorToProto(op), nil
}

// CreateOperator creates a new operator account. Admin-only.
func (s *OperatorService) CreateOperator(ctx context.Context, req *quicktunv1.CreateOperatorRequest) (*quicktunv1.Operator, error) {
	if _, err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}
	if req.GetOperator() == nil {
		return nil, status.Error(codes.InvalidArgument, "operator body is required")
	}
	if req.Operator.GetEmail() == "" {
		return nil, status.Error(codes.InvalidArgument, "operator.email is required")
	}
	if req.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "password is required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, status.Error(codes.Internal, "hash failed")
	}
	row, err := s.operators.Create(ctx, req.Operator.Email, string(hash), req.Operator.IsAdmin)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return nil, status.Error(codes.AlreadyExists, "operator email already exists")
		}
		return nil, status.Error(codes.Internal, "create failed")
	}

	s.writeAudit(ctx, audit.Entry{
		Action: "operator.create",
		Target: formatOperatorName(row.ID),
		Extra: map[string]any{
			"email":    row.Email,
			"is_admin": row.IsAdmin,
		},
	})

	return operatorToProto(row), nil
}

// UpdateOperator currently supports flipping is_admin and rotating password.
// Email is immutable here; create + delete to migrate.
func (s *OperatorService) UpdateOperator(ctx context.Context, req *quicktunv1.UpdateOperatorRequest) (*quicktunv1.Operator, error) {
	caller, err := s.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetOperator() == nil {
		return nil, status.Error(codes.InvalidArgument, "operator body is required")
	}
	id, err := parseOperatorName(req.Operator.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	target, err := s.operators.FindByID(ctx, id)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "operator not found")
		}
		return nil, status.Error(codes.Internal, "lookup failed")
	}

	mask := strings.Split(req.GetUpdateMask(), ",")
	for i := range mask {
		mask[i] = strings.TrimSpace(mask[i])
	}
	wantIsAdmin := false
	wantPassword := false
	for _, m := range mask {
		switch m {
		case "is_admin":
			wantIsAdmin = true
		case "password":
			wantPassword = true
		case "":
			// tolerate empty mask token
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unsupported update_mask field %q", m)
		}
	}
	if !wantIsAdmin && !wantPassword {
		return nil, status.Error(codes.InvalidArgument, "update_mask must list at least one of: is_admin, password")
	}

	if wantIsAdmin && req.Operator.IsAdmin != target.IsAdmin {
		// Safety guard: refuse demoting yourself.
		if caller.ID == target.ID && !req.Operator.IsAdmin {
			return nil, status.Error(codes.FailedPrecondition, "cannot remove admin role from your own account")
		}
		// Safety guard: refuse demoting the last admin.
		if target.IsAdmin && !req.Operator.IsAdmin {
			n, cerr := s.operators.CountAdmins(ctx)
			if cerr != nil {
				return nil, status.Error(codes.Internal, "count admins failed")
			}
			if n <= 1 {
				return nil, status.Error(codes.FailedPrecondition, "cannot demote the last admin")
			}
		}
		if err := s.operators.UpdateIsAdmin(ctx, target.ID, req.Operator.IsAdmin); err != nil {
			return nil, status.Error(codes.Internal, "update is_admin failed")
		}
		s.writeAudit(ctx, audit.Entry{
			Action: "operator.update.is_admin",
			Target: formatOperatorName(target.ID),
			Extra: map[string]any{
				"email":    target.Email,
				"is_admin": req.Operator.IsAdmin,
			},
		})
		target.IsAdmin = req.Operator.IsAdmin
	}

	if wantPassword {
		if req.GetPassword() == "" {
			return nil, status.Error(codes.InvalidArgument, "password is required when update_mask names password")
		}
		hash, herr := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if herr != nil {
			return nil, status.Error(codes.Internal, "hash failed")
		}
		if err := s.operators.UpdatePassword(ctx, target.ID, string(hash)); err != nil {
			return nil, status.Error(codes.Internal, "update password failed")
		}
		s.writeAudit(ctx, audit.Entry{
			Action: "operator.update.password",
			Target: formatOperatorName(target.ID),
			Extra: map[string]any{"email": target.Email},
		})
	}

	return operatorToProto(target), nil
}

// DeleteOperator soft-deletes one operator. Refuses self and last-admin.
func (s *OperatorService) DeleteOperator(ctx context.Context, req *quicktunv1.DeleteOperatorRequest) (*emptypb.Empty, error) {
	caller, err := s.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseOperatorName(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if caller.ID == id {
		return nil, status.Error(codes.FailedPrecondition, "cannot delete your own account")
	}
	target, err := s.operators.FindByID(ctx, id)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "operator not found")
		}
		return nil, status.Error(codes.Internal, "lookup failed")
	}
	if target.IsAdmin {
		n, cerr := s.operators.CountAdmins(ctx)
		if cerr != nil {
			return nil, status.Error(codes.Internal, "count admins failed")
		}
		if n <= 1 {
			return nil, status.Error(codes.FailedPrecondition, "cannot delete the last admin")
		}
	}
	if err := s.operators.Delete(ctx, target.ID); err != nil {
		return nil, status.Error(codes.Internal, "delete failed")
	}
	s.writeAudit(ctx, audit.Entry{
		Action: "operator.delete",
		Target: formatOperatorName(target.ID),
		Extra:  map[string]any{"email": target.Email},
	})
	return &emptypb.Empty{}, nil
}

// ListProjectAccess lists per-project access grants for one operator.
func (s *OperatorService) ListProjectAccess(ctx context.Context, req *quicktunv1.ListProjectAccessRequest) (*quicktunv1.ListProjectAccessResponse, error) {
	if _, err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}
	id, err := parseOperatorName(req.GetOperator())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := s.operators.FindByID(ctx, id); err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "operator not found")
		}
		return nil, status.Error(codes.Internal, "lookup failed")
	}

	rows, err := s.access.List(ctx, id)
	if err != nil {
		return nil, status.Error(codes.Internal, "list access failed")
	}
	out := &quicktunv1.ListProjectAccessResponse{
		Access: make([]*quicktunv1.OperatorProjectAccess, len(rows)),
	}
	for i := range rows {
		out.Access[i] = &quicktunv1.OperatorProjectAccess{
			Operator:    formatOperatorName(id),
			ProjectSlug: rows[i].ProjectSlug,
			Role:        string(rows[i].Role),
			GrantTime:   timestamppb.New(rows[i].CreatedAt),
		}
	}
	return out, nil
}

// GrantProjectAccess upserts an access grant.
func (s *OperatorService) GrantProjectAccess(ctx context.Context, req *quicktunv1.GrantProjectAccessRequest) (*quicktunv1.OperatorProjectAccess, error) {
	if _, err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}
	opID, err := parseOperatorName(req.GetOperator())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if req.GetProjectSlug() == "" {
		return nil, status.Error(codes.InvalidArgument, "project_slug is required")
	}
	role, err := parseProjectRole(req.GetRole())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := s.operators.FindByID(ctx, opID); err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "operator not found")
		}
		return nil, status.Error(codes.Internal, "operator lookup failed")
	}
	p, err := s.projects.FindBySlug(ctx, req.ProjectSlug)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "project not found")
		}
		return nil, status.Error(codes.Internal, "project lookup failed")
	}

	row, err := s.access.Grant(ctx, opID, p.ID, role)
	if err != nil {
		return nil, status.Error(codes.Internal, "grant failed")
	}
	s.writeAudit(ctx, audit.Entry{
		ProjectID: ptrUint64(p.ID),
		Action:    "operator.access.grant",
		Target:    formatOperatorName(opID),
		Extra: map[string]any{
			"project_slug": p.Slug,
			"role":         string(role),
		},
	})

	return &quicktunv1.OperatorProjectAccess{
		Operator:    formatOperatorName(opID),
		ProjectSlug: p.Slug,
		Role:        string(row.Role),
		GrantTime:   timestamppb.New(row.CreatedAt),
	}, nil
}

// RevokeProjectAccess removes one (operator, project) grant. Idempotent.
func (s *OperatorService) RevokeProjectAccess(ctx context.Context, req *quicktunv1.RevokeProjectAccessRequest) (*emptypb.Empty, error) {
	if _, err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}
	opID, err := parseOperatorName(req.GetOperator())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if req.GetProjectSlug() == "" {
		return nil, status.Error(codes.InvalidArgument, "project_slug is required")
	}
	p, err := s.projects.FindBySlug(ctx, req.ProjectSlug)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "project not found")
		}
		return nil, status.Error(codes.Internal, "project lookup failed")
	}
	if err := s.access.Revoke(ctx, opID, p.ID); err != nil {
		return nil, status.Error(codes.Internal, "revoke failed")
	}
	s.writeAudit(ctx, audit.Entry{
		ProjectID: ptrUint64(p.ID),
		Action:    "operator.access.revoke",
		Target:    formatOperatorName(opID),
		Extra:     map[string]any{"project_slug": p.Slug},
	})
	return &emptypb.Empty{}, nil
}

func parseProjectRole(s string) (model.ProjectRole, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "owner":
		return model.ProjectRoleOwner, nil
	case "operator":
		return model.ProjectRoleOperator, nil
	case "viewer":
		return model.ProjectRoleViewer, nil
	}
	return "", fmt.Errorf("invalid role %q (want viewer | operator | owner)", s)
}
