package dao

import (
	"context"
	"fmt"
	"strconv"

	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/model"
)

// ProjectDAO encapsulates queries against the projects table.
type ProjectDAO struct{ db *gorm.DB }

// NewProjectDAO constructs a ProjectDAO bound to db.
func NewProjectDAO(db *gorm.DB) *ProjectDAO { return &ProjectDAO{db: db} }

// Create inserts a new project. The caller must populate Slug, Name, and
// RelayPortRange. Defaults from struct tags fill DefaultMode, Backend, Status
// when left zero.
func (d *ProjectDAO) Create(ctx context.Context, p *model.Project) (*model.Project, error) {
	if err := d.db.WithContext(ctx).Create(p).Error; err != nil {
		return nil, fmt.Errorf("dao: create project: %w", err)
	}
	return p, nil
}

// FindBySlug returns the live project with the given slug.
func (d *ProjectDAO) FindBySlug(ctx context.Context, slug string) (*model.Project, error) {
	var p model.Project
	if err := d.db.WithContext(ctx).Where("slug = ?", slug).First(&p).Error; err != nil {
		return nil, fmt.Errorf("dao: find project by slug: %w", err)
	}
	return &p, nil
}

// FindByID returns the live project at id.
func (d *ProjectDAO) FindByID(ctx context.Context, id uint64) (*model.Project, error) {
	var p model.Project
	if err := d.db.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, fmt.Errorf("dao: find project by id: %w", err)
	}
	return &p, nil
}

// List returns up to pageSize projects starting after the project ID encoded
// in pageToken. An empty token starts from the beginning.
func (d *ProjectDAO) List(ctx context.Context, pageSize int, pageToken string) ([]model.Project, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	q := d.db.WithContext(ctx).Order("id ASC").Limit(pageSize)
	if pageToken != "" {
		afterID, err := strconv.ParseUint(pageToken, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("dao: invalid page token: %w", err)
		}
		q = q.Where("id > ?", afterID)
	}
	var out []model.Project
	if err := q.Find(&out).Error; err != nil {
		return nil, fmt.Errorf("dao: list projects: %w", err)
	}
	return out, nil
}

// NextProjectPageToken returns the page token to fetch the page AFTER the
// given page. Returns "" when the page is empty.
func NextProjectPageToken(page []model.Project) string {
	if len(page) == 0 {
		return ""
	}
	return strconv.FormatUint(page[len(page)-1].ID, 10)
}

// Update writes mutable fields back to the row. Caller must already have
// fetched the project (so ID is set). Soft-delete state is unaffected.
func (d *ProjectDAO) Update(ctx context.Context, p *model.Project) error {
	if p.ID == 0 {
		return fmt.Errorf("dao: update project: missing ID")
	}
	res := d.db.WithContext(ctx).Save(p)
	if res.Error != nil {
		return fmt.Errorf("dao: update project: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("dao: update project: no rows affected")
	}
	return nil
}

// Delete soft-deletes the project (and via gorm cascade settings, NOT the
// related sites — those need explicit handling). Idempotent: returns nil if
// already deleted.
func (d *ProjectDAO) Delete(ctx context.Context, id uint64) error {
	res := d.db.WithContext(ctx).Delete(&model.Project{}, id)
	if res.Error != nil {
		return fmt.Errorf("dao: delete project: %w", res.Error)
	}
	return nil
}

// CountSites returns the number of live sites belonging to projectID.
func (d *ProjectDAO) CountSites(ctx context.Context, projectID uint64) (int64, error) {
	var n int64
	err := d.db.WithContext(ctx).Model(&model.Site{}).
		Where("project_id = ?", projectID).
		Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("dao: count sites: %w", err)
	}
	return n, nil
}

// Db returns the underlying *gorm.DB. Service handlers use this for
// cross-table queries (e.g., counting access grants) where adding a
// dedicated DAO method would be overkill.
func (d *ProjectDAO) Db() *gorm.DB { return d.db }

// ListAccessible returns up to pageSize projects that the operator has any
// access grant on, starting after pageToken (project ID).
func (d *ProjectDAO) ListAccessible(ctx context.Context, operatorID uint64, pageSize int, pageToken string) ([]model.Project, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	q := d.db.WithContext(ctx).
		Where("id IN (?)",
			d.db.Session(&gorm.Session{NewDB: true}).
				Table("operator_project_access").
				Select("project_id").
				Where("operator_id = ? AND deleted_at IS NULL", operatorID),
		).
		Order("id ASC").
		Limit(pageSize)
	if pageToken != "" {
		afterID, err := strconv.ParseUint(pageToken, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("dao: invalid page token: %w", err)
		}
		q = q.Where("id > ?", afterID)
	}
	var out []model.Project
	if err := q.Find(&out).Error; err != nil {
		return nil, fmt.Errorf("dao: list accessible projects: %w", err)
	}
	return out, nil
}
