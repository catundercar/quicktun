package grpcsvc

import (
	"context"
	"errors"
	"strings"

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
	"github.com/tulip/quicktun/internal/resource"
)

// RelayManager is the subset of relay.Manager that gRPC services depend on.
// Defined here so tests can supply a stub without importing the relay package.
type RelayManager interface {
	AddProject(ctx context.Context, projectID uint64) error
	RemoveProject(ctx context.Context, projectID uint64) error
	Refresh(ctx context.Context, projectID uint64) error
	SupervisorCount() int
}

// noopRelayManager satisfies RelayManager but does nothing. Used by services
// constructed without an explicit manager (typically tests).
type noopRelayManager struct{}

func (noopRelayManager) AddProject(context.Context, uint64) error    { return nil }
func (noopRelayManager) RemoveProject(context.Context, uint64) error { return nil }
func (noopRelayManager) Refresh(context.Context, uint64) error       { return nil }
func (noopRelayManager) SupervisorCount() int                        { return 0 }

// ProjectService implements quicktunv1.ProjectServiceServer.
type ProjectService struct {
	quicktunv1.UnimplementedProjectServiceServer
	projects *dao.ProjectDAO
	audit    *audit.Writer
	lg       *zap.Logger
	relay    RelayManager
}

// NewProjectService constructs a ProjectService. If lg is nil a no-op logger
// is substituted; if relay is nil a no-op manager is substituted.
func NewProjectService(projects *dao.ProjectDAO, audit *audit.Writer, lg *zap.Logger, relay RelayManager) *ProjectService {
	if lg == nil {
		lg = zap.NewNop()
	}
	if relay == nil {
		relay = noopRelayManager{}
	}
	return &ProjectService{projects: projects, audit: audit, lg: lg, relay: relay}
}

// GetProject implements quicktunv1.ProjectServiceServer.
func (s *ProjectService) GetProject(ctx context.Context, req *quicktunv1.GetProjectRequest) (*quicktunv1.Project, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	slug, err := resource.ParseProjectName(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	p, err := s.projects.FindBySlug(ctx, slug)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "project not found")
		}
		return nil, status.Error(codes.Internal, "lookup failed")
	}
	if !op.IsAdmin {
		if !s.hasAccess(ctx, op.ID, p.ID) {
			return nil, status.Error(codes.NotFound, "project not found")
		}
	}
	return projectToProto(p), nil
}

// ListProjects implements quicktunv1.ProjectServiceServer.
//
// Admins see every project; non-admins see only projects they have an access
// grant on.
func (s *ProjectService) ListProjects(ctx context.Context, req *quicktunv1.ListProjectsRequest) (*quicktunv1.ListProjectsResponse, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	pageSize := int(req.GetPage().GetPageSize())
	pageToken := req.GetPage().GetPageToken()

	var rows []model.Project
	var err error
	if op.IsAdmin {
		rows, err = s.projects.List(ctx, pageSize, pageToken)
	} else {
		rows, err = s.projects.ListAccessible(ctx, op.ID, pageSize, pageToken)
	}
	if err != nil {
		if errors.Is(err, dao.ErrInvalidPageToken) {
			return nil, status.Error(codes.InvalidArgument, "invalid page_token")
		}
		return nil, status.Error(codes.Internal, "list failed")
	}

	out := &quicktunv1.ListProjectsResponse{
		Projects: make([]*quicktunv1.Project, len(rows)),
		Page:     &quicktunv1.PageResponse{NextPageToken: dao.NextProjectPageToken(rows)},
	}
	for i := range rows {
		out.Projects[i] = projectToProto(&rows[i])
	}
	return out, nil
}

func (s *ProjectService) hasAccess(ctx context.Context, operatorID, projectID uint64) bool {
	var count int64
	err := s.projects.Db().WithContext(ctx).
		Model(&model.OperatorProjectAccess{}).
		Where("operator_id = ? AND project_id = ?", operatorID, projectID).
		Count(&count).Error
	return err == nil && count > 0
}

func projectToProto(p *model.Project) *quicktunv1.Project {
	out := &quicktunv1.Project{
		Name:           resource.FormatProjectName(p.Slug),
		ProjectId:      p.Slug,
		CreateTime:     timestamppb.New(p.CreatedAt),
		UpdateTime:     timestamppb.New(p.UpdatedAt),
		DisplayName:    p.Name,
		RelayPortRange: p.RelayPortRange,
		Status:         projectStatusToProto(p.Status),
		DefaultMode:    siteModeToProto(p.DefaultMode),
		Backend:        backendToProto(p.Backend),
	}
	return out
}

func projectStatusToProto(s model.ProjectStatus) quicktunv1.ProjectStatus {
	switch s {
	case model.ProjectStatusActive:
		return quicktunv1.ProjectStatus_PROJECT_STATUS_ACTIVE
	case model.ProjectStatusDisabled:
		return quicktunv1.ProjectStatus_PROJECT_STATUS_DISABLED
	}
	return quicktunv1.ProjectStatus_PROJECT_STATUS_UNSPECIFIED
}

func siteModeToProto(m model.SiteMode) quicktunv1.SiteMode {
	switch m {
	case model.SiteModeEndpoint:
		return quicktunv1.SiteMode_SITE_MODE_ENDPOINT
	case model.SiteModeSubnet:
		return quicktunv1.SiteMode_SITE_MODE_SUBNET
	}
	return quicktunv1.SiteMode_SITE_MODE_UNSPECIFIED
}

func backendToProto(b model.Backend) quicktunv1.Backend {
	switch b {
	case model.BackendRathole:
		return quicktunv1.Backend_BACKEND_RATHOLE
	case model.BackendNetbird:
		return quicktunv1.Backend_BACKEND_NETBIRD
	}
	return quicktunv1.Backend_BACKEND_UNSPECIFIED
}

// CreateProject implements quicktunv1.ProjectServiceServer.
func (s *ProjectService) CreateProject(ctx context.Context, req *quicktunv1.CreateProjectRequest) (*quicktunv1.Project, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if !op.IsAdmin {
		return nil, status.Error(codes.PermissionDenied, "admin role required")
	}
	if req.GetProject() == nil {
		return nil, status.Error(codes.InvalidArgument, "project body is required")
	}
	if err := resource.ValidateSlug(req.GetProjectId()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if req.Project.GetDisplayName() == "" {
		return nil, status.Error(codes.InvalidArgument, "project.display_name is required")
	}
	if req.Project.GetRelayPortRange() == "" {
		return nil, status.Error(codes.InvalidArgument, "project.relay_port_range is required")
	}
	minP, maxP, err := resource.ParsePortRange(req.Project.RelayPortRange)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "relay_port_range: %v", err)
	}
	overlap, err := s.projects.OverlapsAny(ctx, minP, maxP, 0)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "overlap check: %v", err)
	}
	if overlap {
		return nil, status.Errorf(codes.FailedPrecondition,
			"relay_port_range %q overlaps another project's range", req.Project.RelayPortRange)
	}

	row := &model.Project{
		Slug:           req.ProjectId,
		Name:           req.Project.DisplayName,
		RelayPortRange: req.Project.RelayPortRange,
		DefaultMode:    siteModeFromProto(req.Project.DefaultMode),
		Backend:        backendFromProto(req.Project.Backend),
		Status:         model.ProjectStatusActive,
	}
	if row.DefaultMode == "" {
		row.DefaultMode = model.SiteModeEndpoint
	}
	if row.Backend == "" {
		row.Backend = model.BackendRathole
	}

	if _, err := s.projects.Create(ctx, row); err != nil {
		// SQLite returns "UNIQUE constraint failed: projects.slug" on dup.
		if isUniqueConstraintErr(err) {
			return nil, status.Error(codes.AlreadyExists, "project slug already exists")
		}
		return nil, status.Error(codes.Internal, "create failed")
	}

	if err := s.audit.Log(ctx, audit.Entry{
		ProjectID: ptrUint64(row.ID),
		Action:    "project.create",
		Target:    resource.FormatProjectName(row.Slug),
		Extra: map[string]any{
			"display_name":     row.Name,
			"relay_port_range": row.RelayPortRange,
		},
	}); err != nil {
		// Audit failure is non-fatal — log but do not unwind the create.
		// Production would emit a metric here; Phase 1 swallows.
		_ = err
	}

	if err := s.relay.AddProject(ctx, row.ID); err != nil {
		s.lg.Warn("relay refresh failed",
			zap.Uint64("project_id", row.ID),
			zap.String("op", "project.create"),
			zap.Error(err))
	}

	return projectToProto(row), nil
}

func siteModeFromProto(m quicktunv1.SiteMode) model.SiteMode {
	switch m {
	case quicktunv1.SiteMode_SITE_MODE_ENDPOINT:
		return model.SiteModeEndpoint
	case quicktunv1.SiteMode_SITE_MODE_SUBNET:
		return model.SiteModeSubnet
	}
	return ""
}

func backendFromProto(b quicktunv1.Backend) model.Backend {
	switch b {
	case quicktunv1.Backend_BACKEND_RATHOLE:
		return model.BackendRathole
	case quicktunv1.Backend_BACKEND_NETBIRD:
		return model.BackendNetbird
	}
	return ""
}

func ptrUint64(v uint64) *uint64 { return &v }

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "unique constraint")
}

// UpdateProject implements quicktunv1.ProjectServiceServer with FieldMask
// semantics: only paths listed in update_mask are written.
func (s *ProjectService) UpdateProject(ctx context.Context, req *quicktunv1.UpdateProjectRequest) (*quicktunv1.Project, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if !op.IsAdmin {
		return nil, status.Error(codes.PermissionDenied, "admin role required")
	}
	if req.GetProject() == nil {
		return nil, status.Error(codes.InvalidArgument, "project body is required")
	}
	if req.GetUpdateMask() == nil || len(req.UpdateMask.Paths) == 0 {
		return nil, status.Error(codes.InvalidArgument, "update_mask is required")
	}
	slug, err := resource.ParseProjectName(req.Project.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	cur, err := s.projects.FindBySlug(ctx, slug)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "project not found")
		}
		return nil, status.Error(codes.Internal, "lookup failed")
	}

	changed := map[string]any{}
	for _, path := range req.UpdateMask.Paths {
		switch path {
		case "display_name":
			if req.Project.DisplayName == "" {
				return nil, status.Error(codes.InvalidArgument, "display_name cannot be empty")
			}
			cur.Name = req.Project.DisplayName
			changed["display_name"] = req.Project.DisplayName
		case "relay_port_range":
			if req.Project.RelayPortRange == "" {
				return nil, status.Error(codes.InvalidArgument, "relay_port_range cannot be empty")
			}
			rMin, rMax, parseErr := resource.ParsePortRange(req.Project.RelayPortRange)
			if parseErr != nil {
				return nil, status.Errorf(codes.InvalidArgument, "relay_port_range: %v", parseErr)
			}
			overlap, overlapErr := s.projects.OverlapsAny(ctx, rMin, rMax, cur.ID)
			if overlapErr != nil {
				return nil, status.Errorf(codes.Internal, "overlap check: %v", overlapErr)
			}
			if overlap {
				return nil, status.Errorf(codes.FailedPrecondition,
					"relay_port_range %q overlaps another project's range", req.Project.RelayPortRange)
			}
			cur.RelayPortRange = req.Project.RelayPortRange
			changed["relay_port_range"] = req.Project.RelayPortRange
		case "default_mode":
			m := siteModeFromProto(req.Project.DefaultMode)
			if m == "" {
				return nil, status.Error(codes.InvalidArgument, "default_mode must be ENDPOINT or SUBNET")
			}
			cur.DefaultMode = m
			changed["default_mode"] = string(m)
		case "backend":
			b := backendFromProto(req.Project.Backend)
			if b == "" {
				return nil, status.Error(codes.InvalidArgument, "backend must be RATHOLE or NETBIRD")
			}
			cur.Backend = b
			changed["backend"] = string(b)
		case "status":
			st := projectStatusFromProto(req.Project.Status)
			if st == "" {
				return nil, status.Error(codes.InvalidArgument, "status must be ACTIVE or DISABLED")
			}
			cur.Status = st
			changed["status"] = string(cur.Status)
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unknown update_mask path: %q", path)
		}
	}

	if err := s.projects.Update(ctx, cur); err != nil {
		return nil, status.Error(codes.Internal, "update failed")
	}

	_ = s.audit.Log(ctx, audit.Entry{
		ProjectID: ptrUint64(cur.ID),
		Action:    "project.update",
		Target:    resource.FormatProjectName(cur.Slug),
		Extra:     changed,
	})

	switch cur.Status {
	case model.ProjectStatusActive:
		if err := s.relay.Refresh(ctx, cur.ID); err != nil {
			s.lg.Warn("relay refresh failed",
				zap.Uint64("project_id", cur.ID),
				zap.String("op", "project.update"),
				zap.Error(err))
		}
	case model.ProjectStatusDisabled:
		if err := s.relay.RemoveProject(ctx, cur.ID); err != nil {
			s.lg.Warn("relay remove failed",
				zap.Uint64("project_id", cur.ID),
				zap.String("op", "project.update.disable"),
				zap.Error(err))
		}
	default:
		// No relay action for unknown/future statuses — safe no-op.
	}

	return projectToProto(cur), nil
}

// DeleteProject implements quicktunv1.ProjectServiceServer.
//
// Refuses if the project has live sites and force=false.
func (s *ProjectService) DeleteProject(ctx context.Context, req *quicktunv1.DeleteProjectRequest) (*emptypb.Empty, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if !op.IsAdmin {
		return nil, status.Error(codes.PermissionDenied, "admin role required")
	}
	slug, err := resource.ParseProjectName(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	p, err := s.projects.FindBySlug(ctx, slug)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "project not found")
		}
		return nil, status.Error(codes.Internal, "lookup failed")
	}

	if !req.GetForce() {
		n, err := s.projects.CountSites(ctx, p.ID)
		if err != nil {
			return nil, status.Error(codes.Internal, "site count failed")
		}
		if n > 0 {
			return nil, status.Errorf(codes.FailedPrecondition,
				"project has %d sites; pass force=true to cascade", n)
		}
	}

	if err := s.projects.Delete(ctx, p.ID); err != nil {
		return nil, status.Error(codes.Internal, "delete failed")
	}

	_ = s.audit.Log(ctx, audit.Entry{
		ProjectID: ptrUint64(p.ID),
		Action:    "project.delete",
		Target:    resource.FormatProjectName(p.Slug),
		Extra:     map[string]any{"force": req.GetForce()},
	})

	if err := s.relay.RemoveProject(ctx, p.ID); err != nil {
		s.lg.Warn("relay refresh failed",
			zap.Uint64("project_id", p.ID),
			zap.String("op", "project.delete"),
			zap.Error(err))
	}

	return &emptypb.Empty{}, nil
}

func projectStatusFromProto(s quicktunv1.ProjectStatus) model.ProjectStatus {
	switch s {
	case quicktunv1.ProjectStatus_PROJECT_STATUS_ACTIVE:
		return model.ProjectStatusActive
	case quicktunv1.ProjectStatus_PROJECT_STATUS_DISABLED:
		return model.ProjectStatusDisabled
	}
	return ""
}
