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
	"strings"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/dan4thewin/tibbitwiki/internal/editor"
	"github.com/dan4thewin/tibbitwiki/internal/rcs"
	"github.com/dan4thewin/tibbitwiki/internal/search"
	"github.com/dan4thewin/tibbitwiki/internal/store"
	"github.com/dan4thewin/tibbitwiki/internal/tid"
)

// newTestServer creates a Server with a pre-populated store and a
// temp index.html file for testing.
func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	s := store.New()

	// Add a test note.
	td := &tid.Note{Fields: map[string]string{
		"title":   "Hello",
		"tags":    "Test",
		"text":    "Hello world.",
		"created": "20230101000000000",
	}}
	s.PutWithPath(td, "/fake/Hello.tid")

	// Create a temp index.html.
	f, err := os.CreateTemp("", "index-*.html")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	f.WriteString("<html><body>TibbitWiki</body></html>")
	f.Close()

	ed := editor.New("echo %s")
	srv := New(s, f.Name(), ed)
	return srv, s
}

// TestHandleStatus verifies the /status endpoint returns expected
// JSON fields.
func TestHandleStatus(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/status", nil)
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
	req := httptest.NewRequest("GET", "/", nil)
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
	s := store.New()
	srv := New(s, "", nil)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// TestHandleIndexNotFound verifies that non-root paths return 404.
func TestHandleIndexNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/nonexistent", nil)
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
	req := httptest.NewRequest("GET", "/notes", nil)
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
	req := httptest.NewRequest("GET", "/notes/Hello", nil)
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

// TestGetNoteNotFound verifies GET for a missing note returns 404.
func TestGetNoteNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/notes/NoSuch", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestPutNote verifies PUT creates/updates a note and returns
// 204 with ETag.
func TestPutNote(t *testing.T) {
	srv, s := newTestServer(t)
	body := `{"title":"NewOne","tags":"Test","text":"New body."}`
	req := httptest.NewRequest("PUT", "/notes/NewOne",
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
	got := s.Get("NewOne")
	if got == nil {
		t.Fatal("expected note in store")
	}
	if got.Text() != "New body." {
		t.Errorf("text = %q, want %q", got.Text(), "New body.")
	}
}

// TestPutNewNoteWritesToDisk verifies that PUT for a new note
// creates a .tid file on disk when noteDir is configured.
func TestPutNewNoteWritesToDisk(t *testing.T) {
	srv, s := newTestServer(t)
	dir := t.TempDir()
	srv.SetNoteDir(dir)

	body := `{"title":"DiskWrite","tags":"Test","text":"Written to disk."}`
	req := httptest.NewRequest("PUT", "/notes/DiskWrite",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// Verify it's in the store with a file path.
	got := s.Get("DiskWrite")
	if got == nil {
		t.Fatal("expected note in store")
	}
	fp := s.FilePath("DiskWrite")
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
	srv, s := newTestServer(t)
	// PUT with _is_skinny flag — should preserve existing text.
	body := `{"title":"Hello","tags":"Updated","_is_skinny":"true"}`
	req := httptest.NewRequest("PUT", "/notes/Hello",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	got := s.Get("Hello")
	if got.Text() != "Hello world." {
		t.Errorf("text = %q, want %q (preserved)", got.Text(), "Hello world.")
	}
	if got.Fields["tags"] != "Updated" {
		t.Errorf("tags = %q, want %q", got.Fields["tags"], "Updated")
	}
}

// TestPutNoteBadJSON verifies that invalid JSON returns 400.
func TestPutNoteBadJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("PUT", "/notes/Bad",
		strings.NewReader("not json"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestDeleteNote verifies DELETE removes a note and returns 204.
func TestDeleteNote(t *testing.T) {
	srv, s := newTestServer(t)
	req := httptest.NewRequest("DELETE", "/notes/Hello", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := s.Get("Hello"); got != nil {
		t.Error("note should be deleted")
	}
}

// TestNoteMethodNotAllowed verifies unsupported methods return 405.
func TestNoteMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("PATCH", "/notes/Hello", nil)
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
	req := httptest.NewRequest("GET", "/favicon.ico", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestHandleFaviconExists verifies favicon is served from the
// $:/favicon.ico note.
func TestHandleFaviconExists(t *testing.T) {
	srv, s := newTestServer(t)
	s.Put(&tid.Note{Fields: map[string]string{
		"title": "$:/favicon.ico",
		"text":  "fake-icon-data",
	}})

	req := httptest.NewRequest("GET", "/favicon.ico", nil)
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
	req := httptest.NewRequest("POST", "/edit-external/Hello", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

// TestHandleEditExternalNoPath verifies edit-external returns 404
// for notes with no file path.
func TestHandleEditExternalNoPath(t *testing.T) {
	srv, s := newTestServer(t)
	s.Put(&tid.Note{Fields: map[string]string{"title": "NoFile"}})

	req := httptest.NewRequest("POST", "/edit-external/NoFile", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestHandleEditExternalNoEditor verifies edit-external returns 500
// when no editor is configured.
func TestHandleEditExternalNoEditor(t *testing.T) {
	s := store.New()
	s.PutWithPath(&tid.Note{Fields: map[string]string{"title": "X"}}, "/x.tid")
	srv := New(s, "", nil)

	req := httptest.NewRequest("POST", "/edit-external/X", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// TestHandleEditExternalMethodNotAllowed verifies non-POST returns 405.
func TestHandleEditExternalMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/edit-external/Hello", nil)
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
	req := httptest.NewRequest("GET", "/notes/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestDeleteMissingTitle verifies empty title on delete returns 400.
func TestDeleteMissingTitle(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("DELETE", "/notes/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestEditExternalMissingTitle verifies empty title returns 400.
func TestEditExternalMissingTitle(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/edit-external/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestPutNoteWithNestedFields verifies that nested "fields"
// objects are flattened.
func TestPutNoteWithNestedFields(t *testing.T) {
	srv, s := newTestServer(t)
	body := `{"title":"Nested","fields":{"custom_field":"value"},"text":"Body."}`
	req := httptest.NewRequest("PUT", "/notes/Nested",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	got := s.Get("Nested")
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
	srv, s := newTestServer(t)
	// newTestServer has no noteDir set, so new notes go through s.store.Put.
	body := `{"title":"NoFile","tags":"Mem","text":"In-memory only."}`
	req := httptest.NewRequest("PUT", "/notes/NoFile",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	got := s.Get("NoFile")
	if got == nil {
		t.Fatal("expected note in store")
	}
	if got.Text() != "In-memory only." {
		t.Errorf("text = %q, want %q", got.Text(), "In-memory only.")
	}
	// Should have no file path since there is no noteDir.
	if fp := s.FilePath("NoFile"); fp != "" {
		t.Errorf("file path = %q, want empty", fp)
	}
}

// TestDeleteNoteWithDiskFile verifies that DELETE removes the
// backing .tid file from disk when the note has a file path.
func TestDeleteNoteWithDiskFile(t *testing.T) {
	srv, s := newTestServer(t)

	// Create a real temp file to act as the note's backing file.
	f, err := os.CreateTemp("", "note-*.tid")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("title: DiskDel\n\nBody.")
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	s.PutWithPath(&tid.Note{Fields: map[string]string{
		"title": "DiskDel",
		"text":  "Body.",
	}}, f.Name())

	req := httptest.NewRequest("DELETE", "/notes/DiskDel", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	// Verify the file was removed from disk.
	if _, err := os.Stat(f.Name()); !os.IsNotExist(err) {
		t.Errorf("expected file %q to be deleted, err = %v", f.Name(), err)
	}
	// Verify the note is gone from the store.
	if got := s.Get("DiskDel"); got != nil {
		t.Error("note should be deleted from store")
	}
}

// TestHandleEditExternalEditorFails verifies that edit-external returns
// 500 when the editor command fails to start.
func TestHandleEditExternalEditorFails(t *testing.T) {
	s := store.New()
	s.PutWithPath(&tid.Note{Fields: map[string]string{
		"title": "FailEdit",
		"text":  "Body.",
	}}, "/tmp/FailEdit.tid")

	// Use a nonexistent binary so exec.Command.Start returns an error.
	ed := editor.New("/nonexistent/editor %s")
	srv := New(s, "", ed)

	req := httptest.NewRequest("POST", "/edit-external/FailEdit", nil)
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

	srv, s := newTestServer(t)
	dir := t.TempDir()
	srv.SetNoteDir(dir)

	body := `{"title":"WriteErr","text":"Body."}`
	req := httptest.NewRequest("PUT", "/notes/WriteErr",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	// Note should still be stored (PutWithPath is called after the error log).
	if got := s.Get("WriteErr"); got == nil {
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

	srv, s := newTestServer(t)
	dir := t.TempDir()
	srv.SetNoteDir(dir)

	body := `{"title":"RCSErr","text":"Body."}`
	req := httptest.NewRequest("PUT", "/notes/RCSErr",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := s.Get("RCSErr"); got == nil {
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

	srv, s := newTestServer(t)
	s.PutWithPath(&tid.Note{Fields: map[string]string{
		"title": "RemErr",
		"text":  "Body.",
	}}, "/fake/RemErr.tid")

	req := httptest.NewRequest("DELETE", "/notes/RemErr", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := s.Get("RemErr"); got != nil {
		t.Error("note should be deleted from store")
	}
}

// TestLineEditReplace verifies replacing a body line via /line-edit/.
func TestLineEditReplace(t *testing.T) {
	srv, s := newTestServer(t)

	// Create a .tid file with headers and a list body.
	dir := t.TempDir()
	path := dir + "/LineTest.tid"
	content := "title: LineTest\n\n- alpha\n- beta\n- gamma\n"
	os.WriteFile(path, []byte(content), 0644)
	td, _ := tid.Parse([]byte(content))
	s.PutWithPath(td, path)
	srv.SetNoteDir(dir)

	body := `{"line":2,"text":"- BETA"}`
	req := httptest.NewRequest("POST", "/line-edit/LineTest", strings.NewReader(body))
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
	srv, s := newTestServer(t)

	dir := t.TempDir()
	path := dir + "/InsTest.tid"
	content := "title: InsTest\n\n- first\n- second\n"
	os.WriteFile(path, []byte(content), 0644)
	td, _ := tid.Parse([]byte(content))
	s.PutWithPath(td, path)
	srv.SetNoteDir(dir)

	body := `{"line":1,"text":"- inserted","insert":true}`
	req := httptest.NewRequest("POST", "/line-edit/InsTest", strings.NewReader(body))
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
	srv, s := newTestServer(t)

	dir := t.TempDir()
	path := dir + "/Short.tid"
	content := "title: Short\n\n- one\n"
	os.WriteFile(path, []byte(content), 0644)
	td, _ := tid.Parse([]byte(content))
	s.PutWithPath(td, path)
	srv.SetNoteDir(dir)

	body := `{"line":99,"text":"nope"}`
	req := httptest.NewRequest("POST", "/line-edit/Short", strings.NewReader(body))
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
	req := httptest.NewRequest("POST", "/line-edit/NoSuch", strings.NewReader(body))
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
	req := httptest.NewRequest("GET", "/theme.css", nil)
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
	req := httptest.NewRequest("GET", "/search", nil)
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
	srv, _ := newTestServer(t)
	// noteDir is empty by default in newTestServer.
	req := httptest.NewRequest("GET", "/search?q=hello", nil)
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

	srv, s := newTestServer(t)
	dir := t.TempDir()
	srv.SetNoteDir(dir)

	// Create a .tid file with searchable content.
	content := "title: SearchMe\ntags: Test\n\nUnique searchable content here.\n"
	path := dir + "/SearchMe.tid"
	os.WriteFile(path, []byte(content), 0644)
	td, _ := tid.Parse([]byte(content))
	s.PutWithPath(td, path)

	req := httptest.NewRequest("GET", "/search?q=searchable", nil)
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
	req := httptest.NewRequest("POST", "/line-edit/Hello", strings.NewReader("not json"))
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
	req := httptest.NewRequest("GET", "/line-edit/Hello", nil)
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
	req := httptest.NewRequest("POST", "/line-edit/Hello", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// --- Coverage gap tests ---

// TestPutNoteRevisionConflict verifies that a PUT with a stale
// revision returns 409 Conflict. This covers lines 231-237 (revision
// conflict) of server.go.
func TestPutNoteRevisionConflict(t *testing.T) {
	srv, s := newTestServer(t)
	dir := t.TempDir()
	srv.SetNoteDir(dir)

	// Create a note in the store — PutWithPath sets revision to 1.
	s.PutWithPath(&tid.Note{Fields: map[string]string{
		"title": "Conflict",
		"text":  "Original.",
	}}, dir+"/Conflict.tid")

	// PUT again to bump revision to 2.
	body1 := `{"title":"Conflict","text":"Updated."}`
	req1 := httptest.NewRequest("PUT", "/notes/Conflict", strings.NewReader(body1))
	w1 := httptest.NewRecorder()
	srv.ServeHTTP(w1, req1)
	if w1.Code != http.StatusNoContent {
		t.Fatalf("first PUT status = %d, want %d", w1.Code, http.StatusNoContent)
	}

	// PUT with stale revision "1" — server revision is now > 1.
	body2 := `{"title":"Conflict","text":"Stale.","revision":"1"}`
	req2 := httptest.NewRequest("PUT", "/notes/Conflict", strings.NewReader(body2))
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Fatalf("stale PUT status = %d, want %d (409 Conflict)", w2.Code, http.StatusConflict)
	}
}

// TestPutNoteRevisionFieldSkipped verifies that a PUT with a
// matching revision still skips the "revision" key in tFields.
// This covers lines 244-245 of server.go.
func TestPutNoteRevisionFieldSkipped(t *testing.T) {
	srv, s := newTestServer(t)
	dir := t.TempDir()
	srv.SetNoteDir(dir)

	// Create note at revision 1.
	s.PutWithPath(&tid.Note{Fields: map[string]string{
		"title": "RevSkip",
		"text":  "Original.",
	}}, dir+"/RevSkip.tid")

	// PUT with revision that matches current (1) — should succeed
	// and the "revision" key should NOT appear in the stored fields.
	body := `{"title":"RevSkip","text":"New.","revision":"1"}`
	req := httptest.NewRequest("PUT", "/notes/RevSkip", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	// The stored note should not have a "revision" field.
	td := s.Get("RevSkip")
	if td == nil {
		t.Fatal("note not found in store")
	}
	if _, ok := td.Fields["revision"]; ok {
		t.Error("revision field should not be stored in note fields")
	}
}

// TestPutNoteRevisionNonString verifies that a numeric revision
// value (not a string) is ignored — no conflict check occurs.
func TestPutNoteRevisionNonString(t *testing.T) {
	srv, s := newTestServer(t)
	dir := t.TempDir()
	srv.SetNoteDir(dir)

	s.PutWithPath(&tid.Note{Fields: map[string]string{
		"title": "RevNum",
		"text":  "Original.",
	}}, dir+"/RevNum.tid")

	// PUT with revision as a number (JSON parses as float64).
	body := `{"title":"RevNum","text":"New.","revision":999}`
	req := httptest.NewRequest("PUT", "/notes/RevNum", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (non-string revision should be ignored)", w.Code, http.StatusNoContent)
	}
}

// returns 400. This covers lines 209-212 of server.go.
func TestPutNoteReadBodyError(t *testing.T) {
	srv, _ := newTestServer(t)

	// errReader always returns an error on Read.
	req := httptest.NewRequest("PUT", "/notes/BadBody", &errReader{})
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
// title returns 400. This covers lines 345-348 of server.go.
func TestLineEditMissingTitle(t *testing.T) {
	srv, _ := newTestServer(t)
	body := `{"line":1,"text":"x"}`
	req := httptest.NewRequest("POST", "/line-edit/", strings.NewReader(body))
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
// returns 500. This covers lines 371-374 of server.go.
func TestLineEditReadFileError(t *testing.T) {
	patches := gomonkey.ApplyFunc(os.ReadFile, func(name string) ([]byte, error) {
		return nil, fmt.Errorf("disk error")
	})
	defer patches.Reset()

	srv, s := newTestServer(t)
	// Note must be in store with a filePath so we get past the
	// "no file for note" check.
	s.PutWithPath(&tid.Note{Fields: map[string]string{
		"title": "ReadErr",
		"text":  "Body.",
	}}, "/fake/ReadErr.tid")

	body := `{"line":1,"text":"x"}`
	req := httptest.NewRequest("POST", "/line-edit/ReadErr", strings.NewReader(body))
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
// during line-edit returns 500. This covers lines 404-407 of server.go.
func TestLineEditWriteFileError(t *testing.T) {
	srv, s := newTestServer(t)

	dir := t.TempDir()
	path := dir + "/WrErr.tid"
	content := "title: WrErr\n\n- alpha\n- beta\n"
	os.WriteFile(path, []byte(content), 0644)
	td, _ := tid.Parse([]byte(content))
	s.PutWithPath(td, path)
	srv.SetNoteDir(dir)

	// Mock os.WriteFile to fail AFTER os.ReadFile succeeds.
	patches := gomonkey.ApplyFunc(os.WriteFile, func(name string, data []byte, perm os.FileMode) error {
		return fmt.Errorf("disk full")
	})
	defer patches.Reset()

	body := `{"line":1,"text":"- ALPHA"}`
	req := httptest.NewRequest("POST", "/line-edit/WrErr", strings.NewReader(body))
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
// This covers lines 408-410 of server.go.
func TestLineEditRCSCheckinError(t *testing.T) {
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(path string) error {
		return fmt.Errorf("rcs failed")
	})
	defer patches.Reset()

	srv, s := newTestServer(t)

	dir := t.TempDir()
	path := dir + "/RcsErr.tid"
	content := "title: RcsErr\n\n- alpha\n- beta\n"
	os.WriteFile(path, []byte(content), 0644)
	td, _ := tid.Parse([]byte(content))
	s.PutWithPath(td, path)
	srv.SetNoteDir(dir)

	body := `{"line":1,"text":"- ALPHA"}`
	req := httptest.NewRequest("POST", "/line-edit/RcsErr", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Should succeed (200) despite rcs error — the error is just logged.
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// TestLineEditParseError verifies that a tid.Parse failure after
// writing returns 500. This covers lines 414-417 of server.go.
func TestLineEditParseError(t *testing.T) {
	srv, s := newTestServer(t)

	dir := t.TempDir()
	path := dir + "/ParseErr.tid"
	content := "title: ParseErr\n\n- alpha\n- beta\n"
	os.WriteFile(path, []byte(content), 0644)
	td, _ := tid.Parse([]byte(content))
	s.PutWithPath(td, path)
	srv.SetNoteDir(dir)

	// Mock tid.Parse to fail (called after WriteFile succeeds).
	patches := gomonkey.ApplyFunc(tid.Parse, func(data []byte) (*tid.Note, error) {
		return nil, fmt.Errorf("parse boom")
	})
	defer patches.Reset()

	body := `{"line":1,"text":"- ALPHA"}`
	req := httptest.NewRequest("POST", "/line-edit/ParseErr", strings.NewReader(body))
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
// 500. This covers lines 85-88 of server.go.
func TestHandleSearchError(t *testing.T) {
	patches := gomonkey.ApplyFunc(search.Search, func(dir, query string) ([]search.Result, error) {
		return nil, fmt.Errorf("rg exploded")
	})
	defer patches.Reset()

	srv, _ := newTestServer(t)
	srv.SetNoteDir("/tmp") // non-empty so we get past noteDir check
	req := httptest.NewRequest("GET", "/search?q=hello", nil)
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
// is coalesced to an empty slice. This covers lines 99-101 of server.go.
func TestHandleSearchNilLines(t *testing.T) {
	// Return a hit with nil Lines (e.g. a tag-only match where
	// all matched lines are header lines, filtered out by rg).
	patches := gomonkey.ApplyFunc(search.Search, func(dir, query string) ([]search.Result, error) {
		return []search.Result{
			{File: "Hello.tid", Lines: nil},
		}, nil
	})
	defer patches.Reset()

	srv, _ := newTestServer(t)
	srv.SetNoteDir("/tmp")
	req := httptest.NewRequest("GET", "/search?q=Test", nil)
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
// match any note in the store, results is coalesced to []. This
// covers lines 108-110 of server.go.
func TestHandleSearchNilResults(t *testing.T) {
	// Return a hit for a file whose title isn't in the store.
	patches := gomonkey.ApplyFunc(search.Search, func(dir, query string) ([]search.Result, error) {
		return []search.Result{
			{File: "NoSuchNote.tid", Lines: []string{"some line"}},
		}, nil
	})
	defer patches.Reset()

	srv, _ := newTestServer(t)
	srv.SetNoteDir("/tmp")
	req := httptest.NewRequest("GET", "/search?q=missing", nil)
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

// Keep unused imports from erroring.
var _ = io.Discard
