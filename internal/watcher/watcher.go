// Package watcher monitors the notes directory for file changes
// and notifies a callback.
package watcher

import (
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors a directory for .md file changes and calls
// onChange with the path of each changed file.
type Watcher struct {
	dir      string
	onChange func(string)
	watcher  *fsnotify.Watcher
	done     chan struct{}

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

// New creates a Watcher for the given directory. onChange is called
// with the absolute path of each changed .md file after debouncing.
func New(dir string, onChange func(string)) (*Watcher, error) {
	fw, err := newFsWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		dir:      dir,
		onChange: onChange,
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
	// Only care about .md files.
	if !strings.HasSuffix(event.Name, ".md") {
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
		w.onChange(path)
	}
}
