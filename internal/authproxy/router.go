// Package authproxy is a TCP gateway that authenticates incoming connections
// via HTTP CONNECT + Bearer tokens and forwards them to the appropriate
// per-project rathole-server backend on loopback.
package authproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/resource"
)

// Errors returned by Router.Route.
var (
	ErrUnauthenticated = errors.New("authproxy: invalid or expired token")
	ErrInternal        = errors.New("authproxy: internal error")
)

// Router resolves a Bearer token to the loopback backend address it maps to.
// Sharing the same DB file as the control plane (read-only) is fine for
// Phase 1.
type Router struct {
	db *gorm.DB
}

// NewRouter constructs a Router.
func NewRouter(db *gorm.DB) *Router {
	return &Router{db: db}
}

// Route validates rawToken against site agent tokens AND operator session
// tokens. Returns the loopback backend address ("127.0.0.1:<port>") to
// forward this CONNECT to.
//
// For site agent tokens: target is ignored. Backend is "127.0.0.1:<minP>"
// where minP is the lowest port in the agent's project's relay_port_range
// (the rathole-server control port). This is the agent → rathole-client
// → CONNECT → auth-proxy → rathole-server flow.
//
// For operator session tokens: target must be of the form "127.0.0.1:<port>"
// where <port> is a service.relay_port within a project the operator has
// access to. The backend is the target verbatim. This is the operator
// "ssh -o ProxyCommand=..." flow that hits a specific service port.
//
// Returns ErrUnauthenticated for any auth/lookup failure (we don't leak
// internal details to attackers). ErrInternal for server-side config issues
// (e.g. a corrupted port_range stored in DB).
func (r *Router) Route(ctx context.Context, rawToken, target string) (string, error) {
	if rawToken == "" {
		return "", ErrUnauthenticated
	}

	// Try site agent token path first — agents are the high-volume caller.
	if siteID, err := dao.NewSiteAgentTokenDAO(r.db).ValidateRaw(ctx, rawToken); err == nil {
		return r.routeSiteAgent(ctx, siteID)
	}

	// Fall back to operator session path.
	if opID, err := dao.NewSessionDAO(r.db).ValidateSessionRaw(ctx, rawToken); err == nil {
		return r.routeOperator(ctx, opID, target)
	}

	return "", ErrUnauthenticated
}

// routeSiteAgent resolves a site agent token to its project's rathole-server
// control-port backend ("127.0.0.1:<minP>").
func (r *Router) routeSiteAgent(ctx context.Context, siteID uint64) (string, error) {
	var site model.Site
	if err := r.db.WithContext(ctx).First(&site, siteID).Error; err != nil {
		return "", ErrUnauthenticated
	}

	var project model.Project
	if err := r.db.WithContext(ctx).First(&project, site.ProjectID).Error; err != nil {
		return "", ErrUnauthenticated
	}
	if project.Status != model.ProjectStatusActive {
		return "", ErrUnauthenticated
	}

	minP, _, err := resource.ParsePortRange(project.RelayPortRange)
	if err != nil {
		return "", fmt.Errorf("%w: parse port range: %v", ErrInternal, err)
	}
	return fmt.Sprintf("127.0.0.1:%d", minP), nil
}

// routeOperator resolves an operator session + CONNECT target to a service's
// relay-port backend on loopback. target must be "127.0.0.1:<port>" where
// <port> is allocated to a service in a project the operator has access to.
// Anything else → ErrUnauthenticated (no SSRF surface; the operator cannot
// make auth-proxy dial arbitrary internal hosts).
func (r *Router) routeOperator(ctx context.Context, operatorID uint64, target string) (string, error) {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return "", ErrUnauthenticated
	}
	if host != "127.0.0.1" {
		return "", ErrUnauthenticated
	}
	port64, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return "", ErrUnauthenticated
	}
	port := uint16(port64)

	var service model.Service
	if err := r.db.WithContext(ctx).
		Where("relay_port = ?", port).
		First(&service).Error; err != nil {
		return "", ErrUnauthenticated
	}

	var site model.Site
	if err := r.db.WithContext(ctx).First(&site, service.SiteID).Error; err != nil {
		return "", ErrUnauthenticated
	}

	var project model.Project
	if err := r.db.WithContext(ctx).First(&project, site.ProjectID).Error; err != nil {
		return "", ErrUnauthenticated
	}
	if project.Status != model.ProjectStatusActive {
		return "", ErrUnauthenticated
	}

	var access model.OperatorProjectAccess
	if err := r.db.WithContext(ctx).
		Where("operator_id = ? AND project_id = ?", operatorID, project.ID).
		First(&access).Error; err != nil {
		return "", ErrUnauthenticated
	}

	return target, nil
}
