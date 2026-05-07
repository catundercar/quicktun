// Package sweeper periodically flips online sites whose last_seen_at has gone
// stale to status=offline. Without this, sites that crash hard without
// graceful shutdown stay online forever.
//
// Heartbeat handling is unaffected: AgentService.Heartbeat continues to flip
// online + last_seen_at = now. The sweeper is the OPPOSITE direction
// (online → offline) and runs in a single background goroutine in the
// control-plane process.
package sweeper

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/metrics"
)

// Config controls the sweeper's loop cadence and staleness threshold.
//
// Both fields default to zero, which DISABLES the sweeper. Setting one but
// not the other is a misconfiguration and also disables the sweeper rather
// than running with surprising semantics.
type Config struct {
	// Interval is the period between sweeps. Typical: 30s.
	Interval time.Duration
	// OfflineAfter is the maximum time since the last heartbeat a site may go
	// without being flipped offline. Typical: 90s (~3x agent heartbeat).
	OfflineAfter time.Duration
}

// Sweeper holds dependencies for the periodic offline-flip loop.
type Sweeper struct {
	sites   *dao.SiteDAO
	cfg     Config
	lg      *zap.Logger
	metrics *metrics.ServerMetrics // may be nil; helpers are nil-safe
}

// New returns a Sweeper. A nil logger is replaced with zap.NewNop() so callers
// don't have to worry about nil-checks. metrics may be nil — every helper on
// *metrics.ServerMetrics is nil-safe.
func New(sites *dao.SiteDAO, cfg Config, lg *zap.Logger, m *metrics.ServerMetrics) *Sweeper {
	if lg == nil {
		lg = zap.NewNop()
	}
	return &Sweeper{sites: sites, cfg: cfg, lg: lg, metrics: m}
}

// Run blocks until ctx is cancelled, performing Tick every cfg.Interval.
//
// If either cfg.Interval or cfg.OfflineAfter is <= 0, Run logs the disabled
// state and returns immediately. This makes "no sweeper config" the explicit
// off switch (useful in tests and for operators who want manual control).
func (s *Sweeper) Run(ctx context.Context) {
	if s.cfg.Interval <= 0 || s.cfg.OfflineAfter <= 0 {
		s.lg.Info("sweeper disabled (interval or offline_after <= 0)",
			zap.Duration("interval", s.cfg.Interval),
			zap.Duration("offline_after", s.cfg.OfflineAfter))
		return
	}
	s.lg.Info("sweeper starting",
		zap.Duration("interval", s.cfg.Interval),
		zap.Duration("offline_after", s.cfg.OfflineAfter))

	t := time.NewTicker(s.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.lg.Info("sweeper stopping")
			return
		case <-t.C:
			if err := s.Tick(ctx); err != nil {
				s.lg.Warn("sweeper tick failed", zap.Error(err))
			}
		}
	}
}

// Tick performs a single sweep pass. Exposed for tests so callers can drive
// the loop deterministically.
func (s *Sweeper) Tick(ctx context.Context) error {
	threshold := time.Now().UTC().Add(-s.cfg.OfflineAfter)
	n, err := s.sites.MarkStaleOffline(ctx, threshold)
	if err != nil {
		return err
	}
	if n > 0 {
		// Phase 1: zap-only audit. Per-site audit log entries are deferred to
		// keep this commit small; operators see the count here and can
		// cross-reference last_seen_at via `quicktun status`.
		s.lg.Info("sweeper marked sites offline",
			zap.Int64("count", n),
			zap.Time("threshold", threshold))
		s.metrics.IncSweeperFlipped(n)
	}
	return nil
}
