// Package notify delivers structured events to an external endpoint via HTTP
// POST. The control plane uses it to alert on supervisor crash loops and
// other operationally-significant events; site agents and auth-proxy don't
// notify anywhere themselves.
//
// The package is intentionally minimal: a Notifier interface and one
// implementation (Webhook). When the operator leaves the URL empty, New
// returns a noop notifier so callers don't need to special-case the
// "monitoring disabled" path.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// Event is a single structured notification. Type is a coarse category
// ("supervisor_crash_loop", future: "auth_failure_burst", ...). Subject is a
// short identifier (e.g. "project=42") that the receiving system can use
// for deduplication / routing. Extra is for free-form key/values that
// shouldn't be promoted to top-level fields yet.
type Event struct {
	Type    string         `json:"type"`
	Subject string         `json:"subject,omitempty"`
	Message string         `json:"message,omitempty"`
	Time    time.Time      `json:"time"`
	Extra   map[string]any `json:"extra,omitempty"`
}

// Notifier is the contract the control plane talks to when it wants to
// surface an event externally. Implementations are responsible for any
// retry / timeout policy.
type Notifier interface {
	Notify(ctx context.Context, e Event) error
}

// New returns a Webhook when url is non-empty, or a no-op notifier when it
// is. lg may be nil. timeout <= 0 falls back to 5s.
func New(url string, timeout time.Duration, lg *zap.Logger) Notifier {
	if url == "" {
		return noopNotifier{}
	}
	return NewWebhook(url, timeout, lg)
}

// Webhook POSTs a JSON-serialised Event to a configured URL.
type Webhook struct {
	URL     string
	Timeout time.Duration
	Client  *http.Client
	lg      *zap.Logger
}

// NewWebhook constructs a Webhook with sensible defaults. timeout <= 0
// becomes 5s. lg=nil becomes zap.NewNop().
func NewWebhook(url string, timeout time.Duration, lg *zap.Logger) *Webhook {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if lg == nil {
		lg = zap.NewNop()
	}
	return &Webhook{
		URL:     url,
		Timeout: timeout,
		Client:  &http.Client{Timeout: timeout},
		lg:      lg,
	}
}

// Notify serialises e and POSTs it to the configured URL. Returns an error
// when the request fails, the response status is >=400, or ctx is cancelled.
// The HTTP timeout from NewWebhook bounds total time even if ctx has no
// deadline.
func (w *Webhook) Notify(ctx context.Context, e Event) error {
	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("notify: marshal: %w", err)
	}
	// Apply timeout to the request even if the caller passed a no-deadline
	// context. The HTTP client's own Timeout backstops this for connect /
	// read failures.
	rctx, cancel := context.WithTimeout(ctx, w.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(rctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "quicktun-server/notify")

	res, err := w.Client.Do(req)
	if err != nil {
		return fmt.Errorf("notify: do: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return fmt.Errorf("notify: HTTP %d", res.StatusCode)
	}
	return nil
}

// noopNotifier discards events and never returns an error. Used when no
// webhook URL is configured so callers don't need to nil-check.
type noopNotifier struct{}

func (noopNotifier) Notify(context.Context, Event) error { return nil }
