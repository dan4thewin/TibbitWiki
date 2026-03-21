// Package store provides an in-memory tiddler store with revision
// tracking. It is the central data structure that the HTTP server
// reads from and the file watcher/API writes to.
package store

import (
	"sort"
	"sync"

	"github.com/dan4thewin/go-tiddle/internal/tid"
)

// Store holds tiddlers in memory with per-tiddler revision counters.
type Store struct {
	mu        sync.RWMutex
	tiddlers  map[string]*tid.Tiddler // title → tiddler
	revisions map[string]int          // title → change count
	// filePaths tracks title → absolute file path on disk.
	filePaths map[string]string
}

// New creates an empty Store.
func New() *Store {
	return &Store{
		tiddlers:  make(map[string]*tid.Tiddler),
		revisions: make(map[string]int),
		filePaths: make(map[string]string),
	}
}

// Put adds or updates a tiddler and bumps its revision counter.
func (s *Store) Put(t *tid.Tiddler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	title := t.Title()
	s.tiddlers[title] = t
	s.revisions[title]++
}

// PutWithPath adds or updates a tiddler and records its file path.
func (s *Store) PutWithPath(t *tid.Tiddler, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	title := t.Title()
	s.tiddlers[title] = t
	s.revisions[title]++
	s.filePaths[title] = path
}

// Get returns a tiddler by title, or nil if not found.
func (s *Store) Get(title string) *tid.Tiddler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tiddlers[title]
}

// Delete removes a tiddler by title. Returns true if it existed.
func (s *Store) Delete(title string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tiddlers[title]; !ok {
		return false
	}
	delete(s.tiddlers, title)
	delete(s.revisions, title)
	delete(s.filePaths, title)
	return true
}

// Revision returns the current revision for a tiddler title.
func (s *Store) Revision(title string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revisions[title]
}

// FilePath returns the on-disk path for a tiddler title.
func (s *Store) FilePath(title string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.filePaths[title]
}

// SetFilePath records the on-disk path for a tiddler without
// bumping the revision (used during initial load).
func (s *Store) SetFilePath(title, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filePaths[title] = path
}

// All returns all tiddlers sorted by title.
func (s *Store) All() []*tid.Tiddler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*tid.Tiddler, 0, len(s.tiddlers))
	for _, t := range s.tiddlers {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Title() < result[j].Title()
	})
	return result
}

// Count returns the number of tiddlers in the store.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tiddlers)
}
