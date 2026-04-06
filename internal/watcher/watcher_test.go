package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// TestHandleEventFiltersTid verifies that handleEvent ignores
// non-.md files and temp files.
func TestHandleEventFiltersTid(t *testing.T) {
	w := &Watcher{
		pending: make(map[string]time.Time),
	}

	// Non-md file — should be ignored.
	w.handleEvent(fakeEvent("notes.txt"))
	if len(w.pending) != 0 {
		t.Error("non-.md file should be ignored")
	}

	// Temp file starting with dot — should be ignored.
	w.handleEvent(fakeEvent(".tmp_file.md"))
	if len(w.pending) != 0 {
		t.Error("dotfile .md should be ignored")
	}

	// Temp file starting with tilde — should be ignored.
	w.handleEvent(fakeEvent("~backup.md"))
	if len(w.pending) != 0 {
		t.Error("tilde .md should be ignored")
	}

	// Valid .md file — should be queued.
	w.handleEvent(fakeEvent("/path/to/My Note.md"))
	if len(w.pending) != 1 {
		t.Errorf("pending count = %d, want 1", len(w.pending))
	}
}

// TestProcessPendingDebounce verifies that events within the
// debounce window are not processed, and events outside it are.
func TestProcessPendingDebounce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Debounced.md")
	os.WriteFile(path, []byte("title: Debounced\n\nBody."), 0644)

	var mu sync.Mutex
	var called []string
	onChange := func(p string) {
		mu.Lock()
		called = append(called, p)
		mu.Unlock()
	}

	w := &Watcher{
		dir:      dir,
		onChange: onChange,
		pending:  make(map[string]time.Time),
		debounce: 200 * time.Millisecond,
	}

	// Add event with "now" timestamp — should NOT be processed yet.
	w.pending[path] = time.Now()
	w.processPending()
	mu.Lock()
	if len(called) != 0 {
		t.Error("event should not be processed within debounce window")
	}
	mu.Unlock()

	// Backdate the event — should now be processed.
	w.pending[path] = time.Now().Add(-300 * time.Millisecond)
	w.processPending()
	mu.Lock()
	if len(called) != 1 || called[0] != path {
		t.Errorf("expected callback with %q, got %v", path, called)
	}
	mu.Unlock()
}

// fakeEvent creates a minimal fsnotify.Event for testing.
func fakeEvent(name string) fsnotify.Event {
	return fsnotify.Event{Name: name, Op: fsnotify.Write}
}

// pollCheck polls until check returns true or timeout expires.
func pollCheck(t *testing.T, check func() bool, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// TestNewCreatesWatcher verifies that New returns a valid Watcher
// with all fields initialised.
func TestNewCreatesWatcher(t *testing.T) {
	dir := t.TempDir()
	cb := func(string) {}

	w, err := New(dir, cb)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(func() { w.watcher.Close() })

	if w.dir != dir {
		t.Errorf("dir = %q, want %q", w.dir, dir)
	}
	if w.onChange == nil {
		t.Error("onChange not set")
	}
	if w.watcher == nil {
		t.Error("internal fsnotify watcher is nil")
	}
	if w.pending == nil {
		t.Error("pending map is nil")
	}
	if w.done == nil {
		t.Error("done channel is nil")
	}
	if w.debounce != 500*time.Millisecond {
		t.Errorf("debounce = %v, want 500ms", w.debounce)
	}
}

// TestStartAndStopLifecycle verifies that Start and Stop complete
// without error or panic.
func TestStartAndStopLifecycle(t *testing.T) {
	dir := t.TempDir()

	w, err := New(dir, func(string) {})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := w.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	w.Stop()
	time.Sleep(50 * time.Millisecond)
}

// TestStartBadDirectory verifies that Start returns an error when
// the underlying fsnotify watcher has already been closed.
func TestStartBadDirectory(t *testing.T) {
	dir := t.TempDir()

	w, err := New(dir, func(string) {})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	w.watcher.Close()
	if err := w.Start(); err == nil {
		t.Fatal("Start() should return an error on a closed watcher")
	}
}

// TestFullLifecycleWriteFile verifies the complete pipeline:
// fsnotify detects a new .md file → eventLoop → handleEvent →
// debounceLoop → processPending → onChange callback.
func TestFullLifecycleWriteFile(t *testing.T) {
	dir := t.TempDir()

	var mu sync.Mutex
	var calledPaths []string
	onChange := func(path string) {
		mu.Lock()
		calledPaths = append(calledPaths, path)
		mu.Unlock()
	}

	w, err := New(dir, onChange)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	w.debounce = 100 * time.Millisecond

	if err := w.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() { w.Stop() })

	// Write a .md file into the watched directory.
	path := filepath.Join(dir, "LiveTest.md")
	os.WriteFile(path, []byte("title: LiveTest\n\nLive body."), 0644)

	// Poll until callback is called with our path.
	found := pollCheck(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, p := range calledPaths {
			if p == path {
				return true
			}
		}
		return false
	}, 3*time.Second)
	if !found {
		t.Fatal("onChange was never called for LiveTest.md")
	}
}

// TestFullLifecycleDeleteFile verifies that deleting a watched .md
// file triggers the onChange callback.
func TestFullLifecycleDeleteFile(t *testing.T) {
	dir := t.TempDir()

	// Create a .md file BEFORE starting the watcher.
	path := filepath.Join(dir, "DeleteMe.md")
	os.WriteFile(path, []byte("title: DeleteMe\n\nGoing away."), 0644)

	var mu sync.Mutex
	var calledPaths []string
	onChange := func(p string) {
		mu.Lock()
		calledPaths = append(calledPaths, p)
		mu.Unlock()
	}

	w, err := New(dir, onChange)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	w.debounce = 100 * time.Millisecond

	if err := w.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() { w.Stop() })

	os.Remove(path)

	found := pollCheck(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, p := range calledPaths {
			if p == path {
				return true
			}
		}
		return false
	}, 3*time.Second)
	if !found {
		t.Fatal("onChange was never called after file deletion")
	}
}

// TestNewFsnotifyError verifies that New returns an error when
// fsnotify.NewWatcher fails.
func TestNewFsnotifyError(t *testing.T) {
	orig := newFsWatcher
	newFsWatcher = func() (*fsnotify.Watcher, error) {
		return nil, fmt.Errorf("inotify init failed")
	}
	defer func() { newFsWatcher = orig }()

	w, err := New(t.TempDir(), func(string) {})
	if err == nil {
		t.Fatal("expected error from New when fsnotify.NewWatcher fails")
	}
	if w != nil {
		t.Errorf("expected nil Watcher, got %v", w)
	}
}

// TestEventLoopEventsChannelClosed verifies that eventLoop returns
// when the Events channel is closed.
func TestEventLoopEventsChannelClosed(t *testing.T) {
	events := make(chan fsnotify.Event)
	errors := make(chan error)

	w := &Watcher{
		done:    make(chan struct{}),
		pending: make(map[string]time.Time),
		watcher: &fsnotify.Watcher{Events: events, Errors: errors},
	}

	close(events)
	done := make(chan struct{})
	go func() {
		w.eventLoop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("eventLoop did not exit after Events channel closed")
	}
}

// TestEventLoopErrorsChannelClosed verifies the !ok branch on the
// Errors channel.
func TestEventLoopErrorsChannelClosed(t *testing.T) {
	events := make(chan fsnotify.Event)
	errors := make(chan error)
	close(errors)

	w := &Watcher{
		done:    make(chan struct{}),
		pending: make(map[string]time.Time),
		watcher: &fsnotify.Watcher{Events: events, Errors: errors},
	}

	done := make(chan struct{})
	go func() {
		w.eventLoop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("eventLoop did not exit after Errors channel closed")
	}
}

// TestEventLoopHandlesError verifies the log.Printf path when an
// error arrives on the Errors channel.
func TestEventLoopHandlesError(t *testing.T) {
	events := make(chan fsnotify.Event)
	errors := make(chan error, 1)

	w := &Watcher{
		done:    make(chan struct{}),
		pending: make(map[string]time.Time),
		watcher: &fsnotify.Watcher{Events: events, Errors: errors},
	}

	errors <- fmt.Errorf("synthetic error")
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(w.done)
	}()
	w.eventLoop()
}

// TestStopCloseError verifies that a watcher.Close error is logged
// but Stop still completes.
func TestStopCloseError(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, func(string) {})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	w.closeFunc = func() error {
		return fmt.Errorf("close failed")
	}
	w.Stop()
	// Real watcher left open — clean up.
	w.watcher.Close()
}
