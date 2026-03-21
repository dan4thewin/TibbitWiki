// Package store provides an in-memory note store with revision
// tracking. It is the central data structure that the HTTP server
// reads from and the file watcher/API writes to.
package store

import (
	"sort"
	"sync"

	"github.com/dan4thewin/tibbitwiki/internal/tid"
)

// Store holds notes in memory with per-note revision counters.
type Store struct {
	mu        sync.RWMutex
	notes     map[string]*tid.Note // title → note
	revisions map[string]int       // title → change count
	// filePaths tracks title → absolute file path on disk.
	filePaths map[string]string
}

// New creates an empty Store.
func New() *Store {
	return &Store{
		notes:     make(map[string]*tid.Note),
		revisions: make(map[string]int),
		filePaths: make(map[string]string),
	}
}

// Put adds or updates a note and bumps its revision counter.
func (s *Store) Put(t *tid.Note) {
	s.mu.Lock()
	defer s.mu.Unlock()
	title := t.Title()
	s.notes[title] = t
	s.revisions[title]++
}

// PutWithPath adds or updates a note and records its file path.
func (s *Store) PutWithPath(t *tid.Note, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	title := t.Title()
	s.notes[title] = t
	s.revisions[title]++
	s.filePaths[title] = path
}

// Get returns a note by title, or nil if not found.
func (s *Store) Get(title string) *tid.Note {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.notes[title]
}

// Delete removes a note by title. Returns true if it existed.
func (s *Store) Delete(title string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.notes[title]; !ok {
		return false
	}
	delete(s.notes, title)
	delete(s.revisions, title)
	delete(s.filePaths, title)
	return true
}

// Revision returns the current revision for a note title.
func (s *Store) Revision(title string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revisions[title]
}

// FilePath returns the on-disk path for a note title.
func (s *Store) FilePath(title string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.filePaths[title]
}

// SetFilePath records the on-disk path for a note without
// bumping the revision (used during initial load).
func (s *Store) SetFilePath(title, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filePaths[title] = path
}

// All returns all notes sorted by title.
func (s *Store) All() []*tid.Note {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*tid.Note, 0, len(s.notes))
	for _, t := range s.notes {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Title() < result[j].Title()
	})
	return result
}

// Count returns the number of notes in the store.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.notes)
}
