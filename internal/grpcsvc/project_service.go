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
