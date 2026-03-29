package wiki

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/dan4thewin/tibbitwiki/internal/editor"
	"github.com/dan4thewin/tibbitwiki/internal/rcs"
	"github.com/dan4thewin/tibbitwiki/internal/search"
	"github.com/dan4thewin/tibbitwiki/internal/tid"
)

// testWiki creates a Wiki with a temp dir, starts it, and registers
// cleanup. Returns the wiki and its noteDir.
func testWiki(t *testing.T) *Wiki {
	t.Helper()
	dir := t.TempDir()
	wk := New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	return wk
}

// writeNote writes a .md file and adds the note to the store.
func writeNote(t *testing.T, wk *Wiki, title, text string) string {
	t.Helper()
	td := &tid.Note{Fields: map[string]string{
		"title": title,
		"text":  text,
	}}
	path := filepath.Join(wk.NoteDir(), tid.TitleToFilename(title))
	if err := os.WriteFile(path, tid.Serialize(td), 0644); err != nil {
		t.Fatal(err)
	}
	wk.Store().PutWithPath(td, path)
	wk.SeedBody(title, text)
	return path
}

// --- Event loop tests ---

func TestWikiStartStop(t *testing.T) {
	wk := New("test", t.TempDir(), nil, 100)
	wk.Start()
	wk.Stop()
}

func TestWikiGet(t *testing.T) {
	wk := testWiki(t)
	writeNote(t, wk, "Hello", "world")

	fields := wk.Get("Hello", false)
	if fields == nil {
		t.Fatal("expected fields, got nil")
	}
	if fields["title"] != "Hello" {
		t.Errorf("title = %q, want %q", fields["title"], "Hello")
	}
	if fields["text"] != "world" {
		t.Errorf("text = %q, want %q", fields["text"], "world")
	}
	if fields["revision"] == "" {
		t.Error("expected revision")
	}
	if fields["html"] == "" {
		t.Error("expected rendered html")
	}
}

func TestWikiGetNotFound(t *testing.T) {
	wk := testWiki(t)
	if got := wk.Get("NoSuch", false); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestWikiGetPlainRender(t *testing.T) {
	wk := testWiki(t)
	writeNote(t, wk, "Code", "```go\nfmt.Println()\n```")

	fields := wk.Get("Code", true)
	if fields == nil {
		t.Fatal("expected fields")
	}
	html := fields["html"]
	if html == "" {
		t.Fatal("expected html")
	}
	if !contains(html, "data-line=") {
		t.Error("plain render should have data-line")
	}
}

func TestWikiPut(t *testing.T) {
	wk := testWiki(t)
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error { return nil })
	defer patches.Reset()

	fields := map[string]string{
		"title": "NewNote",
		"text":  "content",
	}
	rev, conflict := wk.Put("NewNote", fields, "")
	if conflict {
		t.Error("unexpected conflict")
	}
	if rev < 1 {
		t.Errorf("rev = %d, want >= 1", rev)
	}
	// Should be in store.
	got := wk.Store().Get("NewNote")
	if got == nil {
		t.Fatal("note not in store")
	}
	// Should have timestamps.
	if got.Fields["modified"] == "" {
		t.Error("expected modified timestamp")
	}
	if got.Fields["created"] == "" {
		t.Error("expected created timestamp")
	}
	// Should have file on disk.
	path := filepath.Join(wk.NoteDir(), "NewNote.md")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestWikiPutConflict(t *testing.T) {
	wk := testWiki(t)
	writeNote(t, wk, "Conflict", "v1")

	_, conflict := wk.Put("Conflict", map[string]string{
		"title": "Conflict",
		"text":  "v2",
	}, "999")
	if !conflict {
		t.Error("expected conflict")
	}
}

func TestWikiPutPreservesCreated(t *testing.T) {
	wk := testWiki(t)
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error { return nil })
	defer patches.Reset()

	// First put — creates timestamp.
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	orig := NowFunc
	NowFunc = func() time.Time { return fixed }
	defer func() { NowFunc = orig }()

	wk.Put("Ts", map[string]string{"title": "Ts", "text": "a"}, "")
	got := wk.Store().Get("Ts")
	created := got.Fields["created"]
	if created == "" {
		t.Fatal("expected created on first put")
	}

	// Second put — should preserve created.
	NowFunc = func() time.Time { return fixed.Add(time.Hour) }
	wk.Put("Ts", map[string]string{"title": "Ts", "text": "b"}, "")
	got = wk.Store().Get("Ts")
	if got.Fields["created"] != created {
		t.Errorf("created changed: %q → %q", created, got.Fields["created"])
	}
	if got.Fields["modified"] == created {
		t.Error("modified should differ from created")
	}
}

func TestWikiPutSkinny(t *testing.T) {
	wk := testWiki(t)
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error { return nil })
	defer patches.Reset()

	writeNote(t, wk, "Skinny", "original body")

	// Skinny PUT should preserve existing text.
	wk.Put("Skinny", map[string]string{
		"title":      "Skinny",
		"tags":       "Updated",
		"_is_skinny": "yes",
	}, "")

	text := wk.Get("Skinny", false)["text"]
	if text != "original body" {
		t.Errorf("text = %q, want %q", text, "original body")
	}
	meta := wk.Store().Get("Skinny")
	if meta.Fields["tags"] != "Updated" {
		t.Errorf("tags = %q, want %q", meta.Fields["tags"], "Updated")
	}
}

func TestWikiDelete(t *testing.T) {
	wk := testWiki(t)
	path := writeNote(t, wk, "Doomed", "bye")

	wk.Delete("Doomed")

	if wk.Store().Get("Doomed") != nil {
		t.Error("note should be deleted from store")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should be deleted from disk")
	}
}

// TestWikiDeletePreservesRCS verifies that deleting a note moves
// its RCS archive to last+chance/ for recovery.
func TestWikiDeletePreservesRCS(t *testing.T) {
	wk := testWiki(t)
	path := writeNote(t, wk, "Saved", "history")

	// Create a fake RCS archive.
	rcsDir := filepath.Join(wk.NoteDir(), "RCS")
	os.MkdirAll(rcsDir, 0755)
	rcsFile := filepath.Join(rcsDir, "Saved.md,v")
	os.WriteFile(rcsFile, []byte("rcs-data"), 0644)

	wk.Delete("Saved")

	// Note file should be gone.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("note file should be deleted")
	}
	// RCS archive should be gone from RCS/.
	if _, err := os.Stat(rcsFile); !os.IsNotExist(err) {
		t.Error("RCS archive should be moved out of RCS/")
	}
	// RCS archive should be in last+chance/.
	lcFile := filepath.Join(wk.NoteDir(), "last+chance", "Saved.md,v")
	if _, err := os.Stat(lcFile); err != nil {
		t.Errorf("RCS archive should be in last+chance/: %v", err)
	}
	data, _ := os.ReadFile(lcFile)
	if string(data) != "rcs-data" {
		t.Errorf("last+chance content = %q, want %q", data, "rcs-data")
	}
}

// TestWikiDeleteNoRCS verifies delete works fine when there's no
// RCS archive (new note never checked in).
func TestWikiDeleteNoRCS(t *testing.T) {
	wk := testWiki(t)
	writeNote(t, wk, "NoHistory", "ephemeral")

	wk.Delete("NoHistory")

	if wk.Store().Get("NoHistory") != nil {
		t.Error("note should be deleted from store")
	}
	// No last+chance/ dir should be created.
	lcDir := filepath.Join(wk.NoteDir(), "last+chance")
	if _, err := os.Stat(lcDir); !os.IsNotExist(err) {
		t.Error("last+chance/ should not be created when no RCS archive exists")
	}
}

// TestWikiDeleteNotInStore verifies deleting a title with no file
// path (not in store) is a harmless no-op.
func TestWikiDeleteNotInStore(t *testing.T) {
	wk := testWiki(t)
	wk.Delete("Ghost")
	// No panic, no crash — just a no-op.
}

// TestWikiDeleteLastChanceMkdirError verifies that a MkdirAll failure
// for last+chance/ is logged but the note is still deleted.
func TestWikiDeleteLastChanceMkdirError(t *testing.T) {
	wk := testWiki(t)
	writeNote(t, wk, "MkdirFail", "body")

	// Create RCS archive so the last+chance path is attempted.
	rcsDir := filepath.Join(wk.NoteDir(), "RCS")
	os.MkdirAll(rcsDir, 0755)
	os.WriteFile(filepath.Join(rcsDir, "MkdirFail.md,v"), []byte("v"), 0644)

	// Block last+chance creation by putting a file where the dir would be.
	os.WriteFile(filepath.Join(wk.NoteDir(), "last+chance"), []byte("block"), 0644)

	wk.Delete("MkdirFail")

	if wk.Store().Get("MkdirFail") != nil {
		t.Error("note should be deleted from store despite MkdirAll error")
	}
}

// TestWikiDeleteLastChanceRenameError verifies that a Rename failure
// is logged but the note is still deleted.
func TestWikiDeleteLastChanceRenameError(t *testing.T) {
	wk := testWiki(t)
	writeNote(t, wk, "RenameFail", "body")

	// Create RCS archive.
	rcsDir := filepath.Join(wk.NoteDir(), "RCS")
	os.MkdirAll(rcsDir, 0755)
	os.WriteFile(filepath.Join(rcsDir, "RenameFail.md,v"), []byte("v"), 0644)

	// Create last+chance/ as unwritable so Rename fails.
	lcDir := filepath.Join(wk.NoteDir(), "last+chance")
	os.MkdirAll(lcDir, 0755)
	os.Chmod(lcDir, 0555)
	t.Cleanup(func() { os.Chmod(lcDir, 0755) })

	wk.Delete("RenameFail")

	if wk.Store().Get("RenameFail") != nil {
		t.Error("note should be deleted from store despite Rename error")
	}
}

func TestWikiList(t *testing.T) {
	wk := testWiki(t)
	writeNote(t, wk, "Alpha", "a")
	writeNote(t, wk, "Beta", "b")

	list := wk.List()
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
	// Should not include text.
	for _, m := range list {
		if _, ok := m["text"]; ok {
			t.Error("list should not include text")
		}
		if m["revision"] == "" {
			t.Error("list should include revision")
		}
	}
}

func TestWikiLineEdit(t *testing.T) {
	wk := testWiki(t)
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error { return nil })
	defer patches.Reset()

	writeNote(t, wk, "Edit", "- item one\n- item two")

	html, rev, err := wk.LineEdit("Edit", 1, "- replaced", false)
	if err != nil {
		t.Fatal(err)
	}
	if rev < 1 {
		t.Errorf("rev = %d", rev)
	}
	if html == "" {
		t.Error("expected rendered html")
	}
	text := wk.Get("Edit", false)["text"]
	if !contains(text, "replaced") {
		t.Errorf("text = %q, should contain 'replaced'", text)
	}
}

func TestWikiLineEditInsert(t *testing.T) {
	wk := testWiki(t)
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error { return nil })
	defer patches.Reset()

	writeNote(t, wk, "Ins", "- one\n- two")

	_, _, err := wk.LineEdit("Ins", 1, "- inserted", true)
	if err != nil {
		t.Fatal(err)
	}
	text := wk.Get("Ins", false)["text"]
	if !contains(text, "inserted") {
		t.Errorf("text = %q, should contain 'inserted'", text)
	}
}

func TestWikiLineEditNoFile(t *testing.T) {
	wk := testWiki(t)
	// Note in store but no file path.
	wk.Store().Put(&tid.Note{Fields: map[string]string{
		"title": "NoFile",
		"text":  "body",
	}})
	_, _, err := wk.LineEdit("NoFile", 1, "x", false)
	if err == nil {
		t.Error("expected error for note without file")
	}
}

func TestWikiLineEditOutOfRange(t *testing.T) {
	wk := testWiki(t)
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error { return nil })
	defer patches.Reset()
	writeNote(t, wk, "Short", "one line")

	_, _, err := wk.LineEdit("Short", 999, "x", false)
	if err == nil || !contains(err.Error(), "out of range") {
		t.Errorf("expected out of range error, got %v", err)
	}
}

func TestWikiEditExternalNoFile(t *testing.T) {
	wk := testWiki(t)
	wk.Store().Put(&tid.Note{Fields: map[string]string{
		"title": "NoFile",
	}})
	err := wk.EditExternal("NoFile")
	if err == nil || !contains(err.Error(), "no file") {
		t.Errorf("expected 'no file' error, got %v", err)
	}
}

func TestWikiEditExternalNoEditor(t *testing.T) {
	wk := testWiki(t) // no editor
	writeNote(t, wk, "HasFile", "body")

	err := wk.EditExternal("HasFile")
	if err == nil || !contains(err.Error(), "not configured") {
		t.Errorf("expected 'not configured' error, got %v", err)
	}
}

// --- Reload (watcher callback) tests ---

func TestWikiReloadAddsNote(t *testing.T) {
	wk := testWiki(t)
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error { return nil })
	defer patches.Reset()

	content := "title: Reloaded\ntags: Test\n\nBody here."
	path := filepath.Join(wk.NoteDir(), "Reloaded.md")
	os.WriteFile(path, []byte(content), 0644)

	// Reload is async; use do() to synchronize.
	wk.Reload(path)
	wk.do(func() {}) // drain the queue

	if wk.Store().Get("Reloaded") == nil {
		t.Fatal("expected note in store after reload")
	}
	if text := wk.Get("Reloaded", false)["text"]; text != "Body here." {
		t.Errorf("text = %q", text)
	}
}

func TestWikiReloadDeletesNote(t *testing.T) {
	wk := testWiki(t)
	path := writeNote(t, wk, "Gone", "bye")
	os.Remove(path)

	wk.Reload(path)
	wk.do(func() {})

	if wk.Store().Get("Gone") != nil {
		t.Error("note should be deleted after reload of missing file")
	}
}

func TestWikiReloadSkipsUnchanged(t *testing.T) {
	wk := testWiki(t)
	var checkinCalled bool
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error {
		checkinCalled = true
		return nil
	})
	defer patches.Reset()

	path := writeNote(t, wk, "Same", "body")
	checkinCalled = false

	wk.Reload(path)
	wk.do(func() {})

	if checkinCalled {
		t.Error("should not checkin unchanged content")
	}
}

func TestWikiReloadBadParse(t *testing.T) {
	wk := testWiki(t)
	path := filepath.Join(wk.NoteDir(), "Bad.md")
	os.WriteFile(path, []byte("no title here\n\nbody"), 0644)

	wk.Reload(path)
	wk.do(func() {})

	if wk.Store().Count() != 0 {
		t.Error("bad file should not be added to store")
	}
}

func TestWikiReloadReadError(t *testing.T) {
	wk := testWiki(t)
	path := filepath.Join(wk.NoteDir(), "Unreadable.md")
	os.WriteFile(path, []byte("title: X\n\nbody"), 0644)
	os.Chmod(path, 0000)
	t.Cleanup(func() { os.Chmod(path, 0644) })

	wk.Reload(path)
	wk.do(func() {})

	if wk.Store().Get("X") != nil {
		t.Error("unreadable file should not be in store")
	}
}

func TestWikiReloadRCSError(t *testing.T) {
	wk := testWiki(t)
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error {
		return fmt.Errorf("rcs failed")
	})
	defer patches.Reset()

	content := "title: RCSFail\n\nBody."
	path := filepath.Join(wk.NoteDir(), "RCSFail.md")
	os.WriteFile(path, []byte(content), 0644)

	wk.Reload(path)
	wk.do(func() {})

	// Note should still be in store despite RCS error.
	if wk.Store().Get("RCSFail") == nil {
		t.Error("note should be in store despite RCS error")
	}
}

// --- atime tests ---

func TestTouchAtimeUpdatesAccessTime(t *testing.T) {
	f, err := os.CreateTemp("", "atime-*.md")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("title: Test\n\nhi")
	f.Close()
	defer os.Remove(f.Name())

	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	os.Chtimes(f.Name(), past, past)

	touchAtime(f.Name())

	info, _ := os.Stat(f.Name())
	if info.ModTime().After(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("touchAtime changed mtime: %v", info.ModTime())
	}
}

func TestTouchAtimeNonExistent(t *testing.T) {
	touchAtime("/no/such/file.md")
}

func TestFileAtimeNonExistent(t *testing.T) {
	got := fileAtime("/no/such/file.md")
	if got != "" {
		t.Errorf("fileAtime = %q, want empty", got)
	}
}

func TestFileAtimeReturnsISO(t *testing.T) {
	f, err := os.CreateTemp("", "atime-*.md")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("data")
	f.Close()
	defer os.Remove(f.Name())

	got := fileAtime(f.Name())
	if got == "" {
		t.Fatal("fileAtime returned empty for existing file")
	}
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Errorf("fileAtime not valid ISO 8601: %q", got)
	}
}

func TestAtimeFunction(t *testing.T) {
	f, err := os.CreateTemp("", "atime-fn-*.md")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("data")
	f.Close()
	defer os.Remove(f.Name())

	info, err := os.Stat(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	at := atime(info)
	if at.IsZero() {
		t.Error("atime returned zero time")
	}
}

// --- Helper tests ---

func TestMetaFieldsEqualDifferentLen(t *testing.T) {
	stored := map[string]string{"k": "v"}
	parsed := map[string]string{"k": "v", "k2": "v2"}
	if metaFieldsEqual(stored, parsed) {
		t.Error("expected false for different-length maps")
	}
}

func TestMetaFieldsEqualDifferentValues(t *testing.T) {
	stored := map[string]string{"k": "v1"}
	parsed := map[string]string{"k": "v2"}
	if metaFieldsEqual(stored, parsed) {
		t.Error("expected false for different values")
	}
}

func TestMetaFieldsEqualIdentical(t *testing.T) {
	stored := map[string]string{"k": "v"}
	parsed := map[string]string{"k": "v"}
	if !metaFieldsEqual(stored, parsed) {
		t.Error("expected true for identical maps")
	}
}

func TestMetaFieldsEqualIgnoresText(t *testing.T) {
	// stored has no text (stripped by store), parsed has text
	stored := map[string]string{"title": "A", "tags": "x"}
	parsed := map[string]string{"title": "A", "tags": "x", "text": "body"}
	if !metaFieldsEqual(stored, parsed) {
		t.Error("expected true: text field should be ignored")
	}
}

func TestSpliceFileLineReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "splice.md")
	os.WriteFile(path, []byte("title: T\n\n- one\n- two"), 0644)

	data, err := spliceFileLine(path, 1, "- replaced", false)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(data), "replaced") {
		t.Errorf("got %q", string(data))
	}
}

func TestSpliceFileLineInsert(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "splice.md")
	os.WriteFile(path, []byte("title: T\n\n- one\n- two"), 0644)

	data, err := spliceFileLine(path, 1, "- inserted", true)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(data), "inserted") {
		t.Errorf("got %q", string(data))
	}
}

func TestSpliceFileLineOutOfRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "splice.md")
	os.WriteFile(path, []byte("title: T\n\nline"), 0644)

	_, err := spliceFileLine(path, 999, "x", false)
	if err == nil {
		t.Error("expected out of range error")
	}
}

func TestSpliceFileLineReadError(t *testing.T) {
	_, err := spliceFileLine("/no/such/file.md", 1, "x", false)
	if err == nil {
		t.Error("expected read error")
	}
}

// --- Hub tests ---

func TestNewHub(t *testing.T) {
	w1 := New("work", t.TempDir(), nil, 100)
	w2 := New("personal", t.TempDir(), nil, 100)
	hub := NewHub(map[string]*Wiki{"work": w1, "personal": w2})

	if hub.Wiki("work") != w1 {
		t.Error("Wiki('work') should return w1")
	}
	if hub.Wiki("personal") != w2 {
		t.Error("Wiki('personal') should return w2")
	}
	if hub.Wiki("missing") != nil {
		t.Error("Wiki('missing') should return nil")
	}
	names := hub.Names()
	if len(names) != 2 || names[0] != "personal" || names[1] != "work" {
		t.Errorf("Names() = %v, want [personal work]", names)
	}
}

func TestNewHubSingle(t *testing.T) {
	wk := New("solo", t.TempDir(), nil, 100)
	hub := NewHub(map[string]*Wiki{"solo": wk})

	if hub.Wiki("solo") != wk {
		t.Error("Wiki('solo') should return the wiki")
	}
	names := hub.Names()
	if len(names) != 1 || names[0] != "solo" {
		t.Errorf("Names() = %v, want [solo]", names)
	}
}

// --- persistNote edge cases ---

func TestPersistNoteNoDir(t *testing.T) {
	// Wiki with empty noteDir — Put should still work (store-only).
	wk := New("test", "", nil, 100)
	wk.Start()
	defer wk.Stop()

	wk.Put("InMem", map[string]string{
		"title": "InMem",
		"text":  "only in store",
	}, "")
	if wk.Store().Get("InMem") == nil {
		t.Error("note should be in store")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && containsImpl(s, sub)
}

func containsImpl(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- Delete edge cases ---

// TestWikiDeleteRemoveError verifies that a non-NotExist os.Remove
// error is logged but the note is still deleted from the store.
func TestWikiDeleteRemoveError(t *testing.T) {
	patches := gomonkey.ApplyFunc(os.Remove, func(name string) error {
		return fmt.Errorf("permission denied")
	})
	defer patches.Reset()

	wk := testWiki(t)
	writeNote(t, wk, "RemErr", "body")

	wk.Delete("RemErr")

	if wk.Store().Get("RemErr") != nil {
		t.Error("note should be deleted from store despite Remove error")
	}
}

// --- Search tests ---

// TestWikiSearchEmptyDir verifies search returns error when
// noteDir is empty.
func TestWikiSearchEmptyDir(t *testing.T) {
	wk := New("test", "", nil, 100)
	wk.Start()
	defer wk.Stop()

	_, err := wk.Search("hello")
	if err == nil {
		t.Error("expected error for empty noteDir")
	}
}

// TestWikiSearchWithResults verifies search returns matching notes.
func TestWikiSearchWithResults(t *testing.T) {
	orig := searchFunc
	searchFunc = func(dir, query string) ([]search.Result, error) {
		return []search.Result{
			{File: "Found.md", Lines: []string{"matching line"}},
		}, nil
	}
	defer func() { searchFunc = orig }()

	wk := testWiki(t)
	writeNote(t, wk, "Found", "matching line")

	results, err := wk.Search("matching")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Found" {
		t.Errorf("title = %q, want %q", results[0].Title, "Found")
	}
	if len(results[0].Matches) != 1 {
		t.Errorf("matches = %d, want 1", len(results[0].Matches))
	}
}

// TestWikiSearchError verifies search propagates errors.
func TestWikiSearchError(t *testing.T) {
	orig := searchFunc
	searchFunc = func(dir, query string) ([]search.Result, error) {
		return nil, fmt.Errorf("rg exploded")
	}
	defer func() { searchFunc = orig }()

	wk := testWiki(t)
	_, err := wk.Search("hello")
	if err == nil {
		t.Error("expected error")
	}
}

// TestWikiSearchNilLines verifies nil Lines are coalesced to [].
func TestWikiSearchNilLines(t *testing.T) {
	orig := searchFunc
	searchFunc = func(dir, query string) ([]search.Result, error) {
		return []search.Result{
			{File: "NilLine.md", Lines: nil},
		}, nil
	}
	defer func() { searchFunc = orig }()

	wk := testWiki(t)
	writeNote(t, wk, "NilLine", "body")

	results, err := wk.Search("body")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Matches == nil {
		t.Error("matches should be [] not nil")
	}
}

// TestWikiSearchNilResults verifies that when no hits match stored
// notes, results is coalesced to [].
func TestWikiSearchNilResults(t *testing.T) {
	orig := searchFunc
	searchFunc = func(dir, query string) ([]search.Result, error) {
		return []search.Result{
			{File: "NoSuch.md", Lines: []string{"x"}},
		}, nil
	}
	defer func() { searchFunc = orig }()

	wk := testWiki(t)

	results, err := wk.Search("x")
	if err != nil {
		t.Fatal(err)
	}
	if results == nil {
		t.Error("results should be [] not nil")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// --- LineEdit error paths ---

// TestWikiLineEditWriteError verifies os.WriteFile failure returns error.
func TestWikiLineEditWriteError(t *testing.T) {
	wk := testWiki(t)
	writeNote(t, wk, "WrErr", "- alpha\n- beta")

	patches := gomonkey.ApplyFunc(os.WriteFile, func(name string, data []byte, perm os.FileMode) error {
		return fmt.Errorf("disk full")
	})
	defer patches.Reset()

	_, _, err := wk.LineEdit("WrErr", 1, "- ALPHA", false)
	if err == nil || !contains(err.Error(), "writing file") {
		t.Errorf("expected 'writing file' error, got %v", err)
	}
}

// TestWikiLineEditRCSError verifies rcs.Checkin failure during
// line-edit is logged but does not prevent completion.
func TestWikiLineEditRCSError(t *testing.T) {
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error {
		return fmt.Errorf("rcs failed")
	})
	defer patches.Reset()

	wk := testWiki(t)
	writeNote(t, wk, "RcsErr", "- alpha\n- beta")

	html, rev, err := wk.LineEdit("RcsErr", 1, "- ALPHA", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rev < 1 {
		t.Errorf("rev = %d", rev)
	}
	if html == "" {
		t.Error("expected html")
	}
}

// TestWikiLineEditParseError verifies tid.Parse failure returns error.
func TestWikiLineEditParseError(t *testing.T) {
	wk := testWiki(t)
	writeNote(t, wk, "ParseErr", "- alpha\n- beta")

	patches := gomonkey.ApplyFunc(tid.Parse, func(data []byte) (*tid.Note, error) {
		return nil, fmt.Errorf("parse boom")
	})
	defer patches.Reset()

	_, _, err := wk.LineEdit("ParseErr", 1, "- ALPHA", false)
	if err == nil || !contains(err.Error(), "parse error") {
		t.Errorf("expected 'parse error', got %v", err)
	}
}

// --- EditExternal success ---

// TestWikiEditExternalSuccess verifies editor.Open is called
// successfully when everything is configured.
func TestWikiEditExternalSuccess(t *testing.T) {
	dir := t.TempDir()
	ed, _ := editor.New("echo %s")
	wk := New("test", dir, ed, 100)
	wk.Start()
	t.Cleanup(wk.Stop)

	path := filepath.Join(dir, "Edit.md")
	os.WriteFile(path, []byte("title: Edit\n\nbody"), 0644)
	wk.Store().PutWithPath(&tid.Note{Fields: map[string]string{
		"title": "Edit",
		"text":  "body",
	}}, path)

	err := wk.EditExternal("Edit")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- persistNote error paths ---

// TestWikiPutWriteFileError verifies os.WriteFile error is logged
// but note is still stored.
func TestWikiPutWriteFileError(t *testing.T) {
	patches := gomonkey.ApplyFunc(os.WriteFile, func(name string, data []byte, perm os.FileMode) error {
		return fmt.Errorf("disk full")
	})
	defer patches.Reset()

	wk := testWiki(t)
	wk.Put("WriteErr", map[string]string{
		"title": "WriteErr",
		"text":  "body",
	}, "")

	// Note should still be in the store despite WriteFile error.
	if wk.Store().Get("WriteErr") == nil {
		t.Error("note should be in store despite write error")
	}
}

// TestWikiPutRCSError verifies rcs.Checkin error is logged but
// note is still stored.
func TestWikiPutRCSError(t *testing.T) {
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error {
		return fmt.Errorf("rcs failed")
	})
	defer patches.Reset()

	wk := testWiki(t)
	wk.Put("RCSErr", map[string]string{
		"title": "RCSErr",
		"text":  "body",
	}, "")

	if wk.Store().Get("RCSErr") == nil {
		t.Error("note should be in store despite RCS error")
	}
}

// --- Body cache tests ---

// TestFetchBodyCacheMiss verifies fetchBody reads from disk
// when the body is not in the LRU cache.
func TestFetchBodyCacheMiss(t *testing.T) {
	wk := testWiki(t)
	// Write file and store metadata, but do NOT seed body cache.
	td := &tid.Note{Fields: map[string]string{
		"title": "Disk",
		"text":  "from disk",
	}}
	path := filepath.Join(wk.NoteDir(), "Disk.md")
	os.WriteFile(path, tid.Serialize(td), 0644)
	wk.Store().PutWithPath(td, path)

	// Get should fetch body from disk on cache miss.
	got := wk.Get("Disk", false)
	if got["text"] != "from disk" {
		t.Errorf("text = %q, want %q", got["text"], "from disk")
	}
}

// TestFetchBodyNoFilePath verifies fetchBody returns "" when
// the note has no file path (in-memory only, body evicted).
func TestFetchBodyNoFilePath(t *testing.T) {
	wk := testWiki(t)
	// Put metadata only, no file path, no body cache.
	wk.Store().Put(&tid.Note{Fields: map[string]string{
		"title": "NoPath",
	}})

	got := wk.Get("NoPath", false)
	if got["text"] != "" {
		t.Errorf("text = %q, want empty", got["text"])
	}
}

// TestFetchBodyReadError verifies fetchBody returns "" when the
// file cannot be read.
func TestFetchBodyReadError(t *testing.T) {
	wk := testWiki(t)
	path := filepath.Join(wk.NoteDir(), "Unread.md")
	os.WriteFile(path, []byte("title: Unread\n\nbody"), 0644)
	wk.Store().PutWithPath(&tid.Note{Fields: map[string]string{
		"title": "Unread",
	}}, path)
	os.Chmod(path, 0000)
	t.Cleanup(func() { os.Chmod(path, 0644) })

	got := wk.Get("Unread", false)
	if got["text"] != "" {
		t.Errorf("text = %q, want empty on read error", got["text"])
	}
}

// TestFetchBodyParseError verifies fetchBody returns "" when the
// file has invalid format.
func TestFetchBodyParseError(t *testing.T) {
	wk := testWiki(t)
	path := filepath.Join(wk.NoteDir(), "BadFmt.md")
	os.WriteFile(path, []byte("no valid headers here"), 0644)
	wk.Store().PutWithPath(&tid.Note{Fields: map[string]string{
		"title": "BadFmt",
	}}, path)

	got := wk.Get("BadFmt", false)
	if got["text"] != "" {
		t.Errorf("text = %q, want empty on parse error", got["text"])
	}
}

// TestBodyCacheEviction verifies that LRU eviction works: when
// the cache is full, the oldest entry is evicted.
func TestBodyCacheEviction(t *testing.T) {
	dir := t.TempDir()
	// Create a wiki with cache size 2.
	wk := New("test", dir, nil, 2)
	wk.Start()
	t.Cleanup(wk.Stop)
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error { return nil })
	defer patches.Reset()

	// Put 3 notes — first one should be evicted from cache.
	for i, name := range []string{"A", "B", "C"} {
		wk.Put(name, map[string]string{
			"title": name,
			"text":  fmt.Sprintf("body%d", i),
		}, "")
	}

	// A should be evicted, B and C should be cached.
	// A's file is on disk, so fetchBody can reload it.
	gotA := wk.Get("A", false)
	if gotA["text"] != "body0" {
		t.Errorf("A text = %q, want %q (should reload from disk)", gotA["text"], "body0")
	}
	gotC := wk.Get("C", false)
	if gotC["text"] != "body2" {
		t.Errorf("C text = %q, want %q", gotC["text"], "body2")
	}
}

// TestReloadSkipsEmptyTitle verifies that a file with an empty title
// header is not added to the store.
func TestReloadSkipsEmptyTitle(t *testing.T) {
	wk := testWiki(t)
	path := filepath.Join(wk.NoteDir(), "Draft.md")
	os.WriteFile(path, []byte("title: \ntags: \n\n"), 0644)
	wk.Reload(path)
	wk.do(func() {})
	// Should not appear in store
	if wk.Store().Get("Draft") != nil {
		t.Error("empty title should not be in store")
	}
	if wk.Store().Get("") != nil {
		t.Error("empty string title should not be in store")
	}
}

// TestReloadRenamesOnTitleMismatch verifies that when a file's title
// header differs from the filename, the file is renamed and the store
// is rescanned.
func TestReloadRenamesOnTitleMismatch(t *testing.T) {
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error { return nil })
	defer patches.Reset()

	wk := testWiki(t)
	// Create a file named "OldName.md" but with title "NewName"
	path := filepath.Join(wk.NoteDir(), "OldName.md")
	os.WriteFile(path, []byte("title: NewName\ntags: test\n\nBody."), 0644)
	wk.Reload(path)
	wk.do(func() {})

	// OldName.md should no longer exist
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("old file should be gone")
	}
	// NewName.md should exist
	newPath := filepath.Join(wk.NoteDir(), "NewName.md")
	if _, err := os.Stat(newPath); err != nil {
		t.Error("new file should exist")
	}
	// Store should have NewName, not OldName
	if wk.Store().Get("NewName") == nil {
		t.Error("NewName should be in store")
	}
	if wk.Store().Get("OldName") != nil {
		t.Error("OldName should not be in store")
	}
}

// TestReloadRenameError verifies that a failed rename does not crash
// the wiki; the original file remains on disk.
func TestReloadRenameError(t *testing.T) {
	wk := testWiki(t)
	patches := gomonkey.ApplyFunc(os.Rename, func(old, new string) error {
		return fmt.Errorf("permission denied")
	})
	defer patches.Reset()

	path := filepath.Join(wk.NoteDir(), "Src.md")
	os.WriteFile(path, []byte("title: Different\ntags: \n\nBody."), 0644)
	wk.Reload(path)
	wk.do(func() {})
	// Should not crash; file still on disk
	if _, err := os.Stat(path); err != nil {
		t.Error("original file should still exist")
	}
}

// TestReloadRenameRCSError verifies rename proceeds even when
// rcs.Checkin fails after the rename.
func TestReloadRenameRCSError(t *testing.T) {
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error {
		return fmt.Errorf("rcs failed")
	})
	defer patches.Reset()

	wk := testWiki(t)
	path := filepath.Join(wk.NoteDir(), "Before.md")
	os.WriteFile(path, []byte("title: After\ntags: \n\nBody."), 0644)
	wk.Reload(path)
	wk.do(func() {})

	// Rename should have happened despite RCS error.
	newPath := filepath.Join(wk.NoteDir(), "After.md")
	if _, err := os.Stat(newPath); err != nil {
		t.Error("renamed file should exist")
	}
	if wk.Store().Get("After") == nil {
		t.Error("After should be in store after rescan")
	}
}

// TestRescan verifies that a title/filename mismatch triggers a
// rescan that clears the store and reloads all files from disk.
func TestRescan(t *testing.T) {
	wk := testWiki(t)
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error { return nil })
	defer patches.Reset()

	// Add a note the normal way
	writeNote(t, wk, "Alpha", "alpha body")
	// Add another note directly to disk (not in store)
	os.WriteFile(filepath.Join(wk.NoteDir(), "Beta.md"),
		[]byte("title: Beta\ntags: \n\nBeta body."), 0644)

	// Before rescan: Alpha in store, Beta not
	if wk.Store().Get("Alpha") == nil {
		t.Error("Alpha should be in store")
	}
	if wk.Store().Get("Beta") != nil {
		t.Error("Beta should not be in store yet")
	}

	// Trigger rescan by creating a title/filename mismatch
	mismatch := filepath.Join(wk.NoteDir(), "WrongName.md")
	os.WriteFile(mismatch, []byte("title: RightName\ntags: \n\nBody."), 0644)
	wk.Reload(mismatch)
	wk.do(func() {})

	// After rescan: both should be in store, plus RightName
	if wk.Store().Get("Alpha") == nil {
		t.Error("Alpha should survive rescan")
	}
	if wk.Store().Get("Beta") == nil {
		t.Error("Beta should appear after rescan")
	}
	if wk.Store().Get("RightName") == nil {
		t.Error("RightName should appear after rename+rescan")
	}
	if wk.Store().Get("WrongName") != nil {
		t.Error("WrongName should not be in store")
	}
}

// TestNewDraft verifies that NewDraft creates a Draft.md file and
// returns its path.
func TestNewDraft(t *testing.T) {
	dir := t.TempDir()
	ed, _ := editor.New("echo %s")
	wk := New("test", dir, ed, 100)
	wk.Start()
	t.Cleanup(wk.Stop)

	fp, err := wk.NewDraft()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(fp, "Draft.md") {
		t.Errorf("path = %q, want Draft.md suffix", fp)
	}
	// File should exist on disk
	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "title:") {
		t.Error("should contain title header")
	}
}

// TestNewDraftAlreadyExists verifies that NewDraft does not overwrite
// an existing Draft.md file.
func TestNewDraftAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	ed, _ := editor.New("echo %s")
	wk := New("test", dir, ed, 100)
	wk.Start()
	t.Cleanup(wk.Stop)

	// Create an existing Draft.md with content
	draftPath := filepath.Join(dir, "Draft.md")
	os.WriteFile(draftPath, []byte("title: \ntags: \n\nwork in progress"), 0644)

	fp, err := wk.NewDraft()
	if err != nil {
		t.Fatal(err)
	}
	// Should NOT overwrite
	data, _ := os.ReadFile(fp)
	if !strings.Contains(string(data), "work in progress") {
		t.Error("should not overwrite existing Draft.md")
	}
}

// TestNewDraftNoEditor verifies that NewDraft returns an error when
// no editor is configured.
func TestNewDraftNoEditor(t *testing.T) {
	wk := testWiki(t) // testWiki uses nil editor
	_, err := wk.NewDraft()
	if err == nil {
		t.Error("expected error when no editor")
	}
}

// TestNewDraftWriteError verifies that NewDraft returns an error
// when the file cannot be written.
func TestNewDraftWriteError(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0555)
	t.Cleanup(func() { os.Chmod(dir, 0755) })
	ed, _ := editor.New("echo %s")
	wk := New("test", dir, ed, 100)
	wk.Start()
	t.Cleanup(wk.Stop)

	_, err := wk.NewDraft()
	if err == nil {
		t.Error("expected error writing to read-only dir")
	}
}

// TestRescanSkipsErrors verifies rescan handles unreadable and
// unparseable files gracefully.
func TestRescanSkipsErrors(t *testing.T) {
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error { return nil })
	defer patches.Reset()

	wk := testWiki(t)
	// Good file
	writeNote(t, wk, "Good", "body")
	// Unreadable file
	unreadable := filepath.Join(wk.NoteDir(), "Unread.md")
	os.WriteFile(unreadable, []byte("title: Unread\n\nbody"), 0000)
	t.Cleanup(func() { os.Chmod(unreadable, 0644) })
	// Unparseable file
	os.WriteFile(filepath.Join(wk.NoteDir(), "Bad.md"), []byte("no headers"), 0644)
	// Subdir (should be skipped)
	os.Mkdir(filepath.Join(wk.NoteDir(), "subdir"), 0755)
	// Non-md file
	os.WriteFile(filepath.Join(wk.NoteDir(), "readme.txt"), []byte("skip"), 0644)

	// Trigger rescan via rename mismatch
	mismatch := filepath.Join(wk.NoteDir(), "Wrong.md")
	os.WriteFile(mismatch, []byte("title: Right\ntags: \n\nBody."), 0644)
	wk.Reload(mismatch)
	wk.do(func() {})

	if wk.Store().Get("Good") == nil {
		t.Error("Good should survive rescan")
	}
	if wk.Store().Get("Right") == nil {
		t.Error("Right should appear after rename+rescan")
	}
}

// TestRescanReadDirError verifies rescan handles unreadable noteDir.
func TestRescanReadDirError(t *testing.T) {
	wk := New("test", "/no/such/dir", nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	// Direct call — rescan should log error but not panic.
	wk.do(func() { wk.rescan() })
}

// TestListIncludesSymlinkedNotes verifies that List() merges local
// notes with notes delegated from owner wikis via symlinks.
func TestListIncludesSymlinkedNotes(t *testing.T) {
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error { return nil })
	defer patches.Reset()

	// "work" wiki owns the real note.
	work := testWiki(t)
	writeNote(t, work, "Meeting", "meeting body")

	// "evernote" wiki has a local note plus a symlink to work's Meeting.
	evernote := testWiki(t)
	writeNote(t, evernote, "Local", "local body")
	evernote.SetSymlinks(map[string]string{"Meeting": "work"})

	// Wire up the hub so evernote can delegate.
	hub := NewHub(map[string]*Wiki{"work": work, "evernote": evernote})
	evernote.SetHub(hub)

	list := evernote.List()
	titles := map[string]string{} // title → owner (or "")
	for _, fields := range list {
		titles[fields["title"]] = fields["owner"]
	}

	if _, ok := titles["Local"]; !ok {
		t.Error("expected Local in list")
	}
	if titles["Local"] != "" {
		t.Errorf("Local owner = %q, want empty", titles["Local"])
	}
	if titles["Meeting"] != "work" {
		t.Errorf("Meeting owner = %q, want %q", titles["Meeting"], "work")
	}
	if len(list) != 2 {
		t.Errorf("list len = %d, want 2", len(list))
	}
}

// TestListSkipsMissingOwnerWiki verifies that a symlink pointing
// to a non-existent wiki is silently skipped in List().
func TestListSkipsMissingOwnerWiki(t *testing.T) {
	wk := testWiki(t)
	wk.SetSymlinks(map[string]string{"Ghost": "nonexistent"})
	hub := NewHub(map[string]*Wiki{"test": wk})
	wk.SetHub(hub)

	list := wk.List()
	for _, fields := range list {
		if fields["title"] == "Ghost" {
			t.Error("Ghost should not appear (owner wiki missing)")
		}
	}
}

// TestGetDelegatesSymlinkedNote verifies that Get() delegates to the
// owning wiki when the requested title is a symlink, returning the
// full note with owner field set.
func TestGetDelegatesSymlinkedNote(t *testing.T) {
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error { return nil })
	defer patches.Reset()

	// "work" wiki owns the real note.
	work := testWiki(t)
	writeNote(t, work, "Meeting", "meeting body")

	// "evernote" wiki has a symlink to work's Meeting.
	evernote := testWiki(t)
	evernote.SetSymlinks(map[string]string{"Meeting": "work"})
	hub := NewHub(map[string]*Wiki{"work": work, "evernote": evernote})
	evernote.SetHub(hub)

	// Get via the evernote wiki should delegate to work.
	result := evernote.Get("Meeting", false)
	if result == nil {
		t.Fatal("expected non-nil result for symlinked note")
	}
	if result["title"] != "Meeting" {
		t.Errorf("title = %q, want Meeting", result["title"])
	}
	if result["owner"] != "work" {
		t.Errorf("owner = %q, want work", result["owner"])
	}
	if result["text"] != "meeting body" {
		t.Errorf("text = %q, want %q", result["text"], "meeting body")
	}
	if result["html"] == "" {
		t.Error("expected rendered html")
	}
}

// TestGetSymlinkMissingOwnerReturnsNil verifies that Get() returns
// nil when a symlink points to a non-existent wiki.
func TestGetSymlinkMissingOwnerReturnsNil(t *testing.T) {
	wk := testWiki(t)
	wk.SetSymlinks(map[string]string{"Ghost": "nonexistent"})
	hub := NewHub(map[string]*Wiki{"test": wk})
	wk.SetHub(hub)

	if result := wk.Get("Ghost", false); result != nil {
		t.Errorf("expected nil for missing owner wiki, got %v", result)
	}
}

// TestReloadFileDetectsNewSymlink verifies that when a symlink
// appears at runtime, reloadFile adds it to the symlinks map
// instead of loading it into the store.
func TestReloadFileDetectsNewSymlink(t *testing.T) {
	parent := t.TempDir()
	workDir := filepath.Join(parent, "work")
	evernoteDir := filepath.Join(parent, "evernote")
	os.Mkdir(workDir, 0755)
	os.Mkdir(evernoteDir, 0755)

	// Create the real note in work.
	writeFile := func(dir, name, content string) {
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
	}
	writeFile(workDir, "Shared.md", "title: Shared\ntags: Work\n\nShared body.")

	// Create evernote wiki with no symlinks initially.
	evernote := New("evernote", evernoteDir, nil, 100)
	evernote.Start()
	t.Cleanup(evernote.Stop)

	// Verify no symlinks yet.
	if len(evernote.symlinks) != 0 {
		t.Fatalf("expected no symlinks, got %v", evernote.symlinks)
	}

	// Create a symlink at runtime (simulating user adding it).
	linkPath := filepath.Join(evernoteDir, "Shared.md")
	os.Symlink("../work/Shared.md", linkPath)

	// Trigger reload as the watcher would.
	evernote.Reload(linkPath)
	evernote.do(func() {}) // drain queue

	// Symlink should be in the map, NOT the store.
	if evernote.symlinks["Shared"] != "work" {
		t.Errorf("symlinks[Shared] = %q, want %q", evernote.symlinks["Shared"], "work")
	}
	if evernote.store.Get("Shared") != nil {
		t.Error("Shared should not be in store (symlink)")
	}
}

// TestReloadFileDeletesSymlink verifies that when a symlink is
// removed, reloadFile cleans up the symlinks map.
func TestReloadFileDeletesSymlink(t *testing.T) {
	parent := t.TempDir()
	workDir := filepath.Join(parent, "work")
	evernoteDir := filepath.Join(parent, "evernote")
	os.Mkdir(workDir, 0755)
	os.Mkdir(evernoteDir, 0755)

	writeFile := func(dir, name, content string) {
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
	}
	writeFile(workDir, "Shared.md", "title: Shared\n\nBody.")

	// Create symlink and load it.
	linkPath := filepath.Join(evernoteDir, "Shared.md")
	os.Symlink("../work/Shared.md", linkPath)

	evernote := New("evernote", evernoteDir, nil, 100)
	evernote.symlinks["Shared"] = "work"
	evernote.Start()
	t.Cleanup(evernote.Stop)

	// Delete the symlink.
	os.Remove(linkPath)

	// Trigger reload for the deleted path.
	evernote.Reload(linkPath)
	evernote.do(func() {}) // drain queue

	if _, ok := evernote.symlinks["Shared"]; ok {
		t.Error("Shared should be removed from symlinks map")
	}
}

// --- Revision tests ---

// writeNoteWithRevisions creates a note with multiple RCS revisions.
// Returns the wiki and the note file path.
func writeNoteWithRevisions(t *testing.T, title string, bodies []string) (*Wiki, string) {
	t.Helper()
	wk := testWiki(t)
	path := filepath.Join(wk.NoteDir(), tid.TitleToFilename(title))
	for _, body := range bodies {
		td := &tid.Note{Fields: map[string]string{
			"title": title,
			"text":  body,
		}}
		os.WriteFile(path, tid.Serialize(td), 0644)
		if err := rcs.Checkin(path); err != nil {
			t.Fatalf("checkin: %v", err)
		}
	}
	// Parse final version into store.
	data, _ := os.ReadFile(path)
	td, _ := tid.Parse(data)
	wk.Store().PutWithPath(td, path)
	wk.SeedBody(title, td.Text())
	return wk, path
}

// TestRevisionsReturnsHistory verifies Revisions returns all revision
// numbers and diff HTML for consecutive pairs.
func TestRevisionsReturnsHistory(t *testing.T) {
	wk, _ := writeNoteWithRevisions(t, "History",
		[]string{"Version 1.", "Version 2.", "Version 3."})

	result, err := wk.Revisions("History", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Revisions) != 3 {
		t.Errorf("revisions count = %d, want 3", len(result.Revisions))
	}
	if result.Revisions[0] != "1.1" || result.Revisions[2] != "1.3" {
		t.Errorf("revisions = %v", result.Revisions)
	}
	if !result.IsHead {
		t.Error("should be at head")
	}
	// Diff HTML should contain both transition headers.
	if !strings.Contains(result.DiffHTML, "1.1 → 1.2") {
		t.Error("missing 1.1 → 1.2 diff header")
	}
	if !strings.Contains(result.DiffHTML, "1.2 → 1.3") {
		t.Error("missing 1.2 → 1.3 diff header")
	}
}

// TestRevisionsMaxDiffsCap verifies that only the most recent
// maxDiffs pairs are included in the diff output.
func TestRevisionsMaxDiffsCap(t *testing.T) {
	wk, _ := writeNoteWithRevisions(t, "Capped",
		[]string{"V1.", "V2.", "V3.", "V4."})

	result, err := wk.Revisions("Capped", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Revisions) != 4 {
		t.Errorf("revisions count = %d, want 4", len(result.Revisions))
	}
	// Only last 2 diff pairs: 1.2→1.3, 1.3→1.4.
	if strings.Contains(result.DiffHTML, "1.1 → 1.2") {
		t.Error("1.1 → 1.2 should be excluded by maxDiffs cap")
	}
	if !strings.Contains(result.DiffHTML, "1.2 → 1.3") {
		t.Error("missing 1.2 → 1.3")
	}
	if !strings.Contains(result.DiffHTML, "1.3 → 1.4") {
		t.Error("missing 1.3 → 1.4")
	}
}

// TestRevisionsSingleRev verifies that a note with only one revision
// returns an empty diff.
func TestRevisionsSingleRev(t *testing.T) {
	wk, _ := writeNoteWithRevisions(t, "Solo", []string{"Only one."})

	result, err := wk.Revisions("Solo", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Revisions) != 1 {
		t.Errorf("revisions count = %d, want 1", len(result.Revisions))
	}
	if result.DiffHTML != "" {
		t.Errorf("expected empty diff, got: %s", result.DiffHTML)
	}
}

// TestRevisionsNoFile verifies Revisions returns an error for a
// note not in the store.
func TestRevisionsNoFile(t *testing.T) {
	wk := testWiki(t)
	_, err := wk.Revisions("Ghost", 10)
	if err == nil {
		t.Fatal("expected error for missing note")
	}
}

// TestCheckoutRevisionMakesReadOnly verifies checking out an old
// revision makes the file read-only, and IsReadOnly detects it.
func TestCheckoutRevisionMakesReadOnly(t *testing.T) {
	wk, _ := writeNoteWithRevisions(t, "Pinned",
		[]string{"V1.", "V2.", "V3."})

	if wk.IsReadOnly("Pinned") {
		t.Error("should be writable before checkout")
	}

	if err := wk.CheckoutRevision("Pinned", "1.1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !wk.IsReadOnly("Pinned") {
		t.Error("should be read-only after checkout of old revision")
	}
}

// TestCheckoutRevisionRestoreHead verifies restoring head makes
// the file writable again.
func TestCheckoutRevisionRestoreHead(t *testing.T) {
	wk, _ := writeNoteWithRevisions(t, "Restore",
		[]string{"V1.", "V2."})

	wk.CheckoutRevision("Restore", "1.1")
	if !wk.IsReadOnly("Restore") {
		t.Fatal("should be read-only after old checkout")
	}

	// Restore head (empty rev).
	if err := wk.CheckoutRevision("Restore", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if wk.IsReadOnly("Restore") {
		t.Error("should be writable after head restore")
	}
}

// TestCheckoutRevisionNoFile verifies CheckoutRevision returns an
// error for a note not in the store.
func TestCheckoutRevisionNoFile(t *testing.T) {
	wk := testWiki(t)
	err := wk.CheckoutRevision("Ghost", "1.1")
	if err == nil {
		t.Fatal("expected error for missing note")
	}
}

// TestIsReadOnlyMissing verifies IsReadOnly returns false for a
// note not in the store.
func TestIsReadOnlyMissing(t *testing.T) {
	wk := testWiki(t)
	if wk.IsReadOnly("Ghost") {
		t.Error("should return false for missing note")
	}
}

// TestBuildRevList verifies revision list generation from head string.
func TestBuildRevList(t *testing.T) {
	revs := buildRevList("1.5")
	want := []string{"1.1", "1.2", "1.3", "1.4", "1.5"}
	if len(revs) != len(want) {
		t.Fatalf("len = %d, want %d", len(revs), len(want))
	}
	for i, r := range revs {
		if r != want[i] {
			t.Errorf("rev[%d] = %q, want %q", i, r, want[i])
		}
	}
}

// TestIsReadOnlyPermission verifies the isReadOnly helper with
// actual file permissions.
func TestIsReadOnlyPermission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.md")
	os.WriteFile(path, []byte("test"), 0644)
	if isReadOnly(path) {
		t.Error("0644 should not be read-only")
	}
	os.Chmod(path, 0444)
	if !isReadOnly(path) {
		t.Error("0444 should be read-only")
	}
}

// TestIsReadOnlyNoFile verifies isReadOnly returns false for
// a nonexistent file.
func TestIsReadOnlyNoFile(t *testing.T) {
	if isReadOnly("/no/such/file") {
		t.Error("should return false for nonexistent file")
	}
}

// TestReloadSkipsCheckinForReadOnly verifies that reloading a
// read-only file (checked out at non-head) updates the store but
// does NOT create a new RCS revision.
func TestReloadSkipsCheckinForReadOnly(t *testing.T) {
	wk, path := writeNoteWithRevisions(t, "NoCheckin",
		[]string{"V1.", "V2.", "V3."})

	// Get head rev before checkout.
	headBefore, _ := rcs.HeadRev(path)
	if headBefore != "1.3" {
		t.Fatalf("expected head 1.3, got %s", headBefore)
	}

	// Checkout old revision (read-only).
	rcs.Checkout(path, "1.1")

	// Simulate watcher reload.
	wk.Reload(path)
	wk.Do(func() {}) // drain queue

	// Head rev must NOT have changed — no new checkin.
	headAfter, _ := rcs.HeadRev(path)
	if headAfter != "1.3" {
		t.Errorf("head changed from 1.3 to %s — reloadFile should not checkin read-only files", headAfter)
	}

	// File should still be read-only.
	fi, _ := os.Stat(path)
	if fi.Mode().Perm()&0200 != 0 {
		t.Error("file should still be read-only after reload")
	}
}

// TestGetReadOnlyField verifies that Get includes readOnly=true
// for files checked out at a non-head revision.
func TestGetReadOnlyField(t *testing.T) {
	wk, _ := writeNoteWithRevisions(t, "ROField",
		[]string{"V1.", "V2."})

	// At head — no readOnly.
	fields := wk.Get("ROField", false)
	if fields["readOnly"] != "" {
		t.Errorf("expected no readOnly at head, got %q", fields["readOnly"])
	}

	// Checkout old — readOnly=true.
	wk.CheckoutRevision("ROField", "1.1")
	wk.Reload(wk.Store().FilePath("ROField"))
	wk.Do(func() {})
	fields = wk.Get("ROField", false)
	if fields["readOnly"] != "true" {
		t.Errorf("expected readOnly=true, got %q", fields["readOnly"])
	}
}

// TestPromoteRevision verifies that PromoteRevision checks in the
// old content as a new trunk revision and clears RCS state.
func TestPromoteRevision(t *testing.T) {
	wk, path := writeNoteWithRevisions(t, "Promote",
		[]string{"Version 1.", "Version 2.", "Version 3."})

	// Checkout old revision 1.1.
	if err := wk.CheckoutRevision("Promote", "1.1"); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	wk.Reload(path)
	wk.Do(func() {})

	// Verify checked-out state.
	rev, head := wk.Store().RCS("Promote")
	if rev != "1.1" || head != "1.3" {
		t.Errorf("rcs state = %q/%q, want 1.1/1.3", rev, head)
	}
	if !wk.IsReadOnly("Promote") {
		t.Fatal("expected read-only after checkout")
	}

	// Promote: old content becomes new head.
	if err := wk.PromoteRevision("Promote"); err != nil {
		t.Fatalf("promote: %v", err)
	}

	// File should be writable (new head).
	if wk.IsReadOnly("Promote") {
		t.Error("expected writable after promote")
	}
	// RCS state should be cleared.
	rev, head = wk.Store().RCS("Promote")
	if rev != "" || head != "" {
		t.Errorf("rcs state after promote = %q/%q, want empty", rev, head)
	}
	// RCS head should be 1.4 (promoted trunk).
	newHead, err := rcs.HeadRev(path)
	if err != nil {
		t.Fatalf("HeadRev: %v", err)
	}
	if newHead != "1.4" {
		t.Errorf("head = %s, want 1.4", newHead)
	}
	// CHECKOUT tag should be gone.
	tag, _ := rcs.CheckoutTag(path, "CHECKOUT")
	if tag != "" {
		t.Errorf("CHECKOUT tag = %q, want empty", tag)
	}
}

// TestPromoteAtHeadFails verifies that promoting a note already at
// head returns an error.
func TestPromoteAtHeadFails(t *testing.T) {
	wk, _ := writeNoteWithRevisions(t, "AtHead",
		[]string{"Version 1."})
	err := wk.PromoteRevision("AtHead")
	if err == nil {
		t.Fatal("expected error promoting note at head")
	}
	if !strings.Contains(err.Error(), "already at head") {
		t.Errorf("error = %v, want 'already at head'", err)
	}
}

// TestCheckoutSetsTag verifies that CheckoutRevision sets the
// CHECKOUT symbolic tag and populates store RCS state.
func TestCheckoutSetsTag(t *testing.T) {
	wk, path := writeNoteWithRevisions(t, "Tag",
		[]string{"V1.", "V2.", "V3."})

	wk.CheckoutRevision("Tag", "1.2")
	// Verify tag in RCS archive.
	tag, err := rcs.CheckoutTag(path, "CHECKOUT")
	if err != nil {
		t.Fatalf("CheckoutTag: %v", err)
	}
	if tag != "1.2" {
		t.Errorf("tag = %q, want 1.2", tag)
	}
	// Verify store state.
	rev, head := wk.Store().RCS("Tag")
	if rev != "1.2" || head != "1.3" {
		t.Errorf("store rcs = %q/%q, want 1.2/1.3", rev, head)
	}
}

// TestRestoreHeadClearsTag verifies that restoring head (rev="")
// deletes the CHECKOUT tag and clears RCS state.
func TestRestoreHeadClearsTag(t *testing.T) {
	wk, path := writeNoteWithRevisions(t, "Restore",
		[]string{"V1.", "V2."})

	wk.CheckoutRevision("Restore", "1.1")
	wk.CheckoutRevision("Restore", "")
	// Tag should be gone.
	tag, _ := rcs.CheckoutTag(path, "CHECKOUT")
	if tag != "" {
		t.Errorf("tag after restore = %q, want empty", tag)
	}
	// Store state should be cleared.
	rev, head := wk.Store().RCS("Restore")
	if rev != "" || head != "" {
		t.Errorf("store rcs = %q/%q, want empty", rev, head)
	}
}

// TestGetIncludesRCSFields verifies that Get returns rcsRev and
// rcsHead when the note is checked out at a non-head revision.
func TestGetIncludesRCSFields(t *testing.T) {
	wk, path := writeNoteWithRevisions(t, "Fields",
		[]string{"V1.", "V2.", "V3."})

	wk.CheckoutRevision("Fields", "1.1")
	wk.Reload(path)
	wk.Do(func() {})
	fields := wk.Get("Fields", false)
	if fields["rcsRev"] != "1.1" {
		t.Errorf("rcsRev = %q, want 1.1", fields["rcsRev"])
	}
	if fields["rcsHead"] != "1.3" {
		t.Errorf("rcsHead = %q, want 1.3", fields["rcsHead"])
	}
}

// TestScanRCSDetectsCheckout verifies that ScanRCS picks up
// checked-out notes from a CHECKOUT tag at startup.
func TestScanRCSDetectsCheckout(t *testing.T) {
	wk, path := writeNoteWithRevisions(t, "Scan",
		[]string{"V1.", "V2.", "V3."})

	// Simulate a prior checkout: set tag, make read-only.
	rcs.Checkout(path, "1.2")
	rcs.SetTag(path, "CHECKOUT", "1.2")

	wk.ScanRCS()
	rev, head := wk.Store().RCS("Scan")
	if rev != "1.2" || head != "1.3" {
		t.Errorf("ScanRCS: rcs = %q/%q, want 1.2/1.3", rev, head)
	}
}

// TestScanRCSSkipsEmptyFilePath verifies that ScanRCS skips notes
// whose store has no file path recorded (line 86).
func TestScanRCSSkipsEmptyFilePath(t *testing.T) {
	wk := testWiki(t)
	// Add a note without a file path — only in the store.
	wk.Store().Put(&tid.Note{Fields: map[string]string{
		"title": "InMemory",
	}})

	// Should not panic or set RCS state.
	wk.ScanRCS()
	rev, head := wk.Store().RCS("InMemory")
	if rev != "" || head != "" {
		t.Errorf("expected empty RCS state, got %q/%q", rev, head)
	}
}

// TestScanRCSSkipsWritableFile verifies that ScanRCS skips files
// that are writable (not read-only), even if they have an RCS archive.
func TestScanRCSSkipsWritableFile(t *testing.T) {
	wk, _ := writeNoteWithRevisions(t, "Writable", []string{"V1.", "V2."})

	// File is writable (at head) — ScanRCS should skip it.
	wk.ScanRCS()
	rev, head := wk.Store().RCS("Writable")
	if rev != "" || head != "" {
		t.Errorf("expected empty RCS for writable file, got %q/%q", rev, head)
	}
}

// TestScanRCSSkipsNoCheckoutTag verifies that ScanRCS skips
// read-only files that have no CHECKOUT tag (line 90 rev=="").
func TestScanRCSSkipsNoCheckoutTag(t *testing.T) {
	wk, path := writeNoteWithRevisions(t, "NoTag",
		[]string{"V1.", "V2."})

	// Make read-only WITHOUT setting tag.
	rcs.Checkout(path, "1.1")

	wk.ScanRCS()
	rev, head := wk.Store().RCS("NoTag")
	if rev != "" || head != "" {
		t.Errorf("expected empty RCS (no tag), got %q/%q", rev, head)
	}
}

// TestScanRCSCheckoutTagError verifies that ScanRCS skips files
// when CheckoutTag returns an error (line 90 err!=nil).
func TestScanRCSCheckoutTagError(t *testing.T) {
	wk, path := writeNoteWithRevisions(t, "TagErr",
		[]string{"V1.", "V2."})

	// Make read-only.
	rcs.Checkout(path, "1.1")
	rcs.SetTag(path, "CHECKOUT", "1.1")

	// Patch CheckoutTag to fail.
	patches := gomonkey.ApplyFunc(rcs.CheckoutTag,
		func(string, string) (string, error) {
			return "", fmt.Errorf("forced tag error")
		},
	)
	defer patches.Reset()

	wk.ScanRCS()
	rev, head := wk.Store().RCS("TagErr")
	if rev != "" || head != "" {
		t.Errorf("expected empty RCS after tag error, got %q/%q", rev, head)
	}
}

// TestScanRCSHeadRevError verifies that ScanRCS skips files when
// HeadRev fails (line 94-95).
func TestScanRCSHeadRevError(t *testing.T) {
	wk, path := writeNoteWithRevisions(t, "HeadErr",
		[]string{"V1.", "V2."})

	rcs.Checkout(path, "1.1")
	rcs.SetTag(path, "CHECKOUT", "1.1")

	// Patch HeadRev to fail.
	patches := gomonkey.ApplyFunc(rcs.HeadRev,
		func(string) (string, error) {
			return "", fmt.Errorf("forced head error")
		},
	)
	defer patches.Reset()

	wk.ScanRCS()
	rev, head := wk.Store().RCS("HeadErr")
	if rev != "" || head != "" {
		t.Errorf("expected empty RCS after head error, got %q/%q", rev, head)
	}
}

// TestListSymlinkMissingNoteInOwner verifies that List silently
// skips a symlinked title when the owner wiki has no matching note
// (td == nil at line 245).
func TestListSymlinkMissingNoteInOwner(t *testing.T) {
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error { return nil })
	defer patches.Reset()

	// Owner wiki exists but does NOT have "Ghost" note.
	work := testWiki(t)
	writeNote(t, work, "Other", "other body")

	// Local wiki has symlink for "Ghost" → "work".
	local := testWiki(t)
	writeNote(t, local, "Local", "local body")
	local.SetSymlinks(map[string]string{"Ghost": "work"})
	hub := NewHub(map[string]*Wiki{"work": work, "local": local})
	local.SetHub(hub)

	list := local.List()
	for _, fields := range list {
		if fields["title"] == "Ghost" {
			t.Error("Ghost should not appear (owner has no such note)")
		}
	}
	// Only "Local" should be in the list.
	if len(list) != 1 {
		t.Errorf("list len = %d, want 1", len(list))
	}
}

// TestRevisionsHeadRevError verifies that Revisions returns an error
// when rcs.HeadRev fails (lines 288-291).
func TestRevisionsHeadRevError(t *testing.T) {
	wk, _ := writeNoteWithRevisions(t, "HRErr",
		[]string{"V1.", "V2."})

	patches := gomonkey.ApplyFunc(rcs.HeadRev,
		func(string) (string, error) {
			return "", fmt.Errorf("forced headrev error")
		},
	)
	defer patches.Reset()

	_, err := wk.Revisions("HRErr", 10)
	if err == nil {
		t.Fatal("expected error from HeadRev failure")
	}
	if !strings.Contains(err.Error(), "forced headrev error") {
		t.Errorf("error = %v, want 'forced headrev error'", err)
	}
}

// TestPromoteNoFile verifies that PromoteRevision returns a "no file"
// error for a note that has no file path (lines 348-351).
func TestPromoteNoFile(t *testing.T) {
	wk := testWiki(t)
	err := wk.PromoteRevision("Ghost")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "no file") {
		t.Errorf("error = %v, want 'no file'", err)
	}
}

// TestPromoteCheckinError verifies that PromoteRevision returns
// the rcs.Checkin error (lines 356-358).
func TestPromoteCheckinError(t *testing.T) {
	wk, path := writeNoteWithRevisions(t, "ChkErr",
		[]string{"V1.", "V2."})

	// Checkout old revision so file is read-only.
	wk.CheckoutRevision("ChkErr", "1.1")
	wk.Reload(path)
	wk.Do(func() {})

	patches := gomonkey.ApplyFunc(rcs.Checkin,
		func(string) error {
			return fmt.Errorf("forced checkin error")
		},
	)
	defer patches.Reset()

	err := wk.PromoteRevision("ChkErr")
	if err == nil {
		t.Fatal("expected error from Checkin failure")
	}
	if !strings.Contains(err.Error(), "forced checkin error") {
		t.Errorf("error = %v, want 'forced checkin error'", err)
	}
}

// TestBuildDiffHTMLDiffRangeError verifies that buildDiffHTML skips
// pairs where rcs.DiffRange returns an error (line 398-399).
func TestBuildDiffHTMLDiffRangeError(t *testing.T) {
	patches := gomonkey.ApplyFunc(rcs.DiffRange,
		func(path, r1, r2 string) (string, error) {
			return "", fmt.Errorf("forced diff error")
		},
	)
	defer patches.Reset()

	revs := []string{"1.1", "1.2", "1.3"}
	got := buildDiffHTML("/any/path", revs, 10)
	// All pairs failed, so output should be empty.
	if got != "" {
		t.Errorf("expected empty diff HTML, got: %s", got)
	}
}

// TestReloadFileSymlinkUnchanged verifies that reloadFile returns
// early for a known symlink whose owner hasn't changed (line 585-587).
func TestReloadFileSymlinkUnchanged(t *testing.T) {
	parent := t.TempDir()
	workDir := filepath.Join(parent, "work")
	evernoteDir := filepath.Join(parent, "evernote")
	os.Mkdir(workDir, 0755)
	os.Mkdir(evernoteDir, 0755)

	os.WriteFile(filepath.Join(workDir, "Shared.md"),
		[]byte("title: Shared\n\nBody."), 0644)

	// Create symlink in evernote.
	linkPath := filepath.Join(evernoteDir, "Shared.md")
	os.Symlink("../work/Shared.md", linkPath)

	evernote := New("evernote", evernoteDir, nil, 100)
	evernote.symlinks["Shared"] = "work" // already known
	evernote.Start()
	t.Cleanup(evernote.Stop)

	// Reload should be a no-op (unchanged symlink).
	evernote.Reload(linkPath)
	evernote.do(func() {})

	// Symlink still present, no store entry.
	if evernote.symlinks["Shared"] != "work" {
		t.Errorf("symlink should remain, got %q", evernote.symlinks["Shared"])
	}
	if evernote.store.Get("Shared") != nil {
		t.Error("unchanged symlink should not add to store")
	}
}

// TestReloadFileSymlinkReadError verifies that reloadFile handles
// a read error on a new symlink (lines 590-593).
func TestReloadFileSymlinkReadError(t *testing.T) {
	parent := t.TempDir()
	workDir := filepath.Join(parent, "work")
	evernoteDir := filepath.Join(parent, "evernote")
	os.Mkdir(workDir, 0755)
	os.Mkdir(evernoteDir, 0755)

	// Create unreadable symlink target.
	os.WriteFile(filepath.Join(workDir, "Bad.md"),
		[]byte("title: Bad\n\nBody."), 0000)

	linkPath := filepath.Join(evernoteDir, "Bad.md")
	os.Symlink("../work/Bad.md", linkPath)

	evernote := New("evernote", evernoteDir, nil, 100)
	evernote.Start()
	t.Cleanup(evernote.Stop)
	t.Cleanup(func() {
		os.Chmod(filepath.Join(workDir, "Bad.md"), 0644)
	})

	// Reload: symlink is new but target is unreadable.
	evernote.Reload(linkPath)
	evernote.do(func() {})

	// Should not panic; symlink not added.
	if _, ok := evernote.symlinks["Bad"]; ok {
		t.Error("unreadable symlink should not be added")
	}
}

// TestReloadFileSymlinkParseError verifies that reloadFile handles
// a parse error on a new symlink (lines 595-598).
func TestReloadFileSymlinkParseError(t *testing.T) {
	parent := t.TempDir()
	workDir := filepath.Join(parent, "work")
	evernoteDir := filepath.Join(parent, "evernote")
	os.Mkdir(workDir, 0755)
	os.Mkdir(evernoteDir, 0755)

	// Create a file that tid.Parse will reject (no title).
	os.WriteFile(filepath.Join(workDir, "NoTitle.md"),
		[]byte("no title header\n\nBody."), 0644)

	linkPath := filepath.Join(evernoteDir, "NoTitle.md")
	os.Symlink("../work/NoTitle.md", linkPath)

	evernote := New("evernote", evernoteDir, nil, 100)
	evernote.Start()
	t.Cleanup(evernote.Stop)

	// Reload: symlink is new but content has no title.
	evernote.Reload(linkPath)
	evernote.do(func() {})

	// Should not panic; no symlink added.
	if _, ok := evernote.symlinks["NoTitle"]; ok {
		t.Error("bad-parse symlink should not be added")
	}
}

// TestIsUnchangedMetaDiffers verifies that isUnchanged returns false
// when metadata fields differ (line 667-669).
func TestIsUnchangedMetaDiffers(t *testing.T) {
	wk := testWiki(t)
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error { return nil })
	defer patches.Reset()

	// Write initial note with tags.
	writeNote(t, wk, "Meta", "body")

	// Re-parse with different metadata (added "tags" field).
	td := &tid.Note{Fields: map[string]string{
		"title": "Meta",
		"tags":  "NewTag",
		"text":  "body",
	}}
	path := filepath.Join(wk.NoteDir(), tid.TitleToFilename("Meta"))
	os.WriteFile(path, tid.Serialize(td), 0644)

	wk.Reload(path)
	wk.do(func() {})

	// The reload should update the store because meta differs.
	got := wk.Store().Get("Meta")
	if got == nil {
		t.Fatal("expected note in store")
	}
	if got.Fields["tags"] != "NewTag" {
		t.Errorf("tags = %q, want %q", got.Fields["tags"], "NewTag")
	}
}

// TestIsUnchangedCacheMiss verifies that isUnchanged returns true
// when metadata matches but body was evicted from cache (preventing
// phantom RCS checkins from watcher echo).
func TestIsUnchangedCacheMiss(t *testing.T) {
	// cacheSize=2 so third note evicts the first.
	wk := New("test", t.TempDir(), nil, 2)
	wk.Start()
	defer wk.Stop()

	checkins := 0
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(string) error {
		checkins++
		return nil
	})
	defer patches.Reset()

	// Write 3 notes — first body gets evicted from LRU cache.
	writeNote(t, wk, "A", "alpha")
	writeNote(t, wk, "B", "bravo")
	writeNote(t, wk, "C", "charlie")

	// Record checkins so far (3 from writeNote + Put flow).
	before := checkins

	// Trigger reload of A (same content on disk) — should be detected
	// as unchanged even without cache hit, avoiding phantom checkin.
	path := filepath.Join(wk.NoteDir(), tid.TitleToFilename("A"))
	wk.Reload(path)
	wk.Do(func() {})

	if checkins != before {
		t.Errorf("phantom checkin: checkins went from %d to %d", before, checkins)
	}
}

// --- Attachment listing tests ---

// TestNoteAttachmentsWithFiles verifies that buildNote includes an
// attachments JSON array when the note has a subdirectory with files.
// With no exclusion list, all files appear including .pdf.md shadows.
func TestNoteAttachmentsWithFiles(t *testing.T) {
	wk := testWiki(t)
	writeNote(t, wk, "Report", "body text")

	// Create subdir with attachments.
	subdir := filepath.Join(wk.NoteDir(), "Report")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "chart.png"), []byte("img"), 0644)
	os.WriteFile(filepath.Join(subdir, "data.pdf"), []byte("pdf"), 0644)
	os.WriteFile(filepath.Join(subdir, "data.pdf.md"), []byte("shadow"), 0644)

	result := wk.Get("Report", false)
	if result == nil {
		t.Fatal("expected note, got nil")
	}
	a := result["attachments"]
	if a == "" {
		t.Fatal("expected attachments, got empty")
	}
	var names []string
	if err := json.Unmarshal([]byte(a), &names); err != nil {
		t.Fatalf("bad attachments JSON: %v", err)
	}
	// Default: no exclusions, all 3 files visible.
	if len(names) != 3 {
		t.Fatalf("expected 3 attachments, got %d: %v", len(names), names)
	}
}

// TestNoteAttachmentsExclude verifies that attachment_exclude
// hides files matching configured suffixes.
func TestNoteAttachmentsExclude(t *testing.T) {
	wk := testWiki(t)
	wk.SetAttachExclude([]string{".pdf.md", ".bak"})
	writeNote(t, wk, "Report", "body text")

	subdir := filepath.Join(wk.NoteDir(), "Report")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "chart.png"), []byte("img"), 0644)
	os.WriteFile(filepath.Join(subdir, "data.pdf"), []byte("pdf"), 0644)
	os.WriteFile(filepath.Join(subdir, "data.pdf.md"), []byte("shadow"), 0644)
	os.WriteFile(filepath.Join(subdir, "old.bak"), []byte("backup"), 0644)

	result := wk.Get("Report", false)
	if result == nil {
		t.Fatal("expected note, got nil")
	}
	var names []string
	json.Unmarshal([]byte(result["attachments"]), &names)
	// .pdf.md and .bak excluded, chart.png and data.pdf remain.
	if len(names) != 2 {
		t.Fatalf("expected 2 attachments, got %d: %v", len(names), names)
	}
}

// TestNoteAttachmentsNoSubdir verifies that notes without a
// subdirectory have no attachments field.
func TestNoteAttachmentsNoSubdir(t *testing.T) {
	wk := testWiki(t)
	writeNote(t, wk, "Plain", "no attachments")

	result := wk.Get("Plain", false)
	if result == nil {
		t.Fatal("expected note, got nil")
	}
	if a := result["attachments"]; a != "" {
		t.Errorf("expected no attachments, got %q", a)
	}
}

// TestNoteAttachmentsEmptySubdir verifies that an empty subdirectory
// produces no attachments field.
func TestNoteAttachmentsEmptySubdir(t *testing.T) {
	wk := testWiki(t)
	writeNote(t, wk, "Empty", "body")
	os.MkdirAll(filepath.Join(wk.NoteDir(), "Empty"), 0755)

	result := wk.Get("Empty", false)
	if result == nil {
		t.Fatal("expected note, got nil")
	}
	if a := result["attachments"]; a != "" {
		t.Errorf("expected no attachments for empty subdir, got %q", a)
	}
}

// TestNoteAttachmentsNoDirWiki verifies that a wiki with no noteDir
// (in-memory only) returns no attachments.
func TestNoteAttachmentsNoDirWiki(t *testing.T) {
	wk := New("test", "", nil, 100)
	wk.Start()
	defer wk.Stop()
	td := &tid.Note{Fields: map[string]string{"title": "InMem"}}
	wk.Store().Put(td)
	wk.SeedBody("InMem", "text")

	result := wk.Get("InMem", false)
	if result == nil {
		t.Fatal("expected note, got nil")
	}
	if a := result["attachments"]; a != "" {
		t.Errorf("expected no attachments for no-dir wiki, got %q", a)
	}
}

// TestNoteAttachmentsSkipsSubdirs verifies that subdirectories inside
// the note's attachment directory are excluded from the listing.
func TestNoteAttachmentsSkipsSubdirs(t *testing.T) {
	wk := testWiki(t)
	writeNote(t, wk, "Mixed", "body")

	subdir := filepath.Join(wk.NoteDir(), "Mixed")
	os.MkdirAll(filepath.Join(subdir, "nested"), 0755)
	os.WriteFile(filepath.Join(subdir, "file.txt"), []byte("ok"), 0644)

	result := wk.Get("Mixed", false)
	if result == nil {
		t.Fatal("expected note, got nil")
	}
	var names []string
	json.Unmarshal([]byte(result["attachments"]), &names)
	if len(names) != 1 || names[0] != "file.txt" {
		t.Errorf("expected [file.txt], got %v", names)
	}
}

// TestNoteAttachmentsEnqueuePdf verifies that unconverted PDFs
// trigger the configured enqueue command.
func TestNoteAttachmentsEnqueuePdf(t *testing.T) {
	wk := testWiki(t)
	wk.SetPdfConvertCmd("/bin/echo")
	writeNote(t, wk, "Report", "body")

	subdir := filepath.Join(wk.NoteDir(), "Report")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "scan.pdf"), []byte("pdf"), 0644)

	// Capture exec calls via the package-level variable.
	var called []string
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		called = append(called, args[0])
		return exec.Command("true")
	}
	defer func() { execCommand = orig }()

	result := wk.Get("Report", false)
	if result == nil {
		t.Fatal("expected note, got nil")
	}
	var names []string
	json.Unmarshal([]byte(result["attachments"]), &names)
	if len(names) != 1 || names[0] != "scan.pdf" {
		t.Errorf("expected [scan.pdf], got %v", names)
	}
	if len(called) != 1 || !strings.HasSuffix(called[0], "scan.pdf") {
		t.Errorf("expected enqueue of scan.pdf, got %v", called)
	}
}

// TestNoteAttachmentsSkipConvertedPdf verifies that PDFs with a
// sibling .pdf.md are NOT enqueued for conversion.
func TestNoteAttachmentsSkipConvertedPdf(t *testing.T) {
	wk := testWiki(t)
	wk.SetPdfConvertCmd("/bin/echo")
	writeNote(t, wk, "Done", "body")

	subdir := filepath.Join(wk.NoteDir(), "Done")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "doc.pdf"), []byte("pdf"), 0644)
	os.WriteFile(filepath.Join(subdir, "doc.pdf.md"), []byte("shadow"), 0644)

	var called []string
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		called = append(called, args[0])
		return exec.Command("true")
	}
	defer func() { execCommand = orig }()

	wk.Get("Done", false)
	if len(called) != 0 {
		t.Errorf("expected no enqueue, got %v", called)
	}
}

// TestNoteAttachmentsNoConvertCmdDisabled verifies that no enqueue
// happens when pdf_convert_cmd is empty (disabled).
func TestNoteAttachmentsNoConvertCmdDisabled(t *testing.T) {
	wk := testWiki(t)
	// pdfConvertCmd left empty (default).
	writeNote(t, wk, "Silent", "body")

	subdir := filepath.Join(wk.NoteDir(), "Silent")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "test.pdf"), []byte("pdf"), 0644)

	var called int
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		called++
		return exec.Command("true")
	}
	defer func() { execCommand = orig }()

	wk.Get("Silent", false)
	if called != 0 {
		t.Errorf("expected no enqueue when disabled, got %d calls", called)
	}
}

// TestEnqueuePdfsCmdError verifies that a failing command is logged
// but does not prevent attachment listing from returning.
func TestEnqueuePdfsCmdError(t *testing.T) {
	wk := testWiki(t)
	wk.SetPdfConvertCmd("/nonexistent-binary")
	writeNote(t, wk, "Bad", "body")

	subdir := filepath.Join(wk.NoteDir(), "Bad")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "fail.pdf"), []byte("pdf"), 0644)

	// Restore the real exec.Command for this test so Start() fails.
	orig := execCommand
	execCommand = exec.Command
	defer func() { execCommand = orig }()

	result := wk.Get("Bad", false)
	if result == nil {
		t.Fatal("expected note despite enqueue error")
	}
	var names []string
	json.Unmarshal([]byte(result["attachments"]), &names)
	if len(names) != 1 || names[0] != "fail.pdf" {
		t.Errorf("expected [fail.pdf], got %v", names)
	}
}
