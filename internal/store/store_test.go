package store

import (
	"testing"

	"github.com/dan4thewin/tibbitwiki/internal/tid"
)

func makeNote(title, text string) *tid.Note {
	fields := map[string]string{"title": title}
	if text != "" {
		fields["text"] = text
	}
	return &tid.Note{Fields: fields}
}

// TestPutAndGet verifies that a note can be stored and retrieved.
// Store strips text; only metadata is returned.
func TestPutAndGet(t *testing.T) {
	s := New()
	td := makeNote("Hello", "World")
	s.Put(td)

	got := s.Get("Hello")
	if got == nil {
		t.Fatal("expected note, got nil")
	}
	if got.Title() != "Hello" {
		t.Errorf("title = %q, want %q", got.Title(), "Hello")
	}
	if got.Text() != "" {
		t.Errorf("text = %q, want empty (store strips text)", got.Text())
	}
}

// TestGetMissing verifies that Get returns nil for a nonexistent title.
func TestGetMissing(t *testing.T) {
	s := New()
	if got := s.Get("Nope"); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

// TestPutBumpsRevision verifies that each Put increments the
// revision counter.
func TestPutBumpsRevision(t *testing.T) {
	s := New()
	td := makeNote("Rev", "v1")
	s.Put(td)
	if r := s.Revision("Rev"); r != 1 {
		t.Errorf("revision = %d, want 1", r)
	}
	// Update the same note.
	td2 := makeNote("Rev", "v2")
	s.Put(td2)
	if r := s.Revision("Rev"); r != 2 {
		t.Errorf("revision = %d, want 2", r)
	}
}

// TestRevisionMissing verifies that Revision returns 0 for a
// nonexistent title.
func TestRevisionMissing(t *testing.T) {
	s := New()
	if r := s.Revision("Ghost"); r != 0 {
		t.Errorf("revision = %d, want 0", r)
	}
}

// TestDelete verifies that a note can be deleted and is no
// longer retrievable.
func TestDelete(t *testing.T) {
	s := New()
	s.Put(makeNote("Gone", "bye"))
	if !s.Delete("Gone") {
		t.Error("Delete returned false, expected true")
	}
	if got := s.Get("Gone"); got != nil {
		t.Errorf("expected nil after delete, got %+v", got)
	}
	if r := s.Revision("Gone"); r != 0 {
		t.Errorf("revision after delete = %d, want 0", r)
	}
}

// TestDeleteMissing verifies that deleting a nonexistent note
// returns false.
func TestDeleteMissing(t *testing.T) {
	s := New()
	if s.Delete("Nope") {
		t.Error("Delete returned true for nonexistent note")
	}
}

// TestPutWithPath verifies that PutWithPath stores both the note
// and its file path.
func TestPutWithPath(t *testing.T) {
	s := New()
	td := makeNote("Pathed", "text")
	s.PutWithPath(td, "/some/path/Pathed.md")

	got := s.Get("Pathed")
	if got == nil {
		t.Fatal("expected note, got nil")
	}
	if p := s.FilePath("Pathed"); p != "/some/path/Pathed.md" {
		t.Errorf("path = %q, want %q", p, "/some/path/Pathed.md")
	}
	if r := s.Revision("Pathed"); r != 1 {
		t.Errorf("revision = %d, want 1", r)
	}
}

// TestSetFilePath verifies that SetFilePath records the path without
// bumping the revision.
func TestSetFilePath(t *testing.T) {
	s := New()
	s.Put(makeNote("FP", "text"))
	r1 := s.Revision("FP")
	s.SetFilePath("FP", "/new/path.md")
	r2 := s.Revision("FP")
	if r2 != r1 {
		t.Errorf("SetFilePath bumped revision: %d → %d", r1, r2)
	}
	if p := s.FilePath("FP"); p != "/new/path.md" {
		t.Errorf("path = %q, want %q", p, "/new/path.md")
	}
}

// TestFilePathMissing verifies that FilePath returns empty string
// for a note with no recorded path.
func TestFilePathMissing(t *testing.T) {
	s := New()
	if p := s.FilePath("Nope"); p != "" {
		t.Errorf("path = %q, want empty", p)
	}
}

// TestDeleteClearsFilePath verifies that Delete also removes the
// file path mapping.
func TestDeleteClearsFilePath(t *testing.T) {
	s := New()
	s.PutWithPath(makeNote("DP", "x"), "/dp.md")
	s.Delete("DP")
	if p := s.FilePath("DP"); p != "" {
		t.Errorf("path after delete = %q, want empty", p)
	}
}

// TestAll verifies that All returns all notes sorted by title.
func TestAll(t *testing.T) {
	s := New()
	s.Put(makeNote("Charlie", ""))
	s.Put(makeNote("Alpha", ""))
	s.Put(makeNote("Bravo", ""))

	all := s.All()
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3", len(all))
	}
	want := []string{"Alpha", "Bravo", "Charlie"}
	for i, td := range all {
		if td.Title() != want[i] {
			t.Errorf("all[%d].Title() = %q, want %q", i, td.Title(), want[i])
		}
	}
}

// TestAllEmpty verifies that All returns an empty slice for an
// empty store.
func TestAllEmpty(t *testing.T) {
	s := New()
	all := s.All()
	if len(all) != 0 {
		t.Errorf("len = %d, want 0", len(all))
	}
}

// TestCount verifies that Count returns the correct number of
// notes.
func TestCount(t *testing.T) {
	s := New()
	if s.Count() != 0 {
		t.Errorf("empty count = %d, want 0", s.Count())
	}
	s.Put(makeNote("A", ""))
	s.Put(makeNote("B", ""))
	if s.Count() != 2 {
		t.Errorf("count = %d, want 2", s.Count())
	}
	s.Delete("A")
	if s.Count() != 1 {
		t.Errorf("count after delete = %d, want 1", s.Count())
	}
}

// TestStripTextPreservesMetadata verifies that Put strips text
// but preserves all other fields.
func TestStripTextPreservesMetadata(t *testing.T) {
	s := New()
	td := &tid.Note{Fields: map[string]string{
		"title":   "Meta",
		"tags":    "Test",
		"text":    "body content",
		"created": "2024-01-01T00:00:00Z",
	}}
	s.Put(td)

	got := s.Get("Meta")
	if got.Text() != "" {
		t.Errorf("text should be stripped, got %q", got.Text())
	}
	if got.Fields["tags"] != "Test" {
		t.Errorf("tags = %q, want %q", got.Fields["tags"], "Test")
	}
	if got.Fields["created"] != "2024-01-01T00:00:00Z" {
		t.Errorf("created = %q", got.Fields["created"])
	}
}

// TestStripTextDoesNotMutateOriginal verifies that Put does not
// modify the caller's note.
func TestStripTextDoesNotMutateOriginal(t *testing.T) {
	s := New()
	td := makeNote("Orig", "keep me")
	s.Put(td)

	if td.Text() != "keep me" {
		t.Errorf("original note mutated: text = %q", td.Text())
	}
}

// TestClear verifies that Clear removes all notes, file paths,
// and revisions from the store.
func TestClear(t *testing.T) {
	s := New()
	s.PutWithPath(makeNote("A", "text"), "/a.md")
	s.Put(makeNote("B", "text"))
	s.SetRCS("A", "1.1", "1.3")
	s.Clear()
	if s.Count() != 0 {
		t.Errorf("count = %d, want 0", s.Count())
	}
	if s.Get("A") != nil {
		t.Error("A should be gone")
	}
	if s.FilePath("A") != "" {
		t.Error("file path should be gone")
	}
	if s.Revision("A") != 0 {
		t.Error("revision should be gone")
	}
	if r, h := s.RCS("A"); r != "" || h != "" {
		t.Errorf("rcs should be empty, got %q/%q", r, h)
	}
}

// TestSetRCSAndRCS verifies that RCS state can be stored and
// retrieved per note.
func TestSetRCSAndRCS(t *testing.T) {
	s := New()
	s.Put(makeNote("R", ""))
	s.SetRCS("R", "1.1", "1.3")
	rev, head := s.RCS("R")
	if rev != "1.1" || head != "1.3" {
		t.Errorf("rcs = %q/%q, want 1.1/1.3", rev, head)
	}
	// Clear RCS state.
	s.SetRCS("R", "", "")
	rev, head = s.RCS("R")
	if rev != "" || head != "" {
		t.Errorf("rcs after clear = %q/%q, want empty", rev, head)
	}
}

// TestRCSMissing verifies that RCS returns empty for unknown titles.
func TestRCSMissing(t *testing.T) {
	s := New()
	rev, head := s.RCS("Nope")
	if rev != "" || head != "" {
		t.Errorf("rcs = %q/%q, want empty", rev, head)
	}
}

// TestDeleteClearsRCS verifies that Delete removes RCS state.
func TestDeleteClearsRCS(t *testing.T) {
	s := New()
	s.Put(makeNote("D", ""))
	s.SetRCS("D", "1.2", "1.5")
	s.Delete("D")
	rev, head := s.RCS("D")
	if rev != "" || head != "" {
		t.Errorf("rcs after delete = %q/%q, want empty", rev, head)
	}
}

// TestGhostEntryAllSafe verifies that SetFilePath/SetRCS without
// a prior Put does not cause All() to panic on nil notes.
func TestGhostEntryAllSafe(t *testing.T) {
	s := New()
	s.SetFilePath("Ghost", "/tmp/Ghost.md")
	s.SetRCS("Ghost2", "1.1", "1.3")

	// All should not panic and should return empty — ghosts excluded.
	notes := s.All()
	if len(notes) != 0 {
		t.Errorf("All() returned %d notes, want 0 (ghosts excluded)", len(notes))
	}
}

// TestGhostEntryCountExcluded verifies Count excludes ghost entries.
func TestGhostEntryCountExcluded(t *testing.T) {
	s := New()
	s.Put(makeNote("Real", ""))
	s.SetFilePath("Ghost", "/tmp/Ghost.md")

	if c := s.Count(); c != 1 {
		t.Errorf("Count() = %d, want 1 (ghost excluded)", c)
	}
}

// TestGhostEntryGetReturnsNil verifies Get on a ghost entry
// returns nil (no note yet).
func TestGhostEntryGetReturnsNil(t *testing.T) {
	s := New()
	s.SetFilePath("Ghost", "/tmp/Ghost.md")
	if n := s.Get("Ghost"); n != nil {
		t.Errorf("Get(Ghost) = %v, want nil", n)
	}
}

// TestGetReturnsCopy verifies that mutating the Note returned by
// Get does not corrupt the store's internal state.
func TestGetReturnsCopy(t *testing.T) {
	s := New()
	s.Put(makeNote("Immutable", ""))

	got := s.Get("Immutable")
	if got == nil {
		t.Fatal("expected note")
	}
	// Mutate the returned copy.
	got.Fields["tags"] = "hacked"

	// The store's copy should be untouched.
	again := s.Get("Immutable")
	if again.Fields["tags"] == "hacked" {
		t.Error("Get returned internal pointer — mutation corrupted store")
	}
}
