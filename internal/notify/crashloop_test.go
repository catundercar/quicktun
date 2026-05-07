package notify

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// recordingNotifier captures every Event it receives so tests can assert
// on the call count and content.
type recordingNotifier struct {
	mu     sync.Mutex
	events []Event
}

func (r *recordingNotifier) Notify(_ context.Context, e Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

func (r *recordingNotifier) calls() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, len(r.events))
	copy(out, r.events)
	return out
}

// waitFor polls f every 10ms up to 1s, failing the test on timeout. Used
// because crashloop.Record fires the notifier asynchronously.
func waitFor(t *testing.T, f func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", msg)
}

func TestCrashLoopFiresWhenThresholdReached(t *testing.T) {
	rec := &recordingNotifier{}
	cl := NewCrashLoop(3, time.Minute, rec, nil)

	cl.Record("42")
	cl.Record("42")
	require.Empty(t, rec.calls(), "fired before threshold")

	cl.Record("42") // hits threshold
	waitFor(t, func() bool { return len(rec.calls()) == 1 }, "first fire")

	got := rec.calls()[0]
	require.Equal(t, "supervisor_crash_loop", got.Type)
	require.Equal(t, "project=42", got.Subject)
	require.Equal(t, 3, got.Extra["crash_count"])
}

func TestCrashLoopResetsAfterFire(t *testing.T) {
	rec := &recordingNotifier{}
	cl := NewCrashLoop(2, time.Minute, rec, nil)

	cl.Record("1")
	cl.Record("1") // fires
	waitFor(t, func() bool { return len(rec.calls()) == 1 }, "first fire")

	// Just one more event should NOT fire (window was reset).
	cl.Record("1")
	time.Sleep(50 * time.Millisecond)
	require.Len(t, rec.calls(), 1, "should not have fired again after one event")

	// One more crosses threshold for the second window.
	cl.Record("1")
	waitFor(t, func() bool { return len(rec.calls()) == 2 }, "second fire")
}

func TestCrashLoopExpiresOldEvents(t *testing.T) {
	rec := &recordingNotifier{}
	cl := NewCrashLoop(3, time.Minute, rec, nil)

	// Stub the clock so we control event timestamps deterministically.
	t0 := time.Now()
	clock := t0
	cl.now = func() time.Time { return clock }

	cl.Record("1")
	clock = t0.Add(2 * time.Minute) // first event now outside window
	cl.Record("1")
	cl.Record("1") // only 2 events in window — must NOT fire
	time.Sleep(50 * time.Millisecond)
	require.Empty(t, rec.calls(), "should not fire — old event expired")
}

func TestCrashLoopIsolatesPerProject(t *testing.T) {
	rec := &recordingNotifier{}
	cl := NewCrashLoop(2, time.Minute, rec, nil)

	cl.Record("1")
	cl.Record("2") // different project, doesn't accumulate against project 1
	time.Sleep(20 * time.Millisecond)
	require.Empty(t, rec.calls())

	cl.Record("1") // hits threshold for project 1 only
	waitFor(t, func() bool { return len(rec.calls()) == 1 }, "project 1 fires")
	require.Equal(t, "project=1", rec.calls()[0].Subject)
}

func TestCrashLoopThresholdZeroDisabled(t *testing.T) {
	rec := &recordingNotifier{}
	cl := NewCrashLoop(0, time.Minute, rec, nil)
	for i := 0; i < 100; i++ {
		cl.Record("1")
	}
	time.Sleep(20 * time.Millisecond)
	require.Empty(t, rec.calls(), "threshold=0 disables alerts")
}

func TestCrashLoopNilSafe(t *testing.T) {
	var cl *CrashLoop
	cl.Record("1") // must not panic
}
