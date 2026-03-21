package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/dan4thewin/tibbitwiki/internal/rcs"
	"github.com/dan4thewin/tibbitwiki/internal/store"
	"github.com/dan4thewin/tibbitwiki/internal/tid"
	"github.com/fsnotify/fsnotify"
)

// TestReloadFileAddsToStore verifies that reloadFile reads a .md
// file and adds it to the store.
func TestReloadFileAddsToStore(t *testing.T) {
	dir := t.TempDir()
	s := store.New()
	w := &Watcher{dir: dir, store: s}

	// Write a .md file.
	content := "title: TestReload\ntags: Watcher\n\nBody here."
	path := filepath.Join(dir, "TestReload.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	w.reloadFile(path)

	got := s.Get("TestReload")
	if got == nil {
		t.Fatal("expected note in store, got nil")
	}
	if got.Text() != "Body here." {
		t.Errorf("text = %q, want %q", got.Text(), "Body here.")
	}
	if s.FilePath("TestReload") != path {
		t.Errorf("path = %q, want %q", s.FilePath("TestReload"), path)
	}
}

// TestReloadFileDeletedRemovesFromStore verifies that reloading a
// nonexistent file removes the note from the store.
func TestReloadFileDeletedRemovesFromStore(t *testing.T) {
	dir := t.TempDir()
	s := store.New()
	w := &Watcher{dir: dir, store: s}

	// Pre-populate the store.
	td := &tid.Note{Fields: map[string]string{"title": "Gone"}}
	s.PutWithPath(td, filepath.Join(dir, "Gone.md"))

	// Reload a path that doesn't exist on disk.
	w.reloadFile(filepath.Join(dir, "Gone.md"))

	if got := s.Get("Gone"); got != nil {
		t.Errorf("expected nil after delete, got %+v", got)
	}
}

// TestReloadFileBadParse verifies that a malformed .md file is
// skipped without crashing.
func TestReloadFileBadParse(t *testing.T) {
	dir := t.TempDir()
	s := store.New()
	w := &Watcher{dir: dir, store: s}

	// Write a malformed .md file (no title).
	path := filepath.Join(dir, "Bad.md")
	if err := os.WriteFile(path, []byte("tags: Oops\n\nNo title."), 0644); err != nil {
		t.Fatal(err)
	}

	// Should not panic.
	w.reloadFile(path)

	if s.Count() != 0 {
		t.Errorf("store count = %d, want 0 (bad file should be skipped)", s.Count())
	}
}

// TestHandleEventFiltersTid verifies that handleEvent ignores
// non-.md files and temp files.
func TestHandleEventFiltersTid(t *testing.T) {
	w := &Watcher{
		pending: make(map[string]time.Time),
	}

	// Non-tid file — should be ignored.
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
// debounce window are not processed.
func TestProcessPendingDebounce(t *testing.T) {
	dir := t.TempDir()
	s := store.New()

	// Write a .md file so reloadFile can read it.
	content := "title: Debounced\n\nBody."
	path := filepath.Join(dir, "Debounced.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	w := &Watcher{
		dir:      dir,
		store:    s,
		pending:  make(map[string]time.Time),
		debounce: 200 * time.Millisecond,
	}

	// Add event with "now" timestamp — should NOT be processed yet.
	w.pending[path] = time.Now()
	w.processPending()
	if s.Get("Debounced") != nil {
		t.Error("event should not be processed within debounce window")
	}

	// Backdate the event — should now be processed.
	w.pending[path] = time.Now().Add(-300 * time.Millisecond)
	w.processPending()
	if s.Get("Debounced") == nil {
		t.Error("event should be processed after debounce window")
	}
}

// fakeEvent creates a minimal fsnotify.Event for testing.
func fakeEvent(name string) fsnotify.Event {
	return fsnotify.Event{Name: name, Op: fsnotify.Write}
}

// --- Lifecycle tests covering New, Start, Stop, eventLoop, debounceLoop ---

// pollStore polls the store until check returns true or timeout expires.
func pollStore(t *testing.T, check func() bool, timeout time.Duration) bool {
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
	s := store.New()

	w, err := New(dir, s)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(func() { w.watcher.Close() })

	if w.dir != dir {
		t.Errorf("dir = %q, want %q", w.dir, dir)
	}
	if w.store != s {
		t.Error("store not set correctly")
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
// without error or panic. The goroutines launched by Start should
// exit cleanly when Stop is called.
func TestStartAndStopLifecycle(t *testing.T) {
	dir := t.TempDir()
	s := store.New()

	w, err := New(dir, s)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := w.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Give the goroutines a moment to spin up.
	time.Sleep(50 * time.Millisecond)

	// Stop should not panic.
	w.Stop()

	// Allow goroutines to exit.
	time.Sleep(50 * time.Millisecond)
}

// TestStartBadDirectory verifies that Start returns an error when
// the underlying fsnotify watcher has already been closed.
func TestStartBadDirectory(t *testing.T) {
	dir := t.TempDir()
	s := store.New()

	w, err := New(dir, s)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Close the internal watcher before calling Start.
	w.watcher.Close()

	if err := w.Start(); err == nil {
		t.Fatal("Start() should return an error on a closed watcher")
	}
}

// TestFullLifecycleWriteFile is an integration test that verifies
// the complete pipeline: fsnotify detects a new .md file, the
// event flows through eventLoop → handleEvent → debounceLoop →
// processPending → reloadFile → store.
func TestFullLifecycleWriteFile(t *testing.T) {
	dir := t.TempDir()
	s := store.New()

	w, err := New(dir, s)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	// Use a short debounce to speed up the test.
	w.debounce = 100 * time.Millisecond

	if err := w.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() { w.Stop() })

	// Write a .md file into the watched directory.
	content := "title: LiveTest\n\nLive body."
	path := filepath.Join(dir, "LiveTest.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	// Poll until the note appears in the store.
	found := pollStore(t, func() bool {
		return s.Get("LiveTest") != nil
	}, 3*time.Second)
	if !found {
		t.Fatal("note 'LiveTest' never appeared in store")
	}

	got := s.Get("LiveTest")
	if got.Title() != "LiveTest" {
		t.Errorf("title = %q, want %q", got.Title(), "LiveTest")
	}
	if got.Text() != "Live body." {
		t.Errorf("text = %q, want %q", got.Text(), "Live body.")
	}
}

// TestFullLifecycleDeleteFile is an integration test that verifies
// deleting a watched .md file removes the note from the store.
func TestFullLifecycleDeleteFile(t *testing.T) {
	dir := t.TempDir()
	s := store.New()

	// Create a .md file BEFORE starting the watcher.
	content := "title: DeleteMe\n\nGoing away."
	path := filepath.Join(dir, "DeleteMe.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	// Pre-populate the store so there is something to delete.
	td, err := tid.Parse([]byte(content))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	s.PutWithPath(td, path)
	if s.Get("DeleteMe") == nil {
		t.Fatal("store should contain 'DeleteMe' before test")
	}

	w, werr := New(dir, s)
	if werr != nil {
		t.Fatalf("New() error: %v", werr)
	}
	// Use a short debounce to speed up the test.
	w.debounce = 100 * time.Millisecond

	if err := w.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() { w.Stop() })

	// Delete the file from disk.
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove error: %v", err)
	}

	// Poll until the note is removed from the store.
	gone := pollStore(t, func() bool {
		return s.Get("DeleteMe") == nil
	}, 3*time.Second)
	if !gone {
		t.Fatal("note 'DeleteMe' was not removed from store after file deletion")
	}
}

// --- gomonkey-based tests for error-logging branches ---

// TestNewFsnotifyError verifies that New returns nil and an error
// when fsnotify.NewWatcher fails. Uses the package-level newFsWatcher
// variable instead of gomonkey to avoid SIGBUS under -coverprofile.
func TestNewFsnotifyError(t *testing.T) {
	orig := newFsWatcher
	newFsWatcher = func() (*fsnotify.Watcher, error) {
		return nil, fmt.Errorf("inotify init failed")
	}
	defer func() { newFsWatcher = orig }()

	w, err := New(t.TempDir(), store.New())
	if err == nil {
		t.Fatal("expected error from New when fsnotify.NewWatcher fails")
	}
	if w != nil {
		t.Errorf("expected nil Watcher, got %v", w)
	}
}

// TestEventLoopEventsChannelClosed verifies that eventLoop returns
// when the Events channel is closed (lines 79-81 of watcher.go).
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

// TestReloadFileWithRCSSuccess verifies that reloadFile calls
// rcs.Checkin on a successful file load. We patch Checkin to succeed
// (rather than fail because rcs is not installed).
func TestReloadFileWithRCSSuccess(t *testing.T) {
	dir := t.TempDir()
	s := store.New()
	w := &Watcher{dir: dir, store: s}

	var checkedIn string
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(path string) error {
		checkedIn = path
		return nil
	})
	defer patches.Reset()

	content := "title: RCSTest\n\nBody."
	path := filepath.Join(dir, "RCSTest.md")
	os.WriteFile(path, []byte(content), 0644)
	w.reloadFile(path)

	if s.Get("RCSTest") == nil {
		t.Fatal("expected note in store")
	}
	if checkedIn != path {
		t.Errorf("rcs.Checkin called with %q, want %q", checkedIn, path)
	}
}

// TestReloadFileRCSCheckinError verifies the rcs.Checkin error log
// path (lines 179-181 of watcher.go). The note should still be
// stored even when RCS fails.
func TestReloadFileRCSCheckinError(t *testing.T) {
	dir := t.TempDir()
	s := store.New()
	w := &Watcher{dir: dir, store: s}

	patches := gomonkey.ApplyFunc(rcs.Checkin, func(path string) error {
		return fmt.Errorf("rcs unavailable")
	})
	defer patches.Reset()

	content := "title: RCSFail\n\nBody."
	path := filepath.Join(dir, "RCSFail.md")
	os.WriteFile(path, []byte(content), 0644)
	w.reloadFile(path)

	if s.Get("RCSFail") == nil {
		t.Fatal("expected note in store despite rcs.Checkin error")
	}
}

// TestEventLoopErrorsChannelClosed verifies the !ok branch on the
// Errors channel (lines 84-86 of watcher.go). We close only the
// Errors channel; eventLoop returns immediately on the !ok receive.
func TestEventLoopErrorsChannelClosed(t *testing.T) {
	// Use buffered Events so eventLoop blocks on select until
	// Errors is ready, preventing Events !ok from racing.
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
// error arrives on the Errors channel (line 87 of watcher.go).
func TestEventLoopHandlesError(t *testing.T) {
	events := make(chan fsnotify.Event)
	errors := make(chan error, 1)

	w := &Watcher{
		done:    make(chan struct{}),
		pending: make(map[string]time.Time),
		watcher: &fsnotify.Watcher{Events: events, Errors: errors},
	}

	// Send an error, then signal exit.
	errors <- fmt.Errorf("synthetic error")
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(w.done)
	}()
	w.eventLoop()
}

// TestReloadFileReadError verifies the non-NotExist read error
// branch (line 158 of watcher.go). We create a path that exists
// but is unreadable.
func TestReloadFileReadError(t *testing.T) {
	dir := t.TempDir()
	s := store.New()
	w := &Watcher{dir: dir, store: s}

	path := filepath.Join(dir, "Unreadable.md")
	os.WriteFile(path, []byte("title: Unreadable\n\nBody."), 0644)
	// Remove read permissions.
	os.Chmod(path, 0000)
	t.Cleanup(func() { os.Chmod(path, 0644) })

	// Should log the error and return without crashing.
	w.reloadFile(path)
	if s.Get("Unreadable") != nil {
		t.Error("note should not be in store after read error")
	}
}

// TestReloadFileSkipsUnchanged verifies the fieldsEqual early-return
// path (lines 169-172) and the fieldsEqual true path (line 193).
func TestReloadFileSkipsUnchanged(t *testing.T) {
	dir := t.TempDir()
	s := store.New()

	var checkinCalled bool
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(path string) error {
		checkinCalled = true
		return nil
	})
	defer patches.Reset()

	content := "title: SameContent\ntags: Test\n\nBody text."
	path := filepath.Join(dir, "SameContent.md")
	os.WriteFile(path, []byte(content), 0644)

	w := &Watcher{dir: dir, store: s}
	// First load — should add to store.
	w.reloadFile(path)
	if s.Get("SameContent") == nil {
		t.Fatal("expected note in store after first reload")
	}
	checkinCalled = false

	// Second load with identical content — should skip (no rcs call).
	w.reloadFile(path)
	if checkinCalled {
		t.Error("rcs.Checkin should not be called for unchanged content")
	}
}

// TestFieldsEqualDifferentLen covers the len(a) != len(b) branch
// (line 185 of watcher.go).
func TestFieldsEqualDifferentLen(t *testing.T) {
	a := map[string]string{"k": "v"}
	b := map[string]string{"k": "v", "k2": "v2"}
	if fieldsEqual(a, b) {
		t.Error("expected false for different-length maps")
	}
}

// TestFieldsEqualDifferentValues covers the b[k] != v branch
// (lines 189-191 of watcher.go).
func TestFieldsEqualDifferentValues(t *testing.T) {
	a := map[string]string{"k": "v1"}
	b := map[string]string{"k": "v2"}
	if fieldsEqual(a, b) {
		t.Error("expected false for different values")
	}
}

// TestFieldsEqualIdentical covers the return true path (line 193).
func TestFieldsEqualIdentical(t *testing.T) {
	a := map[string]string{"k": "v"}
	b := map[string]string{"k": "v"}
	if !fieldsEqual(a, b) {
		t.Error("expected true for identical maps")
	}
}
