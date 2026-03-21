package loader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dan4thewin/tibbitwiki/internal/store"
)

// TestLoadValidDirectory verifies that Load reads all .tid files
// from a directory and populates the store.
func TestLoadValidDirectory(t *testing.T) {
	dir := t.TempDir()

	// Write two valid .tid files.
	writeFile(t, dir, "Alpha.tid", "title: Alpha\ntags: Test\n\nAlpha body.")
	writeFile(t, dir, "Bravo.tid", "title: Bravo\n\nBravo body.")

	// Write a non-.tid file that should be ignored.
	writeFile(t, dir, "notes.txt", "not a tiddler")

	s := store.New()
	n, err := Load(dir, s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 2 {
		t.Errorf("loaded %d, want 2", n)
	}
	if s.Get("Alpha") == nil {
		t.Error("expected Alpha in store")
	}
	if s.Get("Bravo") == nil {
		t.Error("expected Bravo in store")
	}
	// Verify file path is recorded.
	if p := s.FilePath("Alpha"); p != filepath.Join(dir, "Alpha.tid") {
		t.Errorf("path = %q, want %q", p, filepath.Join(dir, "Alpha.tid"))
	}
}

// TestLoadSkipsMalformed verifies that malformed .tid files are
// skipped without error.
func TestLoadSkipsMalformed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Good.tid", "title: Good\n\nBody.")
	writeFile(t, dir, "Bad.tid", "no title here\n\nOops.")

	s := store.New()
	n, err := Load(dir, s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("loaded %d, want 1", n)
	}
	if s.Get("Good") == nil {
		t.Error("expected Good in store")
	}
}

// TestLoadSystemTiddler verifies that system tiddlers ($__ prefix)
// are loaded correctly.
func TestLoadSystemTiddler(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "$__config_Test.tid", "title: $:/config/Test\n\nvalue")

	s := store.New()
	n, err := Load(dir, s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("loaded %d, want 1", n)
	}
	if s.Get("$:/config/Test") == nil {
		t.Error("expected $:/config/Test in store")
	}
}

// TestLoadEmptyDirectory verifies that an empty directory loads
// zero tiddlers without error.
func TestLoadEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	s := store.New()
	n, err := Load(dir, s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("loaded %d, want 0", n)
	}
}

// TestLoadBadDirectory verifies that a nonexistent directory returns
// an error.
func TestLoadBadDirectory(t *testing.T) {
	s := store.New()
	_, err := Load("/no/such/directory", s)
	if err == nil {
		t.Fatal("expected error for bad directory, got nil")
	}
}

// TestLoadSkipsSubdirectories verifies that subdirectories are not
// treated as tiddler files.
func TestLoadSkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Valid.tid", "title: Valid\n\nBody.")
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	s := store.New()
	n, err := Load(dir, s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("loaded %d, want 1", n)
	}
}

// TestLoadSkipsUnreadableFile verifies that a .tid file with no read
// permissions is skipped without error (covers the os.ReadFile error path).
func TestLoadSkipsUnreadableFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: running as root")
	}

	dir := t.TempDir()
	writeFile(t, dir, "Readable.tid", "title: Readable\n\nBody.")

	// Create a .tid file with no permissions.
	unreadable := filepath.Join(dir, "Unreadable.tid")
	if err := os.WriteFile(unreadable, []byte("title: Unreadable\n\nBody."), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0644) })

	s := store.New()
	n, err := Load(dir, s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("loaded %d, want 1 (only the readable file)", n)
	}
	if s.Get("Readable") == nil {
		t.Error("expected Readable in store")
	}
	if s.Get("Unreadable") != nil {
		t.Error("Unreadable should have been skipped")
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
