package store

import (
	"testing"

	"github.com/dan4thewin/go-tiddle/internal/tid"
)

func makeTiddler(title, text string) *tid.Tiddler {
	fields := map[string]string{"title": title}
	if text != "" {
		fields["text"] = text
	}
	return &tid.Tiddler{Fields: fields}
}

// TestPutAndGet verifies that a tiddler can be stored and retrieved.
func TestPutAndGet(t *testing.T) {
	s := New()
	td := makeTiddler("Hello", "World")
	s.Put(td)

	got := s.Get("Hello")
	if got == nil {
		t.Fatal("expected tiddler, got nil")
	}
	if got.Title() != "Hello" {
		t.Errorf("title = %q, want %q", got.Title(), "Hello")
	}
	if got.Text() != "World" {
		t.Errorf("text = %q, want %q", got.Text(), "World")
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
	td := makeTiddler("Rev", "v1")
	s.Put(td)
	if r := s.Revision("Rev"); r != 1 {
		t.Errorf("revision = %d, want 1", r)
	}
	// Update the same tiddler.
	td2 := makeTiddler("Rev", "v2")
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

// TestDelete verifies that a tiddler can be deleted and is no
// longer retrievable.
func TestDelete(t *testing.T) {
	s := New()
	s.Put(makeTiddler("Gone", "bye"))
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

// TestDeleteMissing verifies that deleting a nonexistent tiddler
// returns false.
func TestDeleteMissing(t *testing.T) {
	s := New()
	if s.Delete("Nope") {
		t.Error("Delete returned true for nonexistent tiddler")
	}
}

// TestPutWithPath verifies that PutWithPath stores both the tiddler
// and its file path.
func TestPutWithPath(t *testing.T) {
	s := New()
	td := makeTiddler("Pathed", "text")
	s.PutWithPath(td, "/some/path/Pathed.tid")

	got := s.Get("Pathed")
	if got == nil {
		t.Fatal("expected tiddler, got nil")
	}
	if p := s.FilePath("Pathed"); p != "/some/path/Pathed.tid" {
		t.Errorf("path = %q, want %q", p, "/some/path/Pathed.tid")
	}
	if r := s.Revision("Pathed"); r != 1 {
		t.Errorf("revision = %d, want 1", r)
	}
}

// TestSetFilePath verifies that SetFilePath records the path without
// bumping the revision.
func TestSetFilePath(t *testing.T) {
	s := New()
	s.Put(makeTiddler("FP", "text"))
	r1 := s.Revision("FP")
	s.SetFilePath("FP", "/new/path.tid")
	r2 := s.Revision("FP")
	if r2 != r1 {
		t.Errorf("SetFilePath bumped revision: %d → %d", r1, r2)
	}
	if p := s.FilePath("FP"); p != "/new/path.tid" {
		t.Errorf("path = %q, want %q", p, "/new/path.tid")
	}
}

// TestFilePathMissing verifies that FilePath returns empty string
// for a tiddler with no recorded path.
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
	s.PutWithPath(makeTiddler("DP", "x"), "/dp.tid")
	s.Delete("DP")
	if p := s.FilePath("DP"); p != "" {
		t.Errorf("path after delete = %q, want empty", p)
	}
}

// TestAll verifies that All returns all tiddlers sorted by title.
func TestAll(t *testing.T) {
	s := New()
	s.Put(makeTiddler("Charlie", ""))
	s.Put(makeTiddler("Alpha", ""))
	s.Put(makeTiddler("Bravo", ""))

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
// tiddlers.
func TestCount(t *testing.T) {
	s := New()
	if s.Count() != 0 {
		t.Errorf("empty count = %d, want 0", s.Count())
	}
	s.Put(makeTiddler("A", ""))
	s.Put(makeTiddler("B", ""))
	if s.Count() != 2 {
		t.Errorf("count = %d, want 2", s.Count())
	}
	s.Delete("A")
	if s.Count() != 1 {
		t.Errorf("count after delete = %d, want 1", s.Count())
	}
}
