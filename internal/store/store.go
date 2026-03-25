// Package store provides an in-memory note store with revision
// tracking. All access is serialized through the owning wiki's
// goroutine event loop — no internal locking needed.
package store

import (
	"sort"

	"github.com/dan4thewin/tibbitwiki/internal/tid"
)

// entry bundles all per-note metadata into a single struct.
type entry struct {
	note     *tid.Note
	revision int
	filePath string
	rcsRev   string // checked-out revision ("" = at head)
	rcsHead  string // RCS head when checked out
}

// Store holds notes in memory with per-note revision counters.
type Store struct {
	entries map[string]*entry // title → entry
}

// New creates an empty Store.
func New() *Store {
	return &Store{entries: make(map[string]*entry)}
}

// Clear removes all entries from the store.
func (s *Store) Clear() {
	s.entries = make(map[string]*entry)
}

func (s *Store) getOrCreate(title string) *entry {
	e := s.entries[title]
	if e == nil {
		e = &entry{}
		s.entries[title] = e
	}
	return e
}

// Put adds or updates a note and bumps its revision counter.
// The text field is stripped; body storage is the caller's concern.
func (s *Store) Put(t *tid.Note) {
	title := t.Title()
	e := s.getOrCreate(title)
	e.note = stripText(t)
	e.revision++
}

// PutWithPath adds or updates a note and records its file path.
// The text field is stripped; body storage is the caller's concern.
func (s *Store) PutWithPath(t *tid.Note, path string) {
	title := t.Title()
	e := s.getOrCreate(title)
	e.note = stripText(t)
	e.revision++
	e.filePath = path
}

// stripText returns a copy of the note with the text field removed.
func stripText(t *tid.Note) *tid.Note {
	meta := &tid.Note{Fields: make(map[string]string, len(t.Fields))}
	for k, v := range t.Fields {
		if k != "text" {
			meta.Fields[k] = v
		}
	}
	return meta
}

// Get returns a note by title, or nil if not found.
func (s *Store) Get(title string) *tid.Note {
	if e := s.entries[title]; e != nil {
		return e.note
	}
	return nil
}

// Delete removes a note by title. Returns true if it existed.
func (s *Store) Delete(title string) bool {
	if _, ok := s.entries[title]; !ok {
		return false
	}
	delete(s.entries, title)
	return true
}

// Revision returns the current revision for a note title.
func (s *Store) Revision(title string) int {
	if e := s.entries[title]; e != nil {
		return e.revision
	}
	return 0
}

// FilePath returns the on-disk path for a note title.
func (s *Store) FilePath(title string) string {
	if e := s.entries[title]; e != nil {
		return e.filePath
	}
	return ""
}

// SetFilePath records the on-disk path for a note without
// bumping the revision (used during initial load).
func (s *Store) SetFilePath(title, path string) {
	s.getOrCreate(title).filePath = path
}

// SetRCS records the checked-out revision and head for a note.
func (s *Store) SetRCS(title, rev, head string) {
	e := s.getOrCreate(title)
	e.rcsRev = rev
	e.rcsHead = head
}

// RCS returns the checked-out revision and head for a note.
// Both are empty when the working file is at head.
func (s *Store) RCS(title string) (rev, head string) {
	if e := s.entries[title]; e != nil {
		return e.rcsRev, e.rcsHead
	}
	return "", ""
}

// All returns all notes sorted by title.
func (s *Store) All() []*tid.Note {
	result := make([]*tid.Note, 0, len(s.entries))
	for _, e := range s.entries {
		result = append(result, e.note)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Title() < result[j].Title()
	})
	return result
}

// Count returns the number of notes in the store.
func (s *Store) Count() int {
	return len(s.entries)
}
