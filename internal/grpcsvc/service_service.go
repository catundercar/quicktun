package grpcsvc

import (
	"context"
	"errors"
	"strconv"

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

// ServiceService implements quicktunv1.ServiceServiceServer.
type ServiceService struct {
	quicktunv1.UnimplementedServiceServiceServer
	projects *dao.ProjectDAO
	sites    *dao.SiteDAO
	services *dao.ServiceDAO
	audit    *audit.Writer
	lg       *zap.Logger
	relay    RelayManager
}

// NewServiceService constructs a ServiceService. If lg is nil a no-op logger
// is substituted; if relay is nil a no-op manager is substituted.
func NewServiceService(projects *dao.ProjectDAO, sites *dao.SiteDAO, services *dao.ServiceDAO, audit *audit.Writer, lg *zap.Logger, relay RelayManager) *ServiceService {
	if lg == nil {
		lg = zap.NewNop()
	}
	if relay == nil {
		relay = noopRelayManager{}
	}
	return &ServiceService{
		projects: projects, sites: sites, services: services, audit: audit,
		lg: lg, relay: relay,
	}
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

// CreateService implements quicktunv1.ServiceServiceServer.
func (s *ServiceService) CreateService(ctx context.Context, req *quicktunv1.CreateServiceRequest) (*quicktunv1.Service, error) {
	p, site, err := s.resolveSiteFromParent(ctx, req.GetParent())
	if err != nil {
		return nil, err
	}
	op := auth.OperatorFromContext(ctx)
	if !op.IsAdmin {
		return nil, status.Error(codes.PermissionDenied, "admin role required")
	}
	if req.GetService() == nil {
		return nil, status.Error(codes.InvalidArgument, "service body is required")
	}
	if err := resource.ValidateSlug(req.GetServiceId()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if req.Service.GetTargetAddr() == "" {
		return nil, status.Error(codes.InvalidArgument, "service.target_addr is required")
	}
	if req.Service.GetTargetPort() == 0 || req.Service.GetTargetPort() > 65535 {
		return nil, status.Error(codes.InvalidArgument, "service.target_port must be 1-65535")
	}

	// Allocate a relay port from the project's range.
	relayPort, err := s.services.AllocateRelayPort(ctx, p)
	if err != nil {
		if errors.Is(err, dao.ErrPortRangeExhausted) {
			return nil, status.Error(codes.ResourceExhausted, "no relay ports available in project range")
		}
		return nil, status.Error(codes.Internal, "port allocation failed")
	}

	proto := protoFromProto(req.Service.Proto)
	if proto == "" {
		proto = model.ProtoTCP
	}

	row := &model.Service{
		SiteID:     site.ID,
		Name:       req.ServiceId,
		TargetAddr: req.Service.TargetAddr,
		TargetPort: uint16(req.Service.TargetPort),
		Proto:      proto,
		RelayPort:  &relayPort,
	}
	if _, err := s.services.Create(ctx, row); err != nil {
		if isUniqueConstraintErr(err) {
			return nil, status.Error(codes.AlreadyExists, "service already exists in site")
		}
		return nil, status.Error(codes.Internal, "create failed")
	}

	_ = s.audit.Log(ctx, audit.Entry{
		ProjectID: ptrUint64(p.ID),
		Action:    "service.create",
		Target:    resource.FormatServiceName(p.Slug, site.Name, row.Name),
		Extra: map[string]any{
			"target":     row.TargetAddr + ":" + strconv.Itoa(int(row.TargetPort)),
			"relay_port": relayPort,
		},
	})

	if err := s.relay.Refresh(ctx, p.ID); err != nil {
		s.lg.Warn("relay refresh failed",
			zap.Uint64("project_id", p.ID),
			zap.String("op", "service.create"),
			zap.Error(err))
	}

	return serviceToProto(p, site, row), nil
}

// UpdateService implements quicktunv1.ServiceServiceServer.
func (s *ServiceService) UpdateService(ctx context.Context, req *quicktunv1.UpdateServiceRequest) (*quicktunv1.Service, error) {
	if req.GetService() == nil {
		return nil, status.Error(codes.InvalidArgument, "service body is required")
	}
	if req.GetUpdateMask() == nil || len(req.UpdateMask.Paths) == 0 {
		return nil, status.Error(codes.InvalidArgument, "update_mask is required")
	}
	p, site, svc, err := s.resolveService(ctx, req.Service.GetName())
	if err != nil {
		return nil, err
	}
	op := auth.OperatorFromContext(ctx)
	if !op.IsAdmin {
		return nil, status.Error(codes.PermissionDenied, "admin role required")
	}

	changed := map[string]any{}
	var displayNameOverride string
	hasDisplayNameOverride := false
	for _, path := range req.UpdateMask.Paths {
		switch path {
		case "display_name":
			// Phase 1: Service.Name doubles as slug+label. Audit captures the request.
			displayNameOverride = req.Service.DisplayName
			hasDisplayNameOverride = true
			changed["display_name"] = req.Service.DisplayName
		case "target_addr":
			if req.Service.TargetAddr == "" {
				return nil, status.Error(codes.InvalidArgument, "target_addr cannot be empty")
			}
			svc.TargetAddr = req.Service.TargetAddr
			changed["target_addr"] = req.Service.TargetAddr
		case "target_port":
			if req.Service.TargetPort == 0 || req.Service.TargetPort > 65535 {
				return nil, status.Error(codes.InvalidArgument, "target_port must be 1-65535")
			}
			svc.TargetPort = uint16(req.Service.TargetPort)
			changed["target_port"] = req.Service.TargetPort
		case "proto":
			pr := protoFromProto(req.Service.Proto)
			if pr == "" {
				return nil, status.Error(codes.InvalidArgument, "proto must be TCP or UDP")
			}
			svc.Proto = pr
			changed["proto"] = string(pr)
		case "relay_port":
			return nil, status.Error(codes.InvalidArgument, "relay_port is allocated by the server and cannot be updated")
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unknown update_mask path: %q", path)
		}
	}

	if err := s.services.Update(ctx, svc); err != nil {
		return nil, status.Error(codes.Internal, "update failed")
	}

	_ = s.audit.Log(ctx, audit.Entry{
		ProjectID: ptrUint64(p.ID),
		Action:    "service.update",
		Target:    resource.FormatServiceName(p.Slug, site.Name, svc.Name),
		Extra:     changed,
	})

	if err := s.relay.Refresh(ctx, p.ID); err != nil {
		s.lg.Warn("relay refresh failed",
			zap.Uint64("project_id", p.ID),
			zap.String("op", "service.update"),
			zap.Error(err))
	}

	out := serviceToProto(p, site, svc)
	if hasDisplayNameOverride {
		out.DisplayName = displayNameOverride
	}
	return out, nil
}

// DeleteService implements quicktunv1.ServiceServiceServer.
func (s *ServiceService) DeleteService(ctx context.Context, req *quicktunv1.DeleteServiceRequest) (*emptypb.Empty, error) {
	p, site, svc, err := s.resolveService(ctx, req.GetName())
	if err != nil {
		return nil, err
	}
	op := auth.OperatorFromContext(ctx)
	if !op.IsAdmin {
		return nil, status.Error(codes.PermissionDenied, "admin role required")
	}
	if err := s.services.Delete(ctx, svc.ID); err != nil {
		return nil, status.Error(codes.Internal, "delete failed")
	}
	extra := map[string]any{}
	if svc.RelayPort != nil {
		extra["relay_port"] = *svc.RelayPort
	}
	_ = s.audit.Log(ctx, audit.Entry{
		ProjectID: ptrUint64(p.ID),
		Action:    "service.delete",
		Target:    resource.FormatServiceName(p.Slug, site.Name, svc.Name),
		Extra:     extra,
	})
	if err := s.relay.Refresh(ctx, p.ID); err != nil {
		s.lg.Warn("relay refresh failed",
			zap.Uint64("project_id", p.ID),
			zap.String("op", "service.delete"),
			zap.Error(err))
	}
	return &emptypb.Empty{}, nil
}
