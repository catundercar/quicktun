package relay

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/supervisor"
)

// ManagerConfig configures a Manager.
type ManagerConfig struct {
	Binary     string   // path to rathole-server (or fake in tests)
	BinaryArgs []string // extra args appended after the config path
	ConfigDir  string   // dir to write per-project config files
}

// Manager owns one *supervisor.Supervisor per active project. Each child is a
// rathole-server (or compatible) process. On mutations, callers invoke Refresh
// (re-render config + restart) or AddProject / RemoveProject.
//
// When Binary is empty, Manager runs in "render-only" mode: configs still get
// written to ConfigDir, but no child processes are spawned. Smoke tests and
// development builds use this mode.
//
// All mutating methods (AddProject, RemoveProject, Refresh, Start, Stop)
// serialize against `opMu`. This avoids races where two concurrent
// AddProject calls for the same project would both pass the existence
// check and double-spawn.
type Manager struct {
	db  *gorm.DB
	cfg ManagerConfig
	lg  *zap.Logger

	// opMu serializes all mutations to the supervisor map. Held across
	// renderToFile + supervisor.New + goroutine launch so AddProject is
	// race-free for concurrent callers targeting the same project.
	opMu sync.Mutex

	// stateMu protects rootCtx + the supervisor maps for cheap reads
	// (e.g. SupervisorCount). Held briefly inside opMu-guarded operations.
	stateMu     sync.Mutex
	rootCtx     context.Context
	supervisors map[uint64]*supervisor.Supervisor
	cancels     map[uint64]context.CancelFunc

	wg sync.WaitGroup
}

// NewManager constructs a Manager. The caller must call Start before using it.
func NewManager(db *gorm.DB, cfg ManagerConfig, lg *zap.Logger) *Manager {
	if lg == nil {
		lg = zap.NewNop()
	}
	if cfg.ConfigDir == "" {
		cfg.ConfigDir = "/tmp/quicktun-relay"
	}
	return &Manager{
		db:          db,
		cfg:         cfg,
		lg:          lg,
		supervisors: map[uint64]*supervisor.Supervisor{},
		cancels:     map[uint64]context.CancelFunc{},
	}
}

// Start reads all active projects and spawns one supervisor per project.
// The returned error indicates a startup failure (e.g., DB read or mkdir).
// Once Start returns nil, child failures don't propagate — they're logged
// + restarted by the supervisor.
func (m *Manager) Start(ctx context.Context) error {
	if err := os.MkdirAll(m.cfg.ConfigDir, 0o755); err != nil {
		return fmt.Errorf("relay: mkdir config dir: %w", err)
	}

	m.stateMu.Lock()
	m.rootCtx = ctx
	m.stateMu.Unlock()

	var projects []model.Project
	err := m.db.WithContext(ctx).
		Where("status = ?", string(model.ProjectStatusActive)).
		Find(&projects).Error
	if err != nil {
		return fmt.Errorf("relay: list projects: %w", err)
	}
	for i := range projects {
		if err := m.AddProject(ctx, projects[i].ID); err != nil {
			m.lg.Warn("relay: add project failed",
				zap.Uint64("project_id", projects[i].ID),
				zap.Error(err))
		}
	}
	return nil
}

// AddProject renders the config for projectID and (if Binary is set) starts a
// new supervisor. Idempotent: if a supervisor already exists, returns nil
// without re-rendering (use Refresh to re-render).
func (m *Manager) AddProject(ctx context.Context, projectID uint64) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	return m.addProjectLocked(ctx, projectID)
}

// addProjectLocked runs the AddProject body. opMu must be held.
func (m *Manager) addProjectLocked(ctx context.Context, projectID uint64) error {
	m.stateMu.Lock()
	if _, ok := m.supervisors[projectID]; ok {
		m.stateMu.Unlock()
		return nil
	}
	rootCtx := m.rootCtx
	m.stateMu.Unlock()

	cfgPath, err := m.renderToFile(ctx, projectID)
	if err != nil {
		return err
	}

	if m.cfg.Binary == "" {
		return nil // render-only mode (smoke / dev)
	}

	if rootCtx == nil {
		rootCtx = context.Background()
	}

	sup := supervisor.New(supervisor.Spec{
		Name:   fmt.Sprintf("rathole-project-%d", projectID),
		Binary: m.cfg.Binary,
		Args:   append([]string{cfgPath}, m.cfg.BinaryArgs...),
		OnLog: func(line, src string) {
			m.lg.Info("relay child log",
				zap.Uint64("project_id", projectID),
				zap.String("source", src),
				zap.String("line", line))
		},
	}, m.lg)

	childCtx, cancel := context.WithCancel(rootCtx)
	m.stateMu.Lock()
	m.supervisors[projectID] = sup
	m.cancels[projectID] = cancel
	m.stateMu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		sup.Run(childCtx)
	}()
	return nil
}

// RemoveProject stops the supervisor for projectID and discards it.
// Best-effort: returns nil even if no such supervisor exists.
func (m *Manager) RemoveProject(ctx context.Context, projectID uint64) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	return m.removeProjectLocked(projectID)
}

// removeProjectLocked runs the RemoveProject body. opMu must be held.
func (m *Manager) removeProjectLocked(projectID uint64) error {
	m.stateMu.Lock()
	cancel, ok := m.cancels[projectID]
	delete(m.supervisors, projectID)
	delete(m.cancels, projectID)
	m.stateMu.Unlock()
	if ok {
		cancel()
	}
	return nil
}

// Refresh re-renders the config for projectID and restarts the supervisor so
// the new config takes effect (rathole does not support SIGHUP reload).
//
// If projectID has no current supervisor (e.g. a brand-new project), Refresh
// behaves like AddProject. Returns an error from renderToFile if the project
// no longer exists in the DB.
func (m *Manager) Refresh(ctx context.Context, projectID uint64) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.stateMu.Lock()
	_, hasSup := m.supervisors[projectID]
	m.stateMu.Unlock()

	if !hasSup {
		return m.addProjectLocked(ctx, projectID)
	}

	// Re-render first so we error out (e.g. project deleted) before stopping
	// the running supervisor. If render succeeds, restart to pick up changes.
	if _, err := m.renderToFile(ctx, projectID); err != nil {
		return err
	}
	if m.cfg.Binary == "" {
		return nil
	}
	if err := m.removeProjectLocked(projectID); err != nil {
		return err
	}
	return m.addProjectLocked(ctx, projectID)
}

// SupervisorCount reports how many supervisors are currently tracked.
func (m *Manager) SupervisorCount() int {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	return len(m.supervisors)
}

// Stop cancels every supervisor and waits for them to exit.
func (m *Manager) Stop() {
	m.opMu.Lock()
	m.stateMu.Lock()
	for _, c := range m.cancels {
		c()
	}
	m.cancels = map[uint64]context.CancelFunc{}
	m.supervisors = map[uint64]*supervisor.Supervisor{}
	m.stateMu.Unlock()
	m.opMu.Unlock()
	m.wg.Wait()
}

func (m *Manager) renderToFile(ctx context.Context, projectID uint64) (string, error) {
	pdao := dao.NewProjectDAO(m.db)
	sdao := dao.NewSiteDAO(m.db)
	svcdao := dao.NewServiceDAO(m.db)

	p, err := pdao.FindByID(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("relay: find project: %w", err)
	}

	sites, err := sdao.ListByProject(ctx, p.ID, 1000, "")
	if err != nil {
		return "", fmt.Errorf("relay: list sites: %w", err)
	}

	var bindings []ServiceBinding
	for _, site := range sites {
		var tokenHash string
		var tk model.SiteAgentToken
		err := m.db.WithContext(ctx).
			Where("site_id = ?", site.ID).
			First(&tk).Error
		if err == nil {
			tokenHash = tk.TokenHash
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("relay: lookup site token: %w", err)
		}

		svcs, err := svcdao.ListBySite(ctx, site.ID, 1000, "")
		if err != nil {
			return "", fmt.Errorf("relay: list services: %w", err)
		}
		for _, svc := range svcs {
			if svc.RelayPort == nil {
				continue
			}
			bindings = append(bindings, ServiceBinding{
				SiteSlug:    site.Name,
				ServiceSlug: svc.Name,
				RelayPort:   *svc.RelayPort,
				AgentToken:  tokenHash,
			})
		}
	}

	toml, err := RenderRatholeServer(p, bindings)
	if err != nil {
		return "", err
	}

	path := filepath.Join(m.cfg.ConfigDir, p.Slug+".toml")
	if err := os.WriteFile(path, []byte(toml), 0o600); err != nil {
		return "", fmt.Errorf("relay: write config: %w", err)
	}
	return path, nil
}
