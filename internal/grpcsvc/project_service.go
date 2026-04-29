package grpcsvc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/audit"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/resource"
)

// ProjectService implements quicktunv1.ProjectServiceServer.
type ProjectService struct {
	quicktunv1.UnimplementedProjectServiceServer
	projects *dao.ProjectDAO
	audit    *audit.Writer
}

// NewProjectService constructs a ProjectService.
func NewProjectService(projects *dao.ProjectDAO, audit *audit.Writer) *ProjectService {
	return &ProjectService{projects: projects, audit: audit}
}

// errInvalidToken is returned by DAO.List when page_token cannot be parsed.
// Sentinel kept here so service can map to InvalidArgument.
var errInvalidToken = errors.New("invalid page token")

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
		if errors.Is(err, errInvalidToken) {
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
	return strContains(msg, "UNIQUE constraint failed") ||
		strContains(msg, "unique constraint")
}

func strContains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
