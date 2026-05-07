package notify_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/notify"
)

func TestWebhookPOSTsJSON(t *testing.T) {
	var got atomic.Pointer[notify.Event]

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var e notify.Event
		require.NoError(t, json.Unmarshal(body, &e))
		got.Store(&e)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := notify.NewWebhook(srv.URL, time.Second, nil)
	now := time.Now().UTC().Truncate(time.Second)
	err := wh.Notify(context.Background(), notify.Event{
		Type:    "supervisor_crash_loop",
		Subject: "project=42",
		Message: "rathole-server for project 42 crashed 5 times in 5m0s",
		Time:    now,
		Extra:   map[string]any{"project_id": 42, "crash_count": 5},
	})
	require.NoError(t, err)
	require.NotNil(t, got.Load())
	require.Equal(t, "supervisor_crash_loop", got.Load().Type)
	require.Equal(t, "project=42", got.Load().Subject)
	require.Equal(t, now, got.Load().Time.UTC().Truncate(time.Second))
}

func TestWebhookSurfacesNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	wh := notify.NewWebhook(srv.URL, time.Second, nil)
	err := wh.Notify(context.Background(), notify.Event{Type: "x"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
}

func TestWebhookTimesOut(t *testing.T) {
	// Server never responds; the client's timeout must surface.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := notify.NewWebhook(srv.URL, 100*time.Millisecond, nil)
	start := time.Now()
	err := wh.Notify(context.Background(), notify.Event{Type: "x"})
	require.Error(t, err)
	require.Less(t, time.Since(start), 1*time.Second, "timeout should bound the call")
}

func TestNewReturnsNoopWhenURLEmpty(t *testing.T) {
	n := notify.New("", time.Second, nil)
	// Must not panic; must succeed.
	require.NoError(t, n.Notify(context.Background(), notify.Event{Type: "x"}))
}

func TestNewReturnsWebhookWhenURLSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := notify.New(srv.URL, time.Second, nil)
	require.NoError(t, n.Notify(context.Background(), notify.Event{Type: "x"}))
}
