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

// ErrManagerClosed is returned by AddProject / Refresh when the Manager has
// already been stopped.
var ErrManagerClosed = errors.New("relay: manager closed")

// ManagerConfig configures a Manager.
type ManagerConfig struct {
	Binary     string   // path to rathole-server (or fake in tests)
	BinaryArgs []string // extra args appended after the config path
	ConfigDir  string   // dir to write per-project config files
}

// projectSup tracks one running supervisor goroutine. `done` is closed when
// the goroutine returns; `cancel` requests it to exit. The pair lets Refresh
// tear an old supervisor down and wait for it to release its bound ports
// before spawning the replacement.
type projectSup struct {
	sup    *supervisor.Supervisor
	cancel context.CancelFunc
	done   chan struct{} // closed when the goroutine exits
}

// Manager owns one *supervisor.Supervisor per active project. Each child is a
// rathole-server (or compatible) process. On mutations, callers invoke Refresh
// (re-render config + restart) or AddProject / RemoveProject.
//
// When Binary is empty, Manager runs in "render-only" mode: configs still get
// written to ConfigDir, but no child processes are spawned. Smoke tests and
// development builds use this mode.
//
// Concurrency invariant: `mu` is held for short critical sections that touch
// `sups`, `rootCtx`, or `closed`. It MUST NOT be held across DB IO, file IO,
// or goroutine launches — callers release it before doing any of those.
// Mutating helpers (`addProjectLocked` / `removeProjectLocked`) acquire and
// release `mu` themselves at the points where they touch state.
type Manager struct {
	db  *gorm.DB
	cfg ManagerConfig
	lg  *zap.Logger

	mu      sync.Mutex
	rootCtx context.Context
	sups    map[uint64]*projectSup
	closed  bool

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
		db:   db,
		cfg:  cfg,
		lg:   lg,
		sups: map[uint64]*projectSup{},
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

	m.mu.Lock()
	m.rootCtx = ctx
	m.mu.Unlock()

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
// without re-rendering (use Refresh to re-render). Returns ErrManagerClosed
// if Stop has already been called.
func (m *Manager) AddProject(ctx context.Context, projectID uint64) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrManagerClosed
	}
	if _, ok := m.sups[projectID]; ok {
		m.mu.Unlock()
		return nil
	}
	rootCtx := m.rootCtx
	m.mu.Unlock()

	return m.spawn(ctx, projectID, rootCtx)
}

// spawn renders the config and (when Binary is set) starts a supervisor
// goroutine. Caller is responsible for ensuring no other supervisor exists
// for projectID — typically by holding a higher-level invariant such as the
// re-check inside the final mu critical section below.
func (m *Manager) spawn(ctx context.Context, projectID uint64, rootCtx context.Context) error {
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
	ps := &projectSup{
		sup:    sup,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		close(ps.done)
		return ErrManagerClosed
	}
	if _, ok := m.sups[projectID]; ok {
		// A concurrent caller raced us. Drop our supervisor and accept theirs.
		m.mu.Unlock()
		cancel()
		close(ps.done)
		return nil
	}
	m.sups[projectID] = ps
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer close(ps.done)
		sup.Run(childCtx)
	}()
	return nil
}

// RemoveProject stops the supervisor for projectID and discards it.
// Best-effort: returns nil even if no such supervisor exists.
func (m *Manager) RemoveProject(ctx context.Context, projectID uint64) error {
	m.mu.Lock()
	ps, ok := m.sups[projectID]
	delete(m.sups, projectID)
	m.mu.Unlock()
	if ok {
		ps.cancel()
	}
	return nil
}

// Refresh re-renders the config for projectID and restarts the supervisor so
// the new config takes effect (rathole does not support SIGHUP reload).
//
// If projectID has no current supervisor (e.g. a brand-new project), Refresh
// behaves like AddProject. Returns an error from renderToFile if the project
// no longer exists in the DB.
//
// Refresh waits for the old supervisor's goroutine to fully exit before
// starting the replacement. This avoids a port-bind race where the new
// rathole-server tries to bind on the same control + service ports while
// the old child is still draining stdio + serving SIGTERM (~5s), which
// would otherwise trip the supervisor backoff and produce a multi-second
// outage on every Refresh.
func (m *Manager) Refresh(ctx context.Context, projectID uint64) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrManagerClosed
	}
	old, hasSup := m.sups[projectID]
	m.mu.Unlock()

	if !hasSup {
		return m.AddProject(ctx, projectID)
	}

	// Re-render before tearing down so a deleted project (or any other
	// render error) leaves the existing supervisor running.
	if _, err := m.renderToFile(ctx, projectID); err != nil {
		return err
	}

	if m.cfg.Binary == "" {
		return nil
	}

	// Tear down old supervisor and wait for its goroutine to exit so the
	// replacement can bind on the same ports without racing the OS.
	old.cancel()
	select {
	case <-old.done:
	case <-ctx.Done():
		return ctx.Err()
	}

	m.mu.Lock()
	// Only delete if the slot still points at the supervisor we cancelled —
	// avoid clobbering anything a concurrent caller may have re-added.
	if cur, ok := m.sups[projectID]; ok && cur == old {
		delete(m.sups, projectID)
	}
	m.mu.Unlock()

	return m.AddProject(ctx, projectID)
}

// SupervisorCount reports how many supervisors are currently tracked.
func (m *Manager) SupervisorCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sups)
}

// Stop cancels every supervisor and waits for them to exit. After Stop
// returns, AddProject / Refresh return ErrManagerClosed.
func (m *Manager) Stop() {
	m.mu.Lock()
	m.closed = true
	sups := m.sups
	m.sups = map[uint64]*projectSup{}
	m.mu.Unlock()
	for _, ps := range sups {
		ps.cancel()
	}
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
