package notify

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// CrashLoop watches a stream of supervisor exit events per project and fires
// a notification when the per-project exit count crosses a threshold within
// a sliding window. After firing, the window resets for that project so a
// noisy supervisor doesn't spam the webhook on every subsequent restart —
// the operator gets one alert and follows up.
type CrashLoop struct {
	threshold int
	window    time.Duration
	notifier  Notifier
	lg        *zap.Logger

	mu     sync.Mutex
	events map[string][]time.Time // projectID (string) -> recent exit timestamps

	// now lets tests inject a deterministic clock. Defaults to time.Now.
	now func() time.Time
}

// NewCrashLoop wires a CrashLoop. threshold <= 0 disables it (Record becomes
// a no-op so the alert path can be turned off without rewiring upstream
// callbacks). window <= 0 collapses to a 1m default.
func NewCrashLoop(threshold int, window time.Duration, n Notifier, lg *zap.Logger) *CrashLoop {
	if window <= 0 {
		window = time.Minute
	}
	if n == nil {
		n = noopNotifier{}
	}
	if lg == nil {
		lg = zap.NewNop()
	}
	return &CrashLoop{
		threshold: threshold,
		window:    window,
		notifier:  n,
		lg:        lg,
		events:    map[string][]time.Time{},
		now:       time.Now,
	}
}

// Record adds one exit event for projectID and triggers the notifier when
// the rolling window crosses the threshold. Caller-side: call this from the
// supervisor's OnExit hook. Safe for concurrent use; the actual webhook
// dispatch runs in a detached goroutine so OnExit doesn't block on network
// IO.
//
// projectID is rendered as a string so callers using uint64 ids can pass
// fmt.Sprint(id) — keeping the alert API decoupled from the DB schema.
func (c *CrashLoop) Record(projectID string) {
	if c == nil || c.threshold <= 0 {
		return
	}
	c.mu.Lock()
	now := c.now()
	cutoff := now.Add(-c.window)
	prev := c.events[projectID]
	recent := prev[:0:0] // start fresh slice (avoid mutating shared backing array)
	for _, t := range prev {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	recent = append(recent, now)

	if len(recent) >= c.threshold {
		// Fire and reset the window so a continuously-crashing supervisor
		// doesn't spam the webhook on every subsequent restart.
		c.events[projectID] = nil
		count := len(recent)
		window := c.window
		notifier := c.notifier
		c.mu.Unlock()

		ev := Event{
			Type:    "supervisor_crash_loop",
			Subject: fmt.Sprintf("project=%s", projectID),
			Message: fmt.Sprintf("rathole-server for project %s crashed %d times in %s", projectID, count, window),
			Time:    now,
			Extra: map[string]any{
				"project_id":     projectID,
				"crash_count":    count,
				"window_seconds": window.Seconds(),
			},
		}
		// Detach the actual POST so OnExit (which runs on the supervisor
		// goroutine) returns immediately.
		go func(lg *zap.Logger) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := notifier.Notify(ctx, ev); err != nil {
				lg.Warn("crashloop: webhook failed", zap.Error(err))
			}
		}(c.lg)
		return
	}

	c.events[projectID] = recent
	c.mu.Unlock()
}
