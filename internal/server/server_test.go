package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/dan4thewin/tibbitwiki/internal/editor"
	"github.com/dan4thewin/tibbitwiki/internal/rcs"
	"github.com/dan4thewin/tibbitwiki/internal/search"
	"github.com/dan4thewin/tibbitwiki/internal/store"
	"github.com/dan4thewin/tibbitwiki/internal/tid"
	"github.com/dan4thewin/tibbitwiki/internal/wiki"
)

// newTestServer creates a Server with a wiki containing a pre-populated
// store and a temp index.html file for testing.
func newTestServer(t *testing.T) (*Server, *wiki.Wiki) {
	t.Helper()
	dir := t.TempDir()
	ed, _ := editor.New("echo %s")
	wk := wiki.New("test", dir, ed, 100)
	wk.Start()
	t.Cleanup(wk.Stop)

	// Add a test note with a file on disk.
	td := &tid.Note{Fields: map[string]string{
		"title":   "Hello",
		"tags":    "Test",
		"text":    "Hello world.",
		"created": "20230101000000000",
	}}
	path := filepath.Join(dir, "Hello.md")
	os.WriteFile(path, tid.Serialize(td), 0644)
	wk.Store().PutWithPath(td, path)
	wk.SeedBody("Hello", "Hello world.")

	// Create a temp index.html.
	f, err := os.CreateTemp("", "index-*.html")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	f.WriteString("<html><body>TibbitWiki</body></html>")
	f.Close()

	// Write per-wiki CSS file for the test.
	cssPath := filepath.Join(dir, "test.css")
	os.WriteFile(cssPath, []byte(":root{} .chroma{}"), 0644)

	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, ed, new(atomic.Int64), 3000, 10, f.Name(), dir, "")
	return srv, wk
}

// TestHandleStatus verifies the /status endpoint returns expected
// JSON fields.
func TestHandleStatus(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/status", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["username"] != "" {
		t.Errorf("username = %v, want empty string", body["username"])
	}
	if body["anonymous"] != false {
		t.Errorf("anonymous = %v, want false", body["anonymous"])
	}
}

// TestHandleIndex verifies that GET / serves the index.html file.
func TestHandleIndex(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "TibbitWiki") {
		t.Error("index response should contain 'TibbitWiki'")
	}
}

// TestHandleIndexEmbedded verifies that GET / serves the embedded
// index.html when no external path is configured.
func TestHandleIndexEmbedded(t *testing.T) {
	wk := wiki.New("test", t.TempDir(), nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")
	req := httptest.NewRequest("GET", "/test/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
}

// TestHandleIndexNotFound verifies that non-root paths return 404.
func TestHandleIndexNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestHandleListNotes verifies the notes.json endpoint
// returns notes without text.
func TestHandleListNotes(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/notes", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body []map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("len = %d, want 1", len(body))
	}
	if body[0]["title"] != "Hello" {
		t.Errorf("title = %q, want %q", body[0]["title"], "Hello")
	}
	// Skinny: should not have "text".
	if _, ok := body[0]["text"]; ok {
		t.Error("skinny note should not include 'text'")
	}
	if body[0]["revision"] == "" {
		t.Error("should include revision")
	}
	// Server created with editor → X-Has-Editor: true.
	if h := w.Header().Get("X-Has-Editor"); h != "true" {
		t.Errorf("X-Has-Editor = %q, want %q", h, "true")
	}
}

// TestHandleListNotesNoEditor verifies that X-Has-Editor is absent
// when the server has no editor configured.
func TestHandleListNotesNoEditor(t *testing.T) {
	wk := wiki.New("test", t.TempDir(), nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")
	req := httptest.NewRequest("GET", "/test/notes", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if h := w.Header().Get("X-Has-Editor"); h != "" {
		t.Errorf("X-Has-Editor = %q, want absent", h)
	}
}

// TestGetNote verifies GET for a single note returns full
// JSON with all fields.
func TestGetNote(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/notes/Hello", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["title"] != "Hello" {
		t.Errorf("title = %q, want %q", body["title"], "Hello")
	}
	if body["text"] != "Hello world." {
		t.Errorf("text = %q, want %q", body["text"], "Hello world.")
	}
	if w.Header().Get("ETag") == "" {
		t.Error("response should include ETag header")
	}
}

// TestGetNoteRenderPlain verifies GET with ?render=plain uses the
// plain renderer (no chroma, per-line data-line on code blocks).
func TestGetNoteRenderPlain(t *testing.T) {
	srv, wk := newTestServer(t)
	// Add a note with a fenced code block.
	td := &tid.Note{Fields: map[string]string{
		"title": "CodeNote",
		"text":  "```go\nfmt.Println()\n```",
		"type":  "text/markdown",
	}}
	path := filepath.Join(wk.NoteDir(), "CodeNote.md")
	os.WriteFile(path, tid.Serialize(td), 0644)
	wk.Store().PutWithPath(td, path)

	req := httptest.NewRequest("GET", "/test/notes/CodeNote?render=plain", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	html := body["html"]
	// Plain render should have data-line on code lines.
	if !strings.Contains(html, "data-line=") {
		t.Errorf("expected data-line in plain render, got:\n%s", html)
	}
	// Plain render should NOT have chroma classes.
	if strings.Contains(html, "chroma") {
		t.Errorf("plain render should not have chroma, got:\n%s", html)
	}
}

// TestGetNoteNotFound verifies GET for a missing note returns 404.
func TestGetNoteNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/notes/NoSuch", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestPutNoteStampsModified verifies PUT stamps modified and created.
func TestPutNoteStampsModified(t *testing.T) {
	srv, wk := newTestServer(t)
	body := `{"title":"Stamped","text":"hi"}`
	req := httptest.NewRequest("PUT", "/test/notes/Stamped",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	td := wk.Store().Get("Stamped")
	if td == nil {
		t.Fatal("note not found after PUT")
	}
	mod := td.Fields["modified"]
	if mod == "" {
		t.Error("modified not stamped")
	}
	if _, err := time.Parse(time.RFC3339, mod); err != nil {
		t.Errorf("modified not ISO 8601: %q", mod)
	}
	created := td.Fields["created"]
	if created == "" {
		t.Error("created not stamped on new note")
	}

	// Second PUT should not overwrite created.
	body2 := `{"title":"Stamped","text":"updated"}`
	req2 := httptest.NewRequest("PUT", "/test/notes/Stamped",
		strings.NewReader(body2))
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	td2 := wk.Store().Get("Stamped")
	if td2.Fields["created"] != created {
		t.Errorf("created changed: %q -> %q", created, td2.Fields["created"])
	}
}

// TestListNotesIncludesAccessed verifies the listing includes
// accessed time from file atime.
func TestListNotesIncludesAccessed(t *testing.T) {
	srv, wk := newTestServer(t)
	// Create a real file in the wiki's noteDir.
	td := &tid.Note{Fields: map[string]string{
		"title": "Listed",
		"text":  "hi",
	}}
	path := filepath.Join(wk.NoteDir(), "Listed.md")
	os.WriteFile(path, tid.Serialize(td), 0644)
	wk.Store().PutWithPath(td, path)

	req := httptest.NewRequest("GET", "/test/notes", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var body []map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	for _, n := range body {
		if n["title"] == "Listed" {
			if n["accessed"] == "" {
				t.Error("listing should include accessed for notes with real files")
			}
			return
		}
	}
	t.Error("Listed note not found in listing")
}

// TestPutNote verifies PUT creates/updates a note and returns
// 204 with ETag.
func TestPutNote(t *testing.T) {
	srv, wk := newTestServer(t)
	body := `{"title":"NewOne","tags":"Test","text":"New body."}`
	req := httptest.NewRequest("PUT", "/test/notes/NewOne",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if w.Header().Get("ETag") == "" {
		t.Error("response should include ETag header")
	}

	// Verify it's in the store.
	if wk.Store().Get("NewOne") == nil {
		t.Fatal("expected note in store")
	}
	if text := wk.Get("NewOne", false)["text"]; text != "New body." {
		t.Errorf("text = %q, want %q", text, "New body.")
	}
}

// TestPutNewNoteWritesToDisk verifies that PUT for a new note
// creates a .md file on disk when noteDir is configured.
func TestPutNewNoteWritesToDisk(t *testing.T) {
	srv, wk := newTestServer(t)

	body := `{"title":"DiskWrite","tags":"Test","text":"Written to disk."}`
	req := httptest.NewRequest("PUT", "/test/notes/DiskWrite",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// Verify it's in the store with a file path.
	got := wk.Store().Get("DiskWrite")
	if got == nil {
		t.Fatal("expected note in store")
	}
	fp := wk.Store().FilePath("DiskWrite")
	if fp == "" {
		t.Fatal("expected file path to be set")
	}

	// Verify the file exists on disk.
	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if !strings.Contains(string(data), "Written to disk.") {
		t.Errorf("file content = %q, should contain 'Written to disk.'", string(data))
	}
}

// TestPutNoteSkinny verifies that a skinny PUT preserves the
// existing text.
func TestPutNoteSkinny(t *testing.T) {
	srv, wk := newTestServer(t)
	// PUT with _is_skinny flag — should preserve existing text.
	body := `{"title":"Hello","tags":"Updated","_is_skinny":"true"}`
	req := httptest.NewRequest("PUT", "/test/notes/Hello",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if text := wk.Get("Hello", false)["text"]; text != "Hello world." {
		t.Errorf("text = %q, want %q (preserved)", text, "Hello world.")
	}
	got := wk.Store().Get("Hello")
	if got.Fields["tags"] != "Updated" {
		t.Errorf("tags = %q, want %q", got.Fields["tags"], "Updated")
	}
}

// TestPutNoteBadJSON verifies that invalid JSON returns 400.
func TestPutNoteBadJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("PUT", "/test/notes/Bad",
		strings.NewReader("not json"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestDeleteNote verifies DELETE removes a note and returns 204.
func TestDeleteNote(t *testing.T) {
	srv, wk := newTestServer(t)
	req := httptest.NewRequest("DELETE", "/test/notes/Hello", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := wk.Store().Get("Hello"); got != nil {
		t.Error("note should be deleted")
	}
}

// TestNoteMethodNotAllowed verifies unsupported methods return 405.
func TestNoteMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("PATCH", "/test/notes/Hello", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// TestHandleEditExternal verifies the edit-external endpoint
// launches the editor.
func TestHandleEditExternal(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/test/edit-external/Hello", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

// TestHandleEditExternalNoPath verifies edit-external returns 404
// for notes with no file path.
func TestHandleEditExternalNoPath(t *testing.T) {
	srv, wk := newTestServer(t)
	wk.Store().Put(&tid.Note{Fields: map[string]string{"title": "NoFile"}})

	req := httptest.NewRequest("POST", "/test/edit-external/NoFile", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestHandleEditExternalNoEditor verifies edit-external returns 500
// when no editor is configured.
func TestHandleEditExternalNoEditor(t *testing.T) {
	dir := t.TempDir()
	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	td := &tid.Note{Fields: map[string]string{"title": "X"}}
	path := filepath.Join(dir, "X.md")
	os.WriteFile(path, tid.Serialize(td), 0644)
	wk.Store().PutWithPath(td, path)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("POST", "/test/edit-external/X", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// TestHandleEditExternalMethodNotAllowed verifies non-POST returns 405.
func TestHandleEditExternalMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/edit-external/Hello", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// TestNoteMissingTitle verifies that requests with empty title
// return 400.
func TestNoteMissingTitle(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/notes/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestDeleteMissingTitle verifies empty title on delete returns 400.
func TestDeleteMissingTitle(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("DELETE", "/test/notes/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestEditExternalMissingTitle verifies empty title returns 400.
func TestEditExternalMissingTitle(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/test/edit-external/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestPutNoteWithNestedFields verifies that nested "fields"
// objects are flattened.
func TestPutNoteWithNestedFields(t *testing.T) {
	srv, wk := newTestServer(t)
	body := `{"title":"Nested","fields":{"custom_field":"value"},"text":"Body."}`
	req := httptest.NewRequest("PUT", "/test/notes/Nested",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	got := wk.Store().Get("Nested")
	if got == nil {
		t.Fatal("expected note in store")
	}
	if got.Fields["custom_field"] != "value" {
		t.Errorf("custom_field = %q, want %q", got.Fields["custom_field"], "value")
	}
}

// TestPutNoteNoFilePathNoDir verifies that PUT on a server with no
// noteDir stores the note via Put (no disk write) when the
// note has no existing file path.
func TestPutNoteNoFilePathNoDir(t *testing.T) {
	// Create a wiki with no noteDir.
	wk := wiki.New("test", "", nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	body := `{"title":"NoFile","tags":"Mem","text":"In-memory only."}`
	req := httptest.NewRequest("PUT", "/test/notes/NoFile",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if wk.Store().Get("NoFile") == nil {
		t.Fatal("expected note in store")
	}
	if text := wk.Get("NoFile", false)["text"]; text != "In-memory only." {
		t.Errorf("text = %q, want %q", text, "In-memory only.")
	}
	// Should have no file path since there is no noteDir.
	if fp := wk.Store().FilePath("NoFile"); fp != "" {
		t.Errorf("file path = %q, want empty", fp)
	}
}

// TestDeleteNoteWithDiskFile verifies that DELETE removes the
// backing .md file from disk when the note has a file path.
func TestDeleteNoteWithDiskFile(t *testing.T) {
	srv, wk := newTestServer(t)

	// Create a real file in the wiki's noteDir.
	td := &tid.Note{Fields: map[string]string{
		"title": "DiskDel",
		"text":  "Body.",
	}}
	path := filepath.Join(wk.NoteDir(), "DiskDel.md")
	os.WriteFile(path, tid.Serialize(td), 0644)
	wk.Store().PutWithPath(td, path)

	req := httptest.NewRequest("DELETE", "/test/notes/DiskDel", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	// Verify the file was removed from disk.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file %q to be deleted, err = %v", path, err)
	}
	// Verify the note is gone from the store.
	if got := wk.Store().Get("DiskDel"); got != nil {
		t.Error("note should be deleted from store")
	}
}

// TestHandleEditExternalEditorFails verifies that edit-external returns
// 500 when the editor command fails to start.
func TestHandleEditExternalEditorFails(t *testing.T) {
	dir := t.TempDir()
	// Use a nonexistent binary so exec.Command.Start returns an error.
	ed, _ := editor.New("/nonexistent/editor %s")
	wk := wiki.New("test", dir, ed, 100)
	wk.Start()
	t.Cleanup(wk.Stop)

	td := &tid.Note{Fields: map[string]string{
		"title": "FailEdit",
		"text":  "Body.",
	}}
	path := filepath.Join(dir, "FailEdit.md")
	os.WriteFile(path, tid.Serialize(td), 0644)
	wk.Store().PutWithPath(td, path)

	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("POST", "/test/edit-external/FailEdit", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// --- gomonkey-based tests for error-logging branches ---

// TestPutNoteWriteFileError verifies that an os.WriteFile error
// causes a 500 and the note is NOT stored.
func TestPutNoteWriteFileError(t *testing.T) {
	patches := gomonkey.ApplyFunc(os.WriteFile, func(name string, data []byte, perm os.FileMode) error {
		return fmt.Errorf("disk full")
	})
	defer patches.Reset()

	srv, wk := newTestServer(t)

	body := `{"title":"WriteErr","text":"Body."}`
	req := httptest.NewRequest("PUT", "/test/notes/WriteErr",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if got := wk.Store().Get("WriteErr"); got != nil {
		t.Fatal("note should NOT be in store after write error")
	}
}

// TestPutNoteRCSCheckinError verifies that an rcs.Checkin error
// causes a 500 and the note is NOT stored.
func TestPutNoteRCSCheckinError(t *testing.T) {
	orig := rcs.Checkin
	rcs.Checkin = func(string) error { return fmt.Errorf("rcs failed") }
	defer func() { rcs.Checkin = orig }()

	srv, wk := newTestServer(t)

	body := `{"title":"RCSErr","text":"Body."}`
	req := httptest.NewRequest("PUT", "/test/notes/RCSErr",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if got := wk.Store().Get("RCSErr"); got != nil {
		t.Fatal("note should NOT be in store after RCS error")
	}
}

// TestDeleteNoteRemoveError verifies that a non-NotExist os.Remove
// error is logged but the note is still deleted from the store (204).
func TestDeleteNoteRemoveError(t *testing.T) {
	patches := gomonkey.ApplyFunc(os.Remove, func(name string) error {
		return fmt.Errorf("permission denied")
	})
	defer patches.Reset()

	srv, wk := newTestServer(t)
	wk.Store().PutWithPath(&tid.Note{Fields: map[string]string{
		"title": "RemErr",
		"text":  "Body.",
	}}, "/fake/RemErr.md")

	req := httptest.NewRequest("DELETE", "/test/notes/RemErr", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := wk.Store().Get("RemErr"); got != nil {
		t.Error("note should be deleted from store")
	}
}

// TestLineEditReplace verifies replacing a body line via /line-edit/.
func TestLineEditReplace(t *testing.T) {
	srv, wk := newTestServer(t)

	// Create a .md file with headers and a list body.
	dir := wk.NoteDir()
	path := filepath.Join(dir, "LineTest.md")
	content := "title: LineTest\n\n- alpha\n- beta\n- gamma\n"
	os.WriteFile(path, []byte(content), 0644)
	td, _ := tid.Parse([]byte(content))
	wk.Store().PutWithPath(td, path)

	body := `{"line":2,"text":"- BETA"}`
	req := httptest.NewRequest("POST", "/test/line-edit/LineTest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	// Verify file on disk was updated.
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "- BETA") {
		t.Errorf("file should contain '- BETA', got:\n%s", data)
	}
	if strings.Contains(string(data), "- beta") {
		t.Errorf("file should not contain '- beta' anymore")
	}

	// Verify response contains re-rendered HTML.
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.Contains(resp["html"], "BETA") {
		t.Errorf("response html should contain BETA, got: %s", resp["html"])
	}
}

// TestLineEditInsert verifies inserting a line after line N.
func TestLineEditInsert(t *testing.T) {
	srv, wk := newTestServer(t)

	dir := wk.NoteDir()
	path := filepath.Join(dir, "InsTest.md")
	content := "title: InsTest\n\n- first\n- second\n"
	os.WriteFile(path, []byte(content), 0644)
	td, _ := tid.Parse([]byte(content))
	wk.Store().PutWithPath(td, path)

	body := `{"line":1,"text":"- inserted","insert":true}`
	req := httptest.NewRequest("POST", "/test/line-edit/InsTest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	data, _ := os.ReadFile(path)
	lines := strings.Split(string(data), "\n")
	// After header (line 0: "title: InsTest", line 1: ""),
	// body: line 2: "- first", line 3: "- inserted", line 4: "- second"
	if len(lines) < 5 || lines[3] != "- inserted" {
		t.Errorf("expected '- inserted' at file line 4, got:\n%s", data)
	}
}

// TestLineEditBadLine verifies out-of-range line returns 400.
func TestLineEditBadLine(t *testing.T) {
	srv, wk := newTestServer(t)

	dir := wk.NoteDir()
	path := filepath.Join(dir, "Short.md")
	content := "title: Short\n\n- one\n"
	os.WriteFile(path, []byte(content), 0644)
	td, _ := tid.Parse([]byte(content))
	wk.Store().PutWithPath(td, path)

	body := `{"line":99,"text":"nope"}`
	req := httptest.NewRequest("POST", "/test/line-edit/Short", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// TestLineEditNoFile verifies missing file returns 404.
func TestLineEditNoFile(t *testing.T) {
	srv, _ := newTestServer(t)
	body := `{"line":1,"text":"x"}`
	req := httptest.NewRequest("POST", "/test/line-edit/NoSuch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// TestHandleThemeCSS verifies GET /theme.css serves the theme CSS file.
func TestHandleThemeCSS(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/theme.css", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", ct)
	}
	if !strings.Contains(w.Body.String(), "chroma") {
		t.Error("theme.css should contain 'chroma'")
	}
}

// TestHandleSearchEmptyQuery verifies GET /search with no query
// parameter returns an empty JSON array.
func TestHandleSearchEmptyQuery(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/search", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if body := strings.TrimSpace(w.Body.String()); body != "[]" {
		t.Errorf("body = %q, want %q", body, "[]")
	}
}

// TestHandleSearchNoNoteDir verifies search returns 500 when
// noteDir is not configured.
func TestHandleSearchNoNoteDir(t *testing.T) {
	wk := wiki.New("test", "", nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("GET", "/test/search?q=hello", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// TestHandleSearchWithResults verifies search returns matching notes
// when noteDir is set and files match the query.
func TestHandleSearchWithResults(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg (ripgrep) not on PATH, skipping search test")
	}

	srv, wk := newTestServer(t)
	dir := wk.NoteDir()

	// Create a .md file with searchable content.
	content := "title: SearchMe\ntags: Test\n\nUnique searchable content here.\n"
	path := filepath.Join(dir, "SearchMe.md")
	os.WriteFile(path, []byte(content), 0644)
	td, _ := tid.Parse([]byte(content))
	wk.Store().PutWithPath(td, path)

	req := httptest.NewRequest("GET", "/test/search?q=searchable", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var results []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one search result")
	}
	if results[0]["title"] != "SearchMe" {
		t.Errorf("title = %v, want %q", results[0]["title"], "SearchMe")
	}
}

// TestLineEditInvalidJSON verifies that a malformed JSON body
// returns 400.
func TestLineEditInvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/test/line-edit/Hello", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestLineEditMethodNotAllowed verifies that GET on /line-edit/
// returns 405.
func TestLineEditMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/line-edit/Hello", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// TestLineEditLineLessThanOne verifies that line=0 returns 400.
func TestLineEditLineLessThanOne(t *testing.T) {
	srv, _ := newTestServer(t)
	body := `{"line":0,"text":"bad"}`
	req := httptest.NewRequest("POST", "/test/line-edit/Hello", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// --- Coverage gap tests ---

// TestPutNoteRevisionConflict verifies that a PUT with a stale
// revision returns 409 Conflict.
func TestPutNoteRevisionConflict(t *testing.T) {
	srv, wk := newTestServer(t)

	// Create a note in the store — PutWithPath sets revision to 1.
	td := &tid.Note{Fields: map[string]string{
		"title": "Conflict",
		"text":  "Original.",
	}}
	path := filepath.Join(wk.NoteDir(), "Conflict.md")
	os.WriteFile(path, tid.Serialize(td), 0644)
	wk.Store().PutWithPath(td, path)

	// PUT again to bump revision to 2.
	body1 := `{"title":"Conflict","text":"Updated."}`
	req1 := httptest.NewRequest("PUT", "/test/notes/Conflict", strings.NewReader(body1))
	w1 := httptest.NewRecorder()
	srv.ServeHTTP(w1, req1)
	if w1.Code != http.StatusNoContent {
		t.Fatalf("first PUT status = %d, want %d", w1.Code, http.StatusNoContent)
	}

	// PUT with stale revision "1" — server revision is now > 1.
	body2 := `{"title":"Conflict","text":"Stale.","revision":"1"}`
	req2 := httptest.NewRequest("PUT", "/test/notes/Conflict", strings.NewReader(body2))
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Fatalf("stale PUT status = %d, want %d (409 Conflict)", w2.Code, http.StatusConflict)
	}
}

// TestPutNoteRevisionFieldSkipped verifies that a PUT with a
// matching revision still skips the "revision" key in tFields.
func TestPutNoteRevisionFieldSkipped(t *testing.T) {
	srv, wk := newTestServer(t)

	// Create note at revision 1.
	td := &tid.Note{Fields: map[string]string{
		"title": "RevSkip",
		"text":  "Original.",
	}}
	path := filepath.Join(wk.NoteDir(), "RevSkip.md")
	os.WriteFile(path, tid.Serialize(td), 0644)
	wk.Store().PutWithPath(td, path)

	// PUT with revision that matches current (1) — should succeed
	// and the "revision" key should NOT appear in the stored fields.
	body := `{"title":"RevSkip","text":"New.","revision":"1"}`
	req := httptest.NewRequest("PUT", "/test/notes/RevSkip", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	// The stored note should not have a "revision" field.
	got := wk.Store().Get("RevSkip")
	if got == nil {
		t.Fatal("note not found in store")
	}
	if _, ok := got.Fields["revision"]; ok {
		t.Error("revision field should not be stored in note fields")
	}
}

// TestPutNoteRevisionNonString verifies that a numeric revision
// value (not a string) is ignored — no conflict check occurs.
func TestPutNoteRevisionNonString(t *testing.T) {
	srv, wk := newTestServer(t)

	td := &tid.Note{Fields: map[string]string{
		"title": "RevNum",
		"text":  "Original.",
	}}
	path := filepath.Join(wk.NoteDir(), "RevNum.md")
	os.WriteFile(path, tid.Serialize(td), 0644)
	wk.Store().PutWithPath(td, path)

	// PUT with revision as a number (JSON parses as float64).
	body := `{"title":"RevNum","text":"New.","revision":999}`
	req := httptest.NewRequest("PUT", "/test/notes/RevNum", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (non-string revision should be ignored)", w.Code, http.StatusNoContent)
	}
}

// TestPutNoteReadBodyError verifies that a body read error
// returns 400.
func TestPutNoteReadBodyError(t *testing.T) {
	srv, _ := newTestServer(t)

	// errReader always returns an error on Read.
	req := httptest.NewRequest("PUT", "/test/notes/BadBody", &errReader{})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// errReader is an io.Reader that always returns an error.
type errReader struct{}

func (e *errReader) Read(p []byte) (int, error) {
	return 0, errors.New("simulated read error")
}

// TestLineEditMissingTitle verifies that POST /line-edit/ with no
// title returns 400.
func TestLineEditMissingTitle(t *testing.T) {
	srv, _ := newTestServer(t)
	body := `{"line":1,"text":"x"}`
	req := httptest.NewRequest("POST", "/test/line-edit/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), "missing note title") {
		t.Errorf("body = %q, want mention of 'missing note title'", w.Body.String())
	}
}

// TestLineEditReadFileError verifies that an os.ReadFile failure
// returns 500.
func TestLineEditReadFileError(t *testing.T) {
	patches := gomonkey.ApplyFunc(os.ReadFile, func(name string) ([]byte, error) {
		return nil, fmt.Errorf("disk error")
	})
	defer patches.Reset()

	srv, wk := newTestServer(t)
	// Note must be in store with a filePath so we get past the
	// "no file for note" check.
	wk.Store().PutWithPath(&tid.Note{Fields: map[string]string{
		"title": "ReadErr",
		"text":  "Body.",
	}}, "/fake/ReadErr.md")

	body := `{"line":1,"text":"x"}`
	req := httptest.NewRequest("POST", "/test/line-edit/ReadErr", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(w.Body.String(), "reading file") {
		t.Errorf("body = %q, want mention of 'reading file'", w.Body.String())
	}
}

// TestLineEditWriteFileError verifies that an os.WriteFile failure
// during line-edit returns 500.
func TestLineEditWriteFileError(t *testing.T) {
	srv, wk := newTestServer(t)

	dir := wk.NoteDir()
	path := filepath.Join(dir, "WrErr.md")
	content := "title: WrErr\n\n- alpha\n- beta\n"
	os.WriteFile(path, []byte(content), 0644)
	td, _ := tid.Parse([]byte(content))
	wk.Store().PutWithPath(td, path)

	// Mock os.WriteFile to fail AFTER os.ReadFile succeeds.
	patches := gomonkey.ApplyFunc(os.WriteFile, func(name string, data []byte, perm os.FileMode) error {
		return fmt.Errorf("disk full")
	})
	defer patches.Reset()

	body := `{"line":1,"text":"- ALPHA"}`
	req := httptest.NewRequest("POST", "/test/line-edit/WrErr", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(w.Body.String(), "writing file") {
		t.Errorf("body = %q, want mention of 'writing file'", w.Body.String())
	}
}

// TestLineEditRCSCheckinError verifies that an rcs.Checkin failure
// during line-edit returns 500.
func TestLineEditRCSCheckinError(t *testing.T) {
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(path string) error {
		return fmt.Errorf("rcs failed")
	})
	defer patches.Reset()

	srv, wk := newTestServer(t)

	dir := wk.NoteDir()
	path := filepath.Join(dir, "RcsErr.md")
	content := "title: RcsErr\n\n- alpha\n- beta\n"
	os.WriteFile(path, []byte(content), 0644)
	td, _ := tid.Parse([]byte(content))
	wk.Store().PutWithPath(td, path)

	body := `{"line":1,"text":"- ALPHA"}`
	req := httptest.NewRequest("POST", "/test/line-edit/RcsErr", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Should fail (500) due to rcs error.
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

// TestLineEditParseError verifies that a tid.Parse failure after
// writing returns 500.
func TestLineEditParseError(t *testing.T) {
	srv, wk := newTestServer(t)

	dir := wk.NoteDir()
	path := filepath.Join(dir, "ParseErr.md")
	content := "title: ParseErr\n\n- alpha\n- beta\n"
	os.WriteFile(path, []byte(content), 0644)
	td, _ := tid.Parse([]byte(content))
	wk.Store().PutWithPath(td, path)

	// Mock tid.Parse to fail (called after WriteFile succeeds).
	parseFail := func(data []byte) (*tid.Note, error) {
		return nil, fmt.Errorf("parse boom")
	}
	patches := gomonkey.ApplyGlobalVar(&wiki.ParseFunc, parseFail)
	defer patches.Reset()

	body := `{"line":1,"text":"- ALPHA"}`
	req := httptest.NewRequest("POST", "/test/line-edit/ParseErr", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(w.Body.String(), "parse error") {
		t.Errorf("body = %q, want mention of 'parse error'", w.Body.String())
	}
}

// TestHandleSearchError verifies that a search.Search error returns
// 500.
func TestHandleSearchError(t *testing.T) {
	patches := gomonkey.ApplyFunc(search.Search, func(dir, query string) ([]search.Result, error) {
		return nil, fmt.Errorf("rg exploded")
	})
	defer patches.Reset()

	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/search?q=hello", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(w.Body.String(), "search error") {
		t.Errorf("body = %q, want mention of 'search error'", w.Body.String())
	}
}

// TestHandleSearchNilLines verifies that a search hit with nil Lines
// is coalesced to an empty slice.
func TestHandleSearchNilLines(t *testing.T) {
	// Return a hit with nil Lines (e.g. a tag-only match where
	// all matched lines are header lines, filtered out by rg).
	patches := gomonkey.ApplyFunc(search.Search, func(dir, query string) ([]search.Result, error) {
		return []search.Result{
			{File: "Hello.md", Lines: nil},
		}, nil
	})
	defer patches.Reset()

	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/search?q=Test", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	// Parse response — matches should be [] not null.
	var results []struct {
		Title   string   `json:"title"`
		Matches []string `json:"matches"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Matches == nil {
		t.Error("matches should be [] not nil")
	}
	if len(results[0].Matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(results[0].Matches))
	}
}

// TestHandleSearchNilResults verifies that when search hits don't
// match any note in the store, results is coalesced to [].
func TestHandleSearchNilResults(t *testing.T) {
	// Return a hit for a file whose title isn't in the store.
	patches := gomonkey.ApplyFunc(search.Search, func(dir, query string) ([]search.Result, error) {
		return []search.Result{
			{File: "NoSuchNote.md", Lines: []string{"some line"}},
		}, nil
	})
	defer patches.Reset()

	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/search?q=missing", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	// Response should be [] (not null), since no results matched
	// notes in the store.
	body := strings.TrimSpace(w.Body.String())
	if body != "[]" {
		t.Errorf("body = %q, want %q", body, "[]")
	}
}

// TestWikiSelectorSingleRedirect verifies GET / with a single wiki
// redirects directly to that wiki.
func TestWikiSelectorSingleRedirect(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if loc != "/test/" {
		t.Errorf("redirect Location = %q, want /test/", loc)
	}
}

// TestWikiSelectorMultiWiki verifies GET / with multiple wikis
// returns the HTML wiki selector page.
func TestWikiSelectorMultiWiki(t *testing.T) {
	dir := t.TempDir()
	ed, _ := editor.New("echo %s")
	wk1 := wiki.New("alpha", dir, ed, 100)
	wk1.Start()
	t.Cleanup(wk1.Stop)
	wk2 := wiki.New("beta", dir, ed, 100)
	wk2.Start()
	t.Cleanup(wk2.Stop)

	hub := wiki.NewHub(map[string]*wiki.Wiki{"alpha": wk1, "beta": wk2})
	srv := New(hub, ed, new(atomic.Int64), 3000, 10, "", dir, "")

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "alpha") {
		t.Errorf("wiki selector should list 'alpha', got:\n%s", body)
	}
	if !strings.Contains(body, "beta") {
		t.Errorf("wiki selector should list 'beta', got:\n%s", body)
	}
}

// TestUnknownWiki verifies that an unknown wiki prefix returns 404.
func TestUnknownWiki(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/nosuch/notes/Hello", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestPathTraversalRejected verifies that titles containing ".."
// are rejected by extractTitle. Go's ServeMux also cleans ".." from
// URL paths (301 redirect), providing defense-in-depth.
func TestPathTraversalRejected(t *testing.T) {
	// extractTitle should return "" for traversal attempts.
	bad := []string{
		"../etc/passwd",
		"foo/../bar",
		"/etc/passwd",
		"foo/bar/..",
	}
	for _, title := range bad {
		got := extractTitle("/notes/"+title, "/notes/")
		if got != "" {
			t.Errorf("extractTitle with %q = %q, want empty", title, got)
		}
	}
	// Valid titles pass through.
	good := []string{"Hello", "My Note", "foo/bar"}
	for _, title := range good {
		got := extractTitle("/notes/"+title, "/notes/")
		if got != title {
			t.Errorf("extractTitle with %q = %q, want %q", title, got, title)
		}
	}
}

// TestRevisionTraversalRejected verifies that handleRevisions rejects
// titles with ".." components in all sub-paths.
func TestRevisionTraversalRejected(t *testing.T) {
	srv, wk := newTestServer(t)
	for _, suffix := range []string{"", "/checkout", "/promote"} {
		title := "foo/../bar"
		r := httptest.NewRequest("GET", "/revisions/"+title+suffix, nil)
		r = r.WithContext(context.WithValue(r.Context(), wikiContextKey, wk))
		w := httptest.NewRecorder()
		srv.handleRevisions(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("revisions/%s%s: status = %d, want 400", title, suffix, w.Code)
		}
	}
}

// TestSplitPrefix verifies the splitPrefix helper.
func TestSplitPrefix(t *testing.T) {
	tests := []struct {
		path, name, rest string
	}{
		{"/work/notes/Hello", "work", "/notes/Hello"},
		{"/work/", "work", "/"},
		{"/work", "work", "/"},
		{"/a/b/c", "a", "/b/c"},
	}
	for _, tt := range tests {
		name, rest := splitPrefix(tt.path)
		if name != tt.name || rest != tt.rest {
			t.Errorf("splitPrefix(%q) = (%q, %q), want (%q, %q)",
				tt.path, name, rest, tt.name, tt.rest)
		}
	}
}

// TestHandleNewDraft verifies that POST /test/new-draft returns 204.
func TestHandleNewDraft(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/test/new-draft", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

// TestHandleNewDraftMethodNotAllowed verifies that GET /test/new-draft
// returns 405.
func TestHandleNewDraftMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/new-draft", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// TestHandleNewDraftError verifies that new-draft returns 500 when
// the editor is not configured.
func TestHandleNewDraftError(t *testing.T) {
	wk := wiki.New("test", t.TempDir(), nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("POST", "/test/new-draft", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// TestHandleEditConfig verifies POST /edit-config launches the editor.
func TestHandleEditConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(configPath, []byte("listen: :8080\n"), 0644)
	ed, _ := editor.New("echo %s")

	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, ed, new(atomic.Int64), 3000, 10, "", "", configPath)

	req := httptest.NewRequest("POST", "/test/edit-config", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

// TestHandleEditConfigNoEditor verifies edit-config returns 500
// when no editor is configured.
func TestHandleEditConfigNoEditor(t *testing.T) {
	wk := wiki.New("test", t.TempDir(), nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "/some/config.yaml")

	req := httptest.NewRequest("POST", "/test/edit-config", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// TestHandleEditConfigNoPath verifies edit-config returns 404
// when no config path was provided.
func TestHandleEditConfigNoPath(t *testing.T) {
	ed, _ := editor.New("echo %s")
	wk := wiki.New("test", t.TempDir(), nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, ed, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("POST", "/test/edit-config", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestHandleEditConfigMethodNotAllowed verifies edit-config rejects
// non-POST methods.
func TestHandleEditConfigMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/edit-config", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// TestHandleEditConfigEditorError verifies edit-config returns 500
// when the editor fails to start.
func TestHandleEditConfigEditorError(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(configPath, []byte("listen: :8080\n"), 0644)

	ed, _ := editor.New("/nonexistent/editor %s")
	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, ed, new(atomic.Int64), 3000, 10, "", "", configPath)

	req := httptest.NewRequest("POST", "/test/edit-config", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// TestHandleRestart verifies POST /restart signals the restart channel.
func TestHandleRestart(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/test/restart", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	select {
	case <-srv.RestartCh():
		// Expected.
	default:
		t.Error("restart channel should have a signal")
	}
}

// TestHandleRestartMethodNotAllowed verifies restart rejects GET.
func TestHandleRestartMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/restart", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// TestHandleRestartIdempotent verifies double restart doesn't block.
func TestHandleRestartIdempotent(t *testing.T) {
	srv, _ := newTestServer(t)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/test/restart", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Errorf("call %d: status = %d, want %d", i, w.Code, http.StatusNoContent)
		}
	}
}

// TestServeAttachment verifies that files in note subdirectories
// are served as static attachments.
func TestServeAttachment(t *testing.T) {
	dir := t.TempDir()
	// Create a note subdir with a file.
	subdir := filepath.Join(dir, "Meeting")
	os.Mkdir(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "report.pdf"), []byte("%PDF-fake"), 0644)

	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("GET", "/test/Meeting/report.pdf", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "%PDF-fake" {
		t.Errorf("body = %q, want %%PDF-fake", w.Body.String())
	}
}

// TestServeAttachmentMdInSubdir verifies that .md files inside
// subdirectories are rendered through goldmark and served as HTML.
func TestServeAttachmentMdInSubdir(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "Notes")
	os.Mkdir(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "readme.md"), []byte("# Hello"), 0644)

	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("GET", "/test/Notes/readme.md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<h1>Hello</h1>") {
		t.Errorf("expected rendered <h1>, got: %s", body)
	}
	if !strings.Contains(body, "theme.css") {
		t.Errorf("expected theme.css link, got: %s", body)
	}
}

// TestServeAttachmentMdEscapesHTML verifies that raw HTML in .md
// attachments is escaped (safe rendering for untrusted content).
func TestServeAttachmentMdEscapesHTML(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "Notes")
	os.Mkdir(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "evil.md"),
		[]byte("hello <script>alert('xss')</script>"), 0644)

	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("GET", "/test/Notes/evil.md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if strings.Contains(w.Body.String(), "<script>") {
		t.Errorf("raw HTML should be escaped in attachment")
	}
}

// TestServeAttachmentMdReadError verifies 404 when an .md attachment
// exists but cannot be read (e.g. permission denied).
func TestServeAttachmentMdReadError(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "Notes")
	os.Mkdir(subdir, 0755)
	fp := filepath.Join(subdir, "unreadable.md")
	os.WriteFile(fp, []byte("# secret"), 0644)
	os.Chmod(fp, 0000)
	t.Cleanup(func() { os.Chmod(fp, 0644) })

	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("GET", "/test/Notes/unreadable.md", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestServeAttachmentNoSubdir verifies that top-level files
// (without a subdir) are not served as attachments.
func TestServeAttachmentNoSubdir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "loose.pdf"), []byte("data"), 0644)

	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("GET", "/test/loose.pdf", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestServeAttachmentTraversal verifies that path traversal
// attempts are blocked (mux normalizes and redirects).
func TestServeAttachmentTraversal(t *testing.T) {
	dir := t.TempDir()
	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("GET", "/test/../../../etc/passwd", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// The Go mux cleans /../ paths. The result is a redirect or
	// 404, never the requested file content.
	if w.Code == http.StatusOK {
		t.Error("traversal should not return 200")
	}
}

// TestServeAttachmentMissing verifies 404 for a non-existent
// file in a valid subdir path.
func TestServeAttachmentMissing(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "Meeting"), 0755)

	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("GET", "/test/Meeting/nofile.pdf", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestServeAttachmentEscapeViaSymlink verifies that a symlinked subdir
// pointing outside the note directory is blocked by EvalSymlinks.
func TestServeAttachmentEscapeViaSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("top secret"), 0644)

	// Create a symlinked subdir that escapes the note directory.
	os.Symlink(outside, filepath.Join(dir, "escape"))

	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("GET", "/test/escape/secret.txt", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// EvalSymlinks resolves the real path, which is outside noteDir.
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d for symlink escape", w.Code, http.StatusNotFound)
	}
}

// TestServeAttachmentPathEscape verifies that a request resolving
// outside the note directory returns 404. We create a symlink inside
// the note dir that points to a file outside it; after EvalSymlinks
// the resolved path won't have the noteDir prefix.
func TestServeAttachmentPathEscape(t *testing.T) {
	noteDir := t.TempDir()
	outsideDir := t.TempDir()
	os.WriteFile(filepath.Join(outsideDir, "secret.txt"), []byte("private"), 0644)

	// Symlink: noteDir/escape/secret.txt → outsideDir/secret.txt
	escapeDir := filepath.Join(noteDir, "escape")
	os.Symlink(outsideDir, escapeDir)

	wk := wiki.New("test", noteDir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("GET", "/test/escape/secret.txt", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d for path escape", w.Code, http.StatusNotFound)
	}
}

// TestServeAttachmentDirIndex verifies that requesting a directory
// with an index.html serves the index.html content.
func TestServeAttachmentDirIndex(t *testing.T) {
	dir := t.TempDir()
	pg := filepath.Join(dir, "playground")
	os.Mkdir(pg, 0755)
	os.WriteFile(filepath.Join(pg, "index.html"), []byte("<html>playground</html>"), 0644)

	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	// Directory path with trailing slash serves index.html.
	req := httptest.NewRequest("GET", "/test/playground/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "<html>playground</html>" {
		t.Errorf("body = %q", w.Body.String())
	}
}

// TestServeAttachmentDirRedirect verifies that a directory without
// trailing slash redirects to add one (so relative URLs resolve correctly).
func TestServeAttachmentDirRedirect(t *testing.T) {
	dir := t.TempDir()
	pg := filepath.Join(dir, "playground")
	os.Mkdir(pg, 0755)
	os.WriteFile(filepath.Join(pg, "index.html"), []byte("<html>pg</html>"), 0644)

	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("GET", "/test/playground", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusMovedPermanently)
	}
	loc := w.Header().Get("Location")
	if loc != "playground/" {
		t.Errorf("Location = %q, want %q", loc, "playground/")
	}
}

// TestServeAttachmentDirNoIndex verifies that a directory without
// index.html returns 404.
func TestServeAttachmentDirNoIndex(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "empty"), 0755)

	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("GET", "/test/empty/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestServeAttachmentIndexHtmlDirect verifies that requesting
// a subdir/index.html directly works (no redirect).
func TestServeAttachmentIndexHtmlDirect(t *testing.T) {
	dir := t.TempDir()
	pg := filepath.Join(dir, "playground")
	os.Mkdir(pg, 0755)
	os.WriteFile(filepath.Join(pg, "index.html"), []byte("<html>direct</html>"), 0644)

	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	req := httptest.NewRequest("GET", "/test/playground/index.html", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (should not redirect)", w.Code, http.StatusOK)
	}
	if w.Body.String() != "<html>direct</html>" {
		t.Errorf("body = %q", w.Body.String())
	}
}

// TestServeAttachmentOpenError covers the os.Open failure path
// (file exists per Stat but becomes unreadable before Open).
func TestServeAttachmentOpenError(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0755)
	fp := filepath.Join(sub, "file.txt")
	os.WriteFile(fp, []byte("data"), 0644)

	wk := wiki.New("test", dir, nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, nil, new(atomic.Int64), 3000, 10, "", "", "")

	// Make the file unreadable after Stat can still see it.
	os.Chmod(fp, 0000)
	t.Cleanup(func() { os.Chmod(fp, 0644) })

	req := httptest.NewRequest("GET", "/test/sub/file.txt", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// Keep unused imports from erroring.
var _ = io.Discard
var _ = (*store.Store)(nil)

// --- Revision endpoint tests ---

// writeNoteWithRCS creates a note with multiple RCS checkins for
// revision endpoint testing. Returns the file path.
func writeNoteWithRCS(t *testing.T, wk *wiki.Wiki, title string, bodies []string) string {
	t.Helper()
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
	data, _ := os.ReadFile(path)
	td, _ := tid.Parse(data)
	wk.Store().PutWithPath(td, path)
	wk.SeedBody(title, td.Text())
	return path
}

// TestGetRevisions verifies GET /revisions/{title} returns revision
// history with diffs.
func TestGetRevisions(t *testing.T) {
	srv, wk := newTestServer(t)
	writeNoteWithRCS(t, wk, "Rev", []string{"V1.", "V2.", "V3."})

	req := httptest.NewRequest("GET", "/test/revisions/Rev", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result wiki.RevisionsResult
	json.NewDecoder(w.Body).Decode(&result)
	if len(result.Revisions) != 3 {
		t.Errorf("revisions count = %d, want 3", len(result.Revisions))
	}
	if !result.IsHead {
		t.Error("should be at head")
	}
	if !strings.Contains(result.DiffHTML, "1.1 → 1.2") {
		t.Error("missing diff header")
	}
}

// TestGetRevisionsNotFound verifies GET /revisions/{title} returns
// 404 for a note not in the store.
func TestGetRevisionsNotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/test/revisions/Ghost", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// TestGetRevisionsMissingTitle verifies GET /revisions/ with no
// title returns 400.
func TestGetRevisionsMissingTitle(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/test/revisions/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestGetRevisionsMethodNotAllowed verifies POST /revisions/{title}
// (without /checkout suffix) returns 405.
func TestGetRevisionsMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	writeNoteWithRCS(t, srv.hub.Wiki("test"), "Rev", []string{"V1."})

	req := httptest.NewRequest("POST", "/test/revisions/Rev", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// TestCheckoutRevision verifies POST /revisions/{title}/checkout
// checks out an old revision.
func TestCheckoutRevision(t *testing.T) {
	srv, wk := newTestServer(t)
	writeNoteWithRCS(t, wk, "Pin", []string{"V1.", "V2.", "V3."})

	req := httptest.NewRequest("POST", "/test/revisions/Pin/checkout?rev=1.1", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	// File should be read-only.
	fp := filepath.Join(wk.NoteDir(), "Pin.md")
	fi, _ := os.Stat(fp)
	if fi.Mode().Perm()&0200 != 0 {
		t.Error("file should be read-only after checkout")
	}
}

// TestCheckoutRevisionRestoreHead verifies POST without rev param
// restores head.
func TestCheckoutRevisionRestoreHead(t *testing.T) {
	srv, wk := newTestServer(t)
	fp := writeNoteWithRCS(t, wk, "Restore", []string{"V1.", "V2."})

	// Checkout old rev first.
	rcs.Checkout(fp, "1.1")

	req := httptest.NewRequest("POST", "/test/revisions/Restore/checkout", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	fi, _ := os.Stat(fp)
	if fi.Mode().Perm()&0200 == 0 {
		t.Error("file should be writable after head restore")
	}
}

// TestCheckoutRevisionNotFound verifies checkout returns 404 for
// a missing note.
func TestCheckoutRevisionNotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("POST", "/test/revisions/Ghost/checkout?rev=1.1", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// TestCheckoutRevisionMethodNotAllowed verifies GET on /checkout
// returns 405.
func TestCheckoutRevisionMethodNotAllowed(t *testing.T) {
	srv, wk := newTestServer(t)
	writeNoteWithRCS(t, wk, "Pin", []string{"V1."})

	req := httptest.NewRequest("GET", "/test/revisions/Pin/checkout", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// TestCheckoutRevisionBadRev verifies that an invalid rev parameter
// returns 400 Bad Request.
func TestCheckoutRevisionBadRev(t *testing.T) {
	srv, wk := newTestServer(t)
	writeNoteWithRCS(t, wk, "Pin", []string{"V1."})

	req := httptest.NewRequest("POST",
		"/test/revisions/Pin/checkout?rev=INJECTED", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestPromoteRevision verifies POST /revisions/{title}/promote
// checks in old content as new head and returns 204.
func TestPromoteRevision(t *testing.T) {
	srv, wk := newTestServer(t)
	writeNoteWithRCS(t, wk, "Pro", []string{"V1.", "V2.", "V3."})

	// Checkout old revision first.
	req := httptest.NewRequest("POST", "/test/revisions/Pro/checkout?rev=1.1", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("checkout status = %d, want 204", w.Code)
	}

	// Promote.
	req = httptest.NewRequest("POST", "/test/revisions/Pro/promote", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("promote status = %d, want 204", w.Code)
	}
}

// TestPromoteAtHeadReturnsConflict verifies promote on a note
// already at head returns 409.
func TestPromoteAtHeadReturnsConflict(t *testing.T) {
	srv, wk := newTestServer(t)
	writeNoteWithRCS(t, wk, "Head", []string{"V1."})

	req := httptest.NewRequest("POST", "/test/revisions/Head/promote", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

// TestPromoteMethodNotAllowed verifies GET on /promote returns 405.
func TestPromoteMethodNotAllowed(t *testing.T) {
	srv, wk := newTestServer(t)
	writeNoteWithRCS(t, wk, "PMeth", []string{"V1."})

	req := httptest.NewRequest("GET", "/test/revisions/PMeth/promote", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// TestPromoteNonexistentReturns404 verifies that POSTing promote for
// a note title that has no file returns 404 (lines 511-513).
func TestPromoteNonexistentReturns404(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("POST", "/test/revisions/NoSuchNote/promote", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- End-to-end RCS happy-path tests ---
//
// These tests exercise the full HTTP → wiki → RCS library path,
// verifying that the pure-Go RCS implementation produces correct
// results through every endpoint. Each test uses real RCS archives
// (no mocking) and verifies observable behavior through the API.

// putJSON is a helper that PUTs a JSON note body and returns the recorder.
func putJSON(t *testing.T, srv http.Handler, urlPath, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("PUT", urlPath, strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

// getJSON is a helper that GETs a JSON endpoint and decodes it.
func getJSON(t *testing.T, srv http.Handler, urlPath string, v interface{}) int {
	t.Helper()
	req := httptest.NewRequest("GET", urlPath, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		json.NewDecoder(w.Body).Decode(v)
	}
	return w.Code
}

// postEmpty is a helper that POSTs with no body and returns the recorder.
func postEmpty(t *testing.T, srv http.Handler, urlPath string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", urlPath, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

// TestE2EPutCreatesRCSArchive verifies that PUT /notes/{title} for
// a new note creates an RCS archive on disk.
func TestE2EPutCreatesRCSArchive(t *testing.T) {
	srv, wk := newTestServer(t)

	w := putJSON(t, srv, "/test/notes/Fresh",
		`{"title":"Fresh","text":"First save."}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want 204", w.Code)
	}

	// RCS archive should exist.
	fp := filepath.Join(wk.NoteDir(), "Fresh.md")
	if !rcs.HasArchive(fp) {
		t.Fatal("expected RCS archive after PUT")
	}
	head, err := rcs.HeadRev(fp)
	if err != nil {
		t.Fatalf("HeadRev: %v", err)
	}
	if head != "1.1" {
		t.Errorf("head = %s, want 1.1", head)
	}
}

// TestE2EPutEditsCreateRevisions verifies that successive PUTs
// create new RCS revisions visible through GET /revisions.
func TestE2EPutEditsCreateRevisions(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create note.
	w := putJSON(t, srv, "/test/notes/Multi",
		`{"title":"Multi","text":"V1."}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("PUT 1 status = %d", w.Code)
	}

	// Edit twice.
	w = putJSON(t, srv, "/test/notes/Multi",
		`{"title":"Multi","text":"V2."}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("PUT 2 status = %d", w.Code)
	}
	w = putJSON(t, srv, "/test/notes/Multi",
		`{"title":"Multi","text":"V3."}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("PUT 3 status = %d", w.Code)
	}

	// GET /revisions should show 3 revisions.
	var result wiki.RevisionsResult
	code := getJSON(t, srv, "/test/revisions/Multi", &result)
	if code != http.StatusOK {
		t.Fatalf("GET /revisions status = %d", code)
	}
	if len(result.Revisions) != 3 {
		t.Errorf("revisions = %d, want 3", len(result.Revisions))
	}
	if !result.IsHead {
		t.Error("should be at head")
	}
}

// TestE2ERevisionLifecycle exercises the full cycle:
// PUT → PUT → GET /revisions → checkout → GET /notes (readOnly) →
// GET /revisions (isHead=false) → restore head → GET /notes (writable).
// Reload is called after checkout/restore to simulate the watcher
// (in production, fsnotify detects the disk change and triggers it).
func TestE2ERevisionLifecycle(t *testing.T) {
	srv, wk := newTestServer(t)

	// Create two versions.
	putJSON(t, srv, "/test/notes/Life",
		`{"title":"Life","text":"Original."}`)
	putJSON(t, srv, "/test/notes/Life",
		`{"title":"Life","text":"Updated."}`)

	fp := filepath.Join(wk.NoteDir(), "Life.md")

	// Verify revisions at head.
	var revResult wiki.RevisionsResult
	getJSON(t, srv, "/test/revisions/Life", &revResult)
	if len(revResult.Revisions) != 2 {
		t.Fatalf("revisions = %d, want 2", len(revResult.Revisions))
	}
	if !revResult.IsHead {
		t.Fatal("should be at head before checkout")
	}

	// Checkout old revision 1.1.
	w := postEmpty(t, srv, "/test/revisions/Life/checkout?rev=1.1")
	if w.Code != http.StatusNoContent {
		t.Fatalf("checkout status = %d, want 204", w.Code)
	}

	// Simulate watcher: reload the file from disk.
	wk.Reload(fp)
	wk.Do(func() {})

	// GET /notes should show readOnly and rcsRev/rcsHead.
	var fields map[string]string
	getJSON(t, srv, "/test/notes/Life", &fields)
	if fields["readOnly"] != "true" {
		t.Errorf("readOnly = %q, want true", fields["readOnly"])
	}
	if fields["rcsRev"] != "1.1" {
		t.Errorf("rcsRev = %q, want 1.1", fields["rcsRev"])
	}
	if fields["rcsHead"] != "1.2" {
		t.Errorf("rcsHead = %q, want 1.2", fields["rcsHead"])
	}
	// Content should be old version after watcher reload.
	if !strings.Contains(fields["text"], "Original.") {
		t.Errorf("text = %q, want old content", fields["text"])
	}

	// GET /revisions should show isHead=false.
	var revResult2 wiki.RevisionsResult
	getJSON(t, srv, "/test/revisions/Life", &revResult2)
	if revResult2.IsHead {
		t.Error("should NOT be at head after checkout")
	}

	// Restore head.
	w = postEmpty(t, srv, "/test/revisions/Life/checkout")
	if w.Code != http.StatusNoContent {
		t.Fatalf("restore status = %d, want 204", w.Code)
	}

	// Simulate watcher reload.
	wk.Reload(fp)
	wk.Do(func() {})

	// GET /notes should no longer be readOnly.
	var fields2 map[string]string
	getJSON(t, srv, "/test/notes/Life", &fields2)
	if fields2["readOnly"] != "" {
		t.Errorf("readOnly should be empty after restore, got %q", fields2["readOnly"])
	}
	if !strings.Contains(fields2["text"], "Updated.") {
		t.Errorf("text = %q, want head content", fields2["text"])
	}
}

// TestE2EPromoteLifecycle exercises the promote flow:
// create 3 versions → checkout 1.1 → promote → verify new head
// is 1.4 with old content → note is writable.
// Reload is called after checkout/promote to simulate the watcher.
func TestE2EPromoteLifecycle(t *testing.T) {
	srv, wk := newTestServer(t)

	// Create three versions.
	putJSON(t, srv, "/test/notes/Promo",
		`{"title":"Promo","text":"V1."}`)
	putJSON(t, srv, "/test/notes/Promo",
		`{"title":"Promo","text":"V2."}`)
	putJSON(t, srv, "/test/notes/Promo",
		`{"title":"Promo","text":"V3."}`)

	fp := filepath.Join(wk.NoteDir(), "Promo.md")

	// Checkout old revision 1.1.
	w := postEmpty(t, srv, "/test/revisions/Promo/checkout?rev=1.1")
	if w.Code != http.StatusNoContent {
		t.Fatalf("checkout status = %d", w.Code)
	}

	// Promote: old content becomes new head.
	w = postEmpty(t, srv, "/test/revisions/Promo/promote")
	if w.Code != http.StatusNoContent {
		t.Fatalf("promote status = %d, want 204", w.Code)
	}

	// Simulate watcher reload.
	wk.Reload(fp)
	wk.Do(func() {})

	// Revisions should now show 4: 3 PUTs + promote.
	var result wiki.RevisionsResult
	getJSON(t, srv, "/test/revisions/Promo", &result)
	if len(result.Revisions) != 4 {
		t.Errorf("revisions = %d, want 4", len(result.Revisions))
	}
	if !result.IsHead {
		t.Error("should be at head after promote")
	}

	// RCS head on disk should be 1.4.
	head, err := rcs.HeadRev(fp)
	if err != nil {
		t.Fatalf("HeadRev: %v", err)
	}
	if head != "1.4" {
		t.Errorf("head = %s, want 1.4", head)
	}

	// CHECKOUT tag should be gone.
	tag, _ := rcs.CheckoutTag(fp, "CHECKOUT")
	if tag != "" {
		t.Errorf("CHECKOUT tag = %q, want empty", tag)
	}

	// GET /notes should be writable and contain old content.
	var fields map[string]string
	getJSON(t, srv, "/test/notes/Promo", &fields)
	if fields["readOnly"] != "" {
		t.Errorf("readOnly = %q, want empty after promote", fields["readOnly"])
	}
	if !strings.Contains(fields["text"], "V1.") {
		t.Errorf("text = %q, should contain promoted V1 content", fields["text"])
	}
}

// TestE2ELineEditCreatesRevision verifies that POST /line-edit
// creates a new RCS revision.
func TestE2ELineEditCreatesRevision(t *testing.T) {
	srv, wk := newTestServer(t)

	// Create a note with list content on disk.
	dir := wk.NoteDir()
	path := filepath.Join(dir, "EditRev.md")
	content := "title: EditRev\n\n- alpha\n- beta\n"
	os.WriteFile(path, []byte(content), 0644)
	td, _ := tid.Parse([]byte(content))
	wk.Store().PutWithPath(td, path)
	wk.SeedBody("EditRev", "- alpha\n- beta\n")
	rcs.Checkin(path)

	// Head should be 1.1 after initial checkin.
	head1, _ := rcs.HeadRev(path)
	if head1 != "1.1" {
		t.Fatalf("initial head = %s, want 1.1", head1)
	}

	// Line edit: replace line 2.
	body := `{"line":2,"text":"- BETA"}`
	req := httptest.NewRequest("POST", "/test/line-edit/EditRev",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("line-edit status = %d, want 200; body: %s",
			w.Code, w.Body.String())
	}

	// Head should now be 1.2.
	head2, _ := rcs.HeadRev(path)
	if head2 != "1.2" {
		t.Errorf("head after line-edit = %s, want 1.2", head2)
	}
}

// TestE2EDiffContentVerification verifies that the unified diff
// produced by the pure-Go implementation contains correct +/- lines
// when retrieved through GET /revisions.
func TestE2EDiffContentVerification(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create two distinct versions with known body text.
	putJSON(t, srv, "/test/notes/DiffCheck",
		`{"title":"DiffCheck","text":"line one\nline two\nline three"}`)
	putJSON(t, srv, "/test/notes/DiffCheck",
		`{"title":"DiffCheck","text":"line one\nline TWO\nline three"}`)

	var result wiki.RevisionsResult
	code := getJSON(t, srv, "/test/revisions/DiffCheck", &result)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}

	// Diff HTML should contain the changed line.
	if !strings.Contains(result.DiffHTML, "line TWO") {
		t.Error("diff should contain 'line TWO'")
	}
	if !strings.Contains(result.DiffHTML, "line two") {
		t.Error("diff should reference old 'line two'")
	}
	// Should have the diff header.
	if !strings.Contains(result.DiffHTML, "1.1 → 1.2") {
		t.Error("diff should have '1.1 → 1.2' header")
	}
}

// TestE2ERevisionsAfterMultiplePutsShowDiffs verifies that multiple
// PUT edits produce diffs between consecutive pairs.
func TestE2ERevisionsAfterMultiplePutsShowDiffs(t *testing.T) {
	srv, _ := newTestServer(t)

	putJSON(t, srv, "/test/notes/Diffs",
		`{"title":"Diffs","text":"AAA"}`)
	putJSON(t, srv, "/test/notes/Diffs",
		`{"title":"Diffs","text":"BBB"}`)
	putJSON(t, srv, "/test/notes/Diffs",
		`{"title":"Diffs","text":"CCC"}`)

	var result wiki.RevisionsResult
	getJSON(t, srv, "/test/revisions/Diffs", &result)

	// Should have diffs for both pairs.
	if !strings.Contains(result.DiffHTML, "1.1 → 1.2") {
		t.Error("missing 1.1 → 1.2 diff")
	}
	if !strings.Contains(result.DiffHTML, "1.2 → 1.3") {
		t.Error("missing 1.2 → 1.3 diff")
	}
	// Each diff should show the change.
	if !strings.Contains(result.DiffHTML, "BBB") {
		t.Error("diff should contain 'BBB'")
	}
}

// TestE2ECheckoutRestoreRoundTrip verifies that checkout+restore
// leaves the note in the exact same state as before (no extra
// revisions created, no stale RCS state).
func TestE2ECheckoutRestoreRoundTrip(t *testing.T) {
	srv, wk := newTestServer(t)

	putJSON(t, srv, "/test/notes/Round",
		`{"title":"Round","text":"V1."}`)
	putJSON(t, srv, "/test/notes/Round",
		`{"title":"Round","text":"V2."}`)

	fp := filepath.Join(wk.NoteDir(), "Round.md")
	headBefore, _ := rcs.HeadRev(fp)

	// Checkout old, then restore.
	postEmpty(t, srv, "/test/revisions/Round/checkout?rev=1.1")
	postEmpty(t, srv, "/test/revisions/Round/checkout")

	// Head should be unchanged.
	headAfter, _ := rcs.HeadRev(fp)
	if headAfter != headBefore {
		t.Errorf("head changed: %s → %s", headBefore, headAfter)
	}

	// No CHECKOUT tag should remain.
	tag, _ := rcs.CheckoutTag(fp, "CHECKOUT")
	if tag != "" {
		t.Errorf("CHECKOUT tag = %q, want empty after restore", tag)
	}

	// Note should not be readOnly.
	var fields map[string]string
	getJSON(t, srv, "/test/notes/Round", &fields)
	if fields["readOnly"] != "" {
		t.Errorf("readOnly = %q after restore", fields["readOnly"])
	}
}

// TestWikiErrorHeader verifies Put returns 500 on write failure.
func TestWikiErrorHeader(t *testing.T) {
	srv, wk := newTestServer(t)

	// Make the note directory read-only so Put fails to write.
	dir := wk.NoteDir()
	os.Chmod(dir, 0555)
	t.Cleanup(func() { os.Chmod(dir, 0755) })

	// PUT a note — the write should fail with 500.
	body := `{"title":"Fail","text":"body"}`
	req := httptest.NewRequest("PUT", "/test/notes/Fail", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d for write failure", w.Code, http.StatusInternalServerError)
	}
}

// TestXWikiErrorHeader verifies the X-Wiki-Error header is delivered
// on the request after a setErr-producing operation, then cleared.
func TestXWikiErrorHeader(t *testing.T) {
	patches := gomonkey.ApplyFunc(os.Remove, func(string) error {
		return fmt.Errorf("permission denied")
	})
	defer patches.Reset()

	srv, wk := newTestServer(t)
	wk.Store().PutWithPath(&tid.Note{Fields: map[string]string{
		"title": "ErrNote",
		"text":  "Body.",
	}}, "/fake/ErrNote.md")

	// Delete triggers setErr (remove fails but note deleted from store).
	req := httptest.NewRequest("DELETE", "/test/notes/ErrNote", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Next request should carry the error header.
	req2 := httptest.NewRequest("GET", "/test/status", nil)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Header().Get("X-Wiki-Error") == "" {
		t.Error("expected X-Wiki-Error header after delete failure")
	}

	// Third request: header should be cleared.
	req3 := httptest.NewRequest("GET", "/test/status", nil)
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, req3)
	if w3.Header().Get("X-Wiki-Error") != "" {
		t.Error("expected X-Wiki-Error cleared after delivery")
	}
}

// TestServeAttachmentEvalSymlinksNoteDirError verifies that an
// EvalSymlinks failure on the note directory returns 404.
func TestServeAttachmentEvalSymlinksNoteDirError(t *testing.T) {
	srv, wk := newTestServer(t)

	dir := wk.NoteDir()
	noteFile := filepath.Join(dir, "Att.md")
	os.WriteFile(noteFile, []byte("title: Att\n\ntext"), 0644)
	attDir := filepath.Join(dir, "Att")
	os.MkdirAll(attDir, 0755)
	os.WriteFile(filepath.Join(attDir, "pic.png"), []byte("PNG"), 0644)
	td, _ := tid.Parse([]byte("title: Att\n\ntext"))
	wk.Store().PutWithPath(td, noteFile)

	// Patch EvalSymlinks: first call succeeds, second fails.
	patches := gomonkey.ApplyFuncSeq(filepath.EvalSymlinks, []gomonkey.OutputCell{
		{Values: gomonkey.Params{filepath.Join(dir, "Att", "pic.png"), nil}},
		{Values: gomonkey.Params{"", fmt.Errorf("eval error")}},
	})
	defer patches.Reset()

	req := httptest.NewRequest("GET", "/test/attachments/Att/pic.png", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for EvalSymlinks noteDir error", w.Code)
	}
}

// TestServeAttachmentStatError verifies that an os.Stat failure on
// the attachment path returns 404.
func TestServeAttachmentStatError(t *testing.T) {
	srv, wk := newTestServer(t)

	dir := wk.NoteDir()
	noteFile := filepath.Join(dir, "StatErr.md")
	os.WriteFile(noteFile, []byte("title: StatErr\n\ntext"), 0644)
	attDir := filepath.Join(dir, "StatErr")
	os.MkdirAll(attDir, 0755)
	attFile := filepath.Join(attDir, "file.txt")
	os.WriteFile(attFile, []byte("data"), 0644)
	td, _ := tid.Parse([]byte("title: StatErr\n\ntext"))
	wk.Store().PutWithPath(td, noteFile)

	// Both EvalSymlinks calls succeed, then Stat fails.
	resolved := filepath.Join(dir, "StatErr", "file.txt")
	patches := gomonkey.ApplyFuncSeq(filepath.EvalSymlinks, []gomonkey.OutputCell{
		{Values: gomonkey.Params{resolved, nil}},
		{Values: gomonkey.Params{dir, nil}},
	})
	defer patches.Reset()
	patches.ApplyFunc(os.Stat, func(string) (os.FileInfo, error) {
		return nil, fmt.Errorf("stat error")
	})

	req := httptest.NewRequest("GET", "/test/attachments/StatErr/file.txt", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for Stat error", w.Code)
	}
}
