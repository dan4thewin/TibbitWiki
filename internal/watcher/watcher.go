// Package watcher monitors the notes directory for file changes
// and updates the in-memory store.
package watcher

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dan4thewin/tibbitwiki/internal/rcs"
	"github.com/dan4thewin/tibbitwiki/internal/store"
	"github.com/dan4thewin/tibbitwiki/internal/tid"
	"github.com/fsnotify/fsnotify"
)

// Watcher monitors a directory for .tid file changes and updates
// the store accordingly.
type Watcher struct {
	dir     string
	store   *store.Store
	watcher *fsnotify.Watcher
	done    chan struct{}

	// debounce tracks pending file events to coalesce rapid
	// successive writes (common with cloud drives).
	mu       sync.Mutex
	pending  map[string]time.Time
	debounce time.Duration
}

// newFsWatcher is the constructor for fsnotify.Watcher, replaceable
// in tests to simulate creation failure without gomonkey (which
// crashes under -coverprofile on darwin/arm64).
var newFsWatcher = fsnotify.NewWatcher

// New creates a Watcher for the given directory.
func New(dir string, s *store.Store) (*Watcher, error) {
	fw, err := newFsWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		dir:      dir,
		store:    s,
		watcher:  fw,
		done:     make(chan struct{}),
		pending:  make(map[string]time.Time),
		debounce: 500 * time.Millisecond,
	}, nil
}

// Start begins watching for file changes. Call Stop to clean up.
func (w *Watcher) Start() error {
	if err := w.watcher.Add(w.dir); err != nil {
		return err
	}

	go w.eventLoop()
	go w.debounceLoop()
	return nil
}

// Stop halts the watcher.
func (w *Watcher) Stop() {
	close(w.done)
	w.watcher.Close()
}

// eventLoop reads fsnotify events and queues them for debouncing.
func (w *Watcher) eventLoop() {
	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("watcher error: %v", err)
		}
	}
}

// handleEvent filters and queues a single fsnotify event.
func (w *Watcher) handleEvent(event fsnotify.Event) {
	// Only care about .tid files.
	if !strings.HasSuffix(event.Name, ".tid") {
		return
	}
	// Ignore cloud drive temp files.
	base := filepath.Base(event.Name)
	if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "~") {
		return
	}

	w.mu.Lock()
	w.pending[event.Name] = time.Now()
	w.mu.Unlock()
}

// debounceLoop periodically processes pending events that have
// settled (no new events within the debounce window).
func (w *Watcher) debounceLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			w.processPending()
		}
	}
}

// processPending handles all pending events older than the debounce
// window.
func (w *Watcher) processPending() {
	w.mu.Lock()
	now := time.Now()
	ready := make(map[string]struct{})
	for path, when := range w.pending {
		if now.Sub(when) >= w.debounce {
			ready[path] = struct{}{}
			delete(w.pending, path)
		}
	}
	w.mu.Unlock()

	for path := range ready {
		w.reloadFile(path)
	}
}

// reloadFile reads a .tid file from disk and updates the store.
// If the file no longer exists, the note is removed. Skips the
// update if content hasn't changed, preventing feedback loops from
// browser PUT-backs and server disk writes.
func (w *Watcher) reloadFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File was deleted — remove from store.
			title := tid.FilenameToTitle(filepath.Base(path))
			w.store.Delete(title)
			log.Printf("watcher: removed %q", title)
			return
		}
		log.Printf("watcher: error reading %s: %v", path, err)
		return
	}

	td, err := tid.Parse(data)
	if err != nil {
		log.Printf("watcher: error parsing %s: %v", path, err)
		return
	}

	// Skip if content matches what's already in the store.
	if existing := w.store.Get(td.Title()); existing != nil {
		if fieldsEqual(existing.Fields, td.Fields) {
			return
		}
	}

	w.store.PutWithPath(td, path)
	log.Printf("watcher: reloaded %q", td.Title())

	// RCS checkin after external edit.
	if err := rcs.Checkin(path); err != nil {
		log.Printf("watcher: rcs checkin %s: %v", path, err)
	}
}

func fieldsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
