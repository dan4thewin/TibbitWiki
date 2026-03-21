package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/dan4thewin/tibbitwiki/internal/editor"
	"github.com/dan4thewin/tibbitwiki/internal/rcs"
	"github.com/dan4thewin/tibbitwiki/internal/store"
	"github.com/dan4thewin/tibbitwiki/internal/tid"
)

// newTestServer creates a Server with a pre-populated store and a
// temp index.html file for testing.
func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	s := store.New()

	// Add a test tiddler.
	td := &tid.Tiddler{Fields: map[string]string{
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
	f.WriteString("<html><body>TiddlyWiki</body></html>")
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
	if !strings.Contains(w.Body.String(), "TiddlyWiki") {
		t.Error("index response should contain 'TiddlyWiki'")
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

// TestHandleSkinnyTiddlers verifies the tiddlers.json endpoint
// returns tiddlers without text.
func TestHandleSkinnyTiddlers(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/recipes/default/tiddlers.json", nil)
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
		t.Error("skinny tiddler should not include 'text'")
	}
	if body[0]["revision"] == "" {
		t.Error("should include revision")
	}
}

// TestGetTiddler verifies GET for a single tiddler returns full
// JSON with all fields.
func TestGetTiddler(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/recipes/default/tiddlers/Hello", nil)
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

// TestGetTiddlerNotFound verifies GET for a missing tiddler returns 404.
func TestGetTiddlerNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/recipes/default/tiddlers/NoSuch", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestPutTiddler verifies PUT creates/updates a tiddler and returns
// 204 with ETag.
func TestPutTiddler(t *testing.T) {
	srv, s := newTestServer(t)
	body := `{"title":"NewOne","tags":"Test","text":"New body."}`
	req := httptest.NewRequest("PUT", "/recipes/default/tiddlers/NewOne",
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
		t.Fatal("expected tiddler in store")
	}
	if got.Text() != "New body." {
		t.Errorf("text = %q, want %q", got.Text(), "New body.")
	}
}

// TestPutNewTiddlerWritesToDisk verifies that PUT for a new tiddler
// creates a .tid file on disk when tiddlerDir is configured.
func TestPutNewTiddlerWritesToDisk(t *testing.T) {
	srv, s := newTestServer(t)
	dir := t.TempDir()
	srv.SetTiddlerDir(dir)

	body := `{"title":"DiskWrite","tags":"Test","text":"Written to disk."}`
	req := httptest.NewRequest("PUT", "/recipes/default/tiddlers/DiskWrite",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// Verify it's in the store with a file path.
	got := s.Get("DiskWrite")
	if got == nil {
		t.Fatal("expected tiddler in store")
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

// TestPutTiddlerSkinny verifies that a skinny PUT preserves the
// existing text.
func TestPutTiddlerSkinny(t *testing.T) {
	srv, s := newTestServer(t)
	// PUT with _is_skinny flag — should preserve existing text.
	body := `{"title":"Hello","tags":"Updated","_is_skinny":"true"}`
	req := httptest.NewRequest("PUT", "/recipes/default/tiddlers/Hello",
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

// TestPutTiddlerBadJSON verifies that invalid JSON returns 400.
func TestPutTiddlerBadJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("PUT", "/recipes/default/tiddlers/Bad",
		strings.NewReader("not json"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestDeleteTiddler verifies DELETE removes a tiddler and returns 204.
func TestDeleteTiddler(t *testing.T) {
	srv, s := newTestServer(t)
	req := httptest.NewRequest("DELETE", "/bags/default/tiddlers/Hello", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := s.Get("Hello"); got != nil {
		t.Error("tiddler should be deleted")
	}
}

// TestDeleteTiddlerMethodNotAllowed verifies non-DELETE returns 405.
func TestDeleteTiddlerMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/bags/default/tiddlers/Hello", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// TestHandleFaviconNotFound verifies favicon returns 404 when no
// $:/favicon.ico tiddler exists.
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
// $:/favicon.ico tiddler.
func TestHandleFaviconExists(t *testing.T) {
	srv, s := newTestServer(t)
	s.Put(&tid.Tiddler{Fields: map[string]string{
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
// for tiddlers with no file path.
func TestHandleEditExternalNoPath(t *testing.T) {
	srv, s := newTestServer(t)
	s.Put(&tid.Tiddler{Fields: map[string]string{"title": "NoFile"}})

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
	s.PutWithPath(&tid.Tiddler{Fields: map[string]string{"title": "X"}}, "/x.tid")
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

// TestTiddlerMissingTitle verifies that requests with empty title
// return 400.
func TestTiddlerMissingTitle(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/recipes/default/tiddlers/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestTiddlerMethodNotAllowed verifies unsupported methods on the
// tiddler endpoint return 405.
func TestTiddlerMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("PATCH", "/recipes/default/tiddlers/Hello", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// TestDeleteMissingTitle verifies empty title on delete returns 400.
func TestDeleteMissingTitle(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("DELETE", "/bags/default/tiddlers/", nil)
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

// TestPutTiddlerWithNestedFields verifies that nested "fields"
// objects are flattened.
func TestPutTiddlerWithNestedFields(t *testing.T) {
	srv, s := newTestServer(t)
	body := `{"title":"Nested","fields":{"custom_field":"value"},"text":"Body."}`
	req := httptest.NewRequest("PUT", "/recipes/default/tiddlers/Nested",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	got := s.Get("Nested")
	if got == nil {
		t.Fatal("expected tiddler in store")
	}
	if got.Fields["custom_field"] != "value" {
		t.Errorf("custom_field = %q, want %q", got.Fields["custom_field"], "value")
	}
}

// TestPutTiddlerNoFilePathNoDir verifies that PUT on a server with no
// tiddlerDir stores the tiddler via Put (no disk write) when the
// tiddler has no existing file path.
func TestPutTiddlerNoFilePathNoDir(t *testing.T) {
	srv, s := newTestServer(t)
	// newTestServer has no tiddlerDir set, so new tiddlers go through s.store.Put.
	body := `{"title":"NoFile","tags":"Mem","text":"In-memory only."}`
	req := httptest.NewRequest("PUT", "/recipes/default/tiddlers/NoFile",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	got := s.Get("NoFile")
	if got == nil {
		t.Fatal("expected tiddler in store")
	}
	if got.Text() != "In-memory only." {
		t.Errorf("text = %q, want %q", got.Text(), "In-memory only.")
	}
	// Should have no file path since there is no tiddlerDir.
	if fp := s.FilePath("NoFile"); fp != "" {
		t.Errorf("file path = %q, want empty", fp)
	}
}

// TestDeleteTiddlerWithDiskFile verifies that DELETE removes the
// backing .tid file from disk when the tiddler has a file path.
func TestDeleteTiddlerWithDiskFile(t *testing.T) {
	srv, s := newTestServer(t)

	// Create a real temp file to act as the tiddler's backing file.
	f, err := os.CreateTemp("", "tiddler-*.tid")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("title: DiskDel\n\nBody.")
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	s.PutWithPath(&tid.Tiddler{Fields: map[string]string{
		"title": "DiskDel",
		"text":  "Body.",
	}}, f.Name())

	req := httptest.NewRequest("DELETE", "/bags/default/tiddlers/DiskDel", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	// Verify the file was removed from disk.
	if _, err := os.Stat(f.Name()); !os.IsNotExist(err) {
		t.Errorf("expected file %q to be deleted, err = %v", f.Name(), err)
	}
	// Verify the tiddler is gone from the store.
	if got := s.Get("DiskDel"); got != nil {
		t.Error("tiddler should be deleted from store")
	}
}

// TestHandleEditExternalEditorFails verifies that edit-external returns
// 500 when the editor command fails to start.
func TestHandleEditExternalEditorFails(t *testing.T) {
	s := store.New()
	s.PutWithPath(&tid.Tiddler{Fields: map[string]string{
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

// TestPutTiddlerWriteFileError verifies that an os.WriteFile error is
// logged but does not prevent the tiddler from being stored (204).
func TestPutTiddlerWriteFileError(t *testing.T) {
	patches := gomonkey.ApplyFunc(os.WriteFile, func(name string, data []byte, perm os.FileMode) error {
		return fmt.Errorf("disk full")
	})
	defer patches.Reset()

	srv, s := newTestServer(t)
	dir := t.TempDir()
	srv.SetTiddlerDir(dir)

	body := `{"title":"WriteErr","text":"Body."}`
	req := httptest.NewRequest("PUT", "/recipes/default/tiddlers/WriteErr",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	// Tiddler should still be stored (PutWithPath is called after the error log).
	if got := s.Get("WriteErr"); got == nil {
		t.Fatal("expected tiddler in store despite WriteFile error")
	}
}

// TestPutTiddlerRCSCheckinError verifies that an rcs.Checkin error
// is logged but does not prevent a 204 response.
func TestPutTiddlerRCSCheckinError(t *testing.T) {
	patches := gomonkey.ApplyFunc(rcs.Checkin, func(path string) error {
		return fmt.Errorf("rcs failed")
	})
	defer patches.Reset()

	srv, s := newTestServer(t)
	dir := t.TempDir()
	srv.SetTiddlerDir(dir)

	body := `{"title":"RCSErr","text":"Body."}`
	req := httptest.NewRequest("PUT", "/recipes/default/tiddlers/RCSErr",
		strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := s.Get("RCSErr"); got == nil {
		t.Fatal("expected tiddler in store despite rcs.Checkin error")
	}
}

// TestDeleteTiddlerRemoveError verifies that a non-NotExist os.Remove
// error is logged but the tiddler is still deleted from the store (204).
func TestDeleteTiddlerRemoveError(t *testing.T) {
	patches := gomonkey.ApplyFunc(os.Remove, func(name string) error {
		return fmt.Errorf("permission denied")
	})
	defer patches.Reset()

	srv, s := newTestServer(t)
	s.PutWithPath(&tid.Tiddler{Fields: map[string]string{
		"title": "RemErr",
		"text":  "Body.",
	}}, "/fake/RemErr.tid")

	req := httptest.NewRequest("DELETE", "/bags/default/tiddlers/RemErr", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := s.Get("RemErr"); got != nil {
		t.Error("tiddler should be deleted from store")
	}
}
