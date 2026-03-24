package loader

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/dan4thewin/tibbitwiki/internal/store"
)

// TestLoadValidDirectory verifies that Load reads all .md files
// from a directory and populates the store.
func TestLoadValidDirectory(t *testing.T) {
	dir := t.TempDir()

	// Write two valid .md files.
	writeFile(t, dir, "Alpha.md", "title: Alpha\ntags: Test\n\nAlpha body.")
	writeFile(t, dir, "Bravo.md", "title: Bravo\n\nBravo body.")

	// Write a non-.md file that should be ignored.
	writeFile(t, dir, "notes.txt", "not a note")

	s := store.New()
	n, _, err := Load(dir, s, nil)
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
	if p := s.FilePath("Alpha"); p != filepath.Join(dir, "Alpha.md") {
		t.Errorf("path = %q, want %q", p, filepath.Join(dir, "Alpha.md"))
	}
}

// TestLoadSkipsMalformed verifies that malformed .md files are
// skipped without error.
func TestLoadSkipsMalformed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Good.md", "title: Good\n\nBody.")
	writeFile(t, dir, "Bad.md", "no title here\n\nOops.")

	s := store.New()
	n, _, err := Load(dir, s, nil)
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

// TestLoadEmptyDirectory verifies that an empty directory loads
// zero notes without error.
func TestLoadEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	s := store.New()
	n, _, err := Load(dir, s, nil)
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
	_, _, err := Load("/no/such/directory", s, nil)
	if err == nil {
		t.Fatal("expected error for bad directory, got nil")
	}
}

// TestLoadSkipsSubdirectories verifies that subdirectories are not
// treated as note files.
func TestLoadSkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Valid.md", "title: Valid\n\nBody.")
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	s := store.New()
	n, _, err := Load(dir, s, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("loaded %d, want 1", n)
	}
}

// TestLoadSkipsUnreadableFile verifies that a .md file with no read
// permissions is skipped without error (covers the os.ReadFile error path).
func TestLoadSkipsUnreadableFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: running as root")
	}

	dir := t.TempDir()
	writeFile(t, dir, "Readable.md", "title: Readable\n\nBody.")

	// Create a .md file with no permissions.
	unreadable := filepath.Join(dir, "Unreadable.md")
	if err := os.WriteFile(unreadable, []byte("title: Unreadable\n\nBody."), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0644) })

	s := store.New()
	n, _, err := Load(dir, s, nil)
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

// TestLoadWithSeeder verifies that the body seeder callback is
// invoked for each loaded note.
func TestLoadWithSeeder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Note.md", "title: Note\n\nBody text.")

	s := store.New()
	seeded := make(map[string]string)
	n, _, err := Load(dir, s, &testSeeder{bodies: seeded})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("loaded %d, want 1", n)
	}
	if seeded["Note"] != "Body text." {
		t.Errorf("seeded body = %q, want %q", seeded["Note"], "Body text.")
	}
}

type testSeeder struct{ bodies map[string]string }

func (s *testSeeder) SeedBody(title, text string) { s.bodies[title] = text }

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestLoadSymlinkReturnsInMap verifies that a symlinked .md file
// is NOT loaded into the store, but appears in the symlinks map.
func TestLoadSymlinkReturnsInMap(t *testing.T) {
	// Create two sibling directories simulating wikis.
	parent := t.TempDir()
	work := filepath.Join(parent, "work")
	evernote := filepath.Join(parent, "evernote")
	os.Mkdir(work, 0755)
	os.Mkdir(evernote, 0755)

	// Put a real note in work.
	writeFile(t, work, "Meeting.md", "title: Meeting\ntags: Work\n\nMeeting notes.")

	// Symlink it from evernote, plus a local note.
	os.Symlink("../work/Meeting.md", filepath.Join(evernote, "Meeting.md"))
	writeFile(t, evernote, "Local.md", "title: Local\n\nLocal body.")

	s := store.New()
	n, sym, err := Load(evernote, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Only the local note is loaded into the store.
	if n != 1 {
		t.Errorf("loaded %d, want 1", n)
	}
	if s.Get("Local") == nil {
		t.Error("expected Local in store")
	}
	// Symlinked note is NOT in the store.
	if s.Get("Meeting") != nil {
		t.Error("Meeting should not be in store (symlink)")
	}
	// But it appears in the symlinks map.
	if sym["Meeting"] != "work" {
		t.Errorf("symlinks[Meeting] = %q, want %q", sym["Meeting"], "work")
	}
}

// TestLoadRegularFileNoSymlinks verifies that a directory with no
// symlinks returns an empty symlinks map.
func TestLoadRegularFileNoSymlinks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Note.md", "title: Note\n\nBody.")

	s := store.New()
	_, sym, _ := Load(dir, s, nil)
	if len(sym) != 0 {
		t.Errorf("symlinks = %v, want empty", sym)
	}
}

// TestSymlinkOwnerNonRelative verifies that an absolute symlink
// is loaded normally (not treated as delegation target).
func TestSymlinkOwnerNonRelative(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	os.Mkdir(target, 0755)
	writeFile(t, target, "Note.md", "title: Note\n\nBody.")

	// Create a symlink using absolute path.
	src := filepath.Join(parent, "src")
	os.Mkdir(src, 0755)
	os.Symlink(filepath.Join(target, "Note.md"), filepath.Join(src, "Note.md"))

	s := store.New()
	n, sym, _ := Load(src, s, nil)
	// Absolute symlink is loaded as a normal note (ReadFile follows it).
	if n != 1 {
		t.Errorf("loaded %d, want 1", n)
	}
	if s.Get("Note") == nil {
		t.Error("expected Note in store")
	}
	if len(sym) != 0 {
		t.Errorf("symlinks = %v, want empty", sym)
	}
}

// TestLoadDanglingSymlinkQuarantined verifies that a dangling symlink
// is moved to lost+found/ and does not prevent loading other notes.
func TestLoadDanglingSymlinkQuarantined(t *testing.T) {
	parent := t.TempDir()
	wiki := filepath.Join(parent, "wiki")
	os.Mkdir(wiki, 0755)

	// A valid note.
	writeFile(t, wiki, "Good.md", "title: Good\n\nBody.")

	// A dangling symlink — target does not exist.
	os.Symlink("../other/Gone.md", filepath.Join(wiki, "Gone.md"))

	s := store.New()
	n, _, err := Load(wiki, s, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("loaded %d, want 1", n)
	}
	if s.Get("Good") == nil {
		t.Error("expected Good in store")
	}

	// Dangling symlink should be in lost+found/.
	lf := filepath.Join(wiki, "lost+found", "Gone.md")
	if _, err := os.Lstat(lf); err != nil {
		t.Errorf("expected quarantined file at %s: %v", lf, err)
	}

	// Original should be gone.
	if _, err := os.Lstat(filepath.Join(wiki, "Gone.md")); err == nil {
		t.Error("dangling symlink should have been moved")
	}
}

// TestQuarantineUnwritableDir verifies that quarantine logs gracefully
// when lost+found/ cannot be created.
func TestQuarantineUnwritableDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: running as root")
	}

	parent := t.TempDir()
	wiki := filepath.Join(parent, "wiki")
	os.Mkdir(wiki, 0755)

	writeFile(t, wiki, "Good.md", "title: Good\n\nBody.")
	os.Symlink("../nowhere/Missing.md", filepath.Join(wiki, "Missing.md"))

	// Make wiki dir read-only so lost+found/ creation fails.
	os.Chmod(wiki, 0555)
	t.Cleanup(func() { os.Chmod(wiki, 0755) })

	s := store.New()
	n, _, err := Load(wiki, s, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Good.md should still load despite quarantine failure.
	if n != 1 {
		t.Errorf("loaded %d, want 1", n)
	}
}

// TestQuarantineRenameError verifies that quarantine logs gracefully
// when the file cannot be moved (e.g. lost+found exists but rename fails).
func TestQuarantineRenameError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: running as root")
	}

	parent := t.TempDir()
	wiki := filepath.Join(parent, "wiki")
	os.Mkdir(wiki, 0755)

	writeFile(t, wiki, "Good.md", "title: Good\n\nBody.")
	os.Symlink("../nowhere/Missing.md", filepath.Join(wiki, "Missing.md"))

	// Pre-create lost+found/ but make it unwritable so rename fails.
	lf := filepath.Join(wiki, "lost+found")
	os.Mkdir(lf, 0555)
	t.Cleanup(func() { os.Chmod(lf, 0755) })

	s := store.New()
	n, _, err := Load(wiki, s, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("loaded %d, want 1", n)
	}
}

// TestLoadSymlinkUnreadableTarget verifies that a symlink pointing
// to an unreadable file is skipped gracefully.
func TestLoadSymlinkUnreadableTarget(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: running as root")
	}

	parent := t.TempDir()
	work := filepath.Join(parent, "work")
	evernote := filepath.Join(parent, "evernote")
	os.Mkdir(work, 0755)
	os.Mkdir(evernote, 0755)

	// Create a target file then make it unreadable.
	writeFile(t, work, "Secret.md", "title: Secret\n\nSecret body.")
	os.Chmod(filepath.Join(work, "Secret.md"), 0000)
	t.Cleanup(func() { os.Chmod(filepath.Join(work, "Secret.md"), 0644) })

	os.Symlink("../work/Secret.md", filepath.Join(evernote, "Secret.md"))
	writeFile(t, evernote, "Local.md", "title: Local\n\nLocal body.")

	s := store.New()
	n, sym, err := Load(evernote, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Only local note loads; unreadable symlink is skipped.
	if n != 1 {
		t.Errorf("loaded %d, want 1", n)
	}
	if len(sym) != 0 {
		t.Errorf("symlinks = %v, want empty (unreadable skipped)", sym)
	}
}

// TestLoadSymlinkMalformedTarget verifies that a symlink pointing
// to a file with unparseable content is skipped gracefully.
func TestLoadSymlinkMalformedTarget(t *testing.T) {
	parent := t.TempDir()
	work := filepath.Join(parent, "work")
	evernote := filepath.Join(parent, "evernote")
	os.Mkdir(work, 0755)
	os.Mkdir(evernote, 0755)

	// Target file has no title header — Parse will fail.
	writeFile(t, work, "Bad.md", "no headers here, just text")
	os.Symlink("../work/Bad.md", filepath.Join(evernote, "Bad.md"))
	writeFile(t, evernote, "Local.md", "title: Local\n\nLocal body.")

	s := store.New()
	n, sym, err := Load(evernote, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("loaded %d, want 1", n)
	}
	if len(sym) != 0 {
		t.Errorf("symlinks = %v, want empty (malformed skipped)", sym)
	}
}

// TestSymlinkOwnerReadlinkError verifies that SymlinkOwner returns ""
// when os.Readlink fails on a valid symlink (e.g. permission error).
func TestSymlinkOwnerReadlinkError(t *testing.T) {
	parent := t.TempDir()
	work := filepath.Join(parent, "work")
	src := filepath.Join(parent, "src")
	os.Mkdir(work, 0755)
	os.Mkdir(src, 0755)
	writeFile(t, work, "Note.md", "title: Note\n\nBody.")
	os.Symlink("../work/Note.md", filepath.Join(src, "Note.md"))

	patches := gomonkey.ApplyFunc(os.Readlink, func(string) (string, error) {
		return "", errors.New("simulated readlink error")
	})
	defer patches.Reset()

	if got := SymlinkOwner(filepath.Join(src, "Note.md")); got != "" {
		t.Errorf("SymlinkOwner = %q, want empty on readlink error", got)
	}
}
func TestIsDanglingRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "regular.md")
	os.WriteFile(path, []byte("data"), 0644)
	if isDangling(path) {
		t.Error("regular file should not be dangling")
	}
}

// TestIsDanglingValidSymlink verifies isDangling returns false
// for a symlink whose target exists.
func TestIsDanglingValidSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.md")
	os.WriteFile(target, []byte("data"), 0644)
	link := filepath.Join(dir, "link.md")
	os.Symlink(target, link)
	if isDangling(link) {
		t.Error("valid symlink should not be dangling")
	}
}
