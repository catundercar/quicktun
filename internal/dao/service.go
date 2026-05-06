package dao

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/resource"
)

// ErrPortRangeExhausted is returned by AllocateRelayPort when no free port
// remains in the project's relay_port_range.
var ErrPortRangeExhausted = errors.New("dao: relay port range exhausted")

// ServiceDAO encapsulates queries against the services table (site-scoped).
type ServiceDAO struct{ db *gorm.DB }

// NewServiceDAO constructs a ServiceDAO bound to db.
func NewServiceDAO(db *gorm.DB) *ServiceDAO { return &ServiceDAO{db: db} }

// Create inserts a new service. Caller must populate SiteID, Name,
// TargetAddr, TargetPort. RelayPort may be nil (unassigned) or set.
func (d *ServiceDAO) Create(ctx context.Context, s *model.Service) (*model.Service, error) {
	if s.Proto == "" {
		s.Proto = model.ProtoTCP
	}
	if err := d.db.WithContext(ctx).Create(s).Error; err != nil {
		return nil, fmt.Errorf("dao: create service: %w", err)
	}
	return s, nil
}

// FindByName returns the live service with (siteID, name).
func (d *ServiceDAO) FindByName(ctx context.Context, siteID uint64, name string) (*model.Service, error) {
	var s model.Service
	err := d.db.WithContext(ctx).
		Where("site_id = ? AND name = ?", siteID, name).
		First(&s).Error
	if err != nil {
		return nil, fmt.Errorf("dao: find service by name: %w", err)
	}
	return &s, nil
}

// ListBySite returns up to pageSize services in siteID, paged by ID cursor.
func (d *ServiceDAO) ListBySite(ctx context.Context, siteID uint64, pageSize int, pageToken string) ([]model.Service, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	q := d.db.WithContext(ctx).
		Where("site_id = ?", siteID).
		Order("id ASC").
		Limit(pageSize)
	if pageToken != "" {
		afterID, err := strconv.ParseUint(pageToken, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPageToken, err)
		}
		q = q.Where("id > ?", afterID)
	}
	var out []model.Service
	if err := q.Find(&out).Error; err != nil {
		return nil, fmt.Errorf("dao: list services: %w", err)
	}
	return out, nil
}

// NextServicePageToken returns the page-token for fetching the page after `page`.
func NextServicePageToken(page []model.Service) string {
	if len(page) == 0 {
		return ""
	}
	return strconv.FormatUint(page[len(page)-1].ID, 10)
}

// Update writes mutable fields back. Caller must already have fetched the service.
func (d *ServiceDAO) Update(ctx context.Context, s *model.Service) error {
	if s.ID == 0 {
		return fmt.Errorf("dao: update service: missing ID")
	}
	res := d.db.WithContext(ctx).Save(s)
	if res.Error != nil {
		return fmt.Errorf("dao: update service: %w", res.Error)
	}
	return nil
}

// Delete soft-deletes the service.
func (d *ServiceDAO) Delete(ctx context.Context, id uint64) error {
	res := d.db.WithContext(ctx).Delete(&model.Service{}, id)
	if res.Error != nil {
		return fmt.Errorf("dao: delete service: %w", res.Error)
	}
	return nil
}

// AllocateRelayPort returns the lowest unused port in the project's
// relay_port_range. Returns ErrPortRangeExhausted if all ports are taken.
//
// The lowest port in the range is reserved for the rathole control port
// (see internal/relay/render.go), so service ports start at minP+1.
func (d *ServiceDAO) AllocateRelayPort(ctx context.Context, project *model.Project) (uint16, error) {
	minP, maxP, err := resource.ParsePortRange(project.RelayPortRange)
	if err != nil {
		return 0, fmt.Errorf("dao: allocate relay port: %w", err)
	}

	var used []uint16
	err = d.db.WithContext(ctx).
		Model(&model.Service{}).
		Joins("JOIN sites ON sites.id = services.site_id AND sites.deleted_at IS NULL").
		Where("sites.project_id = ? AND services.relay_port IS NOT NULL", project.ID).
		Pluck("services.relay_port", &used).Error
	if err != nil {
		return 0, fmt.Errorf("dao: lookup used relay ports: %w", err)
	}

	usedSet := make(map[uint16]struct{}, len(used))
	for _, p := range used {
		usedSet[p] = struct{}{}
	}

	// minP is reserved for the rathole control port; service ports start at minP+1.
	// Iterate with uint32 so the loop terminates when maxP == 65535
	// (uint16 would overflow back to 0 forever).
	for p := uint32(minP) + 1; p <= uint32(maxP); p++ {
		if _, taken := usedSet[uint16(p)]; !taken {
			return uint16(p), nil
		}
	}
	return 0, ErrPortRangeExhausted
}
