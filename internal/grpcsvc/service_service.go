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

// ServiceService implements quicktunv1.ServiceServiceServer.
type ServiceService struct {
	quicktunv1.UnimplementedServiceServiceServer
	projects *dao.ProjectDAO
	sites    *dao.SiteDAO
	services *dao.ServiceDAO
	audit    *audit.Writer
}

// NewServiceService constructs a ServiceService.
func NewServiceService(projects *dao.ProjectDAO, sites *dao.SiteDAO, services *dao.ServiceDAO, audit *audit.Writer) *ServiceService {
	return &ServiceService{projects: projects, sites: sites, services: services, audit: audit}
}

// resolveSiteFromParent parses a "projects/{p}/sites/{s}" parent and returns
// the project + site, performing access checks.
func (s *ServiceService) resolveSiteFromParent(ctx context.Context, parent string) (*model.Project, *model.Site, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	n, err := resource.ParseSiteParent(parent)
	if err != nil {
		return nil, nil, status.Error(codes.InvalidArgument, err.Error())
	}
	p, err := s.projects.FindBySlug(ctx, n.Project)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, nil, status.Error(codes.NotFound, "project not found")
		}
		return nil, nil, status.Error(codes.Internal, "lookup failed")
	}
	if !op.IsAdmin && !s.hasProjectAccess(ctx, op.ID, p.ID) {
		return nil, nil, status.Error(codes.NotFound, "project not found")
	}
	site, err := s.sites.FindByName(ctx, p.ID, n.Site)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, nil, status.Error(codes.NotFound, "site not found")
		}
		return nil, nil, status.Error(codes.Internal, "lookup failed")
	}
	return p, site, nil
}

// resolveService parses a "projects/{p}/sites/{s}/services/{svc}" name and
// returns project + site + service, performing access checks.
func (s *ServiceService) resolveService(ctx context.Context, name string) (*model.Project, *model.Site, *model.Service, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, nil, nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	n, err := resource.ParseServiceName(name)
	if err != nil {
		return nil, nil, nil, status.Error(codes.InvalidArgument, err.Error())
	}
	p, err := s.projects.FindBySlug(ctx, n.Project)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, nil, nil, status.Error(codes.NotFound, "project not found")
		}
		return nil, nil, nil, status.Error(codes.Internal, "lookup failed")
	}
	if !op.IsAdmin && !s.hasProjectAccess(ctx, op.ID, p.ID) {
		return nil, nil, nil, status.Error(codes.NotFound, "project not found")
	}
	site, err := s.sites.FindByName(ctx, p.ID, n.Site)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, nil, nil, status.Error(codes.NotFound, "site not found")
		}
		return nil, nil, nil, status.Error(codes.Internal, "lookup failed")
	}
	svc, err := s.services.FindByName(ctx, site.ID, n.Service)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, nil, nil, status.Error(codes.NotFound, "service not found")
		}
		return nil, nil, nil, status.Error(codes.Internal, "lookup failed")
	}
	return p, site, svc, nil
}

func (s *ServiceService) hasProjectAccess(ctx context.Context, operatorID, projectID uint64) bool {
	var count int64
	err := s.projects.Db().WithContext(ctx).
		Model(&model.OperatorProjectAccess{}).
		Where("operator_id = ? AND project_id = ?", operatorID, projectID).
		Count(&count).Error
	return err == nil && count > 0
}

// GetService implements quicktunv1.ServiceServiceServer.
func (s *ServiceService) GetService(ctx context.Context, req *quicktunv1.GetServiceRequest) (*quicktunv1.Service, error) {
	p, site, svc, err := s.resolveService(ctx, req.GetName())
	if err != nil {
		return nil, err
	}
	return serviceToProto(p, site, svc), nil
}

// ListServices implements quicktunv1.ServiceServiceServer.
func (s *ServiceService) ListServices(ctx context.Context, req *quicktunv1.ListServicesRequest) (*quicktunv1.ListServicesResponse, error) {
	p, site, err := s.resolveSiteFromParent(ctx, req.GetParent())
	if err != nil {
		return nil, err
	}
	pageSize := int(req.GetPage().GetPageSize())
	pageToken := req.GetPage().GetPageToken()
	rows, err := s.services.ListBySite(ctx, site.ID, pageSize, pageToken)
	if err != nil {
		if errors.Is(err, dao.ErrInvalidPageToken) {
			return nil, status.Error(codes.InvalidArgument, "invalid page_token")
		}
		return nil, status.Error(codes.Internal, "list failed")
	}
	out := &quicktunv1.ListServicesResponse{
		Services: make([]*quicktunv1.Service, len(rows)),
		Page:     &quicktunv1.PageResponse{NextPageToken: dao.NextServicePageToken(rows)},
	}
	for i := range rows {
		out.Services[i] = serviceToProto(p, site, &rows[i])
	}
	return out, nil
}

func serviceToProto(p *model.Project, site *model.Site, svc *model.Service) *quicktunv1.Service {
	out := &quicktunv1.Service{
		Name:        resource.FormatServiceName(p.Slug, site.Name, svc.Name),
		ServiceId:   svc.Name,
		DisplayName: svc.Name,
		TargetAddr:  svc.TargetAddr,
		TargetPort:  uint32(svc.TargetPort),
		Proto:       protoFromModel(svc.Proto),
		CreateTime:  timestamppb.New(svc.CreatedAt),
		UpdateTime:  timestamppb.New(svc.UpdatedAt),
	}
	if svc.RelayPort != nil {
		out.RelayPort = uint32(*svc.RelayPort)
	}
	return out
}

func protoFromModel(p model.Proto) quicktunv1.Proto {
	switch p {
	case model.ProtoTCP:
		return quicktunv1.Proto_PROTO_TCP
	case model.ProtoUDP:
		return quicktunv1.Proto_PROTO_UDP
	}
	return quicktunv1.Proto_PROTO_UNSPECIFIED
}

func protoFromProto(p quicktunv1.Proto) model.Proto {
	switch p {
	case quicktunv1.Proto_PROTO_TCP:
		return model.ProtoTCP
	case quicktunv1.Proto_PROTO_UDP:
		return model.ProtoUDP
	}
	return ""
}
