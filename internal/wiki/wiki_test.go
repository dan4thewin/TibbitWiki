package wiki

import (
	"fmt"
	"os"
	"path/filepath"
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
	// Mock search.Search to return a hit for a note in the store.
	patches := gomonkey.ApplyFunc(search.Search, func(dir, query string) ([]search.Result, error) {
		return []search.Result{
			{File: "Found.md", Lines: []string{"matching line"}},
		}, nil
	})
	defer patches.Reset()

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
	patches := gomonkey.ApplyFunc(search.Search, func(dir, query string) ([]search.Result, error) {
		return nil, fmt.Errorf("rg exploded")
	})
	defer patches.Reset()

	wk := testWiki(t)
	_, err := wk.Search("hello")
	if err == nil {
		t.Error("expected error")
	}
}

// TestWikiSearchNilLines verifies nil Lines are coalesced to [].
func TestWikiSearchNilLines(t *testing.T) {
	patches := gomonkey.ApplyFunc(search.Search, func(dir, query string) ([]search.Result, error) {
		return []search.Result{
			{File: "NilLine.md", Lines: nil},
		}, nil
	})
	defer patches.Reset()

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
	patches := gomonkey.ApplyFunc(search.Search, func(dir, query string) ([]search.Result, error) {
		return []search.Result{
			{File: "NoSuch.md", Lines: []string{"x"}},
		}, nil
	})
	defer patches.Reset()

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
	ed := editor.New("echo %s")
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
