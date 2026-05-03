package grpcsvc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

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

// SiteService implements quicktunv1.SiteServiceServer.
type SiteService struct {
	quicktunv1.UnimplementedSiteServiceServer
	projects *dao.ProjectDAO
	sites    *dao.SiteDAO
	tokens   *dao.SiteAgentTokenDAO
	audit    *audit.Writer
}

// NewSiteService constructs a SiteService.
func NewSiteService(projects *dao.ProjectDAO, sites *dao.SiteDAO, tokens *dao.SiteAgentTokenDAO, audit *audit.Writer) *SiteService {
	return &SiteService{projects: projects, sites: sites, tokens: tokens, audit: audit}
}

// parseProjectParent extracts the project slug from "projects/{slug}" without
// enforcing slug length constraints beyond non-empty. This allows short slugs
// used in tests and simple project names.
func parseProjectParent(parent string) (string, error) {
	parts := strings.SplitN(parent, "/", 3)
	if len(parts) != 2 || parts[0] != "projects" || parts[1] == "" {
		return "", errors.New(`name must be "projects/{slug}"`)
	}
	return parts[1], nil
}

// parseSiteName extracts (projectSlug, siteSlug) from "projects/{p}/sites/{s}"
// without enforcing slug length constraints beyond non-empty.
func parseSiteName(name string) (projectSlug, siteSlug string, err error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "projects" || parts[2] != "sites" ||
		parts[1] == "" || parts[3] == "" {
		return "", "", errors.New(`name must be "projects/{p}/sites/{s}"`)
	}
	return parts[1], parts[3], nil
}

// resolveProject parses parent, looks up the project, and authorizes the
// caller. Non-admins must have an access grant on the project.
// Auth is checked before slug parsing so unauthenticated callers get
// Unauthenticated regardless of the name format.
func (s *SiteService) resolveProject(ctx context.Context, parent string) (*model.Project, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	slug, err := parseProjectParent(parent)
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
	if !op.IsAdmin && !s.hasProjectAccess(ctx, op.ID, p.ID) {
		// Mask as NotFound to avoid project enumeration.
		return nil, status.Error(codes.NotFound, "project not found")
	}
	return p, nil
}

// resolveSite parses a "projects/{p}/sites/{s}" name, checks auth first, then
// finds the project, authorizes access, and returns project + site.
func (s *SiteService) resolveSite(ctx context.Context, name string) (*model.Project, *model.Site, error) {
	// Check auth before structural parsing so unauthenticated callers always
	// receive Unauthenticated, not InvalidArgument.
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	projectSlug, siteSlug, err := parseSiteName(name)
	if err != nil {
		return nil, nil, status.Error(codes.InvalidArgument, err.Error())
	}
	p, err := s.resolveProject(ctx, "projects/"+projectSlug)
	if err != nil {
		return nil, nil, err
	}
	site, err := s.sites.FindByName(ctx, p.ID, siteSlug)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, nil, status.Error(codes.NotFound, "site not found")
		}
		return nil, nil, status.Error(codes.Internal, "lookup failed")
	}
	return p, site, nil
}

func (s *SiteService) hasProjectAccess(ctx context.Context, operatorID, projectID uint64) bool {
	var count int64
	err := s.projects.Db().WithContext(ctx).
		Model(&model.OperatorProjectAccess{}).
		Where("operator_id = ? AND project_id = ?", operatorID, projectID).
		Count(&count).Error
	return err == nil && count > 0
}

// GetSite implements quicktunv1.SiteServiceServer.
func (s *SiteService) GetSite(ctx context.Context, req *quicktunv1.GetSiteRequest) (*quicktunv1.Site, error) {
	p, site, err := s.resolveSite(ctx, req.GetName())
	if err != nil {
		return nil, err
	}
	return siteToProto(p, site), nil
}

// ListSites implements quicktunv1.SiteServiceServer.
func (s *SiteService) ListSites(ctx context.Context, req *quicktunv1.ListSitesRequest) (*quicktunv1.ListSitesResponse, error) {
	p, err := s.resolveProject(ctx, req.GetParent())
	if err != nil {
		return nil, err
	}

	pageSize := int(req.GetPage().GetPageSize())
	pageToken := req.GetPage().GetPageToken()
	rows, err := s.sites.ListByProject(ctx, p.ID, pageSize, pageToken)
	if err != nil {
		if errors.Is(err, dao.ErrInvalidPageToken) {
			return nil, status.Error(codes.InvalidArgument, "invalid page_token")
		}
		return nil, status.Error(codes.Internal, "list failed")
	}

	out := &quicktunv1.ListSitesResponse{
		Sites: make([]*quicktunv1.Site, len(rows)),
		Page:  &quicktunv1.PageResponse{NextPageToken: dao.NextSitePageToken(rows)},
	}
	for i := range rows {
		out.Sites[i] = siteToProto(p, &rows[i])
	}
	return out, nil
}

func siteToProto(p *model.Project, s *model.Site) *quicktunv1.Site {
	out := &quicktunv1.Site{
		Name:         resource.FormatSiteName(p.Slug, s.Name),
		SiteId:       s.Name,
		DisplayName:  s.Name,
		Mode:         siteModeToProto(s.Mode),
		Backend:      backendToProto(s.Backend),
		Status:       siteStatusToProto(s.Status),
		Hostname:     s.Hostname,
		Os:           s.OS,
		AgentVersion: s.AgentVersion,
		CreateTime:   timestamppb.New(s.CreatedAt),
		UpdateTime:   timestamppb.New(s.UpdatedAt),
	}
	if s.LastSeenAt != nil {
		out.LastSeenTime = timestamppb.New(*s.LastSeenAt)
	}
	if s.LanCidrsJSON != "" {
		var cidrs []string
		if err := json.Unmarshal([]byte(s.LanCidrsJSON), &cidrs); err == nil {
			out.LanCidrs = cidrs
		}
	}
	return out
}

func siteStatusToProto(s model.SiteStatus) quicktunv1.SiteStatus {
	switch s {
	case model.SiteStatusPending:
		return quicktunv1.SiteStatus_SITE_STATUS_PENDING
	case model.SiteStatusOnline:
		return quicktunv1.SiteStatus_SITE_STATUS_ONLINE
	case model.SiteStatusOffline:
		return quicktunv1.SiteStatus_SITE_STATUS_OFFLINE
	}
	return quicktunv1.SiteStatus_SITE_STATUS_UNSPECIFIED
}
