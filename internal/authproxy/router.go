// Package authproxy is a TCP gateway that authenticates incoming connections
// via HTTP CONNECT + Bearer tokens and forwards them to the appropriate
// per-project rathole-server backend on loopback.
package authproxy

import (
	"context"
	"errors"
	"fmt"

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

// Route validates the raw bearer token and returns the loopback backend
// address ("127.0.0.1:<port>") for the agent's project's rathole-server.
//
// Routing logic:
//  1. token → site_id (via SiteAgentTokenDAO.ValidateRaw)
//  2. site_id → site → project
//  3. project must be active
//  4. backend = "127.0.0.1:" + minP-of-relay-port-range
//
// Returns ErrUnauthenticated for any auth/lookup failure (we don't leak
// internal details). ErrInternal for server-side config issues (bad port
// range stored in DB).
func (r *Router) Route(ctx context.Context, rawToken string) (string, error) {
	if rawToken == "" {
		return "", ErrUnauthenticated
	}
	siteID, err := dao.NewSiteAgentTokenDAO(r.db).ValidateRaw(ctx, rawToken)
	if err != nil {
		return "", ErrUnauthenticated
	}

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
