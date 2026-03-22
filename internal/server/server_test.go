package server

import (
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
	ed := editor.New("echo %s")
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

	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, f.Name())
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
	if body["username"] != "DG" {
		t.Errorf("username = %v, want %q", body["username"], "DG")
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

// TestHandleIndexNotConfigured verifies that GET / returns 500 if
// no index.html is configured.
func TestHandleIndexNotConfigured(t *testing.T) {
	wk := wiki.New("test", t.TempDir(), nil, 100)
	wk.Start()
	t.Cleanup(wk.Stop)
	hub := wiki.NewHub(map[string]*wiki.Wiki{"test": wk})
	srv := New(hub, "")
	req := httptest.NewRequest("GET", "/test/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
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

// TestHandleFaviconNotFound verifies favicon returns 404 when no
// $:/favicon.ico note exists.
func TestHandleFaviconNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/favicon.ico", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestHandleFaviconExists verifies favicon is served from the
// $:/favicon.ico note.
func TestHandleFaviconExists(t *testing.T) {
	srv, wk := newTestServer(t)
	wk.Put("$:/favicon.ico", map[string]string{
		"title": "$:/favicon.ico",
		"text":  "fake-icon-data",
	}, "")

	req := httptest.NewRequest("GET", "/test/favicon.ico", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "fake-icon-data" {
		t.Errorf("body = %q, want %q", w.Body.String(), "fake-icon-data")
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
	srv := New(hub, "")

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
	srv := New(hub, "")

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
	ed := editor.New("/nonexistent/editor %s")
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
	srv := New(hub, "")

	req := httptest.NewRequest("POST", "/test/edit-external/FailEdit", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// --- gomonkey-based tests for error-logging branches ---

// TestPutNoteWriteFileError verifies that an os.WriteFile error is
// logged but does not prevent the note from being stored (204).
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

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	// Note should still be stored (PutWithPath is called after the error log).
	if got := wk.Store().Get("WriteErr"); got == nil {
		t.Fatal("expected note in store despite WriteFile error")
	}
}

// TestPutNoteRCSCheckinError verifies that an rcs.Checkin error
// is logged but does not prevent a 204 response.
func TestPutNoteRCSCheckinError(t *testing.T) {
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(path string) error {
		return fmt.Errorf("rcs failed")
	})
	defer patches.Reset()

	srv, wk := newTestServer(t)

	body := `{"title":"RCSErr","text":"Body."}`
	req := httptest.NewRequest("PUT", "/test/notes/RCSErr",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := wk.Store().Get("RCSErr"); got == nil {
		t.Fatal("expected note in store despite rcs.Checkin error")
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

// TestHandleThemeCSS verifies GET /theme.css returns CSS containing
// chroma syntax-highlighting styles.
func TestHandleThemeCSS(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/test/theme.css", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/css" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/css")
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
	srv := New(hub, "")

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
// during line-edit is logged but doesn't prevent completion.
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

	// Should succeed (200) despite rcs error — the error is just logged.
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
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
	patches := gomonkey.ApplyFunc(tid.Parse, func(data []byte) (*tid.Note, error) {
		return nil, fmt.Errorf("parse boom")
	})
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

// TestWikiSelector verifies GET / returns HTML listing wiki names.
func TestWikiSelector(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "test") {
		t.Errorf("wiki selector should list 'test', got:\n%s", body)
	}
	if !strings.Contains(body, `href="/test/"`) {
		t.Errorf("wiki selector should link to /test/, got:\n%s", body)
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

// Keep unused imports from erroring.
var _ = io.Discard
var _ = (*store.Store)(nil)
