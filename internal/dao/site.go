package dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/model"
)

// SiteDAO encapsulates queries against the sites table (project-scoped).
type SiteDAO struct{ db *gorm.DB }

// NewSiteDAO constructs a SiteDAO bound to db.
func NewSiteDAO(db *gorm.DB) *SiteDAO { return &SiteDAO{db: db} }

// Create inserts a new site under projectID. Caller must populate ProjectID, Name.
func (d *SiteDAO) Create(ctx context.Context, s *model.Site) (*model.Site, error) {
	if s.Status == "" {
		s.Status = model.SiteStatusPending
	}
	if s.Mode == "" {
		s.Mode = model.SiteModeEndpoint
	}
	if err := d.db.WithContext(ctx).Create(s).Error; err != nil {
		return nil, fmt.Errorf("dao: create site: %w", err)
	}
	return s, nil
}

// FindByName returns the live site with (projectID, name).
func (d *SiteDAO) FindByName(ctx context.Context, projectID uint64, name string) (*model.Site, error) {
	var s model.Site
	err := d.db.WithContext(ctx).
		Where("project_id = ? AND name = ?", projectID, name).
		First(&s).Error
	if err != nil {
		return nil, fmt.Errorf("dao: find site by name: %w", err)
	}
	return &s, nil
}

// FindByID returns the live site at id.
func (d *SiteDAO) FindByID(ctx context.Context, id uint64) (*model.Site, error) {
	var s model.Site
	if err := d.db.WithContext(ctx).First(&s, id).Error; err != nil {
		return nil, fmt.Errorf("dao: find site by id: %w", err)
	}
	return &s, nil
}

// UpdateAgentMeta refreshes heartbeat-derived columns on a Site row.
// Empty string fields are skipped so a heartbeat that omits hostname doesn't
// clobber a previously-set value. last_seen_at is always updated. lan_cidrs
// is JSON-encoded into the lan_cidrs_json column.
//
// lan_cidrs is only written when len(lanCidrs) > 0 — Bootstrap passes nil
// since BootstrapRequest doesn't carry them; Heartbeat is the channel for
// CIDR updates.
//
// Status is intentionally NOT touched here: the policy decision of whether a
// heartbeat should flip Status to online belongs in the handler (e.g., do not
// mark online when the project is disabled). Callers should use SetStatus
// after a successful UpdateAgentMeta.
func (d *SiteDAO) UpdateAgentMeta(ctx context.Context, siteID uint64, hostname, osName, agentVersion string, lanCidrs []string) error {
	now := time.Now().UTC()
	updates := map[string]any{
		"last_seen_at": &now,
	}
	if hostname != "" {
		updates["hostname"] = hostname
	}
	if osName != "" {
		updates["os"] = osName
	}
	if agentVersion != "" {
		updates["agent_version"] = agentVersion
	}
	if len(lanCidrs) > 0 {
		b, err := json.Marshal(lanCidrs)
		if err != nil {
			return fmt.Errorf("dao: marshal lan_cidrs: %w", err)
		}
		updates["lan_cidrs_json"] = string(b)
	}
	res := d.db.WithContext(ctx).Model(&model.Site{}).
		Where("id = ?", siteID).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("dao: update agent meta: %w", res.Error)
	}
	return nil
}

// SetStatus updates only the Site.Status column for siteID. Used by the
// agent gRPC handler to flip Status to online after a successful Bootstrap or
// Heartbeat against an active project.
func (d *SiteDAO) SetStatus(ctx context.Context, siteID uint64, s model.SiteStatus) error {
	res := d.db.WithContext(ctx).Model(&model.Site{}).
		Where("id = ?", siteID).
		Update("status", s)
	if res.Error != nil {
		return fmt.Errorf("dao: set site status: %w", res.Error)
	}
	return nil
}

// MarkStaleOffline updates every site whose status is online and last_seen_at
// is before threshold to status=offline. Returns the count of rows updated.
//
// Sites with status=pending are intentionally skipped: a "never seen yet" row
// is more useful to the human eye than a misleading offline flip. Sites with
// last_seen_at IS NULL are also skipped — that's the same "never reported"
// state, just without the explicit pending status.
func (d *SiteDAO) MarkStaleOffline(ctx context.Context, threshold time.Time) (int64, error) {
	res := d.db.WithContext(ctx).Model(&model.Site{}).
		Where("status = ? AND last_seen_at IS NOT NULL AND last_seen_at < ?",
			model.SiteStatusOnline, threshold).
		Update("status", model.SiteStatusOffline)
	if res.Error != nil {
		return 0, fmt.Errorf("dao: mark stale offline: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// ListByProject returns up to pageSize sites in projectID, paged by ID cursor.
func (d *SiteDAO) ListByProject(ctx context.Context, projectID uint64, pageSize int, pageToken string) ([]model.Site, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	q := d.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("id ASC").
		Limit(pageSize)
	if pageToken != "" {
		afterID, err := strconv.ParseUint(pageToken, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPageToken, err)
		}
		q = q.Where("id > ?", afterID)
	}
	var out []model.Site
	if err := q.Find(&out).Error; err != nil {
		return nil, fmt.Errorf("dao: list sites: %w", err)
	}
	return out, nil
}

// NextSitePageToken returns the page-token for fetching the page after `page`.
func NextSitePageToken(page []model.Site) string {
	if len(page) == 0 {
		return ""
	}
	return strconv.FormatUint(page[len(page)-1].ID, 10)
}

// Update writes mutable fields back. Caller must already have fetched the site.
func (d *SiteDAO) Update(ctx context.Context, s *model.Site) error {
	if s.ID == 0 {
		return fmt.Errorf("dao: update site: missing ID")
	}
	res := d.db.WithContext(ctx).Save(s)
	if res.Error != nil {
		return fmt.Errorf("dao: update site: %w", res.Error)
	}
	return nil
}

// Delete soft-deletes the site (services are NOT cascade-deleted at DAO; the
// service handler is responsible for force semantics).
func (d *SiteDAO) Delete(ctx context.Context, id uint64) error {
	res := d.db.WithContext(ctx).Delete(&model.Site{}, id)
	if res.Error != nil {
		return fmt.Errorf("dao: delete site: %w", res.Error)
	}
	return nil
}

// CountServices returns live service count for siteID.
func (d *SiteDAO) CountServices(ctx context.Context, siteID uint64) (int64, error) {
	var n int64
	err := d.db.WithContext(ctx).Model(&model.Service{}).
		Where("site_id = ?", siteID).
		Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("dao: count services: %w", err)
	}
	return n, nil
}

// Db returns the underlying *gorm.DB.
func (d *SiteDAO) Db() *gorm.DB { return d.db }

// -----------------------------------------------------------------------------
// SiteAgentTokenDAO: per-site long-lived agent credentials.
// -----------------------------------------------------------------------------

type SiteAgentTokenDAO struct{ db *gorm.DB }

func NewSiteAgentTokenDAO(db *gorm.DB) *SiteAgentTokenDAO { return &SiteAgentTokenDAO{db: db} }

// Issue rotates the live agent token for siteID: any existing live token is
// soft-deleted, then a new token is issued. Returns the persisted record and
// the raw token (shown to the operator once).
func (d *SiteAgentTokenDAO) Issue(ctx context.Context, siteID uint64, ttl time.Duration) (*model.SiteAgentToken, string, error) {
	raw, hash, err := auth.IssueToken()
	if err != nil {
		return nil, "", err
	}
	var rec *model.SiteAgentToken
	err = d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Soft-delete any existing live token for the site.
		if err := tx.Where("site_id = ?", siteID).Delete(&model.SiteAgentToken{}).Error; err != nil {
			return err
		}
		var exp *time.Time
		if ttl > 0 {
			t := time.Now().UTC().Add(ttl)
			exp = &t
		}
		rec = &model.SiteAgentToken{
			SiteID:    siteID,
			TokenHash: hash,
			ExpiresAt: exp,
		}
		return tx.Create(rec).Error
	})
	if err != nil {
		return nil, "", fmt.Errorf("dao: issue site agent token: %w", err)
	}
	return rec, raw, nil
}

// ValidateRaw looks up the live, non-expired token for rawToken and returns
// the owning site_id.
func (d *SiteAgentTokenDAO) ValidateRaw(ctx context.Context, raw string) (uint64, error) {
	hash := auth.HashToken(raw)
	var rec model.SiteAgentToken
	q := d.db.WithContext(ctx).Where("token_hash = ?", hash)
	q = q.Where("expires_at IS NULL OR expires_at > ?", time.Now().UTC())
	err := q.First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, fmt.Errorf("dao: site token: invalid or expired")
		}
		return 0, fmt.Errorf("dao: site token lookup: %w", err)
	}
	now := time.Now().UTC()
	if err := d.db.WithContext(ctx).Model(&model.SiteAgentToken{}).
		Where("id = ?", rec.ID).Update("last_used_at", &now).Error; err != nil {
		fmt.Fprintf(os.Stderr, "dao: site token last_used_at update: %v\n", err)
	}
	return rec.SiteID, nil
}
