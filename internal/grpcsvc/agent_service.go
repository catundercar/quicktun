// Package grpcsvc — AgentService: site-agent control-plane RPCs.
//
// AgentService is invoked by the per-site agent process running on the
// bastion host. Bootstrap is called once at startup to fetch the desired
// tunnel set; Heartbeat is called periodically to refresh last_seen_at and
// detect config drift via a stable hash. Auth is handled by
// auth.AgentInterceptor (site agent tokens, distinct from operator sessions).
package grpcsvc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/resource"
)

// heartbeatSeconds is the cadence the server tells agents to call Heartbeat
// at. Hardcoded for Phase 1; can move to project config later.
const heartbeatSeconds int32 = 15

// AgentService implements quicktunv1.AgentServiceServer.
type AgentService struct {
	quicktunv1.UnimplementedAgentServiceServer
	projects          *dao.ProjectDAO
	sites             *dao.SiteDAO
	services          *dao.ServiceDAO
	lg                *zap.Logger
	relayHost         string // public hostname for rathole control endpoint (no port)
	authProxyEndpoint string // if non-empty, sent verbatim as auth_proxy_endpoint
}

// NewAgentService constructs an AgentService. relayHost is the publicly
// reachable hostname an agent will dial to reach this server's per-project
// rathole-server (e.g., "relay.example.com"). The port is derived per-project
// from the project's relay_port_range[0] (the rathole control port).
// authProxyEndpoint, if non-empty, is returned verbatim as auth_proxy_endpoint
// in BootstrapResponse instead of the legacy relayHost:minP construction.
// If lg is nil a no-op logger is used.
func NewAgentService(
	projects *dao.ProjectDAO,
	sites *dao.SiteDAO,
	services *dao.ServiceDAO,
	lg *zap.Logger,
	relayHost string,
	authProxyEndpoint string,
) *AgentService {
	if lg == nil {
		lg = zap.NewNop()
	}
	return &AgentService{
		projects:          projects,
		sites:             sites,
		services:          services,
		lg:                lg,
		relayHost:         relayHost,
		authProxyEndpoint: authProxyEndpoint,
	}
}

// Bootstrap returns the agent's full desired-state: site identity, rathole
// control endpoint, the tunnel binding set, and the heartbeat cadence. Also
// updates last_seen_at and (where supplied) hostname/os/agent_version.
func (a *AgentService) Bootstrap(ctx context.Context, req *quicktunv1.BootstrapRequest) (*quicktunv1.BootstrapResponse, error) {
	pr, ok := auth.AgentFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no agent principal")
	}

	site, err := a.sites.FindByID(ctx, pr.SiteID)
	if err != nil {
		a.lg.Warn("agent: site lookup failed", zap.Uint64("site_id", pr.SiteID), zap.Error(err))
		return nil, status.Error(codes.Internal, "internal error")
	}
	project, err := a.projects.FindByID(ctx, pr.ProjectID)
	if err != nil {
		a.lg.Warn("agent: project lookup failed", zap.Uint64("project_id", pr.ProjectID), zap.Error(err))
		return nil, status.Error(codes.Internal, "internal error")
	}
	if project.Status != model.ProjectStatusActive {
		return nil, status.Error(codes.FailedPrecondition, "project is disabled")
	}

	minP, _, err := resource.ParsePortRange(project.RelayPortRange)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "port range: %v", err)
	}

	// Bootstrap doesn't carry lan_cidrs (only Heartbeat does), so pass nil.
	if err := a.sites.UpdateAgentMeta(ctx, site.ID, req.GetHostname(), req.GetOs(), req.GetAgentVersion(), nil); err != nil {
		a.lg.Warn("agent: update meta failed", zap.Uint64("site_id", site.ID), zap.Error(err))
		return nil, status.Error(codes.Internal, "internal error")
	}
	if err := a.sites.SetStatus(ctx, site.ID, model.SiteStatusOnline); err != nil {
		// non-fatal: a successful Bootstrap is more important than the status flip.
		a.lg.Warn("agent: failed to set site online", zap.Uint64("site_id", site.ID), zap.Error(err))
	}

	// TODO(plan9): handle pagination if a site grows beyond 1000 services.
	// Today this silently caps at 1000.
	svcs, err := a.services.ListBySite(ctx, site.ID, 1000, "")
	if err != nil {
		a.lg.Warn("agent: list services failed", zap.Uint64("site_id", site.ID), zap.Error(err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	tunnels := make([]*quicktunv1.TunnelBinding, 0, len(svcs))
	for _, s := range svcs {
		if s.RelayPort == nil {
			continue
		}
		tunnels = append(tunnels, &quicktunv1.TunnelBinding{
			ServiceSlug: s.Name,
			TargetAddr:  s.TargetAddr,
			TargetPort:  uint32(s.TargetPort),
			Proto:       string(s.Proto),
			RelayPort:   uint32(*s.RelayPort),
		})
	}

	endpoint := a.authProxyEndpoint
	if endpoint == "" {
		// Fallback: legacy direct-rathole wiring (Plan 7 behavior).
		endpoint = a.relayHost + ":" + strconv.Itoa(int(minP))
	}

	return &quicktunv1.BootstrapResponse{
		SiteName:          resource.FormatSiteName(project.Slug, site.Name),
		ProjectSlug:       project.Slug,
		SiteSlug:          site.Name,
		AuthProxyEndpoint: endpoint,
		Tunnels:           tunnels,
		HeartbeatSeconds:  heartbeatSeconds,
		ConfigVersion:     computeConfigVersion(svcs),
	}, nil
}

// Heartbeat updates last_seen_at and tells the agent whether to re-Bootstrap
// based on the agent-supplied ConfigVersion vs. the server's current view.
func (a *AgentService) Heartbeat(ctx context.Context, req *quicktunv1.HeartbeatRequest) (*quicktunv1.HeartbeatResponse, error) {
	pr, ok := auth.AgentFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no agent principal")
	}

	project, err := a.projects.FindByID(ctx, pr.ProjectID)
	if err != nil {
		a.lg.Warn("agent: project lookup failed", zap.Uint64("project_id", pr.ProjectID), zap.Error(err))
		return nil, status.Error(codes.Internal, "internal error")
	}
	if project.Status != model.ProjectStatusActive {
		return nil, status.Error(codes.FailedPrecondition, "project is disabled")
	}

	if err := a.sites.UpdateAgentMeta(ctx, pr.SiteID, req.GetHostname(), req.GetOs(), req.GetAgentVersion(), req.GetLanCidrs()); err != nil {
		a.lg.Warn("agent: update meta failed", zap.Uint64("site_id", pr.SiteID), zap.Error(err))
		return nil, status.Error(codes.Internal, "internal error")
	}
	if err := a.sites.SetStatus(ctx, pr.SiteID, model.SiteStatusOnline); err != nil {
		// non-fatal: heartbeat success is more important than the status flip.
		a.lg.Warn("agent: failed to set site online", zap.Uint64("site_id", pr.SiteID), zap.Error(err))
	}

	// TODO(plan9): handle pagination if a site grows beyond 1000 services.
	// Today this silently caps at 1000.
	svcs, err := a.services.ListBySite(ctx, pr.SiteID, 1000, "")
	if err != nil {
		a.lg.Warn("agent: list services failed", zap.Uint64("site_id", pr.SiteID), zap.Error(err))
		return nil, status.Error(codes.Internal, "internal error")
	}
	current := computeConfigVersion(svcs)

	return &quicktunv1.HeartbeatResponse{
		ShouldRebootstrap: req.GetConfigVersion() != current,
		ServerTime:        timestamppb.Now(),
	}, nil
}

// computeConfigVersion is a stable hash over the (service_slug, relay_port,
// target_addr, target_port, proto) tuples for a site. Same logical state
// across calls produces the same value. Returns the first 16 hex chars of
// sha256 over a sorted, pipe-delimited rendering.
func computeConfigVersion(svcs []model.Service) string {
	type row struct {
		Slug       string
		RelayPort  uint16
		TargetAddr string
		TargetPort uint16
		Proto      string
	}
	rows := make([]row, 0, len(svcs))
	for _, s := range svcs {
		if s.RelayPort == nil {
			continue
		}
		rows = append(rows, row{
			Slug:       s.Name,
			RelayPort:  *s.RelayPort,
			TargetAddr: s.TargetAddr,
			TargetPort: s.TargetPort,
			Proto:      string(s.Proto),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Slug < rows[j].Slug })

	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.Slug)
		b.WriteByte('|')
		b.WriteString(strconv.FormatUint(uint64(r.RelayPort), 10))
		b.WriteByte('|')
		b.WriteString(r.TargetAddr)
		b.WriteByte('|')
		b.WriteString(strconv.FormatUint(uint64(r.TargetPort), 10))
		b.WriteByte('|')
		b.WriteString(r.Proto)
		b.WriteByte('\n')
	}
	h := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(h[:8])
}
