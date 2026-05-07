package grpcsvc

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/resource"
)

// AdminService implements quicktunv1.AdminServiceServer. It exposes a single
// admin-only aggregation RPC (GetSystemStatus) used by `quicktun status`
// and external monitoring.
type AdminService struct {
	quicktunv1.UnimplementedAdminServiceServer
	db    *gorm.DB
	relay RelayManager
}

// NewAdminService constructs an AdminService. If relay is nil a no-op manager
// is substituted so unit tests can pass nil and get SupervisorCount=0.
func NewAdminService(db *gorm.DB, relay RelayManager) *AdminService {
	if relay == nil {
		relay = noopRelayManager{}
	}
	return &AdminService{db: db, relay: relay}
}

// staleThreshold: AgentService.Heartbeat fires every 15s server-side. Mark
// stale at 2× heartbeat so transient one-tick gaps don't show up; sites past
// site_offline_after (default 90s) get fully flipped offline by the sweeper.
const staleThreshold = 30 * time.Second

// GetSystemStatus aggregates DB counts + relay supervisor count + a list of
// online sites whose last heartbeat is older than staleThreshold.
func (a *AdminService) GetSystemStatus(ctx context.Context, _ *quicktunv1.GetSystemStatusRequest) (*quicktunv1.GetSystemStatusResponse, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if !op.IsAdmin {
		return nil, status.Error(codes.PermissionDenied, "admin only")
	}

	resp := &quicktunv1.GetSystemStatusResponse{}

	count := func(out *uint32, m any, where ...any) error {
		var n int64
		q := a.db.WithContext(ctx).Model(m)
		if len(where) > 0 {
			q = q.Where(where[0], where[1:]...)
		}
		if err := q.Count(&n).Error; err != nil {
			return err
		}
		*out = uint32(n)
		return nil
	}

	if err := count(&resp.OperatorCount, &model.Operator{}); err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}
	if err := count(&resp.ProjectCountActive, &model.Project{}, "status = ?", model.ProjectStatusActive); err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}
	if err := count(&resp.ProjectCountDisabled, &model.Project{}, "status = ?", model.ProjectStatusDisabled); err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}
	if err := count(&resp.SiteCountOnline, &model.Site{}, "status = ?", model.SiteStatusOnline); err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}
	if err := count(&resp.SiteCountOffline, &model.Site{}, "status = ?", model.SiteStatusOffline); err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}
	if err := count(&resp.SiteCountPending, &model.Site{}, "status = ?", model.SiteStatusPending); err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}
	if err := count(&resp.ServiceCount, &model.Service{}); err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}
	resp.SupervisorRunningCount = uint32(a.relay.SupervisorCount())

	threshold := time.Now().UTC().Add(-staleThreshold)
	type staleRow struct {
		SiteName    string
		ProjectSlug string
		LastSeenAt  *time.Time
		SiteStatus  string
		Hostname    string
	}
	var rows []staleRow
	if err := a.db.WithContext(ctx).
		Table("sites").
		Select("sites.name AS site_name, projects.slug AS project_slug, sites.last_seen_at AS last_seen_at, sites.status AS site_status, sites.hostname AS hostname").
		Joins("JOIN projects ON projects.id = sites.project_id").
		Where("sites.deleted_at IS NULL AND sites.status = ? AND sites.last_seen_at IS NOT NULL AND sites.last_seen_at < ?",
			model.SiteStatusOnline, threshold).
		Order("sites.last_seen_at ASC").
		Find(&rows).Error; err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}
	for _, r := range rows {
		sum := &quicktunv1.SiteHealthSummary{
			Name:     resource.FormatSiteName(r.ProjectSlug, r.SiteName),
			Status:   r.SiteStatus,
			Hostname: r.Hostname,
		}
		if r.LastSeenAt != nil {
			sum.LastSeenAt = timestamppb.New(*r.LastSeenAt)
		}
		resp.StaleSites = append(resp.StaleSites, sum)
	}

	resp.Now = timestamppb.Now()
	return resp, nil
}
