package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dan4thewin/go-tiddle/internal/store"
	"github.com/dan4thewin/go-tiddle/internal/tid"
	"github.com/fsnotify/fsnotify"
)

// TestReloadFileAddsToStore verifies that reloadFile reads a .tid
// file and adds it to the store.
func TestReloadFileAddsToStore(t *testing.T) {
	dir := t.TempDir()
	s := store.New()
	w := &Watcher{dir: dir, store: s}

	// Write a .tid file.
	content := "title: TestReload\ntags: Watcher\n\nBody here."
	path := filepath.Join(dir, "TestReload.tid")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	w.reloadFile(path)

	got := s.Get("TestReload")
	if got == nil {
		t.Fatal("expected tiddler in store, got nil")
	}
	if got.Text() != "Body here." {
		t.Errorf("text = %q, want %q", got.Text(), "Body here.")
	}
	if s.FilePath("TestReload") != path {
		t.Errorf("path = %q, want %q", s.FilePath("TestReload"), path)
	}
}

// TestReloadFileDeletedRemovesFromStore verifies that reloading a
// nonexistent file removes the tiddler from the store.
func TestReloadFileDeletedRemovesFromStore(t *testing.T) {
	dir := t.TempDir()
	s := store.New()
	w := &Watcher{dir: dir, store: s}

	// Pre-populate the store.
	td := &tid.Tiddler{Fields: map[string]string{"title": "Gone"}}
	s.PutWithPath(td, filepath.Join(dir, "Gone.tid"))

	// Reload a path that doesn't exist on disk.
	w.reloadFile(filepath.Join(dir, "Gone.tid"))

	if got := s.Get("Gone"); got != nil {
		t.Errorf("expected nil after delete, got %+v", got)
	}
}

// TestReloadFileBadParse verifies that a malformed .tid file is
// skipped without crashing.
func TestReloadFileBadParse(t *testing.T) {
	dir := t.TempDir()
	s := store.New()
	w := &Watcher{dir: dir, store: s}

	// Write a malformed .tid file (no title).
	path := filepath.Join(dir, "Bad.tid")
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
// non-.tid files and temp files.
func TestHandleEventFiltersTid(t *testing.T) {
	w := &Watcher{
		pending: make(map[string]time.Time),
	}

	// Non-tid file — should be ignored.
	w.handleEvent(fakeEvent("notes.txt"))
	if len(w.pending) != 0 {
		t.Error("non-.tid file should be ignored")
	}

	// Temp file starting with dot — should be ignored.
	w.handleEvent(fakeEvent(".tmp_file.tid"))
	if len(w.pending) != 0 {
		t.Error("dotfile .tid should be ignored")
	}

	// Temp file starting with tilde — should be ignored.
	w.handleEvent(fakeEvent("~backup.tid"))
	if len(w.pending) != 0 {
		t.Error("tilde .tid should be ignored")
	}

	// Valid .tid file — should be queued.
	w.handleEvent(fakeEvent("/path/to/My Tiddler.tid"))
	if len(w.pending) != 1 {
		t.Errorf("pending count = %d, want 1", len(w.pending))
	}
}

// TestProcessPendingDebounce verifies that events within the
// debounce window are not processed.
func TestProcessPendingDebounce(t *testing.T) {
	dir := t.TempDir()
	s := store.New()

	// Write a .tid file so reloadFile can read it.
	content := "title: Debounced\n\nBody."
	path := filepath.Join(dir, "Debounced.tid")
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
